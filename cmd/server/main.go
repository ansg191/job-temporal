package main

import (
	"log"
	"log/slog"
	"net/http"
	"os"

	"go.temporal.io/sdk/client"

	"github.com/ansg191/job-temporal/internal/database"
	"github.com/ansg191/job-temporal/internal/webhook"
)

func main() {
	if err := database.EnsureMigrations(); err != nil {
		log.Fatalln("Unable to ensure database migrations:", err)
	}

	temporalAddress := os.Getenv("TEMPORAL_ADDRESS")
	if temporalAddress == "" {
		temporalAddress = client.DefaultHostPort
	}

	tc, err := client.Dial(client.Options{
		HostPort: temporalAddress,
	})
	if err != nil {
		log.Fatalf("failed to create temporal client: %v", err)
	}
	defer tc.Close()

	webhookSecret := os.Getenv("GITHUB_WEBHOOK_SECRET")
	if webhookSecret == "" {
		log.Fatal("GITHUB_WEBHOOK_SECRET environment variable is required")
	}

	db, err := database.NewPostgresDatabase()
	if err != nil {
		log.Fatalf("failed to create database client: %v", err)
	}
	defer db.Close()

	handler := webhook.NewHandler(tc, db, webhookSecret)

	mux := http.NewServeMux()
	mux.Handle("/webhook", handler)
	mux.HandleFunc("/health", healthHandler)

	slog.Info("starting server", "addr", ":8080")
	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}
