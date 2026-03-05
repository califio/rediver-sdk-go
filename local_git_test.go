package rediver

import "testing"

func TestParseProviderFromURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected GitProvider
	}{
		{"github https", "https://github.com/org/repo.git", GitProviderGitHub},
		{"github ssh", "git@github.com:org/repo.git", GitProviderGitHub},
		{"gitlab https", "https://gitlab.com/org/repo.git", GitProviderGitLab},
		{"gitlab ssh", "git@gitlab.com:org/repo.git", GitProviderGitLab},
		{"self-hosted gitlab", "https://gitlab.example.com/org/repo.git", GitProviderGitLab},
		{"bitbucket https", "https://bitbucket.org/org/repo.git", GitProviderBitbucket},
		{"bitbucket ssh", "git@bitbucket.org:org/repo.git", GitProviderBitbucket},
		{"unknown host", "https://gitea.example.com/org/repo.git", GitProviderUnknown},
		{"empty string", "", GitProviderUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseProviderFromURL(tt.url)
			if got != tt.expected {
				t.Errorf("parseProviderFromURL(%q) = %q, want %q", tt.url, got, tt.expected)
			}
		})
	}
}

func TestExtractHostFromGitURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"ssh format", "git@github.com:org/repo.git", "github.com"},
		{"https format", "https://github.com/org/repo.git", "github.com"},
		{"https with port", "https://gitlab.example.com:8443/org/repo.git", "gitlab.example.com:8443"},
		{"empty", "", ""},
		{"invalid", "not-a-url", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractHostFromGitURL(tt.url)
			if got != tt.expected {
				t.Errorf("extractHostFromGitURL(%q) = %q, want %q", tt.url, got, tt.expected)
			}
		})
	}
}

func TestParseRepoName(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		repoDir  string
		expected string
	}{
		{"ssh with .git", "git@github.com:org/repo.git", "/tmp/repo", "org/repo"},
		{"ssh without .git", "git@github.com:org/repo", "/tmp/repo", "org/repo"},
		{"https with .git", "https://github.com/org/repo.git", "/tmp/repo", "org/repo"},
		{"https without .git", "https://github.com/org/repo", "/tmp/repo", "org/repo"},
		{"no remote", "", "/tmp/my-project", "my-project"},
		{"deep path", "https://gitlab.com/group/subgroup/repo.git", "/tmp/repo", "group/subgroup/repo"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseRepoName(tt.url, tt.repoDir)
			if got != tt.expected {
				t.Errorf("parseRepoName(%q, %q) = %q, want %q", tt.url, tt.repoDir, got, tt.expected)
			}
		})
	}
}

func TestRemoteToHtmlURL(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		expected string
	}{
		{"ssh to https", "git@github.com:org/repo.git", "https://github.com/org/repo"},
		{"https strip .git", "https://github.com/org/repo.git", "https://github.com/org/repo"},
		{"https no .git", "https://github.com/org/repo", "https://github.com/org/repo"},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := remoteToHtmlURL(tt.url)
			if got != tt.expected {
				t.Errorf("remoteToHtmlURL(%q) = %q, want %q", tt.url, got, tt.expected)
			}
		})
	}
}

func TestDetectLocalGit_InGitRepo(t *testing.T) {
	// This test runs inside the SDK git repo itself
	ci := detectLocalGit("")
	if ci == nil {
		t.Skip("not inside a git repository")
	}

	if ci.Source != "local" {
		t.Errorf("expected source 'local', got %q", ci.Source)
	}
	if ci.Ref.CommitSHA == "" {
		t.Error("expected non-empty commit SHA")
	}
	if ci.RepoDir == "" {
		t.Error("expected non-empty repo dir")
	}
	if ci.Repo.URL == "" {
		t.Log("warning: no remote URL detected (repo may have no remotes)")
	}
}

func TestDetectLocalGit_NotGitRepo(t *testing.T) {
	ci := detectLocalGit("/tmp")
	if ci != nil {
		t.Errorf("expected nil for non-git directory, got %+v", ci)
	}
}
