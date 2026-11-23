# internal/reducer/advisory/group

## Purpose

Owns the shared advisory-group projection contract for #10411: the grouped
advisory membership the reducer computes and the graph writer reconciles as
`Repository-[:AFFECTED_BY]->AdvisoryGroup`.

## Ownership boundary

**Owns:** the group and edge shapes (`Edge`, `Group`, `WinnerRow`,
`WriteStats`), the entity-key prefix, and `GroupIDFromEntityKey`.

**Does not own:** grouping logic and the projection handler (a sibling lane
adds `handler.go` here later), the Cypher
(`internal/storage/cypher/edge/writer/advisory_group.go`), the schema
(`internal/graph`), or the read surface.

## Exported surface

- `Edge` — one (repository, package) membership with ecosystem and PURL
- `Group` — one reconciled group: CVE/advisory ids plus full edge membership
- `WriteStats` — driver summary counters summed over one reconcile call
- `WinnerRow` — one grouped advisory row the handler reconciles
- `EntityKeyPrefix`, `GroupIDFromEntityKey` — entity-key to group-id mapping
- `PayloadKeyFirstUnderlyingDataAt` — optional intent payload key carrying the row's first-underlying-data time (#11080)
- `AdvisoryOriginNVD`, `AdvisoryOriginGHSA`, `AdvisoryOriginOther` — closed per-verdict origin values for `watermark_waits_total` (#11080)

See `doc.go` for the full godoc contract.

## Dependencies

- Standard library only (`strings`). The package must stay import-free of
  other Eshu packages so the reducer, storage, and (later) handler lanes can
  all import it without a cycle.

## Telemetry

No metrics, spans, or logs: this package holds types only.

## Gotchas / invariants

- A `Group` carries no evidence stamps by design. Do not add
  evidence_source, scope, or generation fields here.
- The (RepositoryID, PackageID) pair is the edge identity. Do not widen the
  `Edge` identity without updating the writer's `AFFECTED_BY` MERGE key.
- `WinnerRow.Members` includes the winner itself.

## Related docs

- Design rationale: (#9209)

## No-regression evidence (#11080)

- No-Regression Evidence (fast-path verdict removal): baseline is the merged fast-path tree (50430586c1, on origin/main), where a stamped ancient row reads stale on first eval. After the correction the verdict ignores the stamp: a stamped ancient first-eval defers not_ready (expected wait, non-counting) and only a still-waiting row whose same-cycle deferral aged past 30 min reads stale and counts attempts; exactly-30-min stays not_ready. Backend is the Go unit backend (in-memory fakes, no live cluster): `go test ./internal/reducer/advisory/group/ ./internal/telemetry/ -count=1` green, `TestAdvisoryGroupProjectionEnqueuer*` (postgres package, unit) green, `go vet` clean on the touched package. Input shape is A2-shaped fixtures (436.1-min claim lag, 70-min E-R gap, RFC3339Nano stamp). Failing-first: both corrected verdict tests RED pre-fix (stale on stamp) and GREEN post-fix; the three shim probes stay green pinning the before-state (eval1=not_ready/eval2=stale, pace 0.860x, drain bars 4.06x/2.53x/1.51x). Why safe: the change only deletes a verdict branch (no queue/claim/lease/worker/batch change, no serialization, no bar change); stamped and unstamped rows share one post-claim clock, and the dead-maintainer loop (sustained deferral to stale to dead-letter) is unchanged and covered green.
- Observability Evidence: per-verdict origin (nvd/ghsa/other) retained on watermark_waits_total and observed in-test as "nvd" (TestDecision3_WatermarkWaitCarriesOriginLabel); marker-loss counter and the pre/post-claim wait split unchanged with their suites green; no new signals and no witness change on this commit.
