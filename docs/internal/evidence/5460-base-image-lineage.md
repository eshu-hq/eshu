# #5460 base-image lineage (DERIVED_FROM): cost and correctness evidence

Base-image lineage projects `ContainerImage-[:DERIVED_FROM]->ContainerImage`
from Dockerfile `FROM` evidence, so "does my image inherit CVE-X from its base"
is answerable on the graph. This records the cost budget, the tiering decisions,
and the proof matrix behind it. The policy it extends is
`docs/internal/design/5472-graph-projection-policy.md`.

## The anchoring trap this design had to avoid

A base image reference is extracted from the `content_entity` envelope of the
repository whose Dockerfile declares it. That envelope carries the same
repository anchor the repository's own built images carry, so a base and a child
arrive at the projection **indistinguishable by repository anchor alone**.
Projecting on that anchor would emit self-loops (an image derived from itself)
and child-to-child edges.

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

Medians over 5 samples on the development machine:

| Benchmark | Local median ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
| `BenchmarkContainerImageBuiltFromRows` (5000 rows) | 696,658 | 1,960,962 | 25,001 |
| `BenchmarkContainerImageDerivedFromRows` (2500 rows from 5000 decisions) | 1,285,812 | 2,641,374 | 22,556 |

`DERIVED_FROM` is the more expensive builder by design: it makes a first pass to
group declared bases per repository and reject ambiguous repositories before
emitting any row, which `BUILT_FROM` does not need.

The committed budget row is **derived, not captured on the enforcement runner**,
and that is stated rather than implied. `BenchmarkContainerImageBuiltFromRows` is
a co-located benchmark over the same decision type with a committed baseline of
583,584 ns/op; this machine measures it at 696,658, a factor of 1.1938. Applying
that factor to the local `DERIVED_FROM` median gives a normalized baseline of
1,077,113 ns/op, and the file's standard 1.5× headroom gives a budget of
1,615,670 ns/op:

```
BenchmarkContainerImageDerivedFromRows	1615670	1077113
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
| Writer join shape | `TestProvenanceEdgeWriterWriteDerivedFromMatchesBothEndpointsByDigest` |
| Retract dispatch | `TestProvenanceEdgeWriterRetractDerivedFromUsesSequentialExecuteNeverGroup` |
| Blank-scope retract is a no-op | `TestProvenanceEdgeWriterDerivedFromEmptyInputsAreNoOps` |
| End-to-end graph truth | B-12 `rc-166` |

## Observability

Observability Evidence: `eshu_dp_provenance_edges_total` gains a new `domain`
label value, `reducer/container-image-base-image`, alongside the existing
`reducer/container-image-identity` and package domains. No new instrument is
registered, so the telemetry contract, coverage doc, and operator dashboard are
unchanged; the projection is visible through the same counter an operator
already uses for provenance-edge volume, split by domain.

## B-12 golden-corpus proof (rc-166)

The existing `BUILT_FROM` proof (`rc-165`) could not be extended to cover this.
Its child image is anchored to `repository:r_69256c06`, which the `cicdrun`
cassette declares but which has no fixture on disk — so there is no Dockerfile to
pair with it.

`rc-166` is therefore driven by a purpose-built `container-base-lineage` fixture
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
ids, so `rc-166` is blocking on add with no second list to edit.

## Known gap (tracked, not silently carried)

`retractable_edge:DERIVED_FROM` is registered as retractable but has no
`delta_tombstone` replay scenario, so the generated replay-coverage dashboard
lists it as uncovered. It joins `BUILT_FROM` and `PUBLISHES`, which #5457 left in
the same state and which follow-up **#5712** tracks. Closing it needs a live
NornicDB write+retract scenario in `go/internal/replay/offlinetier/`, the same
shape as the repo-dependency retract test, and belongs with that follow-up rather
than being claimed here.
