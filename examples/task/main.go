// Example task mode runner demonstrating single-job execution with auto token revocation.
package main

import (
	"context"
	"log"
	"os"

	"github.com/joho/godotenv"

	"github.com/califio/rediver-sdk-go"
)

func main() {
	godotenv.Load()

	runner, err := rediver.NewRunner(
		os.Getenv("REDIVER_URL"),
		os.Getenv("REDIVER_TOKEN"),
	)
	if err != nil {
		log.Fatal(err)
	}

	if err := runner.Add(rediver.NewScanner("nuclei",
		[]rediver.TargetType{rediver.TargetTypeDomain, rediver.TargetTypeService},
		nucleiHandler,
	)); err != nil {
		log.Fatal(err)
	}

	// RunOnce: poll once -> execute -> revoke token -> exit
	if err := runner.RunOnce(context.Background()); err != nil {
		log.Fatal(err)
	}
	log.Println("task complete, token revoked")
}

func nucleiHandler(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
	logger := job.Logger()
	logger.Info("scanning services", "count", len(job.Services()))
	// ... nuclei scan logic ...
	return nil
}
