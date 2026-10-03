# dot-bridge

Offline synthetic prototype of the controlled bridge in [SPECIFICATION.md](SPECIFICATION.md).

The first implementation slice is the Go request/authorization/receipt foundation.
It implements only `read_draft` against host-supplied in-memory synthetic fixtures.
The other five operation names are recognized but return
`OPERATION_NOT_IMPLEMENTED`, with a durable denial receipt. No adapter, shell,
network transport, account login, provider inference, PR access or filesystem
draft-write operation is present.

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
Document revisions hash exact UTF-8 text bytes. Task-envelope canonicalization,
approval binding and idempotency are not implemented yet and will use a separate
versioned canonical format. Read requests are capped at 8 KiB and reject duplicate
JSON keys, trailing values, null fields, excessive nesting and invalid UTF-8.

## Status and next slice

This is an initial stage-2 offline core slice, not a completed stage-2 gate or
a dogfood release. The specification remains the target; its stage-0 decisions
remain open, and no connectivity or real-execution gate has been passed.

Next: exact frozen task-envelope schemas and host-only approval issuance,
transactional approval consumption/idempotency, then a durable deterministic
stub and run recovery. Subsequent slices add draft-store filesystem protections,
synthetic PR pagination, export/quota/rate controls, cancellation and the full
scenario matrix before transport is considered.

Current limitations include no task approvals/runs, idempotency, policy reload,
rate limiting, quotas beyond the single read byte cap, input transport framing,
secret scanning, display sanitization, protected filesystem draft store or host
approval UI. SQLite journal directory ownership, backup and corruption recovery
are not enforced here; use only synthetic temporary data. Mutations will need
effects and audit records in one transaction or an explicitly reconciled intent
protocol, not a separate call to `Journal.record` after an effect.
