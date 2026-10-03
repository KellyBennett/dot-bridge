# dot-bridge

Offline synthetic prototype of the controlled bridge in [SPECIFICATION.md](SPECIFICATION.md).

The first implementation slice is the request/authorization/receipt foundation.
It implements only `read_draft` against host-supplied in-memory synthetic fixtures.
The other five operation names are recognized but return
`OPERATION_NOT_IMPLEMENTED`, with a durable denial receipt. No adapter, shell,
network transport, account login, provider inference, PR access or filesystem
draft-write operation is present.

## Run the checks

Python 3.12+, standard library only. From the repository root:

```sh
python -m unittest discover -s tests -v
```

Tests use temporary SQLite journals and synthetic fixture data, with an injected
clock. No dependency installation is required.

## Core boundaries

- `Identity` is trusted host context, supplied separately from a public request.
  This is not transport authentication. A future transport must verify identity
  before constructing it; there is deliberately no network listener today.
- `Grant` and fixture registry are host configuration. The prototype accepts
  only the personal environment and registered ASCII document IDs.
- `Broker.dispatch` rejects unknown fields, invalid types, unregistered IDs,
  stale revisions, expired/disabled grants and unsupported actions.
- Export checking defaults to deny. A host may explicitly allow **synthetic**
  fixture text for tests; the implementation is not a production redactor.
- SQLite records allowed and denied receipts transactionally, with sequence,
  input/output digests, identity and policy metadata. Fixture text and exception
  details are not retained in receipts. No read response is returned if journal
  commit fails; `AuditUnavailable` means no bridge receipt is available.
- All returned responses, receipts and fixture data carry `simulated: true`.
  Visible summaries begin `SIMULATED — NO REAL AGENT EXECUTION`.
- Returned text remains untrusted data. Consumers must render it safely without
  interpreting embedded instructions or automatically fetching links.

The JSON digest format is contract-v1: sorted keys, compact separators,
unescaped Unicode encoded as UTF-8, and no non-finite numbers. Raw document
revisions hash exact UTF-8 bytes. This is a local format, not a claim of RFC 8785
compliance or an implemented task-envelope approval protocol.

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
