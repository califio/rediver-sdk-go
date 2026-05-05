package rediver

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"buf.build/gen/go/rediver/api/connectrpc/go/agent/v1/agentv1connect"
	agentv1 "buf.build/gen/go/rediver/api/protocolbuffers/go/agent/v1"
	"connectrpc.com/connect"
)

type ciTokenService struct {
	agentv1connect.UnimplementedTokenServiceHandler
}

func (s *ciTokenService) GenerateToken(_ context.Context, _ *connect.Request[agentv1.GenerateTokenRequest]) (*connect.Response[agentv1.GenerateTokenResponse], error) {
	return connect.NewResponse(&agentv1.GenerateTokenResponse{
		AgentId: "agent-1",
		Token:   "agent-token-1",
	}), nil
}

type ciJobService struct {
	agentv1connect.UnimplementedJobServiceHandler
}

func (s *ciJobService) CreateCiJob(_ context.Context, _ *connect.Request[agentv1.CreateCiJobRequest]) (*connect.Response[agentv1.CreateCiJobResponse], error) {
	return connect.NewResponse(&agentv1.CreateCiJobResponse{
		Success: true,
		JobId:   "job-1",
	}), nil
}

func (s *ciJobService) JobStart(_ context.Context, _ *connect.Request[agentv1.JobStartRequest]) (*connect.Response[agentv1.JobStartResponse], error) {
	return connect.NewResponse(&agentv1.JobStartResponse{}), nil
}

func (s *ciJobService) JobCompleted(_ context.Context, _ *connect.Request[agentv1.JobCompletedRequest]) (*connect.Response[agentv1.JobCompletedResponse], error) {
	return connect.NewResponse(&agentv1.JobCompletedResponse{}), nil
}

func (s *ciJobService) JobFailed(_ context.Context, _ *connect.Request[agentv1.JobFailedRequest]) (*connect.Response[agentv1.JobFailedResponse], error) {
	return connect.NewResponse(&agentv1.JobFailedResponse{}), nil
}

func (s *ciJobService) JobHeartbeat(_ context.Context, _ *connect.Request[agentv1.JobHeartbeatRequest]) (*connect.Response[agentv1.JobHeartbeatResponse], error) {
	return connect.NewResponse(&agentv1.JobHeartbeatResponse{}), nil
}

func newCITestServer(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(agentv1connect.NewTokenServiceHandler(&ciTokenService{}))
	mux.Handle(agentv1connect.NewJobServiceHandler(&ciJobService{}))
	mux.Handle(agentv1connect.NewAgentServiceHandler(&agentv1connect.UnimplementedAgentServiceHandler{}))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestRunCIProvidesExecutionTokenToScanner(t *testing.T) {
	t.Setenv("GITLAB_CI", "")
	t.Setenv("GITHUB_ACTIONS", "")

	repoDir := initTestGitRepo(t, "ci-token-repo")
	serverURL := newCITestServer(t)

	var gotToken string
	scanner := NewScanner("calif-audit", []TargetType{TargetTypeRepository}, func(_ context.Context, job Job, _ func(Result)) error {
		gotToken = job.ExecutionToken()
		wantRepoDir, err := filepath.EvalSymlinks(repoDir)
		if err != nil {
			t.Fatal(err)
		}
		if job.RepoDir() != wantRepoDir {
			t.Fatalf("RepoDir() = %q, want %q", job.RepoDir(), wantRepoDir)
		}
		return nil
	})

	agent, err := NewAgent("cluster-token", scanner, WithServerURL(serverURL), WithRepoDir(repoDir))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	if err := agent.RunCI(context.Background()); err != nil {
		t.Fatalf("RunCI() error = %v", err)
	}
	if gotToken != "agent-token-1" {
		t.Fatalf("ExecutionToken() = %q, want agent-token-1", gotToken)
	}
}

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
