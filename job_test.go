package rediver

import (
	"context"
	"log/slog"
	"testing"

	"github.com/califio/rediver-sdk-go/internal/api"
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
	id := "job-abc"
	j := newJob(&api.JobDetail{Id: &id})
	if j.ID() != "job-abc" {
		t.Errorf("expected job-abc, got %q", j.ID())
	}
}

func TestJob_ID_NilId(t *testing.T) {
	j := newJob(&api.JobDetail{})
	if j.ID() != "" {
		t.Errorf("nil Id should return empty, got %q", j.ID())
	}
}

// --- job.Type ---

func TestJob_Type_Discovery(t *testing.T) {
	j := newJob(&api.JobDetail{})
	if j.Type() != JobTypeDiscovery {
		t.Errorf("default should be discovery, got %v", j.Type())
	}
}

func TestJob_Type_Retest(t *testing.T) {
	retest := true
	j := newJob(&api.JobDetail{Retest: &retest})
	if j.Type() != JobTypeRetest {
		t.Errorf("expected retest, got %v", j.Type())
	}
}

func TestJob_Type_RetestFalse(t *testing.T) {
	retest := false
	j := newJob(&api.JobDetail{Retest: &retest})
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
	j := newJob(&api.JobDetail{})
	if j.Domains() != nil {
		t.Error("nil target should return nil domains")
	}
}

func TestJob_Domains_NilDomains(t *testing.T) {
	j := newJob(&api.JobDetail{Target: &api.JobTarget{}})
	if j.Domains() != nil {
		t.Error("nil domains field should return nil")
	}
}

func TestJob_Domains_WithData(t *testing.T) {
	id := "d1"
	val := "example.com"
	cname := "alias.com"
	ips := []string{"1.2.3.4"}
	domains := []api.DomainAsset{
		{Id: &id, Value: &val, Cname: &cname, Ips: &ips},
		{Value: &val}, // partial data
	}
	j := newJob(&api.JobDetail{Target: &api.JobTarget{Domains: &domains}})
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
	// Partial data: ID should be empty
	if result[1].ID != "" {
		t.Errorf("partial domain should have empty ID, got %q", result[1].ID)
	}
}

// --- job.IPs ---

func TestJob_IPs_NilTarget(t *testing.T) {
	j := newJob(&api.JobDetail{})
	if j.IPs() != nil {
		t.Error("nil target should return nil")
	}
}

func TestJob_IPs_WithData(t *testing.T) {
	id := "ip1"
	val := "192.168.1.1"
	ips := []api.ValueAsset{{Id: &id, Value: &val}}
	j := newJob(&api.JobDetail{Target: &api.JobTarget{Ips: &ips}})
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
	j := newJob(&api.JobDetail{})
	if j.Subnets() != nil {
		t.Error("nil target should return nil")
	}
}

func TestJob_Subnets_WithData(t *testing.T) {
	id := "s1"
	val := "10.0.0.0/24"
	subnets := []api.ValueAsset{{Id: &id, Value: &val}}
	j := newJob(&api.JobDetail{Target: &api.JobTarget{Subnets: &subnets}})
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
	j := newJob(&api.JobDetail{})
	if j.Services() != nil {
		t.Error("nil target should return nil")
	}
}

