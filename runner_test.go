package rediver

import (
	"testing"
)

func TestResolveServerURL_Default(t *testing.T) {
	t.Setenv("REDIVER_URL", "")
	got := resolveServerURL("")
	if got != DefaultServerURL {
		t.Errorf("got %q, want %q", got, DefaultServerURL)
	}
}

func TestResolveServerURL_Env(t *testing.T) {
	t.Setenv("REDIVER_URL", "https://env.example.com")
	got := resolveServerURL("")
	if got != "https://env.example.com" {
		t.Errorf("got %q, want env value", got)
	}
}

func TestResolveServerURL_Explicit(t *testing.T) {
	t.Setenv("REDIVER_URL", "https://env.example.com")
	got := resolveServerURL("https://explicit.example.com")
	if got != "https://explicit.example.com" {
		t.Errorf("got %q, want explicit value", got)
	}
}
