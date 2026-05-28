// Example direct job execution demonstrating RunOnce for orchestrated single-shot workflows.
// The agent polls once for a pending job, executes it, revokes the token, and exits.
// Set REDIVER_JOB_ID in the environment if you need the server to assign a specific job;
// otherwise the server assigns the next available job for the registered scanner.
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

	agent, err := rediver.NewAgent(os.Getenv("REDIVER_TOKEN"),
		rediver.NewScanner("nuclei",
			[]rediver.TargetType{rediver.TargetTypeService},
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
	log.Println("direct job complete, token revoked")
}

func nucleiHandler(ctx context.Context, job rediver.Job, emit func(rediver.Result)) error {
	logger := job.Logger()
	logger.Info("scanning domains", "count", len(job.Domains()), "job_id", job.ID())
	// ... nuclei scan logic ...
	return nil
}
