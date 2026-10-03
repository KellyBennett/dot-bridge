# dot-bridge

Offline synthetic prototype of the controlled bridge in [SPECIFICATION.md](SPECIFICATION.md).

The Go core implements `read_draft` against host-supplied in-memory synthetic
fixtures, plus optional `submit_task` acceptance and `read_run` acceptance lookup.
Task support requires host-supplied `Config.Tasks` and a transactional `Journal`.
Without it, both task operations return `OPERATION_NOT_IMPLEMENTED`.
`write_draft`, `cancel_run` and `read_pr` remain unimplemented and record denial
receipts. No adapter, shell, network transport, account login, provider inference,
PR access or filesystem draft-write operation is present.

## Run the checks

Go 1.23+ with SQLite, Goose migrations and sqlc queries. SQLite uses the
`modernc.org/sqlite` driver, which does not require a C toolchain for ordinary
builds. The race detector requires a supported C toolchain. From the repository
root:

```sh
go test -race ./...
go vet ./...
```

Tests use real temporary SQLite journals and synthetic fixture data, with an
injected clock. Go downloads pinned module dependencies on the first build.
There is no Python runtime, pgx dependency or database server in the application.

`db/migrations` contains embedded Goose migrations. Opening a host-configured
journal applies pending embedded migrations; callers cannot supply migration
SQL or paths. `db/queries` is the source for sqlc-generated code in
`internal/store`. Regenerate with `make generate` (pinned sqlc v1.29.0). Generated
code is committed, so normal builds do not require sqlc. CI verifies regeneration,
vet and the race-enabled test suite. Goose is used as a library; no CLI is needed.

## Columbo CI

