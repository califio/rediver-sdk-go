package rediver

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/califio/rediver-sdk-go/utils"
)

// prepareRepository sets up the repo directory for scanning.
// In CI mode it reuses the existing directory; otherwise it clones or extracts an artifact.
func (j *job) prepareRepository(ctx context.Context) error {
	if j.ciContext != nil {
		j.repoDir = j.ciContext.RepoDir
		return nil
	}

	repo, ok := j.Repository()
	if !ok {
		return fmt.Errorf("no repository target")
	}

	// Artifact path: download pre-uploaded tar.gz instead of git clone.
	if repo.ArtifactID != "" {
		return j.prepareArchive(ctx, repo)
	}

	repoURL, err := j.buildRepoURL(repo)
	if err != nil {
		return err
	}

	workDir, err := os.MkdirTemp(os.TempDir(), "repo_")
	if err != nil {
		return fmt.Errorf("create temp dir: %w", err)
	}

	refs, checkoutRef := buildRefSpecs(repo)
	err = utils.GitCheckout(ctx, utils.CheckoutOptions{
		WorkDir:     workDir,
		RepoURL:     repoURL,
		Refs:        refs,
		CheckoutRef: checkoutRef,
	})
	if err != nil {
		os.RemoveAll(workDir)
		return fmt.Errorf("checkout: %w", err)
	}

	j.repoDir = workDir
	j.clonedRepoDir = workDir

	if repo.BaseCommitSHA != "" {
		utils.EnsureMergeBaseReachable(ctx, workDir, repo.BaseCommitSHA)
	}

	j.resolveCommitSHAs(ctx, repo, workDir)

	return nil
}

// cleanupRepository removes the cloned repo directory if the SDK created it.
func (j *job) cleanupRepository() {
	if j.clonedRepoDir == "" {
		return
	}
	if err := os.RemoveAll(j.clonedRepoDir); err != nil {
		j.Emit(NewLog(LogLevelWarn,
			fmt.Sprintf("cleanup repo failed dir=%s error=%v", j.clonedRepoDir, err)))
	} else {
		j.Emit(NewLog(LogLevelInfo,
			fmt.Sprintf("cleaned up cloned repo dir=%s", j.clonedRepoDir)))
	}
	j.clonedRepoDir = ""
	j.repoDir = ""
}

// buildRepoURL injects credentials into the repository URL when provided.
func (j *job) buildRepoURL(repo *Repository) (string, error) {
	if repo.URL == "" {
		return "", fmt.Errorf("repo URL is empty")
	}

	u, err := url.Parse(repo.URL)
	if err != nil {
		return "", fmt.Errorf("invalid repo URL: %w", err)
	}

	if repo.Username != "" && repo.Password != "" {
		u.User = url.UserPassword(repo.Username, repo.Password)
	}

	return u.String(), nil
}

// prepareArchive downloads the artifact presigned URL and extracts the tar.gz
// into a temp directory, then sets j.repoDir to the extracted path.
func (j *job) prepareArchive(ctx context.Context, repo *Repository) error {
	if j.artifactDownloadFn == nil {
		return fmt.Errorf("artifact download not available in this run mode")
	}

	presignedURL, err := j.artifactDownloadFn(ctx, repo.ArtifactID)
	if err != nil {
		return fmt.Errorf("get artifact download URL: %w", err)
	}

	tmpDir, err := os.MkdirTemp(os.TempDir(), "artifact_")
	if err != nil {
		return fmt.Errorf("create artifact temp dir: %w", err)
	}

	if err := j.downloadAndExtract(ctx, presignedURL, tmpDir); err != nil {
		os.RemoveAll(tmpDir)
		return fmt.Errorf("extract artifact: %w", err)
	}

	j.repoDir = tmpDir
	j.clonedRepoDir = tmpDir
	return nil
}

// downloadAndExtract streams a tar.gz from rawURL and extracts it into destDir.
func (j *job) downloadAndExtract(ctx context.Context, rawURL string, destDir string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return fmt.Errorf("build download request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("download artifact: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("download artifact: unexpected status %d", resp.StatusCode)
	}

	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("open gzip stream: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		cleanRel := filepath.Clean("/" + hdr.Name)
		target := filepath.Join(destDir, cleanRel)
		if !strings.HasPrefix(target+string(filepath.Separator), destDir+string(filepath.Separator)) {
			return fmt.Errorf("tar entry %q escapes destination directory", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0750); err != nil {
				return fmt.Errorf("create dir %q: %w", target, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0750); err != nil {
				return fmt.Errorf("create parent dir for %q: %w", target, err)
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0750)
			if err != nil {
				return fmt.Errorf("create file %q: %w", target, err)
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return fmt.Errorf("write file %q: %w", target, err)
			}
			f.Close()
		}
	}
	return nil
}

// buildRefSpecs constructs git refspecs and checkout ref based on event type and provider.
func buildRefSpecs(repo *Repository) (refs []string, checkoutRef string) {
	switch repo.Event {
	case "merge_request", "pull_request":
		refs, checkoutRef = buildMrPrRefSpecs(repo)
	default:
		if strings.HasPrefix(repo.Ref, "refs/tags/") {
			refs = append(refs, repo.Ref)
		} else if repo.Branch != "" {
			refs = append(refs, fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", repo.Branch, repo.Branch))
		}
		if repo.BaseCommitSHA != "" {
			refs = append(refs, repo.BaseCommitSHA)
		}
		checkoutRef = repo.CommitSHA
	}

	if len(refs) == 0 {
		if repo.CommitSHA != "" {
			refs = append(refs, repo.CommitSHA)
		} else if repo.Branch != "" {
			refs = append(refs, fmt.Sprintf("+refs/heads/%s:refs/remotes/origin/%s", repo.Branch, repo.Branch))
		}
	}
	if checkoutRef == "" {
		if repo.CommitSHA != "" {
			checkoutRef = repo.CommitSHA
		} else if repo.Branch != "" {
			checkoutRef = "origin/" + repo.Branch
		}
	}

	return refs, checkoutRef
}

// buildMrPrRefSpecs returns fetch refs + checkout ref for MR/PR events.
func buildMrPrRefSpecs(repo *Repository) (refs []string, checkoutRef string) {
	switch repo.Provider {
	case "gitlab":
		if repo.PrNumber > 0 {
			refs = append(refs, fmt.Sprintf("refs/merge-requests/%d/head", repo.PrNumber))
		}
		checkoutRef = "FETCH_HEAD"
	case "github":
		if repo.CommitSHA != "" {
			refs = append(refs, repo.CommitSHA)
		}
		checkoutRef = repo.CommitSHA
	case "bitbucket":
		if repo.PrNumber > 0 {
			refs = append(refs, fmt.Sprintf("refs/pull-requests/%d/from", repo.PrNumber))
		}
		checkoutRef = "FETCH_HEAD"
	default:
		if repo.CommitSHA != "" {
			refs = append(refs, repo.CommitSHA)
		}
		checkoutRef = repo.CommitSHA
	}

	if repo.BaseBranch != "" {
		refs = append(refs, fmt.Sprintf("refs/heads/%s", repo.BaseBranch))
	}

	return refs, checkoutRef
}
