package rediver

import (
	"context"
	"errors"
	"os"
	"testing"
)

func TestNewAgent_NilScanner(t *testing.T) {
	t.Setenv("REDIVER_TOKEN", "tok")
	_, err := NewAgent("", nil)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("got %v, want ErrInvalidConfig", err)
	}
}

func TestNewAgent_EmptyTokenNoEnv(t *testing.T) {
	os.Unsetenv("REDIVER_TOKEN")
	scanner := NewScanner("test", []TargetType{TargetTypeDomain}, func(_ context.Context, _ Job, _ func(Result)) error { return nil })
	_, err := NewAgent("", scanner)
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("got %v, want ErrInvalidConfig", err)
	}
}

func TestNewAgent_TokenFromEnv(t *testing.T) {
	t.Setenv("REDIVER_TOKEN", "from-env")
	scanner := NewScanner("test", []TargetType{TargetTypeDomain}, func(_ context.Context, _ Job, _ func(Result)) error { return nil })
	a, err := NewAgent("", scanner)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.clusterToken != "from-env" {
		t.Errorf("got token %q, want from-env", a.clusterToken)
	}
}

func TestNewAgent_URLDefault(t *testing.T) {
	t.Setenv("REDIVER_TOKEN", "tok")
	os.Unsetenv("REDIVER_URL")
	scanner := NewScanner("test", []TargetType{TargetTypeDomain}, func(_ context.Context, _ Job, _ func(Result)) error { return nil })
	a, err := NewAgent("tok", scanner)
	if err != nil {
		t.Fatal(err)
	}
	if a.serverURL != DefaultServerURL {
		t.Errorf("got %q, want %q", a.serverURL, DefaultServerURL)
	}
}

func TestNewAgent_URLFromOption(t *testing.T) {
	t.Setenv("REDIVER_TOKEN", "tok")
	scanner := NewScanner("test", []TargetType{TargetTypeDomain}, func(_ context.Context, _ Job, _ func(Result)) error { return nil })
	a, err := NewAgent("tok", scanner, WithServerURL("https://opt.example.com"))
	if err != nil {
		t.Fatal(err)
	}
	if a.serverURL != "https://opt.example.com" {
		t.Errorf("got %q, want option value", a.serverURL)
	}
}
