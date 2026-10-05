# dot-bridge

Offline synthetic prototype of the controlled bridge in [SPECIFICATION.md](SPECIFICATION.md).

The Go core implements `read_draft` against host-supplied synthetic fixtures,
`submit_task` with frozen host approvals, and `read_run` with durable lifecycle,
event pagination and selected synthetic results. Task support requires
host-supplied `Config.Tasks` and a transactional `Journal`; without them, task
operations return `OPERATION_NOT_IMPLEMENTED`.

A deterministic `stub-v1` adapter and host-driven worker advance scripted runs.
`write_draft`, `cancel_run` and `read_pr` remain unimplemented and record denial
receipts. There is no shell, network transport, provider inference, account login,
PR access or filesystem draft-write operation.

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

Up to five runs may be queued per project/environment. Acceptance remains an
immutable outcome at state version 1, with a ten-minute start deadline; replay
returns that original outcome even after execution advances. Submission keys and
approval-consumption tombstones have no automatic deletion path.

## Deterministic execution and recovery

The host constructs `NewStubAdapter(journal, scenario)`, uses its capability
`ProfileRevision` in `TaskConfig` and the approved envelope, then calls
`Broker.NewWorker(adapter)`. The approved adapter must report simulated `stub-v1`,
durable dispatch lookup, structured events and no tool-approval or cancellation
capability. `Worker.Tick(ctx)` performs one synchronous host cycle. It does not
start a listener, daemon or timer; public operations never launch or poll an agent.
The host controls scheduling and the injected clock.

Scenarios are fixed host configuration: `success`, `verification_failure`,
`lost_ack`, `unavailable`, `loss_after_start`, `malformed_events` and
`missing_manifest`. Their immutable revision includes the script and scenario.
Task text cannot select a scenario. The adapter stores an idempotent dispatch
ledger in SQLite, emits 120 scripted progress events and terminates after two
virtual seconds. It creates only labelled synthetic patch/result data; neither
prompt nor spec text is copied into results. Verification evidence names the
fixture check, its labelled scripted output and output digest. No command runs.

A worker serializes claims with the journal writer lock and a 30-second lease.
A database constraint permits one active run per project/environment. Before
preflight and again before start, it checks the grant, frozen specs, workspace,
policy/export/profile revisions, start deadline and lease. Preflight has no
execution effect. A durable dispatch-prepared event and receipt commit before
adapter start; adapter observation, events, result and receipt commit together
afterward. The adapter ledger bridges the separate start/observation transactions.

Existing active intents are inspected by their original dispatch token and
never started again. A lost acknowledgement or crash after ledger insertion
can reconcile the same run. A crash before any verifiable adapter start remains
`uncertain`, blocks the next queued launch and requires host investigation; no
automatic retry or replacement task is issued. Contact loss preserves the last
confirmed state, becomes uncertain after 30 seconds without verification, and
can recover when evidence returns. Malformed evidence or a missing result cannot
be reported as completion. Stale workers cannot commit over a newer lease.

`read_run` accepts exactly one of `run_id` or `idempotency_key`, plus optional
`event_cursor` and `artifact_ids` (at most four unique IDs). All selectors and
opaque cursors are bound to principal/project/environment and run. Reads return
the latest persisted status, up to 100 ordered events, `next_cursor`, and
`truncated` when more events remain. A cursor at the tail can be reused after
new events arrive and survives restart; unknown or mismatched cursors return
`CURSOR_EXPIRED`. Cursors are retained without automatic expiry in this slice.

When a result is durable, its bounded manifest is included; artifact text is
returned only for explicitly selected registered IDs. Early selection returns
`RESULT_NOT_READY`; unknown IDs return `NOT_FOUND`. Raw prompts, specs, dispatch
tokens and adapter transcripts stay local. The complete output bundle passes the
host export check and durable read receipt before it is returned. Export defaults
to deny. The result store is capped at 192 KiB, manifest at 32 KiB, individual
artifacts at 128 KiB, and total read response at 256 KiB with receipt headroom.
Poll attempts for found runs are limited to two per rolling second per
principal/project/environment, persisted across restart. Return `LIMIT_EXCEEDED`
and back off rather than polling faster; denied export/selection attempts also
consume the polling budget.

## Status and next slice

This is an initial stage-2 offline core slice, not a completed stage-2 gate or
a dogfood release. The specification remains the target; its stage-0 decisions
remain open, and no connectivity or real-execution gate has been passed.

Next: transactional queued cancellation and evidence-based running cancellation.
Subsequent slices add
draft-store filesystem protections, synthetic PR pagination, export/quota/rate
controls and the full
scenario matrix before transport is considered.

Current limitations include no real execution, cancellation, live policy reload,
full quotas, input transport framing,
secret scanning, display sanitization, protected filesystem draft store or host
approval UI. SQLite journal directory ownership, backup and corruption recovery
are not enforced here; use only synthetic temporary data. Task acceptance keeps
its effects and audit record in one transaction. Future filesystem or agent
effects need the same atomicity or an explicitly reconciled intent protocol,
not a separate receipt call after an effect.
