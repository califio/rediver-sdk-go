package utils

import (
	"context"
	"os"
	"testing"
)

func TestGitRevParseHead(t *testing.T) {
	// Create a temp git repo with one commit
	dir, err := os.MkdirTemp("", "git-test-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	ctx := context.Background()
	Exec(ctx, "git", []string{"init"}, WithWorkDir(dir))
	Exec(ctx, "git", []string{"config", "user.email", "test@test.com"}, WithWorkDir(dir))
	Exec(ctx, "git", []string{"config", "user.name", "Test"}, WithWorkDir(dir))

	// Write a file and commit
	os.WriteFile(dir+"/test.txt", []byte("hello"), 0644)
	Exec(ctx, "git", []string{"add", "."}, WithWorkDir(dir))
	Exec(ctx, "git", []string{"commit", "-m", "init"}, WithWorkDir(dir))

	sha, err := GitRevParseHead(ctx, dir)
	if err != nil {
		t.Fatalf("GitRevParseHead failed: %v", err)
	}
	if len(sha) != 40 {
		t.Errorf("expected 40-char SHA, got %q (len=%d)", sha, len(sha))
	}
}

func TestGitRevParseHead_NotARepo(t *testing.T) {
	dir, _ := os.MkdirTemp("", "not-a-repo-*")
	defer os.RemoveAll(dir)

	_, err := GitRevParseHead(context.Background(), dir)
	if err == nil {
		t.Error("expected error for non-git directory")
	}
}
