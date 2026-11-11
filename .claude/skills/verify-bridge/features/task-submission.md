# Approve and submit a task

The trusted host reviews an exact task envelope and issues a 30-minute, single-use approval. The dot caller then submits the envelope with that approval and an idempotency key, and gets an accepted run at state version 1. Resubmitting the same input replays the original run. Changed input with the same key is a conflict.

## Sub-features

- `task-review` returns the canonical envelope, its digest, and the frozen spec text.
- `task-approve` issues an approval with effect `approval_issued`, expiring 30 virtual minutes later.
- `task-submit` accepts the run: effect `task_accepted`, run ID `sim_<uuid>`, state `accepted`, `state_version` 1, `start_deadline` 10 minutes later.
- `task-replay` returns the same run for the same key and input, with `replayed: true` and effect `task_replayed`.
- `task-conflict` denies a changed envelope under the same key with `IDEMPOTENCY_CONFLICT`.
- `task-expired-approval` denies a submission after the approval expires (advance `31m`).

## How to get to it (user POV)

- Host: `ReviewTask`, then `IssueApproval`. These are library-only and never dot-callable.
- Dot caller: `Dispatch` with `operation: "submit_task"` and `arguments` `{envelope, approval_id, idempotency_key}`.

## Driving it with bridgehost

Preconditions:

- Baseline run. `W=$BRIDGE_RUN/work`.

- **Envelope.** Run `$S/bh.sh envelope > $W/envelope.json`. It prints a `stub-v1` envelope that references `design`.
- **Review.** Run `$S/bh.sh review $W/envelope.json > $W/review.json`. Expect `.frozen_specs[0].text == "Synthetic design"` and an `.envelope_digest`.
- **Approve.** Run `$S/bh.sh approve $W/review.json > $W/approval.json`. Expect `.receipt.effect == "approval_issued"` and `.expires_at == "2026-10-03T00:30:00Z"`.
- **Submit.** Build the request with `jq -n --slurpfile e $W/envelope.json --slurpfile a $W/approval.json '{contract_version:"1",request_id:"11111111-1111-4111-8111-111111111111",operation:"submit_task",project_id:"personal-demo",arguments:{envelope:$e[0],approval_id:$a[0].approval_id,idempotency_key:"00000000-0000-4000-8000-0000000000aa"}}' > $W/submit.json`. Run `$S/bh.sh dispatch $W/submit.json > $W/submitted.json`. Expect `.receipt.effect == "task_accepted"`, `.run.state == "accepted"` and `.run.state_version == 1`.
- **Replay.** Dispatch `$W/submit.json` again. Expect `.replayed == true`, `.receipt.effect == "task_replayed"`, and the same `.run.run_id`.
- **Conflict.** Run `$S/bh.sh envelope "Changed synthetic prompt" > $W/env2.json`, then `jq --slurpfile e $W/env2.json '.arguments.envelope=$e[0]' $W/submit.json > $W/conflict.json`, and dispatch it. Expect `IDEMPOTENCY_CONFLICT`.
- **Durable proof.** Run `$S/probe.sh "SELECT COUNT(*) n, SUM(consumed_run_id IS NOT NULL) consumed FROM approvals"`. Expect `n` 1 and `consumed` 1. Also check `SELECT COUNT(*) FROM task_runs` returns 1.

## Gotchas

- Editing the envelope by hand breaks its digests. A bad `prompt_digest` gives `INVALID_ARGUMENT`, not a conflict. Generate variants with `bh.sh envelope "<prompt>"`.
- The envelope's `profile_revision` is bound to the run's scenario. An envelope from a run with a different scenario is stale.
- Approvals are single-use. A second key with the same approval is denied. Issue a new approval per submission.
- At most five runs can be queued per project and environment.
