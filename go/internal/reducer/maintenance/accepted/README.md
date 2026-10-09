# Accepted Generation Gate

## Purpose

Gates repo-dependency graph-projection authority on the relationship
generation being active (published) in Postgres, so the graph runner cannot
project edges for a generation the Postgres relationship read models do not
yet expose. Moved here from the flat `maintenance` package under #7648.

## Ownership boundary

Owns the activation-fence decorators, the local lookup/prefetch aliases, and
the relationship-generation lookup contracts. Does not own the Postgres
read models behind the lookups (`storage/postgres`), the repo-dependency
projection runner (`internal/reducer`), or the correlation input loaders
that consume the corpus fence.

## Exported surface

- `GateOnActive`, `GatePrefetchOnActive` — fence decorators for the
  per-key and batched-prefetch authority paths
- `Lookup`, `Prefetch` — local aliases of the reducer-root lookup shapes
- `RelationshipGenerationActiveLookup` — per-generation active check
- `RelationshipGenerationsCompleteLookup` — corpus-wide resolved-set check
- `RelationshipGenerationsIncompleteScopesLookup` — holding-scope list
- `IncompleteScopeIDs` — best-effort holder resolution for deferral errors

See `doc.go` for the full contract.

## Dependencies

- `reducer/sharedintent` — `AcceptanceKey` and `Row` shapes
- `internal/telemetry` — gate-decision counter

Never `internal/reducer`.

## Telemetry

- Metrics: `eshu_dp_repo_dependency_gate_decisions_total` by `decision`
  (`bypassed`/`deferred_error`/`deferred_inactive`/`active`)
- Spans: none; the gate runs inline in the caller's span
- Logs: none; deferrals surface through the caller's retry path

## Gotchas / invariants

- The fence applies ONLY to `repo_dependency`/`repo_dependency:<scope>`
  source runs. Extending it to a scope-generation-ID path permanently
  blocks those intents (B-13).
- A lookup error defers (fail safe): a transient failure must never let
  graph edges publish ahead of the Postgres generation swap.
- `Prefetch` values do not interoperate directly with the reducer root's
  spelling; the adapter in `cmd/reducer/main_helpers.go` is load-bearing.

## Related docs

- `go/internal/reducer/README.md`
- `docs/public/observability/telemetry-coverage.md`
