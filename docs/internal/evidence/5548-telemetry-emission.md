# 5548 — telemetry coverage gate: emission, not just registration

Validation record for the change that made `scripts/verify-telemetry-coverage.sh`
check that a documented metric is actually emitted, wired the one instrument
that turned out to be a genuine gap, and deleted 24 that were dead.

## What changed on a runtime path

One counter increment, inside a branch that already ran:

| site | added | guard |
| --- | --- | --- |
| `go/internal/storage/postgres/ingestion.go` | `EvidenceFactsDiscovered.Add(ctx, int64(len(evidence)))` | nil-checked, and only reached when `len(evidence) > 0` |

A second wiring was attempted and then withdrawn. `FactBatchesCommitted` was
placed at the git snapshot site, which fires before the facts reach Postgres
and fires once per snapshot regardless of how many multi-row batches the stream
becomes — so it would have counted snapshots under a name that says batches.
Review caught it. There is no batch count to emit: `CommitScopeGeneration`
returns only an error, and counting real batches would mean plumbing a count
back through the committer interface. `eshu_dp_facts_committed_total` already
fires on that path after a successful commit, so the metric was deleted with
the other dead ones rather than wired somewhere convenient.

`len(evidence)` is already computed on that line — the log statement
immediately below it prints the same value. Neither site adds a query, an
allocation, a lock, or an I/O call.

`go/internal/telemetry/instruments.go` lost 264 lines: 24 instrument
registrations that nothing emitted, plus 6 bucket-boundary slices only those
registrations used. Startup registers 23 fewer instruments; nothing else about
the path changes.

No-Regression Evidence: the one added call is an OpenTelemetry counter
increment on a code path that already executes, with no new query, allocation,
lock, or I/O. No benchmark was run and none is claimed — a counter `.Add()`
beside three existing `.Add()`/`.Record()` calls at the same site is not a
measurable change against this repo's collector-run baselines, and inventing a
before/after number for it would be worse than saying so. The deletions can
only reduce startup work.

Observability Evidence: this change is entirely about observability, and
the operator-visible effect is the point.

- One stage that emitted nothing now emits: `eshu_dp_evidence_facts_discovered_total`
  counts facts found per evidence-discovery pass.
- 24 metrics that were registered and documented but never emitted are gone.
  Each was checked against `docs/public/observability/dashboards/eshu-operator-overview.json`
  first: the 12 never-emitted ones had **zero** dashboard references, so no
  panel or alert could have been reading them. A metric with no emission site
  has never produced a sample.
- The 12 duplicate registrations are deleted from `instruments.go` only. Each
  metric is still registered and emitted from a dedicated `*_metrics.go` beside
  its emitter, so the names keep reporting.
  `eshu_dp_api_request_duration_seconds` continues to feed its 5 dashboard
  panels from `go/internal/query/request_metrics.go`.
- Coverage rows that cited deleted metrics now cite the signals that fire at
  the same stage — `eshu_dp_canonical_writes_total` and
  `eshu_dp_canonical_projection_duration_seconds` from
  `go/internal/projector/runtime_stages.go:87,90` for the canonical cluster,
  and `eshu_dp_queue_depth` for the semantic-extraction queue.

## Verification run

| check | result |
| --- | --- |
| `scripts/verify-telemetry-coverage.sh` | exit 0 |
| `scripts/test-verify-telemetry-coverage.sh` | 39/39 |
| `go build ./...` | exit 0 |
| `go vet` on telemetry, postgres, collector | exit 0 |
| `go test ./internal/telemetry ./internal/collector -count=1` | exit 0 |
| `scripts/generate-operator-dashboard.sh` | no drift |
| `mkdocs build --strict` | exit 0 |

## The check was watched to fail

A gate never seen failing proves nothing, so the new check was mutated rather
than assumed. Re-adding a registration with no emitter:

```
- instruments.go registers `ZZDeadProbe` but nothing outside instruments.go
  references it: registered and documented, never emitted
```

Verifier exits 1. Removing the probe returns it to 0.

## Why this is safe

The runtime change is one counter increment on a path that already ran. The
deletions remove instruments that produced no samples, or duplicates whose
live registration is untouched. The one behavioural risk worth naming is the
widened registration discovery: check (1) now searches the whole tree, so a
metric registered anywhere counts as registered. That is looser than before,
and deliberately — reading only `instruments.go` is what made the rows citing
`*_metrics.go` registrations unverifiable in the first place.
