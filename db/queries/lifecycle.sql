-- name: InsertLifecycle :exec
INSERT INTO run_lifecycle(run_id,principal_id,environment,project_id,phase,state,state_version,payload)
VALUES (?, ?, ?, ?, 'queued','accepted',1,?);

-- name: WorkerCandidate :one
SELECT l.*, t.dispatch_token, a.frozen_review FROM run_lifecycle l
JOIN task_runs t USING(run_id) JOIN approvals a USING(approval_id)
WHERE l.principal_id=? AND l.environment=? AND l.project_id=? AND l.phase!='terminal'
ORDER BY CASE l.phase WHEN 'active' THEN 0 ELSE 1 END, t.rowid LIMIT 1;

-- name: GetLifecycle :one
SELECT * FROM run_lifecycle WHERE run_id=?;

-- name: UpdateLifecycle :execrows
UPDATE run_lifecycle SET phase=?,state=?,state_version=?,payload=?,adapter_sequence=?,event_sequence=?,lease_owner=?,lease_until=?
WHERE run_id=? AND state_version=? AND lease_owner=?;

-- name: InsertRunEvent :exec
INSERT INTO run_events(run_id,sequence,payload) VALUES(?,?,?);

-- name: ListRunEvents :many
SELECT payload FROM run_events WHERE run_id=? AND sequence>? ORDER BY sequence LIMIT 101;

-- name: InsertRunResult :exec
INSERT INTO run_results(run_id,payload) VALUES(?,?);

-- name: GetRunResult :one
SELECT payload FROM run_results WHERE run_id=?;

-- name: FindStubDispatch :one
SELECT * FROM stub_dispatches WHERE dispatch_token=?;

-- name: InsertStubDispatch :exec
INSERT INTO stub_dispatches(dispatch_token,envelope_digest,profile_revision,started_at,scenario,frozen_task)
VALUES(?,?,?,?,?,?) ON CONFLICT(dispatch_token) DO NOTHING;

-- name: FindRunCursor :one
SELECT after_sequence FROM run_cursors WHERE cursor_id=? AND run_id=? AND principal_id=? AND environment=? AND project_id=?;

-- name: GetRunCursor :one
SELECT cursor_id FROM run_cursors WHERE run_id=? AND principal_id=? AND environment=? AND project_id=? AND after_sequence=?;

-- name: InsertRunCursor :exec
INSERT INTO run_cursors(cursor_id,run_id,principal_id,environment,project_id,after_sequence) VALUES(?,?,?,?,?,?);

-- name: GetPollWindow :one
SELECT started_at,last_at,polls FROM run_poll_windows WHERE principal_id=? AND environment=? AND project_id=?;

-- name: SetPollWindow :exec
INSERT INTO run_poll_windows(principal_id,environment,project_id,started_at,last_at,polls) VALUES(?,?,?,?,?,?)
ON CONFLICT(principal_id,environment,project_id) DO UPDATE SET started_at=excluded.started_at,last_at=excluded.last_at,polls=excluded.polls;
