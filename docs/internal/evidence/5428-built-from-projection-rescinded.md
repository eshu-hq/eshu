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

That narrow set was, when this work started, in-memory only:
`containerImageIdentityPayload` published `source_repository_ids` and not the
build-provenance set, so every cross-domain consumer was forced onto the
conflating join. This branch originally persisted the key itself. While it was in
review, #5817 landed the identical field on `main` for the supply-chain-impact
anchor (#5801), so the producer half is now main's and this branch carries only
the consumer half: decode `build_provenance_repository_ids` on the identity fact
and narrow on it.

The key itself is an additive reducer-publication field, not a governed contract
change (this mattered when the branch still carried the producer hunk, and it is
why #5817 could land the same field independently). Some `reducer_*` kinds ARE governed — the `reducer_derived` family
(`specs/fact-kind-registry.v1.yaml:137-148`) registers seven of them with
`payload_schema_overrides` schemas — so the general rule "reducer kinds are
ungoverned" is false and is not the argument here. The specific kind is:
`containerImageIdentityFactKind` is `reducer_container_image_identity`
(`container_image_identity_writer.go:17`), which appears in the registry only
inside a prose comment and in no family's `kinds:` list, so it has no schema to
diff against. The #4573 payload-usage manifest is derived from typed
`factschema.Decode*` seams; this consumer's read is explicitly raw-payload and
out of that scope. No query or OpenAPI response surfaces the new key —
`decodeContainerImageIdentityRow` reads an explicit field list.

**What this does and does not guard.** Narrowing only ever *reduces* a match
set, and both callers apply it as `if len(repoMatches) > 0 { matches =
repoMatches }`. A digest with exactly one identity row therefore still reaches
`case 1` and promotes to `exact` whether or not that row's build provenance
names the run's repository: narrowing returns zero, the caller keeps the
unfiltered single row, and the promotion happens anyway. That is pre-existing
behaviour which this change neither introduces nor worsens, but it bounds the
fix — #5823's protection binds only on digests with two or more candidate rows.
`TestCICDImageMatchesForRepositoryDoesNotGuardSingleRowDigests` pins it so the
limitation cannot be quietly forgotten.

**Mixed-generation behaviour, and a correction.** Identity facts published
before this change carry no build-provenance key, so they decode to an empty set
and narrowing can never select them.

An earlier revision of this branch instead fell back to the broad
`source_repository_ids` join whenever no candidate row declared the key,
reasoning that treating an absent key as "built nothing" would silently degrade
every correlation against a scope that had not republished. **That reasoning was
wrong, and the fallback was an accuracy regression.** Because the pre-#5766
predicate compared the identity's OCI `repository_id` against a canonical
`repository:r_...` id, narrowing was already a dead no-op for every legacy row,
so a legacy multi-row digest already resolved `ambiguous`. There was no lost
correlation for a fallback to recover. All it could do was select a
reference-only legacy row and manufacture an `exact` the previous behaviour
never produced — the very failure #5823 exists to prevent, reintroduced through
the compatibility path.

Measured on the two-legacy-row, reference-only shape: `origin/main` yields
`ambiguous` with `CanonicalWrites=0`; the revision carrying the fallback yielded
`exact` with `CanonicalWrites=1`. The fallback is deleted. Legacy rows are never
selected, which reproduces `origin/main` exactly for those rows, and the sharper
join engages for a scope as soon as its identity intent republishes.
`TestCICDImageMatchesForRepositoryNeverSelectsLegacyRows` and
`TestClassifyCICDWorkflowImageEvidenceStaysAmbiguousForLegacyPayloads` pin both
callers against a regression.

The key is still always written, even when empty, so a reader can distinguish
"this generation publishes build provenance and it names nobody" from "this fact
predates the field" without inferring it from an absence.

**Producer asymmetry deliberately NOT closed here.** `applySLSADigestRevision`
appends its digest anchor's repositories to `BuildProvenanceRepositoryIDs`;
`applyCIRunDigestRevision` appends them only to `SourceRepositoryIDs`. A
competing decision that wins the upsert therefore carries its genuine builder in
the broad field alone — the same gap class #5808 fixed one path over, and a real
defect.

It was implemented on this branch and then reverted, because closing it here
contradicts the reason the projection above was withdrawn.
`BuildProvenanceRepositoryIDs` is the sole gate for two live graph writers:
`containerImageBuiltFromRows` and `containerImageDerivedFromRows`. Widening it
makes more scopes emit the same `(digest, repository)` pair — and
`recordCIRunDigestAnchor` exists precisely to handle "a competing decision raised
by a deploy repo's content_entity for the same image". Since
`projectContainerImageBuiltFromEdges` retracts per
`(scope_id, evidence_source)` while the writer's `MERGE` matches
on `(start, end, type)` alone, two scopes emitting the same pair means one
scope's retract deletes an edge the other still supports. That is #5827,
amplified inside the one domain that survived this branch — the B-12 snapshot already records
this digest carrying 11 rows across scopes — in the `list_supply_chain_impact_findings`
MCP query-shape description, which notes that
`reducer_container_image_identity` "has no per-digest canonicalization -- the
producer writes one row per triggering scope/ref, and this digest carries 11 rows
in the live corpus that disagree on `source_repository_ids`".

Landing it would make the graph worse today to make a correlation sharper. It is
tracked as **#5829**, blocked on #5827.

The consequence of leaving it open is bounded and conservative: a
competing-decision row whose builder reaches only `source_repository_ids`
narrows to nothing, so the caller keeps the unfiltered set and the correlation
lands `ambiguous`. It never lands a false `exact`.

## Verification

Behavior change, so the proof is the intended delta, not identity with the old
output.

The red-then-green proof is
`TestCICDNarrowingSelectsTheBuilderFromPublishedFacts`. It is written to be
base-portable — it drives real published payloads through
`buildCICDImageIdentityIndex` rather than constructing the internal struct — so
it compiles and runs unchanged against `origin/main`. That matters: the
struct-literal tests below cannot run against `origin/main` at all, because the
fields they set do not exist there, and a compile failure is not a regression
proof.

```
$ git worktree add --detach <tmp> origin/main
$ cp go/internal/reducer/ci_cd_run_correlation_narrowing_regression_test.go <tmp>/go/internal/reducer/
$ cd <tmp>/go && go test ./internal/reducer -run TestCICDNarrowingSelectsTheBuilderFromPublishedFacts -count=1
--- FAIL: TestCICDNarrowingSelectsTheBuilderFromPublishedFacts (0.00s)
    narrowing for the builder = 0 rows ([]reducer.cicdImageIdentity{}), want exactly the
    identity-builder row; comparing the identity's OCI repository_id against a canonical
    repository:r_... id never matches (#5766)
FAIL	github.com/eshu-hq/eshu/go/internal/reducer	1.368s

$ cd go && go test ./internal/reducer -run TestCICDNarrowingSelectsTheBuilderFromPublishedFacts -count=1
ok  	github.com/eshu-hq/eshu/go/internal/reducer	1.123s
```

The remaining tests are unit coverage for the new predicate, the decode seam,
the payload emission, and the previously-untested second caller. They are new
code paths rather than red-then-green regressions, and are listed as such.

```
$ cd go && go test ./internal/reducer -count=1 -v \
    -run 'CICDImageMatches|CICDNarrowingSelects|BuildCICDImageIdentityIndex|ContainerImageIdentityPayload|ClassifyCICDWorkflowImage'
--- PASS: TestCICDNarrowingSelectsTheBuilderFromPublishedFacts                 (red on origin/main, above)
--- PASS: TestCICDImageMatchesForRepositoryNarrowsOnGitSourceRepositories      (#5766)
--- PASS: TestCICDImageMatchesForRepositoryIgnoresOCIRegistryPaths             (#5766)
--- PASS: TestCICDImageMatchesForRepositoryRejectsReferenceOnlyRepository      (#5823)
--- PASS: TestCICDImageMatchesForRepositorySelectsBuildProvenanceRow           (#5823)
--- PASS: TestCICDImageMatchesForRepositoryDoesNotGuardSingleRowDigests        (#5823 bound)
--- PASS: TestCICDImageMatchesForRepositoryNeverSelectsLegacyRows              (no false exact on legacy rows)
--- PASS: TestCICDImageMatchesForRepositoryIgnoresLegacyRowsBesideCurrentOnes  (generations do not contaminate)
--- PASS: TestContainerImageIdentityPayloadPersistsBuildProvenanceRepositoryIDs
--- PASS: TestContainerImageIdentityPayloadEmitsEmptyBuildProvenanceKey
--- PASS: TestBuildCICDImageIdentityIndexReadsBuildProvenance
--- PASS: TestClassifyCICDWorkflowImageEvidenceNarrowsMultipleRowsToExact      (second caller)
--- PASS: TestClassifyCICDWorkflowImageEvidenceStaysAmbiguousForReferenceOnly  (second caller)
--- PASS: TestClassifyCICDWorkflowImageEvidenceStaysAmbiguousForLegacyPayloads (second caller)
--- PASS: TestClassifyCICDWorkflowImageEvidenceFallbackStaysDerived            (second caller)
--- PASS: TestClassifyCICDWorkflowImageEvidenceHandlesNoMatch                  (second caller)
ok  	github.com/eshu-hq/eshu/go/internal/reducer	1.118s
```

`TestCICDImageMatchesForRepositoryRejectsReferenceOnlyRepository` is the
inversion of a test this branch previously added to pin the false positive as
unavoidable. Its failure message named the condition under which it should be
inverted; that condition is now met.

The five `ClassifyCICDWorkflowImageEvidence` tests cover a call site that had no
test file at all. Narrowing was a dead no-op there on `origin/main` for the same
namespace reason, so it always saw the unfiltered match set; it is live now and
can move a decision between `ambiguous`, `derived`, and `exact`, which also
moves `CanonicalWrites`. That transition was previously unasserted.

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
coverage doc are unchanged by this branch, and `rg` finds no reference to
`CICDRunProvenanceEdgeWriter`, `projectCICDRunBuiltFromEdges`, or
`cicdRunBuiltFromRows`. No Cypher statement, graph write, or index changes. The
remaining runtime deltas are one additional map key on a payload the reducer
already writes, and a narrowing predicate that can only ever select a subset of
the matches the caller already had.

No-Observability-Change: no metrics, spans, structured logs, or status fields
are added or altered; `docs/public/observability/telemetry-coverage.md` is untouched by this branch.

## Golden corpus

The B-7 gate covers this surface because reducer publication output changed.
Neither pinned floor asserts the identity payload's key set: the
`list_container_image_identities` floor asserts filtering on
`source_repository_ids` returns at least one row, and the
`list_ci_cd_run_correlations` floor pins `minimum_results: 1` while explicitly
declining to pin an outcome value. The corpus's builder row gains
`build_provenance_repository_ids` through the same `ci.artifact` join that
already gave it `source_repository_ids`, so narrowing selects the same single
row and both floors hold.

Run against this branch at `ba809d158`. Every commit between that gate run and
the reviewed head changes documentation only and touches no runtime path, so the
result applies to the head as pushed:

```
$ COMPOSE_PROJECT_NAME=bpj5823gate4 GATE_COLLECTOR_SETTLE_SECONDS=75 \
  ESHU_POSTGRES_PORT=15496 NEO4J_BOLT_PORT=17696 NEO4J_HTTP_PORT=17496 \
  GATE_API_PORT=18896 GATE_MCP_PORT=18996 \
  bash scripts/verify-golden-corpus-gate.sh

cassette facts landed: 18 credentialed collector sources
summary: 505 pass, 0 required-fail, 2 advisory-warn
=== PASS: B-7 golden corpus gate green (elapsed 155s, budget ceiling 1800s) ===
```

Both advisory warnings are phase-timing only, and neither is a truth assertion:

```
[WARN] phase_collect: observed=75.0s, baseline=20.0s, ceiling=25.0s
[WARN] phase_maintenance_drains: observed=11.0s, baseline=5.0s, ceiling=10.0s
```

`phase_collect` is the collector settle sleep, so its 75s is exactly the
`GATE_COLLECTOR_SETTLE_SECONDS=75` override above, not a pipeline slowdown. The
override was needed because at the default 20s one collector
(`vulnerability-intelligence`) had not written its first commit on this machine;
the gate's own liveness check confirmed the process was still alive rather than
crashed, so this is host contention, not a pipeline defect.
`phase_maintenance_drains` is 1s over its ceiling on a host also running the
review workload.

## Open issues this work produced

- **#5827** — the provenance writer's `MERGE` identity omits `evidence_source`
  and `scope_id`, collapsing same-pair edges across domains and scopes. Live for
  `PUBLISHES` on `main` today and recorded in the B-12 snapshot's rc-164.
  `BUILT_FROM` stays single-owner until it lands.
- **#5828** — `eshu_dp_provenance_edges_total` counts submitted rows as
  `materialized`, including rows whose endpoint node was absent.
- **#5822** — the golden corpus never reaches an `exact` ci_cd_run correlation,
  so the exact path has no deterministic fixture.
