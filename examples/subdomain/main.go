// Example subdomain scanner demonstrating the Rediver SDK Runner API.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"time"

	"github.com/joho/godotenv"

	"github.com/califio/rediver-sdk-go"
)

func main() {
	godotenv.Load()

	// Create scanner with parameters and asset type declarations
	scanner := rediver.NewScanner("subdomain",
		[]rediver.TargetType{rediver.TargetTypeRootDomain},
		discoverSubdomains,
		rediver.WithDisplayName("Subdomain Discovery"),
		rediver.WithRetestHandler(retestSubdomains),
		rediver.WithParam(
			rediver.StringParam("wordlist").
				Label("Wordlist").
				Description("Path to wordlist file").
				Default("/usr/share/wordlists/subdomains.txt").
				Build(),
		),
		rediver.WithParam(
			rediver.IntParam("threads").
				Label("Threads").
				Description("Number of concurrent threads").
				Default(10).
				Build(),
		),
	)

	// Create runner (worker mode: polls for jobs continuously until interrupted)
	runner, err := rediver.NewRunner(
		os.Getenv("REDIVER_URL"),
		os.Getenv("REDIVER_TOKEN"),
		rediver.WithWorkerMode(),
	)
	if err != nil {
		log.Fatalf("create runner: %v", err)
	}

	if err := runner.Add(scanner); err != nil {
		log.Fatalf("register: %v", err)
	}

	// Setup context with signal handling
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	// Run runner
	if err := runner.Run(ctx); err != nil {
		log.Fatalf("run: %v", err)
	}

	log.Println("task complete, token revoked")
}

func discoverSubdomains(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
	logger := job.Logger()
	wordlist := job.Param("wordlist").StringOr("/usr/share/wordlists/subdomains.txt")
	threads := job.Param("threads").IntOr(10)

	logger.Info("starting discovery", "wordlist", wordlist, "threads", threads)

	var domains []rediver.Domain
	for _, target := range job.Domains() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		// Simulated enumeration
		for i := 0; i < 5; i++ {
			subdomain := fmt.Sprintf("sub%d.%s", i, target.Value)
			domains = append(domains, rediver.Domain{
				Domain: subdomain,
			})
		}

		time.Sleep(100 * time.Millisecond)
	}

	emit(rediver.Domains(domains...))
	return nil
}

func retestSubdomains(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
	logger := job.Logger()
	domains := job.Domains()
	logger.Info("starting recheck", "domains_count", len(domains))

	var active []rediver.Domain
	for _, domain := range domains {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if domain.Value == "" {
			continue
		}

		isResolved := true
		if isResolved {
			active = append(active, rediver.Domain{
				Domain: domain.Value,
			})
		}

		time.Sleep(50 * time.Millisecond)
	}

	emit(rediver.Domains(active...))
	return nil
}
