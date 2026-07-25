# #5460 base-image lineage (DERIVED_FROM): cost and correctness evidence

Base-image lineage projects `ContainerImage-[:DERIVED_FROM]->ContainerImage`
from Dockerfile `FROM` evidence, so "does my image inherit CVE-X from its base"
is answerable on the graph. This records the cost budget, the tiering decisions,
and the proof matrix behind it. The policy it extends is
`docs/internal/design/5472-graph-projection-policy.md`.

## The anchoring trap this design had to avoid

A base image reference is extracted from the Dockerfile `file` fact of the
repository whose Dockerfile declares it. That fact carries the same repository
anchor the repository's own built images carry, so a base and a child arrive at
the projection **indistinguishable by repository anchor alone**. Projecting on
that anchor would emit self-loops (an image derived from itself) and
child-to-child edges.

The `file` fact kind is load-bearing and was the source of the one production
bug the golden-corpus gate caught: a Dockerfile is never a `content_entity`
fact (that kind carries per-entity data — functions, classes, k8s resources).
Its parsed `dockerfile_stages` live on the `file` fact. The first cut wired the
extraction and the identity fact filter to `content_entity`, where no fact ever
carries `dockerfile_stages`, so every base was filtered out in SQL and the
feature was inert end to end while every unit test — built on a hand-authored
`content_entity` envelope that production never emits — stayed green. The gate's
rc-167 `count=0` is what surfaced it.

`ContainerImageIdentityDecision.BaseImageForRepositoryIDs` is the separation: a
base is *declared by* a repository, never *built by* it, and the extraction path
deliberately does not populate `SourceRepositoryIDs`, workload ids, or service
ids for a base reference. `TestExtractContainerImageRefsDockerfileBase` asserts a
base is never anchored as a built image of its declaring repository.

## Tiering: exact-only on BOTH endpoints

`DERIVED_FROM` carries `ContainerImage` on both ends and the canonical writer
matches by digest, so #5472 decision 4's exact-only rule binds twice. This is not
only policy conformance: a tag-only base has no `ContainerImage {digest}` node to
attach to, and the CVE-inheritance question is unanswerable unless the base
resolves to the specific digest whose vulnerabilities are known.

## Runtime lineage is the FINAL stage only

An intermediate builder stage does not ship its base OS into the runtime image;
only artifacts an explicit `COPY --from` names cross the stage boundary.
Projecting a builder stage's base would assert CVE inheritance the image does
not carry. The golden fixture pins this negative: its builder stage uses a tag
base that is never observed as a manifest, so it cannot surface as lineage.

## Attribution is conservative, and that is a deliberate coverage trade

Dockerfile evidence names a repository's base but never says which of that
repository's built images came from which Dockerfile. A repository resolving to
more than one distinct base is ambiguous and projects **no** edge. The rejected
alternative — an all-pairs fan-out over every built image and every base — would
write N×M edges where only N are true in a monorepo building several images from
several Dockerfiles. A fabricated inheritance claim is a worse failure than a
missing one, so coverage is the thing given up.

The cost of that choice is real and stated plainly: a multi-image monorepo gets
no base lineage today. `attribution_basis` on every edge is the seam that buys
it back — when CI or SLSA provenance supplies a per-image Dockerfile link, it
lands as a more precise basis value on the same edge type, with no edge-type,
schema, or query change.

Mutation proof that both accuracy rules are load-bearing, not decorative:

| Mutation | Result |
| --- | --- |
| Relax the single-base rule (`len(digests) != 1` → `< 1`) | Emits a fabricated `sha256:base2` edge; `two distinct bases in one repository project nothing` fails |
| Remove the self-loop guard | Emits `sha256:same → sha256:same`; `an image that is its own base projects no self-loop` fails |

## Write-volume / cost-budget evidence

### B-9 (#3802) row-builder handler budget

Benchmark Evidence: `go test ./internal/reducer -bench
'BenchmarkContainerImage(BuiltFrom|DerivedFrom)Rows' -run '^$' -count=5`

Medians over 5 samples on the development machine, owner-scoped builder (one
intent's worst case: owning repo with 2,500 children + 1 base, plus 5,000
cross-scope noise decisions the builder skips):

| Benchmark | Local median ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `BenchmarkContainerImageBuiltFromRows` (5000 rows) | 557,348 | 1,960,962 | 25,001 |
| `BenchmarkContainerImageDerivedFromRows` (2500 rows from 7501 decisions) | 497,007 | 1,782,402 | 12,534 |

Owner-scoping made `DERIVED_FROM` cheaper than the earlier unscoped builder and
than `BUILT_FROM`: it finds the owning repository's single base in one pass and
emits its children in a second, skipping all cross-scope noise, rather than
grouping every repository's bases.

