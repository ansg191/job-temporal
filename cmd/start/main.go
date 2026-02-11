package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"go.temporal.io/sdk/client"

	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/jobsource"
	"github.com/ansg191/job-temporal/internal/workflows"
)

func main() {
	ctx := context.Background()

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt)
	defer stop()

	c, err := client.Dial(client.Options{})
	if err != nil {
		log.Fatalln("Unable to create client", err)
	}
	defer c.Close()

	options := client.StartWorkflowOptions{
		TaskQueue: "my-task-queue",
	}

	if len(os.Args) < 2 {
		log.Fatalln("Usage: go run ./cmd/start/main.go '<job URL>'")
	}

	input := os.Args[1]
	resolver := jobsource.NewDefaultResolver()

	jobDesc, err := resolver.Resolve(ctx, input)
	if err != nil {
		log.Fatalln("Unable to resolve job description", err)
	}

	log.Println("Starting workflow with resolved job description")
	we, err := c.ExecuteWorkflow(
		ctx,
		options,
		workflows.JobWorkflow,
		workflows.JobWorkflowRequest{
			ClientOptions: github.ClientOptions{
				Owner: "ansg191",
				Repo:  "resume",
			},
			JobDesc: jobDesc,
		},
	)
	if err != nil {
		log.Fatalln("Unable to execute workflow", err)
	}
	log.Println("Started workflow", "WorkflowID", we.GetID(), "RunID", we.GetRunID())

	var result string
	err = we.Get(ctx, &result)
	if err != nil {
		log.Fatalln("Unable get workflow result", err)
	}
	log.Println("Workflow result:", result)
}
