# Unimplemented operations

`write_draft`, `cancel_run` and `read_pr` are part of the contract but not implemented in this slice. A granted caller gets a denial with a durable receipt, and no effect occurs.

## Sub-features

- `unimpl-write-draft` denies with `OPERATION_NOT_IMPLEMENTED`.
- `unimpl-cancel-run` denies with `OPERATION_NOT_IMPLEMENTED`, and the target run's state does not change.
- `unimpl-read-pr` denies with `OPERATION_NOT_IMPLEMENTED`.

## How to get to it (user POV)

- Dot caller: `Dispatch` with the operation name. The harness grant includes all three, so the denial comes from the missing implementation, not from policy.

## Driving it with bridgehost

Preconditions:

- Baseline run.

- **Each operation.** Write `{"contract_version":"1","request_id":"55555555-5555-4555-8555-555555555555","operation":"<op>","project_id":"personal-demo","arguments":{}}` for each `<op>` and dispatch it. Expect `.receipt.decision == "denied"`, `.receipt.error_code == "OPERATION_NOT_IMPLEMENTED"` and `.receipt.effect == "none"`.
- **No effect.** For `cancel_run`, take a run from [run-lifecycle](./run-lifecycle.md) and read it before and after the cancel. `state` and `state_version` are unchanged.
- **Durable proof.** Run `$S/probe.sh "SELECT json_extract(payload,'$.operation') op, json_extract(payload,'$.error_code') code FROM receipts"`. Expect one denied row per call.

## Gotchas

- When one of these operations gets implemented, this file is wrong. Move its journeys to a new feature file. Do not delete the denial journey without replacing it.
