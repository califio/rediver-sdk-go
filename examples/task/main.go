// Example task mode scanner demonstrating single-job execution with auto token revocation.
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

	agent, err := rediver.NewAgent(
		os.Getenv("REDIVER_URL"),
		os.Getenv("REDIVER_TOKEN"),
		rediver.WithTaskMode(),
	)
	if err != nil {
		log.Fatal(err)
	}

	if err := agent.Register(rediver.NewScanner("nuclei",
		[]rediver.TargetType{rediver.TargetTypeDomain, rediver.TargetTypeService},
		nucleiHandler,
	)); err != nil {
		log.Fatal(err)
	}

	// Task: poll -> execute -> revoke token -> exit
	if err := agent.Run(context.Background()); err != nil {
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
