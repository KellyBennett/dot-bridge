# Run lifecycle and results

After acceptance, the host advances the run with worker ticks against the deterministic `stub-v1` adapter. The dot caller polls `read_run` for state, ordered event pages, the result manifest and selected artifact text. The scenario that the host fixed at launch decides the outcome. Task text cannot select it.

## Sub-features

- `run-start` moves a tick from `accepted` to `running` (receipts `dispatch_claimed` and then `run_observed`).
- `run-complete` reaches `completed` with `result_available: true` on a tick after `advance 2s`.
- `run-read` returns state, 100 events, `truncated: true` and a `next_cursor`. The first event is `dispatch_prepared`.
- `run-page` with `event_cursor` returns the remaining 25 events.
- `run-artifact` with `artifact_ids: ["SIMULATED_patch"]` returns `SIMULATED_PATCH.diff`. Verification evidence reports `passed`.
- `run-poll-limit` denies a third `read_run` within one virtual second with `LIMIT_EXCEEDED`.
- `run-scenarios`: `verification_failure` ends `failed` with a partial result. `unavailable` ends `failed` with `terminal_reason` `AGENT_UNAVAILABLE` and never starts. Launching with `advance 10m` before the first tick fails with `APPROVAL_STALE`.

## How to get to it (user POV)

- Host: `Worker.Tick` (`$S/bh.sh tick`) and the clock (`$S/bh.sh advance`).
- Dot caller: `Dispatch` with `operation: "read_run"` and exactly one of `run_id` or `idempotency_key`, plus optional `event_cursor` and `artifact_ids` (at most 4).

## Driving it with bridgehost

Preconditions:

- An accepted run from [task-submission](./task-submission.md). `RUN=$(jq -r .run.run_id $W/submitted.json)`.

- **Start.** Run `$S/bh.sh tick`. Expect `.run.state == "running"`.
- **Complete.** Run `$S/bh.sh advance 2s`, then `$S/bh.sh tick`. Expect `.run.state == "completed"` and `.run.result_available == true`.
- **Read.** Run `jq -n --arg r "$RUN" '{contract_version:"1",request_id:"22222222-2222-4222-8222-222222222222",operation:"read_run",project_id:"personal-demo",arguments:{run_id:$r}}' > $W/read1.json`, then `$S/bh.sh dispatch $W/read1.json > $W/read1.out`. Expect 100 `.events`, `.truncated == true`, `.result.artifacts[].id == "SIMULATED_patch"` and `.result.changed_files == ["SIMULATED_demo.txt"]`.
- **Artifact.** Run `$S/bh.sh advance 1s`. Add `.arguments.artifact_ids=["SIMULATED_patch"]` and dispatch the request. Expect `.artifacts[0].filename == "SIMULATED_PATCH.diff"` and `.result.verification[0].outcome == "passed"`.
- **Next page.** Run `$S/bh.sh advance 1s`. Set `.arguments.event_cursor` to `read1.out`'s `.next_cursor` and dispatch the request. Expect 25 events and no `truncated`.
- **Poll limit.** Dispatch the same read twice more without advancing. The third read in that second is denied with `LIMIT_EXCEEDED`.
- **Durable proof.** Run `$S/probe.sh "SELECT COUNT(*) FROM run_events"` (expect 125) and `$S/probe.sh "SELECT json_extract(payload,'$.effect') e, COUNT(*) FROM receipts GROUP BY e"`.
- **Scenarios.** For each non-success scenario, launch a new run with that scenario, repeat the submission and ticks, and check the end state listed in Sub-features.

## Gotchas

- Each tick handles one step. Ticking without `advance 2s` leaves a `success` run in `running`.
- Reading an artifact before the result is durable returns `RESULT_NOT_READY`. An unknown artifact ID returns `NOT_FOUND`.
- Denied `read_run` calls still use poll budget, so a denial can cause a later `LIMIT_EXCEEDED`.
- `uncertain` runs (`lost_ack`, `loss_after_start`) block the next queued launch until the host investigates. Use a new run for each scenario.
- Only `success` has been driven end to end with this harness. The other scenario outcomes are asserted in `bridge/worker_*_test.go` but are not yet proven here.
