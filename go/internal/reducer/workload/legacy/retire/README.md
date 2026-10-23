# retire

Operator-driven retirement of pre-#5385 name-only `Workload` nodes (#8795).

## Why it exists

A deployment that missed the identity cutover's delete step holds
`workload:<name>` nodes beside the repository-scoped `workload:<repo_id>:<name>`
ones. The resolver refuses to guess between them, so by-name lookups are
ambiguous. Deleting graph nodes is a production data change, so it is an
explicit, dry-run-first operator action, not a reducer behavior.

## Flow

```mermaid
flowchart LR
  op[operator] -->|POST dry_run true| route[/admin/retire-legacy-workloads/]
  route --> runner[Runner.Run]
  runner --> detect[graph/legacy/workload Detect]
  detect --> graph[(graph reads)]
  runner -->|dry_run false, Neo4j| del[DeleteNeo4jCypher over eligible ids]
  del --> graph
  runner --> out[counter, logs, audit event]
```

## What it owns

| piece | file | what it does |
|---|---|---|
| `Runner` | `runner.go` | validates, detects, deletes, records |
| `Writer`, `Auditor` | `runner.go` | write and audit ports; the reducer's retrying Cypher executor and the governance audit store satisfy them |

The request, response, and error types (`RetireRequest`, `RetireResponse`,
`RetireCandidate`, `ErrInvalidRequest`, `ErrBackendUnvalidated`,
`ErrWriterUnavailable`) live in `internal/graph/legacy/workload`, so the
runtime admin route that maps the errors to 422, 501, and 503 does not import
this reducer package.

## Contract

- Request: `dry_run` (required boolean), `limit` (1 to 100, default 100,
  eligible nodes), `ids` (optional subset, at most 100), `reason` (required
  when `dry_run` is false).
- A dry run never writes and records no counter or audit event.
- Execute sends one statement over the eligible ids of this request. The
  statement is idempotent: a second run deletes nothing.
- `candidates` lists up to `limit` eligible nodes and up to 100 blocked ones.
  `truncated` is true when more candidates may remain.
- Requests in one process are serialized by a mutex. It holds no queue lane.
- A Neo4j runner with no `Writer` refuses execute with `ErrWriterUnavailable`
  and the audit reason `legacy_workload_retire_refused_writer_unavailable`;
  dry runs still read.

## Telemetry

- `eshu_dp_workload_legacy_retirements_total{outcome}`, outcome one of
  `deleted`, `blocked_has_instances`, `blocked_no_twin`,
  `blocked_relationships`. The blocked outcomes count every node the request
  judged (at most 1,000), not only the 100 the response lists.
- INFO `legacy workload retirement executed`: candidates, eligible, deleted,
  blocked, truncated, reason.
- WARN `legacy workload kept`: workload_id, blocked_reason, relationship_counts,
  one per judged blocked node.
- WARN `legacy workload retirement deleted fewer nodes than were eligible`:
  eligible and deleted. A node changed between the read and the delete, so the
  lock-first statement refused it; re-run the dry run.
- ERROR `legacy workload retirement failed`: eligible, reason, error. The
  request returns an error and the audit event carries `_failed`.
- WARN `legacy workload retirement audit append failed`: error. The audit
  event is lost; the retirement itself is unaffected.
- Governance audit event `admin_recovery_action` with reason code
  `legacy_workload_retire_executed`, `_failed`, or a `_refused_*` code.

## Operating it

See `docs/public/operate/legacy-workload-retirement-runbook.md`.
