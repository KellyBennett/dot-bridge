---
name: verify-bridge
description: Drive the dot-bridge Go library end to end as its trusted host and its dot caller (read_draft, host approval, submit_task, worker ticks, read_run), with a per-run SQLite journal and recorded evidence. Use when you need to prove bridge behavior beyond unit tests, such as after changing broker, task, worker or run-read code, or to reproduce a reported response or receipt.
---

# Verify dot-bridge

dot-bridge has no CLI, listener or UI. Its user surface is the Go API. A host builds a `Broker`, calls `ReviewTask`/`IssueApproval`, and drives a `Worker`. A dot caller sends raw JSON to `Broker.Dispatch`. The `bridgehost` harness in `harness/main.go` plays that host. Each invocation is a fresh process that reopens the journal and rebuilds the broker, so every call also exercises restart durability.

Everything is synthetic. No command, network call or real agent runs. The clock is virtual and moves only when you run `advance`.

All paths below are relative to the repo root. `S=.claude/skills/verify-bridge/scripts`.

## Launch

```sh
S=.claude/skills/verify-bridge/scripts
$S/launch.sh <run-id> [scenario] [--deny-exports]
export BRIDGE_RUN=<path printed on the last line>
```

- `launch.sh` builds `bridgehost` from the working tree, creates `$TMPDIR/dot-bridge-verify/<run-id>/`, and initializes an empty journal with all migrations applied. Ready means the output contains `bridgehost ready scenario=… now=2026-10-03T00:00:00Z`.
- Use a new `<run-id>` per run. Launch refuses to reuse one.
- `scenario` defaults to `success`. The others are `verification_failure`, `lost_ack`, `unavailable`, `loss_after_start`, `malformed_events` and `missing_manifest`. The scenario is fixed for the instance's lifetime. Launch a new run to change it.
- `--deny-exports` builds the host with no export check, so the bridge denies exports by default.
- Go is not always on `PATH`. The scripts fall back to `mise exec go -- go`.
- Shell variables do not persist between tool calls. Re-export `BRIDGE_RUN` in each call.

Host fixture (in `harness/main.go`): principal `verify-principal`, environment `personal`, project `personal-demo`, one document `design` with text `Synthetic design`. The grant lists all six operations and expires at `2026-10-04T00:00:00Z` virtual time.

## Doctor

```sh
$S/doctor.sh
```

Read-only. It checks that the binary exists, host state is present, the journal is at migration v3, and the binary was built from the current `HEAD` with no new uncommitted changes under `bridge/`, `db/`, `internal/` or `go.mod`/`go.sum`. It prints the scenario, the virtual time and the receipt and run counts. Run it first, and again whenever a result looks wrong. If it reports a stale build, launch a new run. Do not rebuild over an existing one.

## Drive

Run every call through `bh.sh`. It records each call as evidence.

| Call | Role | What it does |
| --- | --- | --- |
| `$S/bh.sh dispatch <file>` or `… dispatch -` | dot caller | Sends raw request bytes to `Broker.Dispatch` and prints the `Response`. |
| `$S/bh.sh envelope ["<prompt>"]` | helper | Prints a valid `TaskEnvelope` bound to this host's revisions and scenario profile. |
| `$S/bh.sh review <envelope-file>` | host | `ReviewTask`. Prints the `ApprovalReview`. |
| `$S/bh.sh approve <review-file>` | host | `IssueApproval`. Prints the `Approval`. |
| `$S/bh.sh tick` | host | Runs one `Worker.Tick` with the stub adapter. |
| `$S/bh.sh advance <go-duration>` | host | Moves the virtual clock forward, for example `2s` or `31m`. |
| `$S/probe.sh "<SELECT …>"` | observer | Read-only SQL against the journal. Prints the rows as JSON. |

Put request files in `$BRIDGE_RUN/work/`. A request envelope looks like this:

```json
{"contract_version":"1","request_id":"<uuid>","operation":"read_draft","project_id":"personal-demo","arguments":{"document_id":"design"}}
```

Assert on response fields, not on the summary text. Every response has `.receipt.decision` (`allowed` or `denied`), `.receipt.error_code`, and `.receipt.effect`. A non-zero exit from `bh.sh` means the Go call returned an error, for example `AUDIT_UNAVAILABLE` or an invalid review. A denial is a normal response with exit 0.

Recipes for each feature are in [features/README.md](features/README.md).

## Evidence

- `$BRIDGE_RUN/evidence/calls/NNN-<command>/` holds `argv.txt`, `stdin`, `input` (a copy of the file argument), `stdout`, `stderr` and `exit` for every `bh.sh` call, in order.
- `$BRIDGE_RUN/evidence/probes/` holds each probe's SQL and its JSON rows.
- `$BRIDGE_RUN/evidence/build.txt` records the commit, the dirty-file count and the Go version.
- Cleanup writes `final-<table>.json` snapshots of the receipts, approvals, task_runs, run_lifecycle, run_events and stub_dispatches tables.

Proof standards:

- Drive through `dispatch` with raw JSON, as a dot caller would. Use `review`/`approve`/`tick`/`advance` only for host actions. Do not call unexported functions or edit the journal.
- Prove each effect twice: once in the response and once with a `probe.sh` read of the durable row (a receipt, run or event). A response without its committed receipt is not proof.
- Capture the request and the resulting state. `bh.sh` records both, so cite the call directories.
- A denial is proof only when its `error_code` matches and no allowed effect was recorded.

## Cleanup

```sh
$S/cleanup.sh
```

It snapshots the journal tables into `evidence/`, then deletes `instance/`, `bin/` and `work/` under `$BRIDGE_RUN`. Evidence stays at `$TMPDIR/dot-bridge-verify/<run-id>/evidence/`. Cleanup only touches a directory that `launch.sh` created and marked. There are no processes or ports to stop.

## Isolation

Runs share nothing except the Go build cache, so parallel runs with different run IDs are safe. Never drive another agent's `BRIDGE_RUN`. Its virtual clock lives in `instance/host.json`, and two drivers would race on it.
