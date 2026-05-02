// Example task mode agent demonstrating single-job execution with auto token revocation.
package main

import (
	"context"
	"log"
	"log/slog"
	"os"

	"github.com/joho/godotenv"

	"github.com/califio/rediver-sdk-go"
)

func main() {
	godotenv.Load()

	agent, err := rediver.NewAgent(os.Getenv("REDIVER_TOKEN"),
		rediver.NewScanner("nuclei",
			[]rediver.TargetType{rediver.TargetTypeDomain, rediver.TargetTypeService},
			nucleiHandler,
		),
	)
	if err != nil {
		log.Fatal(err)
	}

	// RunOnce: poll once -> execute -> revoke token -> exit
	if err := agent.RunOnce(context.Background()); err != nil {
		log.Fatal(err)
	}
	log.Println("task complete, token revoked")
}

func nucleiHandler(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
	logger := slog.New(job.SlogHandler())
	logger.Info("scanning services", "count", len(job.Services()))
	// ... nuclei scan logic ...
	return nil
}
