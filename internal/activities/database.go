package activities

import (
	"context"

	"github.com/ansg191/job-temporal/internal/database"
)

func RegisterReviewReadyPR(ctx context.Context, id string, pr int) error {
	db, err := database.NewPostgresDatabase()
	if err != nil {
		return err
	}
	defer db.Close()

	return db.RegisterReviewReadyPR(ctx, id, pr)
}

func FinishReview(ctx context.Context, pr int) error {
	db, err := database.NewPostgresDatabase()
	if err != nil {
		return err
	}
	defer db.Close()

	return db.FinishPR(ctx, pr)
}
