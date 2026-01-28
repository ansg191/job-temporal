package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"go.temporal.io/sdk/client"

	"github.com/ansg191/job-temporal/internal/github"
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

	log.Println("Starting workflow", os.Args[1])
	we, err := c.ExecuteWorkflow(
		ctx,
		options,
		workflows.JobWorkflow,
		workflows.JobWorkflowRequest{
			ClientOptions: github.ClientOptions{
				Owner: "ansg191",
				Repo:  "resume",
			},
			JobDesc: os.Args[1],
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
