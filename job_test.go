package rediver

import (
	"testing"

	commonv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/common/v1"
	scannerv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/scanner/v1"
)

// --- JobType.String ---

func TestJobType_String(t *testing.T) {
	tests := []struct {
		jt   JobType
		want string
	}{
		{JobTypeDiscovery, "discovery"},
		{JobTypeRetest, "retest"},
		{JobType(99), "unknown"},
	}
	for _, tc := range tests {
		if got := tc.jt.String(); got != tc.want {
			t.Errorf("JobType(%d).String() = %q, want %q", tc.jt, got, tc.want)
		}
	}
}

// --- newJob ---

func TestNewJob_NilDetail(t *testing.T) {
	j := newJob(nil)
	if j.ID() != "" {
		t.Errorf("nil detail should return empty ID, got %q", j.ID())
	}
	if j.ExecutionToken() != "" {
		t.Errorf("nil detail should return empty execution token, got %q", j.ExecutionToken())
	}
	if j.Type() != JobTypeDiscovery {
		t.Errorf("nil detail should default to discovery, got %v", j.Type())
	}
	if j.Scanner() != "" {
		t.Error("nil detail should return empty scanner")
	}
	if j.TimeoutMinutes() != 0 {
		t.Error("nil detail should return 0 timeout")
	}
	if j.Version() != 1 {
		t.Errorf("nil detail should return version 1, got %d", j.Version())
	}
}

// --- job.ID ---

func TestJob_ID(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{Id: "job-abc"})
	if j.ID() != "job-abc" {
		t.Errorf("expected job-abc, got %q", j.ID())
	}
}

func TestJob_ID_Empty(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{})
	if j.ID() != "" {
		t.Errorf("empty Id should return empty, got %q", j.ID())
	}
}

func TestJob_ExecutionToken(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{}).(*job)
	j.executionToken = "agent-token-123"
	if got := j.ExecutionToken(); got != "agent-token-123" {
		t.Fatalf("ExecutionToken() = %q, want agent-token-123", got)
	}
}

// --- job.Type ---

func TestJob_Type_Discovery(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{})
	if j.Type() != JobTypeDiscovery {
		t.Errorf("default should be discovery, got %v", j.Type())
	}
}

func TestJob_Type_Retest(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{Retest: true})
	if j.Type() != JobTypeRetest {
		t.Errorf("expected retest, got %v", j.Type())
	}
}

func TestJob_Type_RetestFalse(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{Retest: false})
	if j.Type() != JobTypeDiscovery {
		t.Errorf("retest=false should be discovery, got %v", j.Type())
	}
}

// --- job.Domains ---

func TestJob_Domains_NilDetail(t *testing.T) {
	j := newJob(nil)
	if j.Domains() != nil {
		t.Error("nil detail should return nil domains")
	}
}

func TestJob_Domains_NilTarget(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{})
	if j.Domains() != nil {
		t.Error("nil target should return nil domains")
	}
}

func TestJob_Domains_EmptyDomains(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{Target: &scannerv1.JobTarget{}})
	if len(j.Domains()) != 0 {
		t.Error("empty domains should return empty slice")
	}
}

func TestJob_Domains_WithData(t *testing.T) {
	id := "d1"
	domains := []*scannerv1.DomainAsset{
		{Id: &id, Value: "example.com", Cname: func() *string { s := "alias.com"; return &s }(), Ips: []string{"1.2.3.4"}},
		{Value: "other.com"},
	}
	j := newJob(&scannerv1.GetJobDetailResponse{Target: &scannerv1.JobTarget{Domains: domains}})
	result := j.Domains()
	if len(result) != 2 {
		t.Fatalf("expected 2, got %d", len(result))
	}
	if result[0].ID != "d1" {
		t.Errorf("ID: got %q", result[0].ID)
	}
	if result[0].Value != "example.com" {
		t.Errorf("Value: got %q", result[0].Value)
	}
	if result[0].CNAME != "alias.com" {
		t.Errorf("CNAME: got %q", result[0].CNAME)
	}
	if len(result[0].IPs) != 1 {
		t.Errorf("IPs: got %v", result[0].IPs)
	}
	if result[1].ID != "" {
		t.Errorf("partial domain should have empty ID, got %q", result[1].ID)
	}
}

// --- job.IPs ---

