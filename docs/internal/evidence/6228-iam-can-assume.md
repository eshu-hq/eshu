# #6228 — fourth direct-materialization family: iam_can_assume

The Ifá direct-materialization ledger
(`specs/ifa-materialized-edge-coverage-direct.v1.yaml`) waives 28 families.
`iam_can_assume` now has three of the four things a `coverage:` row asserts:
a cataloged Odù (`IAMCanAssumeFamilyOdu`), a registered edge-type set
(`CAN_ASSUME` in `cypher.singleTypeMaterializedEdgeFamilies`), a vacuity
guard dispatched from `MaterializedEdgeOduResolver`
(`resolveIAMCanAssumeMaterializedEdges`), and a hand-derived expected-edge-set
fixture the guard reproduces exactly. It joins `workload_cloud_relationship`,
whose groundwork is recorded in `6228-workload-cloud-relationship.md`; this
note records only what the fourth family adds, not the shared condition-4
machinery.

It stays waived. The fourth condition — the live `ifa-determinism` /
`ifa-fault-injection` matrices actually driving the family — is unmet, is not
claimed anywhere, and `TestGuardedDirectFamiliesStillCarryTheirWaivers` fails
if this family gains a coverage row while it stays unmet.

## Why this family is the #6181 trap one level down

The port name says IAM-can-assume and the statement metadata carries
`iamCanAssumeEdgeLabel` (`"IAM_CAN_ASSUME"`). Neither is a graph relationship
type. The type the write template actually MERGEs is read off
`canonicalIAMCanAssumeEdgeUpsertCypherFormat` after the closed
`iamCanAssumeRelationshipVocabulary` token is substituted: `CAN_ASSUME`,
screened per row by `validateIAMCanAssumeRelationshipType`. The guard binds
three copies of that token — the extractor's `relationship_type` row value
(stamped from `edgetype.CanAssume`), the registry's `EdgeTypes` key, and its
own literal — so a family that started emitting a different token cannot pass
by having its fixture updated to match. Mutations M1/M3 below prove the guard
literal and the registry copy are each load-bearing.

## What is new versus the workload_cloud_relationship shape

The Odù carries two fact kinds, not one. The production handler loads one
scope generation's `aws_resource` + `aws_iam_permission` facts and partitions
them with `splitIAMCanAssumeEnvelopes` before extracting. The guard partitions
its Odù's facts with the same function — exported as
`iamcan.SplitIAMCanAssumeEnvelopes` in this change, with the handler repointed
to it — so the guard exercises the production split rather than carrying its
own copy. Both fact kinds are built from typed values
(`awsv1.Resource`/`iamv1.Permission` through `factschema.EncodeAWSResource`/
`EncodeAWSIAMPermission`, Contract System v1); the resource-type constants
come from `awsv1`, and the envelope schema versions and collector kind come
from the `facts` / collector constants the live pipeline uses.

The expected set is two edges across thirteen envelopes: one Allow trust
statement fanning out to a scanned role and a scanned user (proving both
`principal_kind` values), with seven deliberate non-producers covering the
deny, wildcard, service-principal, external-unresolved, source-unresolved,
self-assume, and non-trust-source branches, plus a scanned role with no trust
statement and a non-principal resource proving the join index ignores both.

## No-Regression Evidence:

This change adds no Cypher, alters no query or write shape, and changes no
runtime code path. The one production-code edit renames
`splitIAMCanAssumeEnvelopes` to the exported `SplitIAMCanAssumeEnvelopes`
with the handler repointed to it — same body, same call, no behavior change.

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
- `go/internal/ifa/iam_can_assume_family_odu.go` and
  `go/internal/ifa/materializededges/materialized_edges_iam_can_assume.go`
  — new fixture construction and offline vacuity guard. They run inside
  `go test` and inside the Ifá coverage reconciliation; no service binary
  executes them on a request or drain path.

Input shape for the guard, since it does run in CI: thirteen fact envelopes
(five `aws_resource`, eight `aws_iam_permission`), fixed and committed — one
edge-producing Allow trust statement across both principal kinds and seven
deliberate non-producers covering the deny, wildcard, service-principal,
external-unresolved, source-unresolved, self-assume, and non-trust-source
branches, plus a scanned-but-trustless role and a non-principal resource.
Whole-package cost including every other family's guard: `ok
github.com/eshu-hq/eshu/go/internal/ifa/materializededges 1.126s` (`cd go &&
go test ./internal/ifa/... ./internal/storage/cypher/
./internal/reducer/iamcan/... -count=1`, darwin/arm64, go1.26.6).
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
| M1 | guard literal `iamCanAssumeRelationshipType` returns `"CAN_ASSUME_X"` instead of `"CAN_ASSUME"` | 1 | 0 | 1 | 0 |
| M2 | expected-edge fixture truncated to its first edge | 1 | n/a (JSON) | 1 | 0 |
| M3 | registry `EdgeTypes` key `"CAN_ASSUME"` renamed to `"CAN_ASSUME_X"` | 1 | 0 | 1 | 0 |
| M4 | mapper keys source by `role_uid` instead of `principal_uid` | 1 | 0 | 1 | 0 |

M1 ran against `TestGuardedDirectFamiliesResolveTheirOduCovered`,
M2 against the same test, M3 against
`TestGuardedDirectFamiliesResolveToTheirWrittenEdgeTypes`, M4 against
`TestGuardedDirectFamiliesResolveTheirOduCovered`.

What each red proves:

- **M1** — the guard compares the extractor row's `relationship_type` against
  its own literal, not against the fixture. A family emitting a new token
  reds here even if its fixture were updated to match.
- **M2** — the comparison is exact-set, both directions. A fixture that lost
  the user-kind edge reds (EXTRA) rather than asserting the surviving one.
- **M3** — the fixture's types are checked against the registry copy, so the
  registry entry is pinned to what the family declares, not merely present.
- **M4** — the mapper's endpoint assignment is load-bearing: sourcing edges
  from the role uid produces role→role triples the fixture rejects in both
  directions, so a writer-identity misunderstanding cannot pass offline.

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
