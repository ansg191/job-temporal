DROP INDEX IF EXISTS workflows_active_branch_lookup_idx;
DROP INDEX IF EXISTS workflows_active_pr_lookup_idx;

-- Legacy schema requires global uniqueness on pull_request_id.
-- Collapse duplicates deterministically before restoring that constraint,
-- preferring unfinished rows.
WITH ranked AS (
    SELECT
        ctid,
        ROW_NUMBER() OVER (
            PARTITION BY pull_request_id
            ORDER BY
                CASE WHEN finished THEN 1 ELSE 0 END,
                workflow_id
        ) AS rn
    FROM workflows
)
DELETE FROM workflows w
USING ranked r
WHERE w.ctid = r.ctid
  AND r.rn > 1;

ALTER TABLE workflows
    DROP COLUMN IF EXISTS branch_name,
    DROP COLUMN IF EXISTS repo,
    DROP COLUMN IF EXISTS owner;

ALTER TABLE workflows
    ADD CONSTRAINT workflows_pull_request_id_key UNIQUE (pull_request_id);
