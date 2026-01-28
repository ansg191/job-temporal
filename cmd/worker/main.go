package main

import (
	"log"

	"github.com/ansg191/job-temporal/internal/activities"
	"github.com/ansg191/job-temporal/internal/database"
	"github.com/ansg191/job-temporal/internal/workflows"
	"github.com/ansg191/job-temporal/internal/workflows/agents"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/worker"
)

func main() {
	if err := database.EnsureMigrations(); err != nil {
		log.Fatalln("Unable to ensure database migrations", err)
	}

	c, err := client.Dial(client.Options{})
	if err != nil {
		log.Fatalln("Unable to create client", err)
	}
	defer c.Close()

	w := worker.New(c, "my-task-queue", worker.Options{})

	w.RegisterWorkflow(workflows.JobWorkflow)
	w.RegisterWorkflow(workflows.BuilderWorkflow)
	w.RegisterWorkflow(agents.BranchNameAgent)
	w.RegisterWorkflow(agents.BuilderAgent)
	w.RegisterWorkflow(agents.PullRequestAgent)
	w.RegisterWorkflow(agents.ReviewAgent)
	w.RegisterActivity(activities.Greet)
	w.RegisterActivity(activities.CallAI)
	w.RegisterActivity(activities.ReadFile)
	w.RegisterActivity(activities.EditFile)
	w.RegisterActivity(activities.EditLine)
	w.RegisterActivity(activities.Build)
	w.RegisterActivity(activities.ListBranches)
	w.RegisterActivity(activities.CreateBranch)
	w.RegisterActivity(activities.CreatePullRequest)
	w.RegisterActivity(activities.ListGithubTools)
	w.RegisterActivity(activities.CallGithubTool)
	w.RegisterActivity(activities.RegisterReviewReadyPR)
	w.RegisterActivity(activities.FinishReview)

	err = w.Run(worker.InterruptCh())
	if err != nil {
		log.Fatalln("Unable to start worker", err)
	}
}