The committed budget row is **derived, not captured on the enforcement runner**,
and that is stated rather than implied. `BenchmarkContainerImageBuiltFromRows` is
a co-located benchmark over the same decision type with a committed baseline of
583,584 ns/op; this machine measures it at 557,348, a factor of 0.9550. Dividing
the local `DERIVED_FROM` median by that factor gives a normalized baseline of
520,403 ns/op, and the file's standard 1.5× headroom gives a budget of
780,604 ns/op:

```
BenchmarkContainerImageDerivedFromRows	780604	520403
```

This should be refreshed by `scripts/refresh-reducer-handler-budgets.sh` on the
enforcement runner at the next budget refresh, the same as every other row. The
gate runs advisory (`REDUCER_PERF_ENFORCE=false`) today, so a derived row cannot
produce a false red.

### Graph write cost

Exact-only promotion on both endpoints keeps the projected set small: only
repositories with an exact-digest built image AND exactly one exact-digest
declared base contribute at all. The write is a two-MATCH-MERGE, so a row whose
child or base node is absent is a no-op rather than a fabricated node, and the
batch size is the writer default (500).

### Retraction cost

Retract is one statement per scope+evidence_source, dispatched as a sequential
auto-commit `Execute` — never `ExecuteGroup`, because a grouped DELETE
under-applies on the pinned NornicDB v1.1.11
(`docs/public/reference/nornicdb-pitfalls.md`). It runs unconditionally ahead of
the row check so a generation that stops being attributable — a second Dockerfile
added, making the repository ambiguous — still clears the prior edge.

`DERIVED_FROM` has its own `evidence_source`
(`reducer/container-image-base-image`), distinct from the `BUILT_FROM` source the
same handler writes, so neither domain's retract can touch the other's edges. A
test asserts the two constants can never collapse to the same value.

### Owner-scoped projection (the root fix) + sequential dispatch (defense)

The base reference reaches **every** identity intent through the active
cross-scope fact load, so the first cut built and wrote the same DERIVED_FROM
edge from every intent that could see both endpoints — including OCI-registry
and ECS scopes that merely observe the images. That is a correctness defect, not
just waste: each writing intent stamps its own `scope_id`, so the edge's
retract-first-per-generation owner becomes whichever unrelated intent wrote
last, and a later delete of the declaring repository can no longer retract it.

The root fix scopes projection to the child's owning repository:
`containerImageDerivedFromRows` takes the intent's repository
(`repositoryIDFromReducerScope(intent.ScopeID)`) and builds rows only for that
repository. A non-repository scope (OCI/CI/cloud) owns no Dockerfile and
projects nothing. The edge now has exactly one deterministic owner — the
repository whose Dockerfile declares the base — and the redundant cross-scope
writes are gone. `TestProjectContainerImageDerivedFromEdgesNonRepoScopeWritesNothing`
pins that an OCI-scope intent seeing both endpoints still writes nothing.

The DERIVED_FROM **write** additionally dispatches through sequential auto-commit
`Execute`, not `ExecuteGroup` — unlike `BUILT_FROM`/`PUBLISHES`, which group —
as defense in depth for the owning intent itself: its base or child
ContainerImage node (written by the OCI-registry projector in another scope) may
not be committed when the identity intent runs. The golden-corpus gate first
caught the unscoped write against a live backend: the
`container_image_identity` intent for an unrelated ECS scope dead-lettered with

```
projection_bug: write container image derived_from provenance edges:
Neo4jError: Neo.ClientError.Statement.SyntaxError
(UNWIND MERGE chain relationship update failed: not found)
```

`DERIVED_FROM` is the only one of the three provenance edges with the same node
label on both endpoints (`ContainerImage`→`ContainerImage`). That shape selects
NornicDB's `UnwindMergeChain` fast-path, which under a managed transaction
throws when one endpoint node is not yet committed, instead of the
missing-endpoint no-op the #5472 contract requires — so an intent whose base or
child `ContainerImage` node lags the projection dead-letters the whole intent.
Reproduced on the live NornicDB: the identical MERGE run as an auto-commit
`Execute` no-ops cleanly on a missing endpoint (`count=0`, no error), and the
edge is re-projected on a later generation once both nodes exist. This is not
serialization to hide a race — the write is an idempotent MERGE, and auto-commit
is the dispatch mode that honors the no-op-on-missing contract on this backend,
the same reason the retract path already uses it. The failure was
intermittent (it depends on projection ordering across intents), which is
exactly why a unit test could not have surfaced it and the live gate did.

