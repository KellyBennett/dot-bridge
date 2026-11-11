# dot-bridge verification map

This directory is the maintained source for verifying dot-bridge's user-facing behavior. There are two users: the dot caller, who sends JSON requests to `Broker.Dispatch`, and the trusted host, who approves tasks and drives the worker. Read this index, then use the matching feature file as the recipe.

## Baseline preconditions

- A fresh run from `$S/launch.sh <run-id>` with `BRIDGE_RUN` exported. `S=.claude/skills/verify-bridge/scripts`.
- `$S/doctor.sh` passes. The virtual clock reads `2026-10-03T00:00:00Z` and there are 0 receipts.
- The host fixture is unchanged: principal `verify-principal`, project `personal-demo`, document `design` = `Synthetic design`.

## Driving conventions

- Write each request to `$BRIDGE_RUN/work/<name>.json` and send it with `$S/bh.sh dispatch <file>`. Build variants with `jq` from a base request.
- Use a valid lowercase UUID for every `request_id` and `idempotency_key`.
- `read_run` allows 2 polls per rolling virtual second. Run `$S/bh.sh advance 1s` before a third read.
- Host actions (`review`, `approve`, `tick`, `advance`) are never dot-callable. Never route them through `dispatch`.

## Proof and skip reporting

- Cite the `evidence/calls/NNN-*` directories for the action and its response.
- Back each allowed effect with a `probe.sh` read of its durable row.
- Back each denial with its `error_code` and a receipt with `effect: "none"`.
- Report a journey that cannot reach its end state as failed, with the call directory. Never weaken the journey.

## Feature entry contract

Each feature file starts with an H1 title and one paragraph describing the user-visible behavior. It then uses exactly four H2 sections in this order: `Sub-features`, `How to get to it (user POV)`, `Driving it with bridgehost`, `Gotchas`.

## Features

- [Read a draft](./read-draft.md) covers `read_draft`: exact text and revision, revision pinning, unregistered documents, schema rejection, and receipts.
- [Approve and submit a task](./task-submission.md) covers host review and approval, `submit_task` acceptance, idempotent replay, and conflict.
- [Run lifecycle and results](./run-lifecycle.md) covers worker ticks, run states, event pages, artifacts, the poll limit, and scripted failure scenarios.
- [Unimplemented operations](./unimplemented-operations.md) covers `write_draft`, `cancel_run` and `read_pr` denial receipts.
