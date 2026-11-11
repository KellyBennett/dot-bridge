# Read a draft

A dot caller reads a registered synthetic document and gets its exact text, its content revision, and an untrusted-data provenance label. A durable receipt commits before the response is returned.

## Sub-features

- `draft-read` returns `Synthetic design` with revision `sha256:a0a5e56d…e198` and effect `draft_read`.
- `draft-pin` with a matching `expected_revision` succeeds. A mismatched one is denied with `REVISION_MISMATCH`.
- `draft-unregistered` denies an unknown `document_id` with `DENIED`.
- `draft-schema` denies unknown fields, null fields and duplicate keys with `INVALID_ARGUMENT`.
- `draft-receipt` records the receipt's `input_digest` as the SHA-256 of the exact request bytes.
- `draft-export-deny` denies the read when the host has no export check (`--deny-exports`).

## How to get to it (user POV)

- Dot caller: `Dispatch` with `operation: "read_draft"` and `arguments.document_id`.

## Driving it with bridgehost

Preconditions:

- Baseline run. Use a `--deny-exports` run only for `draft-export-deny`.

- **Read.** Write `{"contract_version":"1","request_id":"55555555-5555-4555-8555-555555555555","operation":"read_draft","project_id":"personal-demo","arguments":{"document_id":"design"}}` to `work/read.json`. Run `$S/bh.sh dispatch $BRIDGE_RUN/work/read.json`. Expect `.receipt.decision == "allowed"`, `.receipt.effect == "draft_read"`, `.data.text == "Synthetic design"`, `.data.revision` starting `sha256:a0a5e56d`, and `.simulated == true`.
- **Pin.** Add `"expected_revision"` set to that revision. The read is allowed. Change the revision to 64 zeros after `sha256:`. Expect `REVISION_MISMATCH` and no `.data`.
- **Unregistered.** Use `"document_id":"other"`. Expect `DENIED`.
- **Schema.** Add `"extra":1` to `arguments`. Expect `INVALID_ARGUMENT`.
- **Exact bytes.** Pipe the request without a trailing newline: `printf '%s' '<json>' | $S/bh.sh dispatch -`. Then check that `.receipt.input_digest` equals `"sha256:" + $(printf '%s' '<json>' | shasum -a 256)`.
- **Durable proof.** Run `$S/probe.sh "SELECT sequence, json_extract(payload,'$.operation') op, json_extract(payload,'$.decision') d, json_extract(payload,'$.error_code') code FROM receipts ORDER BY sequence"`. There is one row per dispatch, in order, with matching decisions.

## Gotchas

- `stdin` that `bh.sh` captures includes whatever newline you send, and the digest covers it. Use `printf '%s'` when you compare digests.
- `read_draft` is not covered by the `read_run` poll limit. Do not assume it is limited.
- The grant expires at virtual `2026-10-04T00:00:00Z`. Any `advance` past that point turns every call into a denial.
