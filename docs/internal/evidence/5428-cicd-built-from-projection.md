# #5428 — ci_cd_run_correlation BUILT_FROM projection

Implements the PROJECT disposition
`docs/internal/design/5472-graph-projection-policy.md` assigns this domain:
exact outcomes get a bounded graph write as
`(ContainerImage)-[:BUILT_FROM]->(Repository)` with
`evidence_source=reducer/ci-cd-run-correlation`.

## Edge type: BUILT_FROM, not PRODUCED_IMAGE

Issue #5428's text names `Run-PRODUCED_IMAGE->ContainerImage`. That edge type
does not exist anywhere in the repo (zero hits in `go/internal/graph/edgetype`,
the golden snapshot, or the Cypher writers), and the accepted policy
(`5472-graph-projection-policy.md:51-54` and the disposition table at `:80`)
specifies `BUILT_FROM` reusing #5457's edge with a distinct evidence source.
Implementing the issue text literally would contradict the epic's own design doc
and break the #5457 isolation contract, so this follows the policy.

## Isolation from #5457 on the shared edge

Both this domain and `container_image_identity` write `BUILT_FROM`, and both are
OCI-registry-sourced, so `source_tool` is `oci` for either. `evidence_kinds` is
therefore the only axis that can distinguish them. This adds
`CI_CD_RUN_CORRELATION_EXACT_ARTIFACT` to `provenanceEdgeKindForSource`
(`go/internal/storage/cypher/provenance_edge_writer.go`), which is what lets a
golden-gate required-correlation prove it counted this domain's edges. Retract
passes key on `evidence_source`, so neither domain's retract can touch the
other's edges.

`BUILT_FROM` is already declared retractable
(`specs/replay-depth-requirements.v1.yaml:174`, added by #5457); reusing the edge
type needs no new entry.

## Boundary disclosures STAY (correction to the policy's expectation)

Policy `:154-157` anticipates that a domain's `PostgresOnlyBoundary` disclosure
is removed once its projection lands. That does **not** apply here, and the
disclosures for `get_workload_story` and `trace_deployment_chain` are
deliberately left in place:

- The query package does not read `BUILT_FROM` on any story surface (`rg
  'BUILT_FROM' go/internal/query/` matches only `evidence_boundaries.go`, its
  test, and one unrelated file). Projecting the edge does not make the domain's
  read-model reachable through those surfaces.
- The precedent agrees: `container_image_identity` projects `BUILT_FROM` (#5457)
  and is **still** disclosed for `trace_deployment_chain`
  (`go/internal/query/evidence_boundaries.go:65`).
- Policy `:162-163` disclosures apply when a domain is "genuinely absent from
  that surface's ENTIRE response". A `BUILT_FROM` edge carries "this image was
  built from this repository" — not the correlation read-model (run identity,
  outcome, environment, canonical target). Dropping the disclosure would claim a
  reachability that does not exist.

## Verification

No-Regression Evidence: the projection is additive and dormant unless a
`CICDRunProvenanceEdgeWriter` is injected — `projectCICDRunBuiltFromEdges`
returns nil on a nil writer, so every profile that does not wire an adapter
keeps the exact Postgres-only behavior it had before. When wired, the write is
bounded by the exact-outcome row set (one row per distinct digest+repository,
deduplicated and stably ordered) and reuses the existing batched
`ProvenanceEdgeWriter` with its established `DefaultBatchSize`; no new Cypher
statement shape, no new round trip per decision, no new fact load, and no
worker/lease/queue change. Commands run on this branch:

```
$ cd go && go build ./...
(no output — whole module builds)

$ go test ./internal/reducer/ ./internal/storage/cypher/ ./cmd/reducer/ -count=1
ok  	github.com/eshu-hq/eshu/go/internal/reducer	3.367s
ok  	github.com/eshu-hq/eshu/go/internal/storage/cypher	0.776s
ok  	github.com/eshu-hq/eshu/go/cmd/reducer	1.965s
```

Tests were written before the implementation and each failed against the absent
function (`undefined: cicdRunBuiltFromRows`): exact-only tiering (derived and
ambiguous produce no row), no-fabrication guards (blank digest or blank
repository produce no row, #5463), row dedupe, retract-runs-even-with-zero-rows,
and retract-before-write ordering carrying this domain's evidence source.

No-Observability-Change: no metrics, spans, structured logs, or status fields are
added or altered. The projection runs inside the existing
`ci_cd_run_correlation` handler span and its outcome counters are unchanged.

## Open: golden-corpus assertion depends on an exact outcome existing

A non-vacuous golden assertion (`minimum_count >= 1`) for this edge requires the
20-repo corpus to produce at least one **exact** ci_cd_run correlation. That is
not yet established: #5766 (open) records that `cicdImageMatchesForRepository`
narrows by the OCI `repository_id` namespace, which never matches `ci.run`'s
canonical `repository:r_...`, so the digest match set is not narrowed to one row
and the corpus outcome lands on derived/ambiguous. If that holds, this
projection writes zero edges in the corpus and a `minimum_count >= 1` assertion
would be unsatisfiable — the honest options are a `max: 0` floor documenting the
gap, or landing #5766 first so an exact outcome becomes reachable. This must be
settled by running `scripts/verify-golden-corpus-gate.sh` and reading the actual
outcome, not by assumption, before this change claims corpus coverage.
