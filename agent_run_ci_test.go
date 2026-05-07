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

	authv1 "github.com/califio/rediver-sdk-go/internal/gen/grpc/auth/v1"
	"github.com/califio/rediver-sdk-go/internal/gen/grpc/auth/v1/authv1connect"
	scannerv1 "github.com/califio/rediver-sdk-go/internal/gen/grpc/scanner/v1"
	"github.com/califio/rediver-sdk-go/internal/gen/grpc/scanner/v1/scannerv1connect"
)

type ciAuthTokenService struct {
	authv1connect.UnimplementedTokenServiceHandler
}

func (s *ciAuthTokenService) CreateJobToken(_ context.Context, _ *connect.Request[authv1.CreateJobTokenRequest]) (*connect.Response[authv1.CreateJobTokenResponse], error) {
	return connect.NewResponse(&authv1.CreateJobTokenResponse{Token: "job-jwt-1"}), nil
}

type ciScannerService struct {
	scannerv1connect.UnimplementedScannerServiceHandler
}

func (s *ciScannerService) RegisterAgent(_ context.Context, _ *connect.Request[scannerv1.RegisterAgentRequest]) (*connect.Response[scannerv1.RegisterAgentResponse], error) {
	return connect.NewResponse(&scannerv1.RegisterAgentResponse{RunnerId: "runner-1"}), nil
}

func (s *ciScannerService) JobStart(_ context.Context, _ *connect.Request[scannerv1.JobStartRequest]) (*connect.Response[scannerv1.JobStartResponse], error) {
	return connect.NewResponse(&scannerv1.JobStartResponse{Success: true}), nil
}

func (s *ciScannerService) JobCompleted(_ context.Context, _ *connect.Request[scannerv1.JobCompletedRequest]) (*connect.Response[scannerv1.JobCompletedResponse], error) {
	return connect.NewResponse(&scannerv1.JobCompletedResponse{Success: true}), nil
}

func (s *ciScannerService) JobFailed(_ context.Context, _ *connect.Request[scannerv1.JobFailedRequest]) (*connect.Response[scannerv1.JobFailedResponse], error) {
	return connect.NewResponse(&scannerv1.JobFailedResponse{Success: true}), nil
}

func (s *ciScannerService) JobHeartbeat(_ context.Context, _ *connect.Request[scannerv1.JobHeartbeatRequest]) (*connect.Response[scannerv1.JobHeartbeatResponse], error) {
	return connect.NewResponse(&scannerv1.JobHeartbeatResponse{Success: true}), nil
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

func newCITestServer(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(authv1connect.NewTokenServiceHandler(&ciAuthTokenService{}))
	mux.Handle(scannerv1connect.NewScannerServiceHandler(&ciScannerService{}))
	mux.Handle(agentv1connect.NewJobServiceHandler(&ciJobService{}))
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

	agent, err := NewAgent("agent-token", scanner, WithServerURL(serverURL), WithRepoDir(repoDir))
	if err != nil {
		t.Fatalf("NewAgent() error = %v", err)
	}

	if err := agent.RunCI(context.Background()); err != nil {
		t.Fatalf("RunCI() error = %v", err)
	}
	if gotToken != "job-jwt-1" {
		t.Fatalf("ExecutionToken() = %q, want job-jwt-1", gotToken)
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
