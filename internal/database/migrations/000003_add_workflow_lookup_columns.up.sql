ALTER TABLE workflows
    ADD COLUMN IF NOT EXISTS owner VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS repo VARCHAR(255) NOT NULL DEFAULT '',
    ADD COLUMN IF NOT EXISTS branch_name VARCHAR(255) NOT NULL DEFAULT '';

ALTER TABLE workflows
    DROP CONSTRAINT IF EXISTS workflows_pull_request_id_key;

CREATE UNIQUE INDEX IF NOT EXISTS workflows_active_pr_lookup_idx
    ON workflows (owner, repo, pull_request_id)
    WHERE finished = false;

CREATE UNIQUE INDEX IF NOT EXISTS workflows_active_branch_lookup_idx
    ON workflows (owner, repo, branch_name)
    WHERE finished = false AND branch_name <> '';
