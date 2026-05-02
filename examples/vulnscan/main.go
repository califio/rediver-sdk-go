// Example vulnerability scanner demonstrating finding reporting with the Rediver SDK.
package main

import (
	"context"
	"fmt"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/joho/godotenv"

	"github.com/califio/rediver-sdk-go"
)

func main() {
	if err := godotenv.Load(".env"); err != nil {
		log.Fatalf("Error loading .env file %v", err)
	}

	scanner := rediver.NewScanner("vuln_scan",
		[]rediver.TargetType{rediver.TargetTypeDomain, rediver.TargetTypeService},
		scanVulnerabilities,
		rediver.WithParam(
			rediver.StringParam("severity_threshold").
				Label("Severity Threshold").
				Description("Minimum severity to report").
				Default("low").
				Build(),
		),
		rediver.WithParam(
			rediver.BoolParam("active_scan").
				Label("Active Scan").
				Description("Perform active vulnerability testing").
				Default(false).
				Build(),
		),
		rediver.WithParam(
			rediver.IntParam("rate_limit").
				Label("Rate Limit").
				Description("Maximum requests per second").
				Default(10).
				Build(),
		),
	)

	agent, err := rediver.NewAgent(os.Getenv("REDIVER_TOKEN"), scanner)
	if err != nil {
		log.Fatalf("create agent: %v", err)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	if err := agent.Run(ctx); err != nil {
		log.Fatalf("run: %v", err)
	}

	log.Println("task complete, token revoked")
}

func scanVulnerabilities(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
	logger := slog.New(job.SlogHandler())
	severityThreshold := job.Param("severity_threshold").StringOr("low")
	activeScan := job.Param("active_scan").BoolOr(false)
	rateLimit := job.Param("rate_limit").IntOr(10)

	logger.Info("starting vuln scan", "threshold", severityThreshold, "active", activeScan, "rate_limit", rateLimit)

	var findings []rediver.WebFinding
	for _, svc := range job.Services() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		target := svc.URL
		if target == "" {
			target = fmt.Sprintf("%s:%d", svc.Host, svc.Port)
		}

		findings = append(findings,
			rediver.WebFinding{
				Name: "SQL Injection", Severity: rediver.SeverityCritical,
				Endpoint: fmt.Sprintf("%s/api/login", target),
				Category: "injection", RuleID: "sqli-001",
				CWEs: []string{"CWE-89"}, CVSSScore: 9.8,
				Requests: []rediver.HTTPRequest{
					{
						RawRequest:  "POST /api/login HTTP/1.1\nHost: " + svc.Host + "\n\n{\"username\":\"admin' OR '1'='1\"}",
						RawResponse: "HTTP/1.1 200 OK\n\n{\"success\":true}",
					},
				},
			},
			rediver.WebFinding{
				Name: "Reflected XSS", Severity: rediver.SeverityHigh,
				Endpoint: fmt.Sprintf("%s/search?q=xss", target),
				Category: "xss", RuleID: "xss-reflected-001",
				CWEs: []string{"CWE-79"}, CVSSScore: 6.1,
			},
		)

		time.Sleep(200 * time.Millisecond)
	}

	emit(rediver.WebFindings(findings...))
	return nil
}
