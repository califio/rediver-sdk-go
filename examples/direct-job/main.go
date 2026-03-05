// Example direct job execution demonstrating WithJobID for orchestrated workflows.
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

	jobID := os.Getenv("REDIVER_JOB_ID")
	if jobID == "" {
		log.Fatal("REDIVER_JOB_ID required")
	}

	agent, err := rediver.NewAgent(
		os.Getenv("REDIVER_URL"),
		os.Getenv("REDIVER_TOKEN"),
		rediver.WithTaskMode(),
	)
	if err != nil {
		log.Fatal(err)
	}

	if err := agent.Register(rediver.NewScanner("nuclei",
		[]rediver.TargetType{rediver.TargetTypeService},
		nucleiHandler,
	)); err != nil {
		log.Fatal(err)
	}

	// Direct job: skip pull, fetch detail, validate capability, execute, revoke
	if err := agent.Run(context.Background(), rediver.WithJobID(jobID)); err != nil {
		log.Fatal(err)
	}
	log.Println("direct job complete, token revoked")
}

func nucleiHandler(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
	logger := job.Logger()
	logger.Info("scanning domains", "count", len(job.Domains()), "job_id", job.ID())
	// ... nuclei scan logic ...
	return nil
}