func TestJob_IPs_NilTarget(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{})
	if j.IPs() != nil {
		t.Error("nil target should return nil")
	}
}

func TestJob_IPs_WithData(t *testing.T) {
	id := "ip1"
	ips := []*scannerv1.ValueAsset{{Id: &id, Value: "192.168.1.1"}}
	j := newJob(&scannerv1.GetJobDetailResponse{Target: &scannerv1.JobTarget{Ips: ips}})
	result := j.IPs()
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].ID != "ip1" || result[0].Value != "192.168.1.1" {
		t.Errorf("unexpected: %+v", result[0])
	}
}

// --- job.Subnets ---

func TestJob_Subnets_NilTarget(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{})
	if j.Subnets() != nil {
		t.Error("nil target should return nil")
	}
}

func TestJob_Subnets_WithData(t *testing.T) {
	id := "s1"
	subnets := []*scannerv1.ValueAsset{{Id: &id, Value: "10.0.0.0/24"}}
	j := newJob(&scannerv1.GetJobDetailResponse{Target: &scannerv1.JobTarget{Subnets: subnets}})
	result := j.Subnets()
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].Value != "10.0.0.0/24" {
		t.Errorf("value: got %q", result[0].Value)
	}
}

// --- job.Services ---

func TestJob_Services_NilTarget(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{})
	if j.Services() != nil {
		t.Error("nil target should return nil")
	}
}

func TestJob_Services_WithData(t *testing.T) {
	id := "sv1"
	port := int32(443)
	services := []*scannerv1.ServiceAsset{{Id: &id, Value: "example.com:443", Host: func() *string { s := "example.com"; return &s }(), Port: &port, Url: func() *string { s := "https://example.com"; return &s }()}}
	j := newJob(&scannerv1.GetJobDetailResponse{Target: &scannerv1.JobTarget{Services: services}})
	result := j.Services()
	if len(result) != 1 {
		t.Fatalf("expected 1, got %d", len(result))
	}
	if result[0].Host != "example.com" {
		t.Errorf("host: got %q", result[0].Host)
	}
	if result[0].Port != 443 {
		t.Errorf("port: got %d", result[0].Port)
	}
	if result[0].URL != "https://example.com" {
		t.Errorf("url: got %q", result[0].URL)
	}
}

// --- job.Param ---

func TestJob_Param_NilParams(t *testing.T) {
	j := newJob(nil)
	pv := j.Param("anything")
	if pv.IsSet() {
		t.Error("nil detail should return unset param")
	}
}

func TestJob_Param_Unset(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{})
	pv := j.Param("missing")
	if pv.IsSet() {
		t.Error("expected IsSet false")
	}
	if pv.StringOr("default") != "default" {
		t.Error("expected fallback")
	}
}

// --- job.Repository ---

func TestJob_Repository_None(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{})
	repo, ok := j.Repository()
	if ok {
		t.Error("expected false")
	}
	if repo != nil {
		t.Error("expected nil repo")
	}
}

func TestJob_Repository_NilDetail(t *testing.T) {
	j := newJob(nil)
	repo, ok := j.Repository()
	if ok || repo != nil {
		t.Error("nil detail should return nil, false")
	}
}

func TestJob_Repository_WithData(t *testing.T) {
	repoURL := "https://github.com/org/repo.git"
	branch := "main"
	sha := "abc123"
	diffOnly := true
	prNum := int32(42)
	artifactID := "artifact-1"
	username := "user"
	password := "pass"

	j := newJob(&scannerv1.GetJobDetailResponse{
		Target: &scannerv1.JobTarget{
			Repository: &scannerv1.RepositoryJobTarget{
				ArtifactId: &artifactID,
				DiffOnly:   diffOnly,
				Credential: &scannerv1.RepositoryCredential{
					Username: &username,
					Password: &password,
				},
				Target: &commonv1.RepositoryTarget{
					Url:               repoURL,
					Provider:          commonv1.GitProvider_GIT_PROVIDER_GITHUB,
					Event:             commonv1.CiEvent_CI_EVENT_PUSH,
					Branch:            &branch,
					CommitSha:         &sha,
					PullRequestNumber: &prNum,
				},
			},
		},
	})
	repo, ok := j.Repository()
	if !ok {
		t.Fatal("expected ok")
	}
	if repo.URL != repoURL {
		t.Errorf("URL: got %q", repo.URL)
	}
	if repo.Branch != "main" {
		t.Errorf("branch: got %q", repo.Branch)
	}
	if repo.CommitSHA != "abc123" {
		t.Errorf("SHA: got %q", repo.CommitSHA)
	}
	if repo.Provider != "github" {
		t.Errorf("provider: got %q", repo.Provider)
	}
	if !repo.DiffOnly {
		t.Error("expected DiffOnly true")
	}
	if repo.PrNumber != 42 {
		t.Errorf("PR: got %d", repo.PrNumber)
	}
	if repo.ArtifactID != artifactID {
		t.Errorf("artifact: got %q", repo.ArtifactID)
	}
	if repo.Username != username || repo.Password != password {
		t.Errorf("credential: got %q/%q", repo.Username, repo.Password)
	}
}

