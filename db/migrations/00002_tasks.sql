-- +goose Up
CREATE TABLE approvals (
    approval_id TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL,
    environment TEXT NOT NULL CHECK (environment = 'personal'),
    project_id TEXT NOT NULL,
    envelope_digest TEXT NOT NULL,
    frozen_review TEXT NOT NULL CHECK (json_valid(frozen_review)),
    expires_at TEXT NOT NULL,
    consumed_run_id TEXT,
    CHECK (json_extract(frozen_review, '$.envelope_digest') IS envelope_digest),
    CHECK (json_type(frozen_review, '$.simulated') IS 'true')
);

CREATE TABLE task_runs (
    run_id TEXT PRIMARY KEY,
    principal_id TEXT NOT NULL,
    environment TEXT NOT NULL CHECK (environment = 'personal'),
    project_id TEXT NOT NULL,
    operation TEXT NOT NULL CHECK (operation = 'submit_task'),
    idempotency_key TEXT NOT NULL,
    input_digest TEXT NOT NULL,
    approval_id TEXT NOT NULL UNIQUE REFERENCES approvals(approval_id),
    dispatch_token TEXT NOT NULL UNIQUE,
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    UNIQUE (principal_id, environment, project_id, operation, idempotency_key),
    CHECK (substr(run_id, 1, 4) = 'sim_'),
    CHECK (json_extract(payload, '$.run_id') IS run_id),
    CHECK (json_type(payload, '$.simulated') IS 'true'),
    CHECK (json_extract(payload, '$.state') IS 'accepted')
);

-- +goose Down
DROP TABLE task_runs;
DROP TABLE approvals;
