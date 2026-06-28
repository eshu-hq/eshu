# Fact Records Growth Breakpoint Gate

Issue: #4044

Status: accepted research gate. This record does not choose or implement a
physical `fact_records` schema change. It makes any future `partition`,
`archive`, `split`, `retention_tune`, or `defer` recommendation depend on a
measured hosted-growth proof artifact.

## Problem

`fact_records` is the durable source for many Eshu truths. It carries active
generation facts, collector facts, parser facts, search-document facts, reducer
correlation facts, and specialized JSONB indexes for bounded reads. Retention
and queue backpressure already exist, but growth can still appear as stale
reducer projection, graph-write retry pressure, or false-empty API/MCP results
before it looks like a single slow SQL query.

## Flow

The measured path is:

```text
sync -> discover -> parse -> emit facts -> fact_records storage/indexes ->
enqueue -> reducer -> graph/content projection -> API/MCP query
```

The gate keeps these dimensions separate:

- fact ingestion: aggregate rows/sec and per-family insert p95.
- fact storage: before/after `fact_records` rows, total bytes, index bytes,
  index bloat, dead tuple bytes, and write amplification.
- reducer projection: reducer queue age, claim/drain latency, retrying rows,
  dead letters, stale rows, active claims, and worker count.
- graph-write pressure: graph-write p95, timeout retries, retrying
  `graph_write_timeout` rows, dead letters, and grouped row pressure.
- query plans: active-generation reads, correlation joins, changed-since
  retention reads, and hot API fact reads must be indexed, bounded, non-spilling,
  and not sequential scans.
- retention: superseded rows, oldest superseded age, retention lag, prune
  duration, prune batch rows, and archive posture.

## Decision

Do not implement partitioning, archive tables, fact-family table splits, or
retention tuning in #4044.

The decision for this slice is `no physical schema change`. #4044 is the
breakpoint gate, not a schema migration. The change extends the existing
hosted-growth Postgres proof contract so each operator-local benchmark must
present the measurements above before choosing a physical design. A proof
recommendation is valid only when it also records migration, rollback,
retention, and tenant-isolation implications and the measured fields justify the
chosen recommendation.

Decision triggers:

| Recommendation | Evidence required before choosing it |
| --- | --- |
| `partition` | `fact_records` and its indexes grow past the hosted-growth threshold while active-generation and query-plan evidence show partition-prunable predicates and the migration proof includes partition keys in primary and unique constraints. |
| `archive` | Superseded history and retention lag dominate storage growth, prune batches are bounded but cannot catch up, and active/current reads remain healthy. |
| `split` | One fact family dominates row growth, index bloat, and write amplification while other families stay below threshold and read paths can be preserved without cross-family query regressions. |
| `retention_tune` | Retention lag or stale superseded rows are the breakpoint while active facts, graph writes, and hot query plans stay within threshold. |
| `defer` | Fact rows, index bytes, live queue rows, oldest queue age, and retention lag all remain below the hosted-growth thresholds. |

## Existing Work

The gate links, but does not duplicate, the existing performance work:

- #3741: Performance and end-to-end validation epic.
- #3624: reducer materialization long-pole investigation.
- #3794 through #3804: benchmark, golden-corpus, reducer, and macro regression
  detection tasks.
- #2749: hosted-growth Postgres fact and queue proof verifier.
- #2248: retention semantics for generations, facts, and content.

## Evidence Contract

`scripts/verify-hosted-growth-postgres-proof.sh` now rejects hosted-growth proof
artifacts that omit or contradict:

- `fact_growth`: before/after row and index totals plus collector, parser,
  search-document, and correlation family growth.
- `index_bloat`: table bloat, dead tuple bytes, and bounded index samples.
- `graph_write_pressure`: graph-write p95, timeout retries, retrying rows, dead
  letters, and grouped row pressure.
- `query_plans`: active-generation, correlation, retention changed-since, and
  hot API read classes.
- `retention`: retention lag, prune cost, superseded rows, and archive posture.
- `decision`: one of `partition`, `archive`, `split`, `retention_tune`, or
  `defer`, with links to the existing performance issues, public-safe
  implication labels, and measurements that justify the selected option.

The verifier still rejects private locators, raw logs, request/response bodies,
hostnames, IP addresses, DSNs, paths, account ids, credentials, and repository
names. It projects only whitelisted aggregate fields into the committed summary;
raw evidence and raw query plans remain operator-local.

## No-Regression Evidence

`bash scripts/test-verify-hosted-growth-postgres-proof.sh` proves the verifier
accepts a public-safe aggregate hosted-growth proof and rejects missing or
unsafe breakpoint evidence for fact-family growth, index bloat, graph-write
pressure, query plans, retention, and the final decision. The fixture includes a
retention-lag breakpoint and therefore must choose `retention_tune`; changing it
to `defer` fails because the recommendation no longer matches the measurements.

No schema DDL, table layout, index definition, queue SQL, worker count, graph
write, API/MCP handler, or runtime default changes in this slice.

## No-Observability-Change

This research gate adds no metric, span, log field, status route, queue domain,
worker, lease, batch, runtime knob, schema DDL, graph write, or query handler.
Operators still diagnose the live system through existing relation-size SQL,
Postgres query spans, queue status, reducer graph-write retry rows,
generation-retention events, `/admin/status`, API/MCP query telemetry, and raw
operator-local plans/profiles.
