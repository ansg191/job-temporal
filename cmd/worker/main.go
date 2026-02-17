package main

import (
	"context"
	"log"
	"log/slog"
	"os"

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
	if err := activities.CheckR2ReadWrite(context.Background()); err != nil {
		log.Fatalln("Unable to verify R2 bucket read/write access", err)
	}
	slog.Info("R2 bucket read/write check succeeded")

	temporalAddress := os.Getenv("TEMPORAL_ADDRESS")
	if temporalAddress == "" {
		temporalAddress = client.DefaultHostPort
	}

	c, err := client.Dial(client.Options{
		HostPort: temporalAddress,
	})
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
	w.RegisterWorkflow(agents.BuildAndUploadPDFWorkflow)
	w.RegisterWorkflow(agents.ReviewAgent)
	w.RegisterWorkflow(agents.ReviewPDFLayoutWorkflow)
	w.RegisterActivity(activities.Greet)
	w.RegisterActivity(activities.CreateConversation)
	w.RegisterActivity(activities.CallAI)
	w.RegisterActivity(activities.ReadFile)
	w.RegisterActivity(activities.EditFile)
	w.RegisterActivity(activities.EditLine)
	w.RegisterActivity(activities.Build)
	w.RegisterActivity(activities.RenderLayoutReviewPages)
	w.RegisterActivity(activities.BuildFinalPDF)
	w.RegisterActivity(activities.UploadPDF)
	w.RegisterActivity(activities.DeletePDFByURL)
	w.RegisterActivity(activities.ListBranches)
	w.RegisterActivity(activities.CreateBranch)
	w.RegisterActivity(activities.GetBranchHeadSHA)
	w.RegisterActivity(activities.CreatePullRequest)
	w.RegisterActivity(activities.GetPullRequestBody)
	w.RegisterActivity(activities.UpdatePullRequestBody)
	w.RegisterActivity(activities.ProtectBranch)
	w.RegisterActivity(activities.ListGithubTools)
	w.RegisterActivity(activities.CallGithubTool)
	w.RegisterActivity(activities.RegisterReviewReadyPR)
	w.RegisterActivity(activities.FinishReview)
	w.RegisterActivity(activities.CreateJobRun)
	w.RegisterActivity(activities.UpdateJobRunBranch)
	w.RegisterActivity(activities.GetAgentConfig)

	err = w.Run(worker.InterruptCh())
	if err != nil {
		log.Fatalln("Unable to start worker", err)
	}
}