func TestJob_Repository_ResolvedBaseSHA(t *testing.T) {
	repoURL := "https://github.com/org/repo.git"
	j := &job{
		detail: &scannerv1.GetJobDetailResponse{
			Target: &scannerv1.JobTarget{
				Repository: &scannerv1.RepositoryJobTarget{
					Target: &commonv1.RepositoryTarget{
						Url: repoURL,
					},
				},
			},
		},
		resolvedBaseSHA: "resolved-sha",
	}
	repo, ok := j.Repository()
	if !ok {
		t.Fatal("expected ok")
	}
	if repo.BaseCommitSHA != "resolved-sha" {
		t.Errorf("expected resolved-sha, got %q", repo.BaseCommitSHA)
	}
}

func TestJob_Repository_ResolvedHeadSHA(t *testing.T) {
	repoURL := "https://github.com/org/repo.git"
	j := &job{
		detail: &scannerv1.GetJobDetailResponse{
			Target: &scannerv1.JobTarget{
				Repository: &scannerv1.RepositoryJobTarget{
					Target: &commonv1.RepositoryTarget{
						Url: repoURL,
					},
				},
			},
		},
		resolvedHeadSHA: "head-sha-abc",
	}
	repo, ok := j.Repository()
	if !ok {
		t.Fatal("expected ok")
	}
	if repo.CommitSHA != "head-sha-abc" {
		t.Errorf("expected head-sha-abc, got %q", repo.CommitSHA)
	}
}

func TestJob_Repository_ResolvedHeadSHA_NoOverride(t *testing.T) {
	repoURL := "https://github.com/org/repo.git"
	serverSHA := "server-sha-123"
	j := &job{
		detail: &scannerv1.GetJobDetailResponse{
			Target: &scannerv1.JobTarget{
				Repository: &scannerv1.RepositoryJobTarget{
					Target: &commonv1.RepositoryTarget{
						Url:       repoURL,
						CommitSha: &serverSHA,
					},
				},
			},
		},
		resolvedHeadSHA: "local-sha-456",
	}
	repo, ok := j.Repository()
	if !ok {
		t.Fatal("expected ok")
	}
	if repo.CommitSHA != "server-sha-123" {
		t.Errorf("expected server-sha-123, got %q", repo.CommitSHA)
	}
}

// --- job.Scanner ---

func TestJob_Scanner(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{Scanner: "subdomain"})
	if j.Scanner() != "subdomain" {
		t.Errorf("expected subdomain, got %q", j.Scanner())
	}
}

// --- job.TimeoutMinutes ---

func TestJob_TimeoutMinutes(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{TimeoutMinutes: 30})
	if j.TimeoutMinutes() != 30 {
		t.Errorf("expected 30, got %d", j.TimeoutMinutes())
	}
}

func TestJob_TimeoutMinutes_Zero(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{})
	if j.TimeoutMinutes() != 0 {
		t.Errorf("zero timeout should be 0, got %d", j.TimeoutMinutes())
	}
}

// --- job.Version ---

func TestJob_Version(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{Version: 2})
	if j.Version() != 2 {
		t.Errorf("expected 2, got %d", j.Version())
	}
}

func TestJob_Version_Zero(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{})
	if j.Version() != 1 {
		t.Errorf("zero version should default to 1, got %d", j.Version())
	}
}

// --- job.Integration ---

func TestJob_Integration_Nil(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{})
	if j.Integration() != nil {
		t.Error("nil integration should return nil")
	}
}

