package main

import (
	"bytes"
	"context"
	"embed"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/yuin/goldmark"
	"go.temporal.io/sdk/client"

	"github.com/ansg191/job-temporal/internal/github"
	"github.com/ansg191/job-temporal/internal/jobsource"
	"github.com/ansg191/job-temporal/internal/workflows"
)

//go:embed templates/index.html static/styles.css
var uiFS embed.FS

type pageData struct {
	Repo             string
	JobURL           string
	Error            string
	Success          string
	Markdown         string
	RenderedMarkdown template.HTML
}

type app struct {
	tpl      *template.Template
	tc       client.Client
	resolver *jobsource.Resolver
	md       goldmark.Markdown
}

func main() {
	var (
		tc  client.Client
		err error
	)
	temporalAddress := os.Getenv("TEMPORAL_ADDRESS")
	if temporalAddress == "" {
		temporalAddress = client.DefaultHostPort
	}

	tc, err = client.Dial(client.Options{HostPort: temporalAddress})
	if err != nil {
		log.Fatalln("Unable to create temporal client", err)
	}
	defer tc.Close()

	tpl, err := template.ParseFS(uiFS, "templates/index.html")
	if err != nil {
		log.Fatalln("Unable to parse page template", err)
	}
	staticFS, err := fs.Sub(uiFS, "static")
	if err != nil {
		log.Fatalln("Unable to load static assets", err)
	}

	app := &app{
		tpl:      tpl,
		tc:       tc,
		resolver: jobsource.NewDefaultResolver(),
		md:       goldmark.New(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.handleForm)
	mux.HandleFunc("/submit", app.handleSubmit)
	mux.HandleFunc("/health", healthHandler)
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	addr := ":8090"
	log.Println("Starting trigger server on", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalln("Trigger server failed", err)
	}
}

func (a *app) handleForm(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	a.render(w, pageData{
		Repo: "ansg191/resume",
	})
}

func (a *app) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := r.ParseForm(); err != nil {
		a.render(w, pageData{Error: fmt.Sprintf("invalid form body: %v", err)})
		return
	}

	repoInput := strings.TrimSpace(r.FormValue("repo"))
	jobURL := strings.TrimSpace(r.FormValue("jobUrl"))
	data := pageData{Repo: repoInput, JobURL: jobURL}

	owner, repo, err := parseRepo(repoInput)
	if err != nil {
		data.Error = err.Error()
		a.render(w, data)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()

	jobDesc, err := a.resolver.Resolve(ctx, jobURL)
	if err != nil {
		data.Error = fmt.Sprintf("unable to resolve job description: %v", err)
		a.render(w, data)
		return
	}
	data.Markdown = jobDesc

	var rendered bytes.Buffer
	if err := a.md.Convert([]byte(jobDesc), &rendered); err != nil {
		data.Error = fmt.Sprintf("unable to render markdown: %v", err)
		a.render(w, data)
		return
	}
	data.RenderedMarkdown = template.HTML(rendered.String())

	we, err := a.tc.ExecuteWorkflow(
		ctx,
		client.StartWorkflowOptions{TaskQueue: "my-task-queue"},
		workflows.JobWorkflow,
		workflows.JobWorkflowRequest{
			ClientOptions: github.ClientOptions{Owner: owner, Repo: repo},
			JobDesc:       jobDesc,
			SourceURL:     jobURL,
		},
	)
	if err != nil {
		data.Error = fmt.Sprintf("unable to execute workflow: %v", err)
		a.render(w, data)
		return
	}

	data.Success = fmt.Sprintf("Workflow started. WorkflowID=%s RunID=%s", we.GetID(), we.GetRunID())
	a.render(w, data)
}

func (a *app) render(w http.ResponseWriter, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := a.tpl.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("template render error: %v", err), http.StatusInternalServerError)
	}
}

func healthHandler(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

func parseRepo(input string) (string, string, error) {
	trimmed := strings.TrimSpace(input)
	if trimmed == "" {
		return "", "", errors.New("repo is required and must be in owner/repo format")
	}

	parts := strings.Split(trimmed, "/")
	if len(parts) != 2 {
		return "", "", errors.New("repo must be in owner/repo format")
	}

	owner := strings.TrimSpace(parts[0])
	repo := strings.TrimSpace(parts[1])
	if owner == "" || repo == "" {
		return "", "", errors.New("repo must be in owner/repo format")
	}

	return owner, repo, nil
}