func TestJob_Services_WithData(t *testing.T) {
	id := "sv1"
	val := "example.com:443"
	host := "example.com"
	port := int32(443)
	svcURL := "https://example.com"
	services := []api.ServiceAsset{{Id: &id, Value: &val, Host: &host, Port: &port, Url: &svcURL}}
	j := newJob(&api.JobDetail{Target: &api.JobTarget{Services: &services}})
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

func TestJob_Param_Set(t *testing.T) {
	params := map[string]interface{}{"mode": "fast", "threads": 10}
	j := newJob(&api.JobDetail{Params: &params})
	pv := j.Param("mode")
	if !pv.IsSet() {
		t.Error("expected IsSet true")
	}
	if pv.String() != "fast" {
		t.Errorf("expected 'fast', got %q", pv.String())
	}
}

func TestJob_Param_Unset(t *testing.T) {
	j := newJob(&api.JobDetail{})
	pv := j.Param("missing")
	if pv.IsSet() {
		t.Error("expected IsSet false")
	}
	if pv.StringOr("default") != "default" {
		t.Error("expected fallback")
	}
}

func TestJob_Param_NilParams(t *testing.T) {
	j := newJob(nil)
	pv := j.Param("anything")
	if pv.IsSet() {
		t.Error("nil detail should return unset param")
	}
}

// --- job.Repository ---

func TestJob_Repository_None(t *testing.T) {
	j := newJob(&api.JobDetail{})
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
	provider := "github"
	event := "push"
	diffOnly := true
	username := "user"
	password := "pass"
	prNum := int32(42)
	j := newJob(&api.JobDetail{
		Target: &api.JobTarget{
			Repository: &api.RepositoryJobContext{
				Url:       &repoURL,
				Branch:    &branch,
				CommitSha: &sha,
				Provider:  &provider,
				Event:     &event,
				DiffOnly:  &diffOnly,
				PrNumber:  &prNum,
				Credential: &api.AssetCredential{
					Username: &username,
					Password: &password,
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
	if repo.Username != "user" || repo.Password != "pass" {
		t.Error("credential mismatch")
	}
}

func TestJob_Repository_ResolvedBaseSHA(t *testing.T) {
	repoURL := "https://github.com/org/repo.git"
	j := &job{
		detail: &api.JobDetail{
			Target: &api.JobTarget{
				Repository: &api.RepositoryJobContext{
					Url: &repoURL,
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

// --- job.Scanner ---

func TestJob_Scanner(t *testing.T) {
	name := "subdomain"
	j := newJob(&api.JobDetail{Scanner: &name})
	if j.Scanner() != "subdomain" {
		t.Errorf("expected subdomain, got %q", j.Scanner())
	}
}

// --- job.TimeoutMinutes ---

func TestJob_TimeoutMinutes(t *testing.T) {
	timeout := int32(30)
	j := newJob(&api.JobDetail{TimeoutMinutes: &timeout})
	if j.TimeoutMinutes() != 30 {
		t.Errorf("expected 30, got %d", j.TimeoutMinutes())
	}
}

func TestJob_TimeoutMinutes_Nil(t *testing.T) {
	j := newJob(&api.JobDetail{})
	if j.TimeoutMinutes() != 0 {
		t.Errorf("nil timeout should be 0, got %d", j.TimeoutMinutes())
	}
}

// --- job.Version ---

func TestJob_Version(t *testing.T) {
	v := int32(2)
	j := newJob(&api.JobDetail{Version: &v})
	if j.Version() != 2 {
		t.Errorf("expected 2, got %d", j.Version())
	}
}

func TestJob_Version_Nil(t *testing.T) {
	j := newJob(&api.JobDetail{})
	if j.Version() != 1 {
		t.Errorf("nil version should default to 1, got %d", j.Version())
	}
}

// --- job.Logger ---

func TestJob_Logger_Nil(t *testing.T) {
	j := newJob(nil)
	logger := j.Logger()
	if logger == nil {
		t.Fatal("Logger() should never return nil")
	}
	// Should not panic when logging
	logger.Info("test message")
}


// --- job.Integration ---

func TestJob_Integration_Nil(t *testing.T) {
	j := newJob(&api.JobDetail{})
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
	tokens := []string{"token1", "token2"}
	j := newJob(&api.JobDetail{
		Integration: &api.JobIntegration{
			CloudflareTokens: &tokens,
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
	j := newJob(&api.JobDetail{
		Integration: &api.JobIntegration{},
	})
	intg := j.Integration()
	if intg == nil {
		t.Fatal("expected non-nil integration even without tokens")
	}
	if intg.CloudflareTokens != nil {
		t.Errorf("expected nil tokens, got %v", intg.CloudflareTokens)
	}
}

// --- job.RepoDir ---

func TestJob_RepoDir_Default(t *testing.T) {
	j := newJob(&api.JobDetail{})
	if j.RepoDir() != "" {
		t.Errorf("default repoDir should be empty, got %q", j.RepoDir())
	}
}

// --- job.Targets (internal method) ---

func TestJob_Targets_Mixed(t *testing.T) {
	domainVal := "example.com"
	ipVal := "1.2.3.4"
	subnetVal := "10.0.0.0/24"
	svcHost := "db.local"
	svcPort := int32(3306)
	svcURL := "https://api.example.com"

	domains := []api.DomainAsset{{Value: &domainVal}}
	ips := []api.ValueAsset{{Value: &ipVal}}
	subnets := []api.ValueAsset{{Value: &subnetVal}}
	services := []api.ServiceAsset{
		{Host: &svcHost, Port: &svcPort},
		{Url: &svcURL},
	}

	detail := &api.JobDetail{
		Target: &api.JobTarget{
			Domains:  &domains,
			Ips:      &ips,
			Subnets:  &subnets,
			Services: &services,
		},
	}
	j := newJob(detail).(*job)
	targets := j.Targets()
	if len(targets) != 5 {
		t.Fatalf("expected 5 targets, got %d: %v", len(targets), targets)
	}
	// Order: domains, IPs, subnets, services
	if targets[0] != "example.com" {
		t.Errorf("first target: got %q", targets[0])
	}
	if targets[1] != "1.2.3.4" {
		t.Errorf("second target: got %q", targets[1])
	}
	if targets[2] != "10.0.0.0/24" {
		t.Errorf("third target: got %q", targets[2])
	}
	// Service without URL uses host:port
	if targets[3] != "db.local:3306" {
		t.Errorf("fourth target: got %q", targets[3])
	}
	// Service with URL
	if targets[4] != "https://api.example.com" {
		t.Errorf("fifth target: got %q", targets[4])
	}
}

func TestJob_Targets_Empty(t *testing.T) {
	j := newJob(&api.JobDetail{}).(*job)
	if j.Targets() != nil {
		t.Error("empty targets should return nil")
	}
}

// --- job.URLs ---

func TestJob_URLs_NoServices(t *testing.T) {
	j := newJob(&api.JobDetail{}).(*job)
	if j.URLs() != nil {
		t.Error("no services should return nil URLs")
	}
}

func TestJob_URLs_WithURLs(t *testing.T) {
	url1 := "https://a.com"
	url2 := "https://b.com"
	host := "c.com"
	port := int32(80)
	services := []api.ServiceAsset{
		{Url: &url1},
		{Host: &host, Port: &port}, // no URL
		{Url: &url2},
	}
	j := newJob(&api.JobDetail{Target: &api.JobTarget{Services: &services}}).(*job)
	urls := j.URLs()
	if len(urls) != 2 {
		t.Fatalf("expected 2 URLs, got %d", len(urls))
	}
	if urls[0] != "https://a.com" || urls[1] != "https://b.com" {
		t.Errorf("unexpected URLs: %v", urls)
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
	// Should have MR head ref + base branch ref
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
	j := &job{detail: &api.JobDetail{}}
	url, err := j.buildRepoURL(&Repository{URL: "https://github.com/org/repo.git"})
	if err != nil {
		t.Fatal(err)
	}
	if url != "https://github.com/org/repo.git" {
		t.Errorf("got %q", url)
	}
}

func TestBuildRepoURL_WithCredentials(t *testing.T) {
	j := &job{detail: &api.JobDetail{}}
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
	j := &job{detail: &api.JobDetail{}}
	_, err := j.buildRepoURL(&Repository{})
	if err == nil {
		t.Error("empty URL should return error")
	}
}

func TestBuildRepoURL_InvalidURL(t *testing.T) {
	j := &job{detail: &api.JobDetail{}}
	_, err := j.buildRepoURL(&Repository{URL: "://invalid"})
	if err == nil {
		t.Error("invalid URL should return error")
	}
}

// --- cleanupRepository ---

func TestCleanupRepository_NoClonedDir(t *testing.T) {
	j := &job{repoDir: "/some/ci/dir"}
	j.cleanupRepository() // should be no-op
	// repoDir should NOT be cleared for CI mode (clonedRepoDir is empty)
	if j.repoDir != "/some/ci/dir" {
		t.Errorf("CI repoDir should not be cleared, got %q", j.repoDir)
	}
}

// --- discardHandler ---

func TestDiscardHandler_NoPanic(t *testing.T) {
	h := discardHandler{}
	if h.Enabled(context.Background(), slog.LevelInfo) {
		t.Error("should always return false")
	}
	if err := h.Handle(context.Background(), slog.Record{}); err != nil {
		t.Error("should return nil")
	}
	if h.WithAttrs(nil) != h {
		t.Error("should return self")
	}
	if h.WithGroup("") != h {
		t.Error("should return self")
	}
}