func TestJob_Integration_NilDetail(t *testing.T) {
	j := newJob(nil)
	if j.Integration() != nil {
		t.Error("nil detail should return nil integration")
	}
}

func TestJob_Integration_WithTokens(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{
		Integration: &scannerv1.JobIntegration{
			CloudflareTokens: []string{"token1", "token2"},
		},
	})
	intg := j.Integration()
	if intg == nil {
		t.Fatal("expected non-nil integration")
	}
	if len(intg.CloudflareTokens) != 2 {
		t.Errorf("expected 2 tokens, got %d", len(intg.CloudflareTokens))
	}
}

func TestJob_Integration_EmptyTokens(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{
		Integration: &scannerv1.JobIntegration{},
	})
	// empty integration with no tokens → nil
	intg := j.Integration()
	if intg != nil {
		t.Error("expected nil integration when no tokens")
	}
}

// --- job.RepoDir ---

func TestJob_RepoDir_Default(t *testing.T) {
	j := newJob(&scannerv1.GetJobDetailResponse{})
	if j.RepoDir() != "" {
		t.Errorf("default repoDir should be empty, got %q", j.RepoDir())
	}
}

// --- buildRefSpecs ---

func TestBuildRefSpecs_Push_Branch(t *testing.T) {
	repo := &Repository{
		Event:     "push",
		Branch:    "main",
		CommitSHA: "abc123",
	}
	refs, checkout := buildRefSpecs(repo)
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d: %v", len(refs), refs)
	}
	if refs[0] != "+refs/heads/main:refs/remotes/origin/main" {
		t.Errorf("unexpected ref: %q", refs[0])
	}
	if checkout != "abc123" {
		t.Errorf("checkout: got %q", checkout)
	}
}

func TestBuildRefSpecs_Push_Tag(t *testing.T) {
	repo := &Repository{
		Event:     "push",
		Ref:       "refs/tags/v1.0.0",
		CommitSHA: "tag-sha",
	}
	refs, checkout := buildRefSpecs(repo)
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref, got %d: %v", len(refs), refs)
	}
	if refs[0] != "refs/tags/v1.0.0" {
		t.Errorf("unexpected ref: %q", refs[0])
	}
	if checkout != "tag-sha" {
		t.Errorf("checkout: got %q", checkout)
	}
}

func TestBuildRefSpecs_Push_WithBaseCommitSHA(t *testing.T) {
	repo := &Repository{
		Event:         "push",
		Branch:        "main",
		CommitSHA:     "head-sha",
		BaseCommitSHA: "base-sha",
	}
	refs, _ := buildRefSpecs(repo)
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs (branch + base), got %d: %v", len(refs), refs)
	}
}

func TestBuildRefSpecs_MergeRequest(t *testing.T) {
	repo := &Repository{
		Event:      "merge_request",
		Provider:   "gitlab",
		PrNumber:   10,
		CommitSHA:  "mr-sha",
		BaseBranch: "main",
	}
	refs, checkout := buildRefSpecs(repo)
	if len(refs) < 2 {
		t.Fatalf("expected at least 2 refs, got %d: %v", len(refs), refs)
	}
	if checkout != "FETCH_HEAD" {
		t.Errorf("GitLab MR checkout should be FETCH_HEAD, got %q", checkout)
	}
}

func TestBuildRefSpecs_PullRequest_GitHub(t *testing.T) {
	repo := &Repository{
		Event:      "pull_request",
		Provider:   "github",
		CommitSHA:  "pr-sha",
		BaseBranch: "main",
	}
	refs, checkout := buildRefSpecs(repo)
	if checkout != "pr-sha" {
		t.Errorf("GitHub PR checkout should be SHA, got %q", checkout)
	}
	if len(refs) < 1 {
		t.Fatal("expected at least 1 ref")
	}
}

func TestBuildRefSpecs_Fallback_NoRefs(t *testing.T) {
	repo := &Repository{
		CommitSHA: "fallback-sha",
	}
	refs, checkout := buildRefSpecs(repo)
	if len(refs) != 1 || refs[0] != "fallback-sha" {
		t.Errorf("fallback should use CommitSHA: %v", refs)
	}
	if checkout != "fallback-sha" {
		t.Errorf("fallback checkout: got %q", checkout)
	}
}

