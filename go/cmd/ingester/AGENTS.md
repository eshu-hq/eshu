# cmd/ingester Agent Rules

These rules apply only inside `go/cmd/ingester/`. Root `AGENTS.md` still
controls global proof, performance, concurrency, and skill requirements.

## Read First

- `go/cmd/ingester/README.md`
- `go/cmd/ingester/doc.go`
- `go/cmd/ingester/main.go`
- `go/cmd/ingester/wiring.go`
- `go/cmd/ingester/wiring_nornicdb_env.go`
- `go/cmd/ingester/wiring_nornicdb_config.go`
- `go/internal/collector/README.md`
- `go/internal/projector/README.md`

## Local Invariants

- MUST keep the ingester as the only long-running runtime that owns the
  workspace PVC in Kubernetes.
- MUST keep collector and projector services under the shared cancel context in
  `compositeRunner`; first error cancels the paired service.
- MUST keep deferred relationship maintenance ordered:
  `BackfillAllRelationshipEvidence` before `ReopenDeploymentMappingWorkItems`.
- MUST keep `SkipRelationshipBackfill=true` on `IngestionStore`; per-commit
  backfill is intentionally out of the hot commit path.
- MUST keep webhook trigger handoff on the normal Git sync and snapshot path.
- MUST keep backend-specific writer behavior in `openIngesterCanonicalWriter`
  and `wiring_<backend>_*.go` files, not in collector/projector service wiring.
- MUST account for peak Bolt sessions before raising
  `ESHU_NORNICDB_ENTITY_PHASE_CONCURRENCY`: projector workers multiplied by
  entity-phase concurrency determines demand.

## Change Gates

- New graph backends MUST use a narrow writer seam and update backend docs and
  conformance evidence.
- New NornicDB tuning knobs MUST be parsed in the NornicDB config helpers,
  passed through writer setup, documented in the tuning reference, and covered
  by focused tests.
- Projector worker default changes MUST read projector concurrency guidance and
  include graph-write and queue-age evidence.
- New admin routes MUST be mounted through `app.NewHostedWithStatusServer`
  options, not bespoke HTTP setup.

## Focused Verification

```bash
cd go
go test ./cmd/ingester -count=1
go doc -cmd ./cmd/ingester
```
