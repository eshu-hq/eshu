# AGENTS.md - internal/graphschemacompat guidance for LLM assistants

## Read first

1. `README.md` - package purpose and compatibility contract.
2. `compatibility.go` - marker query and startup refusal logic.
3. `write_fence.go` - the same decision re-checked on the write path of a
   process that is already running.
4. `go/internal/graph/schema_application.go` - fingerprint and compatibility
   source.
5. `go/cmd/bootstrap-data-plane/main.go` - normal marker writer.
6. `go/cmd/bootstrap-index/graph_schema.go` - direct bootstrap-index
   marker-missing initializer.

## Invariants this package enforces

- **Postgres-only startup check** - do not add live graph schema inspection to
  writer startup. Graph object inspection belongs to schema bootstrap adoption.
  Direct bootstrap-index missing-marker recovery applies strict DDL; it does
  not inspect graph metadata.
- **Latest marker wins** - compatibility is evaluated against the latest marker
  row for the selected backend, not any historical row.
- **Explicit compatibility** - only an exact fingerprint match or a latest row
  listing the writer's fingerprint in `compatible_fingerprints` may pass.
- **A read failure is not a refusal** - `WriteFence` stops a write only on a
  marker it read successfully that says no. Do not make it fail closed on a
  query error: every fenced writer would stop together on one Postgres blip,
  and the startup check already admitted the process.

## Common changes and how to scope them

- **Add additive compatibility** - update the compatibility map in
  `internal/graph/schema_application.go`, add a test that the older writer
  fingerprint passes against the newer marker, and document the rolling-upgrade
  rule.
- **Change marker SQL** - update `compatibility.go`, the Postgres schema DDL,
  and `cmd/bootstrap-data-plane` marker writes together.

## Failure modes and how to debug

- Startup refusal with `graph schema marker missing` means
  `eshu-bootstrap-data-plane` has not completed against the same Postgres
  database. Direct `bootstrap-index` may recover by applying strict graph schema
  and writing the marker before it opens the projection writer.
- Startup refusal with `graph schema incompatible` means the latest schema
  marker is not exact and did not declare the writer fingerprint compatible.
- The same message on a *retrying* projector queue row, from a process that
  started fine, is the `WriteFence` refusing a write mid-flight: a schema
  application landed underneath this pod. The work stays queued for the pod that
  replaces it.

## What the write fence actually covers

Narrower than "canonical writers", so check this list before an identity cutover
leans on it. Only `CanonicalNodeWriter` accepts a fence
(`CanonicalNodeWriter.WithSchemaWriteFence`), and only two binaries wire one:

- **Fenced** - `cmd/ingester` (`wiring_canonical_writer_open.go`) and
  `cmd/projector` (`runtime_wiring.go`).
- **Unfenced on purpose** - `cmd/bootstrap-index` (`wiring.go:248`), a one-shot
  seeder that applies or adopts the schema itself and exits. It builds a
  `CanonicalNodeWriter` with no `WithSchemaWriteFence`, so a run that overlaps a
  schema upgrade keeps writing past its own startup check.
- **Unfenced** - every writer `cmd/reducer` builds. A marker recorded under a
  running reducer stops none of them.

### The reducer writer inventory

Regenerate it rather than trusting the prose; the list below is what this
command returned:

```bash
rg -n 'sourcecypher\.New|graphowner\.New' go/cmd/reducer --glob '!*_test.go'
```

| Construction site | Writer |
| --- | --- |
| `main.go:360` | `EdgeWriter` (shared-projection edges, every domain) |
| `endpoint_presence_wiring.go:90` | a second `EdgeWriter` |
| `neo4j_wiring.go:252,260` | `SemanticEntityWriter` / `SemanticEntityWriterWithCanonicalNodeRows` |
| `secrets_iam_graph_wiring.go:63` | `SecretsIAMGraphWriter` |
| `graph_orphan_sweep_wiring.go:27` | `OrphanSweepStore` |
| `canonical_graph_writers.go:78-127` | every field of the `canonicalGraphWriters` struct |

That last row used to be described here as "the specialized cloud, Kubernetes,
and IAM writers", which under-counted it: the struct also holds
`incidentRoutingEvidence`, `codeTaintEvidence`, `codeInterprocEvidence`,
`provenanceEdge`, `crossplaneSatisfiedByEdge`, `observabilityCoverageEdge`, and
`s3ExternalPrincipalGrant`. Read the struct definition at
`canonical_graph_writers.go:17-68` for the current set. The `graphowner` gates
wrapping several of them emit no Cypher of their own, so they add no identity
surface.

### The node identities those writers key on

Every node MERGE an unfenced reducer writer performs is keyed on `uid`, with
five exceptions. Each is named, because a bare count is not checkable:

| Label | Key | Statement | Writer |
| --- | --- | --- | --- |
| `Repository` | `id` | `canonical.go:131,134`, `canonical_relationships.go:161-256`, `canonical_codeowners_edges.go:33`, `canonical_submodule_edges.go:30-31` | `EdgeWriter` |
| `EvidenceArtifact` | `id` | `canonical_relationships.go:278` | `EdgeWriter` |
| `CloudAction` | `id` | `canonical_invokes_cloud_action_edges.go:20` | `EdgeWriter` |
| `CodeownerTeam` | `ref` | `canonical_codeowners_edges.go:34` | `EdgeWriter` |
| `Environment` | `name` | `canonical_relationships.go:311`, `kubernetes_namespace_node_writer.go:89` | `EdgeWriter`, `KubernetesNamespaceNodeWriter` |

Paths are relative to `go/internal/storage/cypher`. Re-derive with
`rg -n 'MERGE \(\w+:' go/internal/storage/cypher --glob '!*_test.go'`, then trace
each constant to the writer that issues it. `Environment` is the sharpest case:
`CanonicalNodeWriter` and an unfenced reducer writer MERGE the same label on the
same key, so an identity change there is fenced on one side of a rollout and not
the other.

The #6102 Module cutover is unaffected: no unfenced writer keys a Module node on
name. `MERGE (m:Module {name, lang})` belongs to `CanonicalNodeWriter`, the
reducer's only Module upsert MERGEs on `uid` (an identity this cutover did not
move, and one the uid uniqueness constraint records as real DDL), and the orphan
sweep already keys Module on `(name, lang)`.

A future cutover on a label a reducer writer MERGEs on gets no such protection -
those pods would be checked at startup only and keep writing through the
rollout. Deployment ordering does not help: it decides when a writer may start,
not whether one already running stops. Two gaps stay open either way: a release
older than the fence contains no call to it, and an unfenced writer has none to
make. Both need those pods stopped before bootstrap records the marker.

The full working is in
`docs/internal/evidence/module-node-identity-6106-review-follow-ups.md`.

## Anti-patterns specific to this package

- Do not query graph backend metadata from graph-writing pods.
- Do not accept any historical marker row after a newer incompatible marker
  exists.
- Do not log connection strings or graph URIs in compatibility errors.
