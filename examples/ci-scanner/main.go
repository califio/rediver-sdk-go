// Example CI scanner demonstrating CI mode with SAST scanning on a local repository.
// CI mode auto-detects the CI environment (GitLab CI, GitHub Actions), creates a job,
// scans the locally checked-out repo, reports findings, and exits.
//
// Usage in GitLab CI (.gitlab-ci.yml):
//
//	sast_scan:
//	  image: your-scanner-image:latest
//	  script:
//	    - ci-scanner
//	  variables:
//	    REDIVER_URL: https://rediver.example.com
//	    REDIVER_TOKEN: $CLUSTER_TOKEN
//
// Usage in GitHub Actions:
//
//   - name: SAST Scan
//     run: ci-scanner
//     env:
//     REDIVER_URL: https://rediver.example.com
//     REDIVER_TOKEN: ${{ secrets.CLUSTER_TOKEN }}
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/joho/godotenv"

	"github.com/califio/rediver-sdk-go"
)

func main() {
	godotenv.Load()

	agent, err := rediver.NewAgent(os.Getenv("REDIVER_TOKEN"),
		rediver.NewScanner("semgrep",
			[]rediver.TargetType{rediver.TargetTypeRepository},
			semgrepHandler,
			rediver.WithDisplayName("Semgrep SAST"),
		),
	)
	if err != nil {
		log.Fatal(err)
	}

	// CI: detect env → create job → scan local repo → report → revoke token → exit
	if err := agent.RunCI(context.Background()); err != nil {
		log.Fatal(err)
	}
	log.Println("CI scan complete")
}

func semgrepHandler(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
	// RepoDir returns the local checkout path (SDK handles clone/cleanup automatically)
	repoDir := job.RepoDir()
	if repoDir == "" {
		return fmt.Errorf("no repository available")
	}

	logger := job.Logger()
	logger.Info("scanning repository", "path", repoDir)

	// Simulated SAST scan — replace with real semgrep execution
	var findings []rediver.SASTFinding

	err := filepath.Walk(repoDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".py") {
			return nil
		}

		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable files
		}

		relPath, _ := filepath.Rel(repoDir, path)

		// Example: detect hardcoded secrets
		lines := strings.Split(string(content), "\n")
		for i, line := range lines {
			if strings.Contains(line, "password") && strings.Contains(line, "=") {
				findings = append(findings, rediver.SASTFinding{
					Name:        "Hardcoded Password",
					Description: "A hardcoded password was found in source code.",
					Severity:    rediver.SeverityHigh,
					File:        relPath,
					StartLine:   i + 1,
					EndLine:     i + 1,
					Snippet:     strings.TrimSpace(line),
					Category:    "secret",
					RuleID:      "hardcoded-password-001",
					CWEs:        []string{"CWE-798"},
					CVSSScore:   7.5,
					Remediation: "Use environment variables or a secrets manager instead of hardcoded credentials.",
				})
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("scan repo: %w", err)
	}

	logger.Info("scan complete", "findings_count", len(findings))
	if len(findings) > 0 {
		emit(rediver.SASTFindings(findings...))
	}
	return nil
}
