package main

import (
	"log"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/workflows"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	c, err := client.Dial(client.Options{})
	if err != nil {
		log.Fatalln("Unable to create client", err)
	}
	defer c.Close()

	w := worker.New(c, "my-task-queue", worker.Options{})

	w.RegisterWorkflow(workflows.SayHelloWorkflow)
	w.RegisterActivity(activities.Greet)
	w.RegisterWorkflow(workflows.AgentWorkflow)
	w.RegisterActivity(activities.CallAI)
	w.RegisterActivity(activities.ReadFile)
	w.RegisterActivity(activities.EditFile)

	err = w.Run(worker.InterruptCh())
	if err != nil {
		log.Fatalln("Unable to start worker", err)
	}
}
