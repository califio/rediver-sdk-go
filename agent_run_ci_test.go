package rediver

import (
	"context"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestAgentDetectGitContextWithRepoDirUsesConfiguredLocalRepo(t *testing.T) {
	t.Setenv("GITLAB_CI", "")
	t.Setenv("GITHUB_ACTIONS", "")

	cwdRepo := initTestGitRepo(t, "cwd-repo")
	targetRepo := initTestGitRepo(t, "target-repo")

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })
	if err := os.Chdir(cwdRepo); err != nil {
		t.Fatal(err)
	}

	a := &Agent{config: &agentConfig{
		repoDir: targetRepo,
		logger:  slog.Default(),
	}}
	ci := a.detectGitContext()
	if ci == nil {
		t.Fatal("detectGitContext() = nil, want local git context")
	}
	wantRepoDir, err := filepath.EvalSymlinks(targetRepo)
	if err != nil {
		t.Fatal(err)
	}
	if ci.RepoDir != wantRepoDir {
		t.Fatalf("RepoDir = %q, want %q", ci.RepoDir, wantRepoDir)
	}
	if ci.Repo.Name != filepath.Base(wantRepoDir) {
		t.Fatalf("Repo.Name = %q, want %q", ci.Repo.Name, filepath.Base(wantRepoDir))
	}
}

func initTestGitRepo(t *testing.T, name string) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, out)
	}
}
