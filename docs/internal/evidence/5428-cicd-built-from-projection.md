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

## Measured: the corpus reaches `derived`, so no golden floor is possible

The open question -- whether a `minimum_count >= 1` floor is justified -- is now
answered by measurement, not assumption. Golden-corpus gate run on this branch
(remote, isolated ports, clean volumes), gate PASSES 495/0/1 in 96s:

```
$ MATCH ()-[r:BUILT_FROM]->() RETURN r.evidence_source AS src, count(*) AS n
["reducer/container-image-identity", 2]
(no reducer/ci-cd-run-correlation rows)

$ SELECT payload->>'outcome', count(*) FROM fact_records
    WHERE fact_kind='reducer_ci_cd_run_correlation' GROUP BY 1;
derived|2

$ SELECT status, count(*) FROM fact_work_items
    WHERE domain='ci_cd_run_correlation' GROUP BY 1;
succeeded|2
```

The projection wrote zero edges and that is **correct behavior**: the corpus
correlation lands on `derived`, and this projection is exact-only by policy.

Why `derived` and not `exact`: the artifact loop takes `case 0` (empty
`matches`), meaning no `container_image_identity` rows were available in the
digest index at correlation time -- not that narrowing rejected them. #5766's
narrowing fix on this branch is therefore correct but never exercised in this
corpus: there is nothing to narrow. The work items are `succeeded`, so the
maintenance reopen did run and still produced `derived`.

Two hypotheses were tested and disproven rather than assumed:

- **Dead-letter race.** Predicted the intent dead-letters when the edge write
  races an unprojected endpoint node. Measured `succeeded|2`, zero dead letters.
  A `WITH`-barrier change to the shared BUILT_FROM statement was written to fix
  that and then **reverted**: with no observed dead letter it fixed nothing
  measurable, and landing a shared-writer Cypher change on an unproven theory is
  exactly what prove-the-theory-first forbids. If that barrier is the right fix
  for #5767, it belongs there with its own reproduction.
- **My change caused a drain regression.** An earlier run showed
  `residual=1, dead_letter=1`; a clean-volume re-run of the same commit passed
  with `residual=0`. The failure was contaminated Docker state from an
  aborted port-collision run, not this branch.

Consequence for this change: it ships with **no golden floor**. Asserting
`minimum_count >= 1` would be unsatisfiable on this corpus, and asserting a
`max: 0` floor would freeze a gap rather than describe one. The projection is
proven by unit tests; corpus-level proof waits on a corpus that can reach an
exact outcome. Making `exact` reachable is upstream work (the identity rows are
not in the digest index when the correlation runs), tracked separately.
