# #5428 — BUILT_FROM projection rescinded: single-owner edge

This branch implemented the PROJECT disposition
`docs/internal/design/5472-graph-projection-policy.md` assigned
`ci_cd_run_correlation`, then withdrew it. The implementation was sound in
isolation and still wrong to ship. This record explains why, so the decision is
not silently re-litigated.

## Why it was withdrawn

The canonical provenance writer upserts with a bare
`MERGE (img)-[rel:BUILT_FROM]->(repo)` and then `SET rel.evidence_source`,
`rel.scope_id`. NornicDB matches an existing relationship on **(start, end,
type) only** — verified at the pinned version,
`github.com/orneryd/nornicdb@v1.0.45/pkg/cypher/merge.go:1769`:
`existingEdge := store.GetEdgeBetween(startNode.ID, endNode.ID, relType)`, with
the parsed relationship property map used only on the create branch.

So a second domain writing the same `(digest, repository)` pair does not create
a second edge. It MERGEs onto the first domain's edge and overwrites the very
`evidence_source` the retract passes key on. After that, one domain's retract
deletes an assertion the other domain still supports, and the loser's retract
matches nothing. The isolation this projection depended on does not exist.

Adding `evidence_source` to the MERGE pattern is a **false fix** on this
backend: the property map is ignored by the match, so the collapse continues
while an emitted-Cypher test goes green. A test asserting that statement shape
was written during this work and deleted for exactly that reason.

The defect is not introduced here. It is live on `main` for PUBLISHES today, and
the B-12 snapshot's rc-164 already records it as current behaviour: *"Both
decisions MERGE onto ONE PUBLISHES edge ... evidence_source/evidence_kinds are
SET, not MERGE keys, so the edge's surviving evidence_kinds is whichever call
ran last."* It is filed as **#5827**.

## No coverage is lost

The CI-run build signal already reaches the graph through the single-owner lane.
`addCICDArtifactImageReference` reads the `ci.artifact` digest, joins the sibling
`ci.run`, and appends that run's repository to `buildProvenanceRepositoryIDs`
with the comment *"A CI run that reported producing this artifact digest is build
evidence, not a mere reference"*. `container_image_identity`'s writer then
projects the `BUILT_FROM` edge from that field under
`evidence_source=reducer/container-image-identity`. The withdrawn projection
emitted the identical `{digest, repository_id}` row for the same node pair — it
was a second owner of an edge that already existed, not a second claim.

Correlation truth that the graph does not carry — run identity, outcome tier,
environment — stays Postgres-only and is disclosed through
`PostgresOnlyBoundary` on `get_workload_story` and `trace_deployment_chain`,
unchanged by this branch.

## Edge type: BUILT_FROM, not PRODUCED_IMAGE

Recorded because the issue text is misleading and will be read again. #5428 names
`Run-PRODUCED_IMAGE->ContainerImage`. That edge type does not exist anywhere in
the repo, and the policy specifies reusing `BUILT_FROM`. Any future revival of
this projection should follow the policy, not the issue text.

## What this branch still ships

Only #5766: `cicdImageMatchesForRepository` now narrows on the identity's
`source_repository_ids` instead of comparing `container_image_identity`'s
`repository_id` — an OCI registry path — against `ci.run`'s canonical
`repository:r_...` id. Those namespaces never compare equal, so narrowing
silently matched nothing and the caller fell back to the unfiltered digest-match
set, degrading correlations that should have been exact into ambiguous.

That fix is correct but imprecise, and the imprecision is disclosed rather than
hidden: `source_repository_ids` conflates "built this image" with "references
this digest", so a repository that only deploys an image can still be selected.
`TestCICDImageMatchesForRepositoryCannotDistinguishBuiltFromReferenced` asserts
that reachable behaviour. The precise join key is
`build_provenance_repository_ids`, which is not persisted on the identity payload
today; persisting it is a contract change (registry, cassettes, B-12) filed as
**#5823**.

## Verification

No-Regression Evidence: after the withdrawal this branch changes no graph write,
no Cypher statement, no writer wiring, and no telemetry — `provenance_edge_writer.go`,
`defaults.go`, `defaults_additive_domains_correlation.go`, `cmd/reducer/main.go`,
and `telemetry-coverage.md` are byte-identical to `origin/main`. The only runtime
change is the narrowing predicate, which can only ever select a subset of the
matches the caller already had.

```
$ rg -n 'CICDRunProvenanceEdgeWriter|projectCICDRunBuiltFromEdges|cicdRunBuiltFromRows' go/
(no matches — projection fully removed)

$ cd go && go build ./...
(no output)

$ go test ./internal/reducer ./internal/storage/cypher ./cmd/reducer -count=1
ok  	github.com/eshu-hq/eshu/go/internal/reducer
ok  	github.com/eshu-hq/eshu/go/internal/storage/cypher
ok  	github.com/eshu-hq/eshu/go/cmd/reducer
```

No-Observability-Change: no metrics, spans, structured logs, or status fields are
added or altered; the telemetry-coverage doc is restored to main's content.

## Open issues this work produced

- **#5827** — provenance writer MERGE identity omits `evidence_source`/`scope_id`
  (the class defect; live for PUBLISHES today). BUILT_FROM stays single-owner
  until it lands.
- **#5828** — `eshu_dp_provenance_edges_total` counts submitted rows as
  `materialized`, including rows whose endpoint was absent.
- **#5823** — persist `build_provenance_repository_ids` so narrowing can
  distinguish built-from from referenced-by.
- **#5822** — the golden corpus never reaches an `exact` ci_cd_run correlation.
