# AGENTS.md — job-temporal

Go service that automates resume/cover-letter tailoring via Temporal workflows,
GitHub webhooks, and LLM agents (OpenAI + Anthropic). Four binaries: `server`
(webhook receiver), `trigger-server` (UI), `worker` (Temporal worker), `start` (CLI).

## Build & Run

```bash
# Build all binaries
just build                           # → bin/{server,trigger-server,worker,start}

# Run locally (reads .env via Justfile dotenv-load)
just run-server
just run-worker
just run-trigger-server

# Full stack (Postgres, Temporal, Traefik, app services)
just dev                             # docker compose with hot-reload
just up                              # production compose
```

## Testing

```bash
just test                            # go test ./...
just test-v                          # verbose
just test-cover                      # with coverage report

# Single package
just test-pkg ./internal/webhook/...

# Single test by name (searches all packages)
just test-run TestProcessPushExtractsBranch

# Direct go test (equivalent)
go test -v -run TestProcessPushExtractsBranch ./internal/webhook/
go test -v ./internal/llm/...
```

No build tags are used. No testcontainers. Tests are pure unit tests using
stdlib `testing` — no external test framework. Some tests rely on fixtures in
`internal/builder/fixtures/`.

## Lint & Format

```bash
just check                           # fmt-check + vet + lint
just lint                            # golangci-lint run
just fmt                             # gofmt -w .
just vet                             # go vet ./...
```

**golangci-lint config** (`.golangci.yml`): version 2, only customization is
`errcheck` disabled. All other default linters are active.

CI runs: `go test`, `gofmt -l .` (must be clean), `golangci-lint-action@v7`.

## Project Layout

```
cmd/
  server/           Webhook receiver (port 8080)
  trigger-server/   Web UI for triggering jobs (port 8090)
  worker/           Temporal worker — registers all workflows & activities
  start/            CLI to start a job workflow
internal/
  activities/       Temporal activities (LLM calls, GitHub, file I/O, PDF, S3)
  builder/          Resume/cover-letter document builder (Typst)
  config/           Agent YAML config loader
  database/         Postgres interface + migrations
  git/              Git operations
  github/           GitHub App client + MCP tool bridge
  jobsource/        Job description scraping (LinkedIn, file)
  llm/              LLM abstraction (OpenAI, Anthropic backends)
  tools/            Tool execution interface for agent loops
  webhook/          GitHub webhook handling + Temporal signal dispatch
  workflows/        Temporal workflow definitions
    agents/         Individual agent workflows (branch naming, PR, review, build)
config/agents/      YAML configs for each agent (instructions, model, temperature)
deploy/             Dockerfiles (prod + dev) for each service
```

## Code Style

### Imports

Three groups separated by blank lines: stdlib, third-party, internal.
No import aliases except for side-effect imports (`_ "github.com/lib/pq"`).

```go
import (
    "context"
    "fmt"

    "go.temporal.io/sdk/workflow"

    "github.com/ansg191/job-temporal/internal/activities"
)
```

### Naming

- **Packages**: lowercase, single word (`llm`, `config`, `webhook`)
- **Exported types/funcs**: PascalCase (`AgentConfig`, `LoadAgentConfig`)
- **Unexported**: camelCase (`getConfigDir`, `parseArgs`)
- **Constants**: PascalCase for exported (`RoleSystem`, `ContentTypeText`),
  camelCase for unexported (`githubToolMaxAttempts`)
- **Constructors**: `NewXxx` returning interface or pointer (`NewBackend`, `NewHandler`, `NewPostgresDatabase`)

### Structs & Interfaces

- Struct fields use `json:"..."` and/or `yaml:"..."` tags.
- Interfaces are defined in the package that *uses* them or alongside
  implementations. Named as nouns or verb-er (`Backend`, `Database`, `WorkflowIDResolver`).
- Unexported implementation structs (`postgresDatabase`, `openAIBackend`) implement exported interfaces.
- Constructor returns the interface type: `func NewPostgresDatabase() (Database, error)`.

### Error Handling

- Wrap with `fmt.Errorf("context: %w", err)` for propagation.
- Sentinel errors as package-level vars: `var ErrNotFound = errors.New("not found")`.
- Custom error types with `Is`/`As` helpers:
  ```go
  type ConfigError struct { msg string }
  func IsConfigError(err error) bool { ... errors.As(...) }
  ```
- Temporal errors for non-retryable failures:
  ```go
  temporal.NewNonRetryableApplicationError("msg", "ErrorType", err)
  ```
- Provider errors are classified and wrapped with retry hints
  (`ClassifyAnthropicError`, `ClassifyProviderError`).
- `errcheck` linter is disabled — unchecked errors are acceptable for
  fire-and-forget calls (e.g., `_, _ = w.Write(...)`).

### Logging

`log/slog` for structured logging in HTTP handlers and startup.
`workflow.GetLogger(ctx)` inside Temporal workflows. Plain `log` package used
in some activities for quick debug output.

### Testing Conventions

- Always `t.Parallel()` at the top of each test function.
- Use stdlib `testing` — no testify assertions, no gomock.
- Assertions use `t.Fatalf` / `t.Errorf` with descriptive messages.
- Test helpers are local to the test file (e.g., `signWebhookPayload`).
- Table-driven tests where multiple cases exist.
- Test files are in the same package (white-box testing):
  `package webhook` not `package webhook_test`.
- Fixtures go in a `fixtures/` or `testdata/` subdirectory.

### Temporal Patterns

- Workflows are plain functions: `func XxxWorkflow(ctx workflow.Context, req XxxRequest) (Result, error)`.
- Activities are plain functions: `func Xxx(ctx context.Context, ...) (Result, error)`.
- All workflows and activities are registered in `cmd/worker/main.go`.
- Child workflows use `agents.MakeChildWorkflowID(ctx, ...)` for deterministic IDs.
- Activity options set `StartToCloseTimeout`; retry policies use Temporal defaults
  unless overridden via `temporal.NewNonRetryableApplicationError`.

### Dependencies & Configuration

- Config loaded from env vars (`os.Getenv`) and YAML files (`config/agents/*.yaml`).
- Database URL from `DATABASE_URL` env var. Migrations run at startup via
  `database.EnsureMigrations()`.
- GitHub App auth via private key PEM file path in `GITHUB_APP_PRIVATE_KEY`.
- LLM keys: `OPENAI_API_KEY`, `ANTHROPIC_API_KEY`.

### General Rules

- No `//go:build` tags — all code compiles unconditionally.
- Formatting: `gofmt` only (no goimports, gofumpt, or custom formatters).
- Keep functions short; prefer early returns for error handling.
- Dependency injection via constructor params, not globals (except shared
  singleton clients in `github` package via `SharedTools`/`SharedCallTool`).
- SQL uses `database/sql` with `lib/pq` driver — no ORM, no query builder.
  Parameterized queries with `$1, $2` positional placeholders.