## Build provenance is required on the child side (codex review P1-a)

`SourceRepositoryIDs` cannot decide which images a repository BUILT. A
digest-pinned third-party image referenced by the repository's own Kubernetes
manifest arrives on that repository's `content_entity` fact and inherits its
scope anchor (`containerImageSourceRepositoryIDs` reads
`repositoryIDFromReducerScope(envelope.ScopeID)`), so it lands in
`SourceRepositoryIDs` exactly like a built image. The first cut gated the child
side on that field, which made a co-deployed `postgres` inherit the repository's
Dockerfile base — a fabricated CVE-inheritance claim about an unrelated image.

`ContainerImageIdentityDecision.BuildProvenanceRepositoryIDs` is the fix. It is
populated only by evidence that the repository actually produced the digest:

- an OCI config source label the image itself carries
  (`org.opencontainers.image.source`, `container_image_identity_provenance.go`), or
- a CI run that reported producing this artifact digest
  (`ci.run.repository_id`, `container_image_identity_typed_evidence.go`).

The generic scope/workload anchoring that fills `SourceRepositoryIDs` never
contributes. `BUILT_FROM` still uses the looser field and carries the same
false-positive class; that is tracked in **#5796** rather than silently widened
here.

### The latent matcher defect this uncovered

Gating on build provenance turned rc-167 red (`count=0`), and the cause was not
the gate: `matchOCIConfigSourceRepository` counted raw repository-fact matches
and required exactly one. A repository legitimately carries several active
`repository` facts (more than one scope or collector observing it), so a second
fact made an unambiguous source label look ambiguous and the whole
OCI-source-label build-evidence tier silently disabled itself in any real
corpus — which is why a single-repo unit test passed while the live corpus
projected nothing. The guard now dedupes by repository identity before applying
the exactly-one rule; two DISTINCT repositories claiming one remote still
resolve to neither. This also repairs the pre-existing
`oci_config_source_label_with_digest` identity tier.

Proof: `TestBuildProvenanceSurvivesDuplicateRepositoryFacts` (fails before),
`TestTwoDifferentRepositoriesClaimingOneRemoteStayAmbiguous` (the ambiguity
guard), and `TestContainerImageDerivedFromRowsCorpusShape` — a unit-level twin of
rc-167 proving provenance survives the merge of an OCI source label with a
Kubernetes reference for the same digest, so this regression is catchable without
a Docker stack.

The golden fixture's child image therefore carries a real
`org.opencontainers.image.source` label naming the corpus-synthesized remote
(`https://github.com/acme/container-base-lineage`, repo id
`repository:r_86b8b612`). rc-167 now passes on genuine build evidence rather than
on the deploy-reference inference it started with.

## The enqueue half: Dockerfile facts must trigger the domain (codex review P1-b)

The reducer extracts a Dockerfile base only inside a `container_image_identity`
intent, and the intent builder's candidate kinds and trigger switch omitted
`file`. A repository that added or edited its Dockerfile with no new image
evidence therefore enqueued nothing: the lineage never projected, and a changed
or deleted base left the prior `DERIVED_FROM` edge stale indefinitely. Loading
active `file` facts cross-scope does not itself enqueue a repository-scoped
intent.

`dockerfileIdentityTriggerFile` adds a narrow `file` trigger. Two recognizers:
parsed `dockerfile_stages` is the precise signal for an added or edited
Dockerfile, while a REMOVED Dockerfile arrives as a tombstone that can carry no
`parsed_file_data`, so the declared language and file name keep the removal path
triggering and let the retract-first pass clear the stale edge. Every generation
carries `file` facts, so an arbitrary source file must never enqueue — pinned by
`TestBuildProjectionDoesNotQueueContainerImageIdentityForNonDockerfileFile`
alongside the failing-then-green
`TestBuildProjectionQueuesContainerImageIdentityForDockerfileBaseImage` (0
intents before, 1 after) and
`TestContainerImageIdentityTriggerFactDockerfileRemoval`.

## Proof matrix

