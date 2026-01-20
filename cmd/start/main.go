package main

import (
	"context"
	"log"
	"os"

	"go.temporal.io/sdk/client"

	"github.com/ansg191/job-temporal/internal/workflows"
)

func main() {
	c, err := client.Dial(client.Options{})
	if err != nil {
		log.Fatalln("Unable to create client", err)
	}
	defer c.Close()

	options := client.StartWorkflowOptions{
		ID:        "ai-workflow",
		TaskQueue: "my-task-queue",
	}

	log.Println("Starting workflow", os.Args[1])
	we, err := c.ExecuteWorkflow(context.Background(), options, workflows.AgentWorkflow, os.Getenv("REMOTE"), os.Args[1])
	if err != nil {
		log.Fatalln("Unable to execute workflow", err)
	}
	log.Println("Started workflow", "WorkflowID", we.GetID(), "RunID", we.GetRunID())

	var result string
	err = we.Get(context.Background(), &result)
	if err != nil {
		log.Fatalln("Unable get workflow result", err)
	}
	log.Println("Workflow result:", result)
}
