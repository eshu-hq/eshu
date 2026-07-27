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

The defect is not introduced here; the B-12 snapshot already documents the
mechanic. rc-164 states it plainly for `PUBLISHES` — *"Cypher MERGE identity is
the pattern only; evidence_source/evidence_kinds are SET, not MERGE keys"* — and
then explains why it is harmless there: *"projectPackageProvenanceEdges always
writes ownership rows before publication rows within one Handle() invocation, so
publication deterministically wins"*, and *"No other reducer domain writes
PUBLISHES (unlike BUILT_FROM, rc-165)"*. One writer, deterministic ordering, no
cross-domain retract.

`BUILT_FROM` is the case that does not hold. rc-165 records it as *"a SHARED edge
type with #5428"* and isolates its own assertion on
`evidence_kinds=[CONTAINER_IMAGE_IDENTITY_EXACT_DIGEST]` so that it *"can never be
satisfied by #5428's edges alone"*. That isolation is exactly what the MERGE
identity cannot deliver: with two domains writing the same (image, repository)
pair there is one edge, and `evidence_kinds` is whichever write landed last. The
snapshot's own note therefore describes an isolation that would not survive this
projection landing. Filed as **#5827**.

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

The producer still always writes the key, even when empty. That no longer drives
any consumer decision here — with the fallback gone, an absent key and an
explicitly empty one decode identically to an empty set and are treated the same
— but it keeps the published payload self-describing for a reader inspecting a
fact directly.

**Producer asymmetry not closed here — closed later, in #5426.**
`applySLSADigestRevision` appends its digest anchor's repositories to
`BuildProvenanceRepositoryIDs`; `applyCIRunDigestRevision` appended them only to
`SourceRepositoryIDs`. A competing decision that wins the upsert therefore
carried its genuine builder in the broad field alone — the same gap class #5808
fixed one path over, and a real defect.

It was implemented on this branch and then reverted. The reasoning at the time:
`BuildProvenanceRepositoryIDs` is the sole gate for two live graph writers
(`containerImageBuiltFromRows`, `containerImageDerivedFromRows`), so widening it
was expected to make MORE SCOPES emit the same `(digest, repository)` pair — and
since `projectContainerImageBuiltFromEdges` retracts per
`(scope_id, generation_id, evidence_source)` while the writer's `MERGE` matches
on `(start, end, type)` alone, two scopes emitting one pair means one scope's
retract deletes an edge the other still supports. That is #5827, and the
conclusion drawn was that landing the fix would make the graph worse today to
make a correlation sharper. It was filed as **#5829, blocked on #5827**.

