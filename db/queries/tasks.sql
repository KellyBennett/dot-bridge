-- name: InsertApproval :exec
INSERT INTO approvals (approval_id, principal_id, environment, project_id, envelope_digest, frozen_review, expires_at)
VALUES (?, ?, ?, ?, ?, ?, ?);

-- name: FindApproval :one
SELECT envelope_digest, expires_at, consumed_run_id FROM approvals
WHERE approval_id = ? AND principal_id = ? AND environment = ? AND project_id = ?;

-- name: ConsumeApproval :execrows
UPDATE approvals SET consumed_run_id = ?
WHERE approval_id = ? AND consumed_run_id IS NULL;

-- name: FindSubmission :one
SELECT payload, input_digest, approval_id FROM task_runs
WHERE principal_id = ? AND environment = ? AND project_id = ? AND operation = 'submit_task' AND idempotency_key = ?;

-- name: FindRun :one
SELECT payload, approval_id FROM task_runs
WHERE principal_id = ? AND environment = ? AND project_id = ? AND run_id = ?;

-- name: CountPendingRuns :one
SELECT COUNT(*) FROM task_runs WHERE project_id = ? AND environment = ?;

-- name: InsertTaskRun :exec
INSERT INTO task_runs (run_id, principal_id, environment, project_id, operation, idempotency_key, input_digest, approval_id, dispatch_token, payload)
VALUES (?, ?, ?, ?, 'submit_task', ?, ?, ?, ?, ?);
