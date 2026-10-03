-- +goose Up
CREATE TABLE run_lifecycle (
    run_id TEXT PRIMARY KEY REFERENCES task_runs(run_id),
    principal_id TEXT NOT NULL,
    environment TEXT NOT NULL CHECK (environment = 'personal'),
    project_id TEXT NOT NULL,
    phase TEXT NOT NULL CHECK (phase IN ('queued','active','terminal')),
    state TEXT NOT NULL CHECK (state IN ('accepted','running','completed','failed','uncertain')),
    state_version INTEGER NOT NULL CHECK (state_version > 0),
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    adapter_sequence INTEGER NOT NULL DEFAULT 0,
    event_sequence INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_until TEXT NOT NULL DEFAULT '',
    CHECK (json_extract(payload, '$.run_id') IS run_id),
    CHECK (json_extract(payload, '$.state') IS state),
    CHECK (json_extract(payload, '$.state_version') IS state_version),
    CHECK (json_type(payload, '$.simulated') IS 'true'),
    CHECK ((phase = 'terminal') = (state IN ('completed','failed')))
);
INSERT INTO run_lifecycle(run_id,principal_id,environment,project_id,phase,state,state_version,payload)
SELECT run_id,principal_id,environment,project_id,'queued','accepted',1,payload FROM task_runs;
CREATE UNIQUE INDEX one_active_run ON run_lifecycle(project_id,environment) WHERE phase='active';

CREATE TABLE run_events (
    run_id TEXT NOT NULL REFERENCES run_lifecycle(run_id),
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    PRIMARY KEY(run_id,sequence),
    CHECK (json_extract(payload, '$.sequence') IS sequence),
    CHECK (json_type(payload, '$.simulated') IS 'true')
);
CREATE TABLE run_results (
    run_id TEXT PRIMARY KEY REFERENCES run_lifecycle(run_id),
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    CHECK (json_type(payload, '$.manifest.simulated') IS 'true')
);
CREATE TABLE stub_dispatches (
    dispatch_token TEXT PRIMARY KEY,
    envelope_digest TEXT NOT NULL,
    profile_revision TEXT NOT NULL,
    started_at TEXT NOT NULL,
    scenario TEXT NOT NULL CHECK (scenario IN ('success','verification_failure','lost_ack','unavailable','loss_after_start','malformed_events','missing_manifest')),
    frozen_task TEXT NOT NULL CHECK (json_valid(frozen_task)),
    CHECK (json_type(frozen_task, '$.envelope.simulated') IS 'true')
);
CREATE TABLE run_cursors (
    cursor_id TEXT PRIMARY KEY,
    run_id TEXT NOT NULL REFERENCES run_lifecycle(run_id),
    principal_id TEXT NOT NULL,
    environment TEXT NOT NULL,
    project_id TEXT NOT NULL,
    after_sequence INTEGER NOT NULL,
    UNIQUE(run_id,principal_id,environment,project_id,after_sequence)
);
CREATE TABLE run_poll_windows (
    principal_id TEXT NOT NULL,
    environment TEXT NOT NULL,
    project_id TEXT NOT NULL,
    started_at TEXT NOT NULL,
    last_at TEXT NOT NULL,
    polls INTEGER NOT NULL CHECK (polls BETWEEN 1 AND 2),
    PRIMARY KEY(principal_id,environment,project_id)
);
-- +goose Down
DROP TABLE run_poll_windows;
DROP TABLE run_cursors;
DROP TABLE stub_dispatches;
DROP TABLE run_results;
DROP TABLE run_events;
DROP TABLE run_lifecycle;