That conclusion was reached by reasoning about the projection, not by measuring
it, and the measurement contradicts it. **#5426 landed the fix.** Two things were
checked before it did, and both are recorded in
[#5426 — what the golden corpus asserts about environment_evidence](5426-golden-corpus-coverage.md#why-5829s-stated-blocker-does-not-apply):

- The widening is INTRA-scope, not cross-scope. `ci.run`/`ci.artifact` are
  loaded scope-locally by `container_image_identity.go` and are absent from
  every arm of `identityFactFilterSQL`, the cross-scope active-fact filter
  (`go/internal/storage/postgres/facts_active_container_image_identity.go`).
  Only the CI scope can confer this provenance, so no second scope gains a
  competing claim on the pair.
- The emitted row SET is unchanged. On the corpus-shaped fixture the projection
  went from 1 row to 2 IDENTICAL rows, both
  `(sha256:abcdef...ab, repository:r_69256c06)` — no new `(start, end, type)`
  pair, so #5827's collapse is not reachable from this change. The duplicate is
  now deduped away inside `containerImageBuiltFromRows`.

#5827 remains open on its own merits; it was simply never a blocker for this
particular widening. The B-12 snapshot's `list_supply_chain_impact_findings`
query-shape description still records the multi-row shape that motivated the
concern — as of #5426 it reads "this digest carries 16 rows in the live corpus
that disagree on `source_repository_ids`". It read 11 when this section was
first written; 16 is what a `--keep` gate run measured for #5426. The count
moved, the shape did not.

Until #5426 landed, the consequence of leaving the asymmetry open was bounded
and conservative in the graph — a competing-decision row whose builder reached
only `source_repository_ids` narrowed to nothing, so the caller kept the
unfiltered set and the correlation landed `ambiguous`, never a false `exact`.
What that framing missed is that `ambiguous` is not free downstream:
`matchingSupplyChainDeployments` rejects a `provenance_only` correlation
outright, so the deployment's environment never reached a supply-chain impact
finding at all. That is the defect #5426 was chasing when it re-opened this.

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
--- PASS: TestBuildCICDImageIdentityIndexReadsBuildProvenance           (decode seam)
--- PASS: TestCICDImageMatchesForRepositoryDoesNotGuardSingleRowDigests (#5823 bound)
--- PASS: TestCICDImageMatchesForRepositoryIgnoresLegacyRowsBesideCurrentOnes(generations do not contaminate)
--- PASS: TestCICDImageMatchesForRepositoryIgnoresOCIRegistryPaths      (#5766)
--- PASS: TestCICDImageMatchesForRepositoryNarrowsOnGitSourceRepositories(#5766)
--- PASS: TestCICDImageMatchesForRepositoryNeverSelectsLegacyRows       (no false exact on legacy rows)
--- PASS: TestCICDImageMatchesForRepositoryRejectsReferenceOnlyRepository(#5823)
--- PASS: TestCICDImageMatchesForRepositorySelectsBuildProvenanceRow    (#5823)
--- PASS: TestCICDNarrowingSelectsTheBuilderFromPublishedFacts          (red on origin/main, above)
--- PASS: TestClassifyCICDWorkflowImageEvidenceDemotesReusableWorkflowInput(produced-image gate, RED without the change)
--- PASS: TestClassifyCICDWorkflowImageEvidenceFailsOpenForUnknownCommandKind(produced-image guard, green either way by design)
--- PASS: TestClassifyCICDWorkflowImageEvidenceFallbackStaysDerived     (second caller)
--- PASS: TestClassifyCICDWorkflowImageEvidenceHandlesNoMatch           (second caller)
--- PASS: TestClassifyCICDWorkflowImageEvidenceKeepsExactForProducedImages(produced-image guard, green either way by design)
--- PASS: TestClassifyCICDWorkflowImageEvidenceNarrowsMultipleRowsToExact(second caller)
--- PASS: TestClassifyCICDWorkflowImageEvidencePrefersProducedOverInput (produced-image gate, RED without the change)
--- PASS: TestClassifyCICDWorkflowImageEvidenceStaysAmbiguousForLegacyPayloads(second caller)
--- PASS: TestClassifyCICDWorkflowImageEvidenceStaysAmbiguousForReferenceOnly(second caller)
--- PASS: TestContainerImageIdentityPayloadEmitsEmptyBuildProvenanceKey (payload contract this join reads)
--- PASS: TestContainerImageIdentityPayloadPersistsBuildProvenanceRepositoryIDs(payload contract this join reads)
ok  	github.com/eshu-hq/eshu/go/internal/reducer	1.088s
```

`TestCICDImageMatchesForRepositoryRejectsReferenceOnlyRepository` is the
inversion of a test this branch previously added to pin the false positive as
unavoidable. Its failure message named the condition under which it should be
inverted; that condition is now met.

The nine `ClassifyCICDWorkflowImageEvidence` tests cover a call site that had no
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

## Produced-image evidence, not merely referenced

`classifyCICDWorkflowImageEvidence` promoted a commit-matched single-identity
resolution to `exact` without consulting `command_kind`. That is wrong for one
extracted kind: `workflowimage.evidenceFromReusableWorkflow` stamps
`reusable_workflow_input` on a `jobs.<job>.with.{image,image_ref,container_image}`
value, which names an image the workflow CONSUMES — typically a scanner, base, or
tooling image — not one it produced. Calling that correlation `exact` asserts
production that never happened.

With the projection withdrawn no `BUILT_FROM` edge is asserted, but the mislabel
still costs something concrete. `incidentCICDPromotionCandidates`
(`go/internal/query/incident_context_build_commit.go`) prefers a digest `exact`
match over every other candidate, and `incidentCICDTruthLabel` then stamps the
incident's build/deploy and commit slots as exact truth. A false `exact` on a
scanner image can take build attribution away from a genuine derived candidate.

Input-only evidence is now capped at `derived` with a reason that says why.
Produced kinds (`docker_build`, `docker_buildx`, `docker_push`, `docker_tag`)
keep the exact promotion. The classifier considers produced-image evidence
before input-only evidence, so a run that both consumes a scanner image and
builds its own is decided by the image it built rather than by slice order.

This is a deny-list rather than an allow-list. `command_kind` is an optional
free-string field, so an absent kind, an unknown kind, or one a future collector
adds all keep the pre-existing behaviour; only the kind proven to be input-only
is denied. A reducer that predates a new produced-image kind therefore degrades
nothing.

Two of the four produced-image tests are genuine red-then-green regressions:
reverting the classifier change leaves
`TestClassifyCICDWorkflowImageEvidenceDemotesReusableWorkflowInput` failing with
`Outcome = "exact", want derived` and
`TestClassifyCICDWorkflowImageEvidencePrefersProducedOverInput` failing with
`ImageRef = "ghcr.io/eshu-hq/scanner:v1", want the built image`. The other two —
`KeepsExactForProducedImages` and `FailsOpenForUnknownCommandKind` — pass before
and after by design; they are behaviour-preservation guards against an
over-broad deny-list, not regressions, and are listed as such rather than
counted as proof of the fix.

The golden gate cannot verify any of this: the corpus contains zero
`ci.workflow_image_evidence` facts, so the entire workflow-image classifier path
is gate-invisible. Unit-tier proof is the honest proof here, and the coverage
gap is filed as **#5830**.

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

Run against this branch at `e186a3e91`. Every commit between that gate run and
the reviewed head changes documentation only and touches no runtime path, so the
result applies to the head as pushed:

```
$ COMPOSE_PROJECT_NAME=bpj5823gate6 GATE_COLLECTOR_SETTLE_SECONDS=75 \
  ESHU_POSTGRES_PORT=15494 NEO4J_BOLT_PORT=17694 NEO4J_HTTP_PORT=17494 \
  GATE_API_PORT=18894 GATE_MCP_PORT=18994 \
  bash scripts/verify-golden-corpus-gate.sh

cassette facts landed: 18 credentialed collector sources
summary: 505 pass, 0 required-fail, 2 advisory-warn
=== PASS: B-7 golden corpus gate green (elapsed 158s, budget ceiling 1800s) ===
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
- **#5830** — the corpus contains no `ci.workflow_image_evidence` at all, so the
  whole workflow-image classifier is invisible to the B-7 gate.
