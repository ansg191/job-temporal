CREATE TABLE IF NOT EXISTS job_runs (
    workflow_id VARCHAR(255) PRIMARY KEY,
    source_url  TEXT NOT NULL,
    scraped_markdown TEXT NOT NULL,
    branch_name VARCHAR(255),
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

