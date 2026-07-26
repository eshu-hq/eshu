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

**#5766 — narrow on a joinable anchor.** The narrowing predicate compared
`container_image_identity`'s `repository_id` — an OCI registry path
(`oci-registry://ghcr.io/org/repo`) — against `ci.run`'s canonical
`repository:r_...` id. Those namespaces never compare equal, so narrowing
silently matched nothing, the caller kept the unfiltered digest-match set, and
correlations that should have been exact degraded to ambiguous. Same trap #5464
found on the supply-chain side.

**#5823 — narrow on build evidence, not on references.** Fixing the namespace
alone would have shipped a worse bug than it cured. `source_repository_ids`
conflates "this repository built the image" with "this repository's manifest
merely references the digest", so a working narrowing predicate could select a
deploy-only row, reduce the match set to one, and promote a repository that
never built the image to `exact`. That is the same conflation #5796 fixed
*inside* the identity domain by gating its own `BUILT_FROM` projection on the
narrower `BuildProvenanceRepositoryIDs`.

That narrow set existed only in memory. `containerImageIdentityPayload` published
`source_repository_ids` and not the build-provenance set, so every cross-domain
consumer was structurally forced onto the conflating join. This branch persists
`build_provenance_repository_ids` and narrows on it.

Persisting it is an additive reducer-publication field, not a governed contract
change: `specs/fact-kind-registry.v1.yaml` governs collector kinds
(`oci_registry.*`), and no `reducer_*` publication kind has a registry entry or
`payload_schema_overrides` schema. The #4573 payload-usage manifest is derived
from typed `factschema.Decode*` seams; this consumer's read is explicitly
raw-payload and out of that scope. No query or OpenAPI response surfaces the new
key.

**Mixed-generation safety.** Identity facts published before this change carry
no build-provenance key at all. Reading an absent key as "built nothing" would
silently degrade every correlation against a dormant scope, so the consumer
records key *presence* separately from key *contents*: when no candidate row
declares the key, the legacy `source_repository_ids` join is used; once any row
declares it, a repository the key does not name stays unnarrowed. Both
directions fail conservatively toward `ambiguous`, never toward a false `exact`.
The key is therefore always written, even when empty — an omitted key on an
empty set would be indistinguishable from a legacy fact.

**Producer asymmetry closed.** `applySLSADigestRevision` appended its anchor's
repositories to `BuildProvenanceRepositoryIDs`; `applyCIRunDigestRevision`
appended them only to `SourceRepositoryIDs`. A competing decision that won the
upsert therefore carried its genuine builder in the broad field alone and had no
build provenance at all — the same gap class #5808 fixed one path over. The
append now sits before the source-revision tier guard, because build provenance
is a set, not a winner-take-all tier: a CI run that reported producing a digest
is build evidence for its repository regardless of which tier won the revision
race or whether a commit resolved.

## Verification

Behavior change, so the proof is the intended delta, not identity with the old
output. Each test below fails on the parent commit and passes here.

```
$ cd go && go test ./internal/reducer \
    -run 'CICDImageMatches|BuildCICDImageIdentityIndex|ContainerImageIdentityPayload|ApplyCIRunDigestRevision' \
    -count=1 -v
--- PASS: TestCICDImageMatchesForRepositoryNarrowsOnGitSourceRepositories      (#5766)
--- PASS: TestCICDImageMatchesForRepositoryIgnoresOCIRegistryRepositoryID      (#5766)
--- PASS: TestCICDImageMatchesForRepositoryRejectsReferenceOnlyRepository      (#5823)
--- PASS: TestCICDImageMatchesForRepositorySelectsBuildProvenanceRow           (#5823)
--- PASS: TestCICDImageMatchesForRepositoryFallsBackForLegacyPayloads          (mixed generation)
--- PASS: TestCICDImageMatchesForRepositoryDoesNotFallBackWhenKeyPresent       (mixed generation)
--- PASS: TestContainerImageIdentityPayloadPersistsBuildProvenanceRepositoryIDs
--- PASS: TestContainerImageIdentityPayloadEmitsEmptyBuildProvenanceKey
--- PASS: TestBuildCICDImageIdentityIndexReadsBuildProvenance
--- PASS: TestApplyCIRunDigestRevisionConfersBuildProvenance
ok  	github.com/eshu-hq/eshu/go/internal/reducer	1.131s
```

`TestCICDImageMatchesForRepositoryRejectsReferenceOnlyRepository` is the
inversion of a test this branch previously added to pin the false positive as
unavoidable. Its failure message named the condition under which it should be
inverted; that condition is now met.

Cross-package consumers and the full owning packages:

```
$ cd go && go test ./internal/reducer ./internal/storage/cypher ./cmd/reducer \
    ./internal/query ./internal/mcp ./internal/projector -count=1
ok  	github.com/eshu-hq/eshu/go/internal/reducer	3.411s
ok  	github.com/eshu-hq/eshu/go/internal/storage/cypher	0.851s
ok  	github.com/eshu-hq/eshu/go/cmd/reducer	1.057s
ok  	github.com/eshu-hq/eshu/go/internal/query	3.372s
ok  	github.com/eshu-hq/eshu/go/internal/mcp	2.750s
ok  	github.com/eshu-hq/eshu/go/internal/projector	0.536s
```

No-Regression Evidence: the withdrawn projection leaves no trace — the
provenance edge writer, its wiring, the reducer entrypoint, and the telemetry
coverage doc are byte-identical to `origin/main`, and `rg` finds no reference to
`CICDRunProvenanceEdgeWriter`, `projectCICDRunBuiltFromEdges`, or
`cicdRunBuiltFromRows`. No Cypher statement, graph write, or index changes. The
remaining runtime deltas are one additional map key on a payload the reducer
already writes, and a narrowing predicate that can only ever select a subset of
the matches the caller already had.

No-Observability-Change: no metrics, spans, structured logs, or status fields
are added or altered; `docs/public/observability/telemetry-coverage.md` is
restored to main's content.

## Golden corpus

The B-7 gate covers this surface because reducer publication output changed.
Neither pinned floor asserts the identity payload's key set: the
`list_container_image_identities` floor asserts filtering on
`source_repository_ids` returns at least one row, and the
`list_ci_cd_run_correlations` floor pins `minimum_results: 1` while explicitly
declining to pin an outcome value. The corpus's builder row gains
`build_provenance_repository_ids` through the same `ci.artifact` join that
already gave it `source_repository_ids`, so narrowing selects the same single
row and both floors hold. The gate run recording that result is cited in the PR.

## Open issues this work produced

- **#5827** — the provenance writer's `MERGE` identity omits `evidence_source`
  and `scope_id`, collapsing same-pair edges across domains and scopes. Live for
  `PUBLISHES` on `main` today and recorded in the B-12 snapshot's rc-164.
  `BUILT_FROM` stays single-owner until it lands.
- **#5828** — `eshu_dp_provenance_edges_total` counts submitted rows as
  `materialized`, including rows whose endpoint node was absent.
- **#5822** — the golden corpus never reaches an `exact` ci_cd_run correlation,
  so the exact path has no deterministic fixture.
