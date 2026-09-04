# #6228 — third direct-materialization family: workload_cloud_relationship

The Ifá direct-materialization ledger
(`specs/ifa-materialized-edge-coverage-direct.v1.yaml`) waives 28 families.
`workload_cloud_relationship` now has three of the four things a `coverage:`
row asserts: a cataloged Odù (`WorkloadCloudRelationshipFamilyOdu`), a
registered edge-type set (`USES` in
`cypher.singleTypeMaterializedEdgeFamilies`), a vacuity guard dispatched from
`MaterializedEdgeOduResolver`
(`resolveWorkloadCloudRelationshipMaterializedEdges`), and a hand-derived
expected-edge-set fixture the guard reproduces exactly. It joins
`kubernetes_namespace_environment` and `iam_instance_profile_role`, whose
groundwork is recorded in `6228-direct-family-coverage.md`; this note records
only what the third family adds, not the shared condition-4 machinery.

It stays waived. The fourth condition — the live `ifa-determinism` /
`ifa-fault-injection` matrices actually driving the family — is unmet, is not
claimed anywhere, and `TestGuardedDirectFamiliesStillCarryTheirWaivers` fails
if this family gains a coverage row while it stays unmet.

## Why this family is the #6181 trap one level down

The port name says workload-cloud-relationship and the statement metadata
carries `workloadCloudRelationshipEdgeLabel`
(`"WORKLOAD_USES_CLOUD_RESOURCE"`). Neither is a graph relationship type. The
type the write template actually MERGEs is read off
`workloadCloudRelationshipUpsertCypherFormat` after the closed
`workloadCloudRelationshipVocabulary` token is substituted: `USES`, screened
per row by `validateWorkloadCloudRelationshipType`. The guard binds three
copies of that token — the extractor's `relationship_type` row value (stamped
from `edgetype.Uses`), the registry's `EdgeTypes` key, and its own literal —
so a family that started emitting a different token cannot pass by having its
fixture updated to match. Mutation W3 below proves the registry copy is
load-bearing; W1 proves the guard literal is.

## No-Regression Evidence:

This change adds no Cypher, alters no query or write shape, and changes no
runtime code path. The performance-evidence gate flags it because the gate is
content-based and the touched files carry Cypher in `const` templates and
comments. Stating the reading here rather than arguing with the flag.

What actually changed, per hot-flagged file:

- `go/internal/storage/cypher/materialized_edge_families.go` — one entry added
  to `singleTypeMaterializedEdgeFamilies`. That table is read only by
  `SingleTypeMaterializedEdgeTypes` / `MaterializedEdgeIdentityProperties`,
  whose callers are `ifa.MaterializedEdgeDomainEdgeTypes` (the `eshu-ifa
  assert-edges` verb and the offline vacuity guards) and package tests. No
  writer, reducer handler, or query handler reads it. Its `RetractCypher` and
  `IdentityCypher` fields reference EXISTING production consts by name; neither
  const's text changed, so every statement the runtime dispatches is
  byte-identical before and after.
- `go/internal/ifa/workload_cloud_relationship_family_odu.go` and
  `go/internal/ifa/materializededges/materialized_edges_workload_cloud_relationship.go`
  — new fixture construction and offline vacuity guard. They run inside
  `go test` and inside the Ifá coverage reconciliation; no service binary
  executes them on a request or drain path.

Input shape for the guard, since it does run in CI: five `aws_resource` fact
envelopes, fixed and committed — two edge-producing anchors (workload+service
and workload-only through the plural key spelling) and three deliberate
non-producers covering the service-only, ambiguous-anchor, and
missing-environment branches. Whole-package cost including every other
family's guard: `ok
github.com/eshu-hq/eshu/go/internal/ifa/materializededges 1.520s` (`cd go &&
go test ./internal/ifa/materializededges ./internal/ifa
./internal/storage/cypher -count=1`, darwin/arm64, go1.26.6, base 82e089cff).
Reported, not compared: the figure is recorded so a later change that makes
this package slow has a number to regress from. Classification: `Diagnostic
win` — no wall-clock claim is made or implied.

## No-Observability-Change:

No metric, span, log field, or status field is added, removed, or renamed. The
guard reports through the existing `replaycoverage` resolver return (`bool`,
`detail string`) every sibling family guard already uses, and the ledger's
finding rendering (`materializedEdgeFinding`) is untouched. An operator sees
the same materialized-edge coverage report shape as before, with one more
family whose waiver reason now says more.

## Mutation evidence

Each case substitutes exactly one production expression, runs `go vet` on the
mutant FIRST so a non-zero test exit is behavioural rather than a compile
failure, runs the target assertion, restores, and re-runs. Exit codes captured
directly, never `$?` after a pipe.

| id | mutated expression | subs | mutant `go vet` | mutant test | restored test |
| --- | --- | ---: | ---: | --- | --- |
| W1 | guard literal `workloadCloudRelationshipRelationshipType` returns `"USES_X"` instead of `"USES"` | 1 | 0 | 1 | 0 |
| W2 | expected-edge fixture truncated to its first edge | 1 | n/a (JSON) | 1 | 0 |
| W3 | registry `EdgeTypes` key `"USES"` renamed to `"USES_X"` | 1 | 0 | 1 | 0 |

W1 ran against `TestGuardedDirectFamiliesResolveTheirOduCovered`,
W2 against the same test, W3 against
`TestGuardedDirectFamiliesResolveToTheirWrittenEdgeTypes`.

What each red proves:

- **W1** — the guard compares the extractor row's `relationship_type` against
  its own literal, not against the fixture. A family emitting a new token
  reds here even if its fixture were updated to match.
- **W2** — the comparison is exact-set, both directions. A fixture that lost
  the plural-spelling edge reds rather than asserting the surviving one.
- **W3** — the fixture's types are checked against the registry copy, so the
  registry entry is pinned to what the family declares, not merely present.

## What is still missing for a coverage row

This family keeps its waiver rows, and the ledger's rows are per (surface,
proof_gate), so the two halves would retire separately. Live gate wiring
(registry row, drive/assert lib, trigger stems, both matrices green) is none
of it here — deliberate, for the reason `6228-direct-family-coverage.md`
records: half-wiring makes a family look driven when no matrix drives it,
which is worse than an honest waiver. No committed cassette either: a
committed cassette extends the golden standard, which needs the owner's
agreement.

Refs #6228, #6181