The separate **Columbo** check follows the latest `main` of
[KellyBennett/Columbo](https://github.com/KellyBennett/Columbo) on every CI run.
It reads the Go version from Columbo's `go.mod`, logs the resolved tool commit,
builds the CLI on Linux amd64, and runs `columbo ./...` with default thresholds
and FAIL severities, including tests. Full bridge history is available, and
Columbo's own source is checked out separately from the analyzed module.
Findings and investigation errors fail the job; no thresholds or suppressions
are added to make it green.

Columbo is public, so checkout uses GitHub Actions' default read-only token.
No Columbo-specific secret is required, and checkout credentials are not
persisted in Git configuration. This is intentional product dogfooding:
rerunning bridge CI picks up the current Columbo `main` without a dependency
update in this repository.

## Core boundaries

The core separates wire/schema validation, grant policy, draft lookup/export,
receipt decisions and database transactions into their own components. The
dispatcher coordinates them and returns a response only after the journal
commits. Test fixtures separate public-response assertions from database probes;
concurrent reads and Goose migration lifecycles still exercise real SQLite.

- `Identity` is trusted host context, supplied separately from a public request.
  This is not transport authentication. A future transport must verify identity
  before constructing it; there is deliberately no network listener today.
- `Grant` and fixture registry are host configuration. The prototype accepts
  only the personal environment and registered ASCII document IDs.
- `Broker.Dispatch` rejects unknown fields, invalid types, unregistered IDs,
  stale revisions, expired/disabled grants and unsupported actions.
- Export checking defaults to deny. A host may explicitly allow **synthetic**
  fixture text for tests; the implementation is not a production redactor.
- SQLite records allowed and denied receipts transactionally through generated
  queries, with sequence,
  input/output digests, identity and policy metadata. Fixture text and exception
  details are not retained in receipts. No read response is returned if journal
  commit fails; `ErrAuditUnavailable` means no bridge receipt is available.
- All returned responses, receipts and fixture data carry `simulated: true`.
  Visible summaries begin `SIMULATED — NO REAL AGENT EXECUTION`.
- Returned text remains untrusted data. Consumers must render it safely without
  interpreting embedded instructions or automatically fetching links.

Receipt input digests hash exact incoming JSON bytes, including whitespace.
Document revisions hash exact UTF-8 text bytes. Task envelopes and submission
identity use a separate versioned canonical format described below.
Read requests are capped at 8 KiB and reject duplicate
JSON keys, trailing values, null fields, excessive nesting and invalid UTF-8.

## Frozen approvals and task acceptance

The host registers one synthetic workspace snapshot, the `stub-v1` profile
revision and export-policy revision in `TaskConfig`. These fixtures and the
grant/document collections are copied when a broker is constructed; rebuild it
to change host policy. Nothing inspects or executes a real workspace.

The trusted host calls `Broker.ReviewTask` with an envelope. The returned
`ApprovalReview` contains the exact canonical envelope, its digest, the principal
and frozen spec text for local review. A host approval interface must display
that entire review as untrusted text, then call `Broker.IssueApproval` only after
the owner approves it. These are host-only library methods, not public operations.
This slice supplies that interface contract; it does not supply a screen, CLI,
transport authentication or remote approval mechanism. Never expose either method
as a dot-callable tool. Conversational approval cannot issue a bridge approval.

The envelope fields follow the candidate specification, with an additional
required `canonical_version: "1"`. Every field is required, including empty
`specs` and `allowed_effects` arrays when applicable. Only personal, simulated
`stub-v1` envelopes are accepted. Specs identify registered document revisions;
their exact contents are frozen in the host store. Prompt bytes are limited to
32 KiB. At most 16 specs and 16 acceptance criteria are supported, with each
criterion limited to 1 KiB. Allowed effects are the fixed vocabulary
`edit_workspace` and `run_approved_verification`; neither causes execution here.

Canonical v1 is compact Go `encoding/json` serialization in the declared
`TaskEnvelope` field order, including its standard HTML/string escaping and
without a trailing newline. Arrays keep their order; strings receive no trimming
or Unicode normalization. The prompt digest hashes decoded UTF-8 prompt bytes.
The envelope digest hashes this canonical serialization. A submission digest
hashes the canonical envelope, approval ID and idempotency key, excluding the
outer request ID and JSON formatting. Receipt input digests still hash exact
incoming bytes. A future change to canonical rules needs a new version.

An approval binds the principal/project/environment to the exact envelope,
including workspace, specs, policy, profile, effects and export revisions.
Issuance records the approval and its attributable receipt atomically.
Approvals expire 30 minutes after issuance and are single-use.

`submit_task` arguments are `envelope`, `approval_id` and `idempotency_key`
(the latter two are lowercase UUIDs). Submission requests have a 256 KiB encoded
JSON cap to accommodate escaped 32 KiB prompts. Under one SQLite transaction, the
broker checks replay first, validates current authority for a new submission,
consumes approval, stores the run and dispatch intent, and finalizes the receipt.
Same scoped key and input returns the original run; changed input returns
`IDEMPOTENCY_CONFLICT`. Replays survive approval expiry, host revision changes
and restart, but still require a live matching grant. Failed transactions leave
no consumed approval, run or acceptance receipt.

Up to five runs may be pending per project/environment. All remain `accepted`
at state version 1 with a recorded ten-minute start deadline; no worker launches
them or advances their state. Acceptance never means execution or verification.
The next adapter slice must enforce the deadline, recheck authority before launch
and preserve the existing dispatch token for reconciliation. Submission keys and
approval-consumption tombstones are retained without an automatic deletion path.

`read_run` accepts exactly one of `run_id` or `idempotency_key`, scoped to the
current principal/project/environment. It exports only labelled acceptance
metadata after the host export check and durable read receipt succeed. No prompt,
spec text, dispatch token, events or result artifacts are exported. Event cursors,
result manifests, poll-rate limits and cancellation belong to later slices.

## Status and next slice

This is an initial stage-2 offline core slice, not a completed stage-2 gate or
a dogfood release. The specification remains the target; its stage-0 decisions
remain open, and no connectivity or real-execution gate has been passed.

Next: a durable deterministic stub, sequenced events and run recovery using the
persisted dispatch intent, followed by cancellation. Subsequent slices add
draft-store filesystem protections, synthetic PR pagination, export/quota/rate
controls and the full
scenario matrix before transport is considered.

Current limitations include no execution or run-state transitions, live policy
reload, rate limiting, full quotas, input transport framing,
secret scanning, display sanitization, protected filesystem draft store or host
approval UI. SQLite journal directory ownership, backup and corruption recovery
are not enforced here; use only synthetic temporary data. Task acceptance keeps
its effects and audit record in one transaction. Future filesystem or agent
effects need the same atomicity or an explicitly reconciled intent protocol,
not a separate receipt call after an effect.
