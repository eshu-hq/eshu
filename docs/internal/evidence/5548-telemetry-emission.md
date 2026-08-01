# 5548 — telemetry coverage gate: emission, not just registration

Validation record for the change that made `scripts/verify-telemetry-coverage.sh`
check that a documented metric is actually emitted, wired the two instruments
that turned out to be genuine gaps, and deleted 23 that were dead.

## What changed on a runtime path

Two counter increments, both inside branches that already ran:

| site | added | guard |
| --- | --- | --- |
| `go/internal/storage/postgres/ingestion.go` | `EvidenceFactsDiscovered.Add(ctx, int64(len(evidence)))` | nil-checked, and only reached when `len(evidence) > 0` |
| `go/internal/collector/git_source_processing.go` | `FactBatchesCommitted.Add(ctx, 1)` | inside the existing `s.Instruments != nil` block, beside three sibling emissions |

`len(evidence)` is already computed on that line — the log statement
immediately below it prints the same value. Neither site adds a query, an
allocation, a lock, or an I/O call.

`go/internal/telemetry/instruments.go` lost 264 lines: 23 instrument
registrations that nothing emitted, plus 6 bucket-boundary slices only those
registrations used. Startup registers 23 fewer instruments; nothing else about
the path changes.

No-Regression Evidence: the two added calls are OpenTelemetry counter
increments on code paths that already execute, with no new query, allocation,
lock, or I/O. No benchmark was run and none is claimed — a counter `.Add()`
beside three existing `.Add()`/`.Record()` calls at the same site is not a
measurable change against this repo's collector-run baselines, and inventing a
before/after number for it would be worse than saying so. The deletions can
only reduce startup work.

Observability Evidence: this change is entirely about observability, and
the operator-visible effect is the point.

- Two stages that emitted nothing now emit. `eshu_dp_evidence_facts_discovered_total`
  counts facts found per evidence-discovery pass; `eshu_dp_fact_batches_committed_total`
  counts git-collector fact batches committed.
- 23 metrics that were registered and documented but never emitted are gone.
  Each was checked against `docs/public/observability/dashboards/eshu-operator-overview.json`
  first: the 11 never-emitted ones had **zero** dashboard references, so no
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

The runtime change is two counter increments on paths that already ran. The
deletions remove instruments that produced no samples, or duplicates whose
live registration is untouched. The one behavioural risk worth naming is the
widened registration discovery: check (1) now searches the whole tree, so a
metric registered anywhere counts as registered. That is looser than before,
and deliberately — reading only `instruments.go` is what made the rows citing
`*_metrics.go` registrations unverifiable in the first place.
