# replay/offlinetier Cassette Fact-Kind Exemption (#4866)

Status: Accepted.

Parent epic: [#4783](https://github.com/eshu-hq/eshu/issues/4783) (W2). Carved
out of [#4797](https://github.com/eshu-hq/eshu/issues/4797) (W2d) during
implementation.

## 1. Context

#4797 (W2d) moves `relationships/` and `replay/` payload reads from raw
`map[string]any` access onto the `factschema.Decode*` typed-decode seam. Most
of that package's reads have a real collector producer and a corresponding
`sdk/go/factschema` family, so the swap is a straight consumer migration.

`go/internal/replay/offlinetier` does not fit that shape. Its
`rowFromPayload` helpers in `materialization.go` and the generation-diffing
logic in `delta.go` read fact envelopes drawn from a committed test cassette
(`testdata/cassettes/replayoffline/nested-directory-tree.json`), not from a
live collector run. The fact kinds those envelopes carry
(`git.repository`, `git.directory`, `git.file`, `git.gitlab_pipeline`, and
`git.gitlab_job`) have no real collector that emits them and no
`sdk/go/factschema` family to decode against. `materialization.go`'s own doc
comment (lines 37-41) states this is intentional:

> Cassette fact kinds the offline tier maps into a canonical materialization.
> These are the durable fact-kind labels carried by the committed
> nested-directory-tree cassette; the cassette format is collector-agnostic,
> so the tier owns the fact-kind -> materialization-row mapping here rather
> than pulling in the full git collector.

The R-5 offline replay gate (epic #4102, issue #4107;
`go/internal/replay/offlinetier/AGENTS.md`) exists specifically to drive the
production `storage/cypher.CanonicalNodeWriter` against a **real** NornicDB
without Docker Compose and without depending on the full git collector. That
decoupling is the point of the tier, not an oversight. Pulling in the git
collector to get typed payloads would reintroduce the dependency weight the
tier was built to avoid, for fact kinds that only exist to drive this one
cassette.

So routing `replay/offlinetier` through `factschema.Decode*` is not a
consumer migration like the rest of W2d. It would require either inventing
typed structs for fact kinds with no real producer, or redesigning the tier
to replay real collector fact kinds. Both are larger than #4797's scope and
both are in tension with the package's stated design. This ADR records the
disposition instead of forking either change inline into #4797.

## 2. Decision

**Exemption.** `replay/offlinetier`'s cassette-to-materialization mapping
stays a raw, collector-agnostic payload read. It is not migrated to
`factschema.Decode*`, and the future W3a raw-`.Payload` ratchet gate
(#4800) must carry a named allowlist entry for it rather than flag it.

This corresponds to option 3 of the three dispositions #4866 posed: document
that the tier's cassette mapping is deliberately raw and collector-agnostic,
and exclude it from the ratchet gate by name.

## 3. Rejected alternatives

### 3.1 Typed cassette structs (option 1)

Introducing a small typed model for the five pseudo fact kinds, either in a
contracts-adjacent module or a tier-local package, was considered.

Rejected because these fact kinds are not real. No collector in this repo,
in-tree or external, emits `git.repository`, `git.directory`, `git.file`,
`git.gitlab_pipeline`, or `git.gitlab_job`; they exist solely as labels
inside one committed cassette. Adding structs and a registry entry for
kinds with zero production producers would create schema ceremony
(registry membership, generated JSON Schema, version admission) for a shape
that only one test cassette will ever populate. It would also blur the
registry's role as the source of truth for real collector-to-reducer
contracts (see [Fact IDL Decision](4574-fact-idl-decision.md)) with a
label that was never meant to reach a live collector or reducer.

### 3.2 Redesign the tier to replay real collector fact-kinds (option 2)

Changing the cassette (and the tier's mapping logic) to emit genuine
collector fact kinds, such as `code.file` and `code.directory`, so the tier
shares the same decode seams as production was also considered.

Rejected because it directly reverses the tier's documented design goal.
`materialization.go`'s doc comment and the AGENTS.md scope both frame the
current cassette format as intentional: the tier owns its own
fact-kind-to-row mapping specifically so it does not have to pull in the
full git collector to exercise the NornicDB phase-group write path. Doing
this work would be a larger redesign of a working, narrowly scoped gate, and
for no accuracy gain: the tier does not claim to validate collector fact
shapes, only the canonical projection writer against a real graph backend.

## 4. Exempt fact kinds

The following `factKind*` constants (`go/internal/replay/offlinetier/materialization.go:42-48`)
are the synthetic, cassette-only pseudo-kinds this exemption covers. None of
them has an `sdk/go/factschema` family or a real collector producer.

| Constant | Value | Row mapping |
| --- | --- | --- |
| `factKindRepository` | `git.repository` | `repositoryRowFromPayload` -> `projector.RepositoryRow` |
| `factKindDirectory` | `git.directory` | `directoryRowFromPayload` -> `projector.DirectoryRow` |
| `factKindFile` | `git.file` | `fileRowFromPayload` -> `projector.FileRow` |
| `factKindGitlabPipeline` | `git.gitlab_pipeline` | `gitlabPipelineEntityRowFromPayload` -> `projector.EntityRow` |
| `factKindGitlabJob` | `git.gitlab_job` | `gitlabJobEntityRowFromPayload` -> `projector.EntityRow` |

`delta.go`'s generation-diffing logic (`DeltaMaterializationFromGenerations`)
builds on the same `MaterializationFromGeneration` seam and does not add any
further raw payload reads beyond these five kinds.

## 5. W3a ratchet gate requirement

#4800 (W3a) has not landed yet; this ADR does not build that gate. When W3a
adds the raw-`.Payload` convention ratchet (the CI check that fails on raw
`env.Payload[...]`/`payload[...]` access outside `factschema_decode*.go`),
it MUST carry a named allowlist entry for
`go/internal/replay/offlinetier/*.go` covering the five row-builder
functions in section 4, rather than requiring this package to route through
`factschema.Decode*`.

The allowlist entry MUST be scoped to this package and MUST NOT be a
wildcard exemption that would also cover a future non-cassette read added to
`replay/offlinetier`. If a later change to this package reads a real,
`sdk/go/factschema`-governed fact kind, that read is not covered by this
exemption and must go through the typed decode seam like any other W2
consumer.

## 6. Consequences

- `replay/offlinetier`'s cassette mapping keeps its current raw
  `map[string]any` payload reads. No struct, schema, or registry entry is
  authored for the five pseudo fact kinds.
- #4800 (W3a) must add the named allowlist entry described in section 5 when
  it builds the ratchet gate; omitting it would make the gate red for a
  package this ADR has explicitly exempted.
- This ADR does not touch the R-5 offline replay gate's live-NornicDB
  invariants (`go/internal/replay/offlinetier/AGENTS.md`); those are out of
  scope for #4866 and unaffected by this decision.
- If `replay/offlinetier` is ever brought into the typed-decode fold, by
  adopting option 1 or option 2 above (most likely because a real collector
  producer starts emitting one of these fact kinds), that change supersedes
  this ADR and must update or remove the W3a allowlist entry in the same PR.