func TestBuildRefSpecs_Empty(t *testing.T) {
	repo := &Repository{}
	refs, checkout := buildRefSpecs(repo)
	if len(refs) != 0 {
		t.Errorf("completely empty repo should have no refs, got %v", refs)
	}
	if checkout != "" {
		t.Errorf("checkout should be empty, got %q", checkout)
	}
}

func TestBuildRefSpecs_EmptyCommitSHA_FallbackToBranch(t *testing.T) {
	repo := &Repository{
		Event:     "manual",
		Branch:    "main",
		CommitSHA: "",
	}
	refs, checkout := buildRefSpecs(repo)
	if len(refs) != 1 {
		t.Fatalf("expected 1 ref (branch), got %v", refs)
	}
	if refs[0] != "+refs/heads/main:refs/remotes/origin/main" {
		t.Errorf("expected branch refspec, got %q", refs[0])
	}
	if checkout != "origin/main" {
		t.Errorf("expected branch checkout fallback, got %q", checkout)
	}
}

func TestBuildRefSpecs_Push_EmptyCommitSHA_WithBranch(t *testing.T) {
	repo := &Repository{
		Event:     "push",
		Branch:    "develop",
		CommitSHA: "",
		Ref:       "refs/heads/develop",
	}
	refs, checkout := buildRefSpecs(repo)
	found := false
	for _, r := range refs {
		if r == "+refs/heads/develop:refs/remotes/origin/develop" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected branch refspec, got %v", refs)
	}
	if checkout != "origin/develop" {
		t.Errorf("expected branch checkout fallback, got %q", checkout)
	}
}

// --- buildMrPrRefSpecs per provider ---

func TestBuildMrPrRefSpecs_Bitbucket(t *testing.T) {
	repo := &Repository{
		Provider:   "bitbucket",
		PrNumber:   5,
		BaseBranch: "develop",
	}
	refs, checkout := buildMrPrRefSpecs(repo)
	if checkout != "FETCH_HEAD" {
		t.Errorf("Bitbucket should use FETCH_HEAD, got %q", checkout)
	}
	found := false
	for _, r := range refs {
		if r == "refs/pull-requests/5/from" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected bitbucket PR ref, got %v", refs)
	}
}

func TestBuildMrPrRefSpecs_UnknownProvider(t *testing.T) {
	repo := &Repository{
		Provider:  "gitea",
		CommitSHA: "sha123",
	}
	refs, checkout := buildMrPrRefSpecs(repo)
	if checkout != "sha123" {
		t.Errorf("unknown provider should use SHA, got %q", checkout)
	}
	if len(refs) < 1 || refs[0] != "sha123" {
		t.Errorf("unknown provider should fetch SHA: %v", refs)
	}
}

// --- buildRepoURL ---

func TestBuildRepoURL_Basic(t *testing.T) {
	j := &job{detail: &scannerv1.GetJobDetailResponse{}}
	url, err := j.buildRepoURL(&Repository{URL: "https://github.com/org/repo.git"})
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/org/repo.git" {
		t.Errorf("got %q", url)
	}
}

func TestBuildRepoURL_WithCredentials(t *testing.T) {
	j := &job{detail: &scannerv1.GetJobDetailResponse{}}
	url, err := j.buildRepoURL(&Repository{
		URL:      "https://github.com/org/repo.git",
		Username: "user",
		Password: "pass",
	})
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://user:pass@github.com/org/repo.git" {
		t.Errorf("got %q", url)
	}
}

func TestBuildRepoURL_Empty(t *testing.T) {
	j := &job{detail: &scannerv1.GetJobDetailResponse{}}
	_, err := j.buildRepoURL(&Repository{})
	if err == nil {
		t.Error("empty URL should return error")
	}
}

func TestBuildRepoURL_InvalidURL(t *testing.T) {
	j := &job{detail: &scannerv1.GetJobDetailResponse{}}
	_, err := j.buildRepoURL(&Repository{URL: "://invalid"})
	if err == nil {
		t.Error("invalid URL should return error")
	}
}

// --- cleanupRepository ---

func TestCleanupRepository_NoClonedDir(t *testing.T) {
	j := &job{repoDir: "/some/ci/dir"}
	j.cleanupRepository() // should be no-op
	if j.repoDir != "/some/ci/dir" {
		t.Errorf("CI repoDir should not be cleared, got %q", j.repoDir)
	}
}
