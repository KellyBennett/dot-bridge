-- +goose Up
CREATE TABLE receipts (
    sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    receipt_id TEXT NOT NULL UNIQUE,
    payload TEXT NOT NULL CHECK (json_valid(payload)),
    CHECK (json_type(payload) = 'object'),
    CHECK (json_type(payload, '$.simulated') IS 'true'),
    CHECK (json_extract(payload, '$.receipt_id') IS receipt_id)
);

-- +goose Down
DROP TABLE receipts;
