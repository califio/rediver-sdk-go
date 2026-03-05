// Example multi-scanner agent demonstrating daemon mode with multiple scanners.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	"github.com/califio/rediver-sdk-go"
)

func main() {
	godotenv.Load()

	agent, err := rediver.NewAgent(
		os.Getenv("REDIVER_URL"),
		os.Getenv("REDIVER_TOKEN"),
		rediver.WithWorkerMode(),
		rediver.WithMaxConcurrency(3),
		rediver.WithVersion("1.0.0"),
	)
	if err != nil {
		log.Fatal(err)
	}

	if err := agent.Register(
		rediver.NewScanner("subfinder",
			[]rediver.TargetType{rediver.TargetTypeDomain, rediver.TargetTypeRootDomain},
			subfinderHandler,
			rediver.WithParam(
				rediver.StringParam("wordlist").
					Label("Wordlist").
					Description("Path to wordlist file").
					Default("/wordlists/dns.txt").
					Build(),
			),
		),
		rediver.NewScanner("httpx",
			[]rediver.TargetType{rediver.TargetTypeDomain, rediver.TargetTypeIP, rediver.TargetTypeService},
			httpxHandler,
		),
	); err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	if err := agent.RunAsWorker(ctx); err != nil {
		log.Fatal(err)
	}
}

func subfinderHandler(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
	logger := job.Logger()
	logger.Info("scanning domains", "count", len(job.Domains()))
	// ... subdomain enumeration logic ...
	return nil
}

func httpxHandler(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
	logger := job.Logger()
	logger.Info("probing targets", "domains", len(job.Domains()), "services", len(job.Services()))
	// ... HTTP probing logic ...
	return nil
}