| Case | Proof |
| --- | --- |
| Digest `FROM` | `TestDockerfileRuntimeBaseImageRef/single stage digest FROM is exact` |
| Tag `FROM` | `.../single stage tag FROM rejoins image and tag` |
| Multi-stage (builder ignored) | `.../multi-stage returns final stage base, not the builder base` |
| Alias `FROM x` | `.../final stage aliasing a prior stage resolves transitively` |
| ARG-parameterized | `.../ARG-parameterized base stays unresolved` + `TestExtractContainerImageRefsDockerfileBaseUnresolved` |
| `scratch` base | `.../scratch base is unresolved` |
| Ambiguous repository | `TestContainerImageDerivedFromRows/two distinct bases in one repository project nothing` |
| Non-exact endpoint | `.../a non-exact base projects nothing`, `.../a non-exact child projects nothing` |
| Cross-repository leakage | `.../a base in one repository never attaches to another repository's child` |
| Self-loop | `.../an image that is its own base projects no self-loop` |
| Extraction anchoring | `TestExtractContainerImageRefsDockerfileBase` |
| End-to-end classification | `TestBuildContainerImageIdentityDecisionsDockerfileBase` |
| Retract-first / stale clear | `TestProjectContainerImageDerivedFromEdgesRetractsEvenWhenNoRowsToWrite` |
| Owner-scoped projection (non-repo scope writes nothing) | `TestProjectContainerImageDerivedFromEdgesNonRepoScopeWritesNothing` |
| Referenced-not-built child (no fabricated edge) | `TestContainerImageDerivedFromRows/an_image_the_repository_only_references_projects_nothing` |
| Built child beside a referenced one | `.../only_the_built_image_is_a_child_when_a_referenced_image_sits_beside_it` |
| Build provenance from an OCI source label | `TestBuildContainerImageIdentityDecisionsBuildProvenanceFromOCISourceLabel` |
| k8s reference is not build provenance | `TestBuildContainerImageIdentityDecisionsKubernetesReferenceIsNotBuildProvenance` |
| Duplicate repository facts still match | `TestBuildProvenanceSurvivesDuplicateRepositoryFacts` |
| Two repositories claiming one remote stay ambiguous | `TestTwoDifferentRepositoriesClaimingOneRemoteStayAmbiguous` |
| rc-167 unit twin (label + k8s merge) | `TestContainerImageDerivedFromRowsCorpusShape` |
| Dockerfile-only generation enqueues the intent | `TestBuildProjectionQueuesContainerImageIdentityForDockerfileBaseImage` |
| Dockerfile removal triggers the retract | `TestContainerImageIdentityTriggerFactDockerfileRemoval` |
| Non-Dockerfile file never enqueues | `TestBuildProjectionDoesNotQueueContainerImageIdentityForNonDockerfileFile` |
| Writer join shape | `TestProvenanceEdgeWriterWriteDerivedFromMatchesBothEndpointsByDigest` |
| Retract dispatch | `TestProvenanceEdgeWriterRetractDerivedFromUsesSequentialExecuteNeverGroup` |
| Blank-scope retract is a no-op | `TestProvenanceEdgeWriterDerivedFromEmptyInputsAreNoOps` |
| End-to-end graph truth | B-12 `rc-167` |

## Observability

Observability Evidence: `eshu_dp_provenance_edges_total` gains a new `domain`
label value, `reducer/container-image-base-image`, alongside the existing
`reducer/container-image-identity` and package domains. No new instrument is
registered, so the telemetry contract, coverage doc, and operator dashboard are
unchanged; the projection is visible through the same counter an operator
already uses for provenance-edge volume, split by domain.

## B-12 golden-corpus proof (rc-167)

The existing `BUILT_FROM` proof (`rc-165`) could not be extended to cover this.
Its child image is anchored to `repository:r_69256c06`, which the `cicdrun`
cassette declares but which has no fixture on disk — so there is no Dockerfile to
pair with it.

`rc-167` is therefore driven by a purpose-built `container-base-lineage` fixture
rather than by perturbing an existing one. Its Dockerfile pins the final stage to
an observed digest and its Deployment runs a second observed digest, so the base
and the built image resolve as two `exact_digest` decisions anchored to the same
repository. Adding a fixture rather than editing one keeps the blast radius to
additions: the snapshot never referenced any existing fixture's image or base
strings, and the `Repository` count tolerance (15..30, 24 before this change)
absorbs one more repo.

The assertion pins `attribution_basis=repository_single_base` as well as
`source_tool=oci`, so a later, more precise CI/SLSA attribution cannot silently
satisfy an assertion written for Dockerfile-only inference. Since #4596 the gate
single-sources its blocking set from the snapshot's own `required_correlations`
ids, so `rc-167` is blocking on add with no second list to edit.

## Known gap (tracked, not silently carried)

`retractable_edge:DERIVED_FROM` is registered as retractable but has no
`delta_tombstone` replay scenario, so the generated replay-coverage dashboard
lists it as uncovered. It joins `BUILT_FROM` and `PUBLISHES`, which #5457 left in
the same state and which follow-up **#5712** tracks. Closing it needs a live
NornicDB write+retract scenario in `go/internal/replay/offlinetier/`, the same
shape as the repo-dependency retract test, and belongs with that follow-up rather
than being claimed here.
