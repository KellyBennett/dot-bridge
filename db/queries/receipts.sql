-- name: InsertReceipt :one
INSERT INTO receipts (receipt_id, payload)
VALUES (?, ?)
RETURNING sequence;

-- name: FinalizeReceipt :execrows
UPDATE receipts SET payload = ? WHERE sequence = ?;

-- name: GetReceipt :one
SELECT payload FROM receipts WHERE receipt_id = ?;
