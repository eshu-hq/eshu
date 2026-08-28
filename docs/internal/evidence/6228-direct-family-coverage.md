# #6228 — first two direct-materialization families registered and guarded

The Ifá direct-materialization ledger
(`specs/ifa-materialized-edge-coverage-direct.v1.yaml`) waives 28 families.
`kubernetes_namespace_environment` and `iam_instance_profile_role` now have
three of the four things a `coverage:` row asserts: a cataloged Odù, a
registered edge-type set, a vacuity guard dispatched from
`MaterializedEdgeOduResolver`, and a hand-derived expected-edge-set fixture the
guard reproduces exactly.

Both stay waived. The fourth condition — the live `ifa-determinism` /
`ifa-fault-injection` matrices actually driving the family — is unmet, is not
claimed anywhere, and `TestGuardedDirectFamiliesStillCarryTheirWaivers` fails if
either family gains a coverage row while it stays unmet.

## Why there is no measurement to report

No-Regression Evidence: this change adds no Cypher, alters no query or write
shape, and changes no runtime code path. The performance-evidence gate flags it
because the gate is content-based and the touched files carry Cypher in `const`
templates and comments. Stating the reading here rather than arguing with the
flag.

What actually changed, per hot-flagged file:

- `go/internal/storage/cypher/materialized_edge_families.go` — two entries added
  to `singleTypeMaterializedEdgeFamilies`. That table is read only by
  `SingleTypeMaterializedEdgeTypes` / `MaterializedEdgeIdentityProperties`,
  whose callers are `ifa.MaterializedEdgeDomainEdgeTypes` (the `eshu-ifa
  assert-edges` verb and the offline vacuity guards) and package tests. No
  writer, reducer handler, or query handler reads it. Its `RetractCypher` and
  `IdentityCypher` fields reference EXISTING production consts by name; neither
  const's text changed, so every statement the runtime dispatches is
  byte-identical before and after.
- `go/internal/storage/cypher/materialized_edge_endpoints.go` — one entry added
  to `materializedEdgeEndpointsByFamily`, read by the same `assert-edges` path
  only.
- `go/internal/reducer/kubernetes_namespace_materialization.go` — one added
  exported constant, `KubernetesNamespaceEvidenceSource`, aliasing the existing
  unexported `kubernetesNamespaceEvidenceSource`. A compile-time alias of a
  string constant: no call site changed, nothing is evaluated at run time.
- `go/internal/ifa/iam_instance_profile_role_family_odu.go` and the two
  `go/internal/ifa/materializededges/materialized_edges_*.go` guard files — new
  fixture construction and offline vacuity guards. They run inside `go test`
  and inside the Ifá coverage reconciliation; no service binary executes them
  on a request or drain path.

Input shape for the guards, since they do run in CI: four
`kubernetes_live.namespace` fact envelopes and five `aws_resource` fact
envelopes, fixed and committed. Whole-package cost including every other
family's guard: `ok github.com/eshu-hq/eshu/go/internal/ifa/materializededges
1.662s` (`cd go && go test ./internal/ifa/materializededges -count=1`,
darwin/arm64, Go toolchain as pinned by `go/go.mod`). Reported, not compared:
there is no before/after pair because the package had no such guards to compare
against. The figure is recorded so a later change that makes this package slow
has a number to regress from. Classification: `Diagnostic win` — no wall-clock
claim is made or implied.

No-Observability-Change: no metric, span, log field, or status field is added,
removed, or renamed. The two guards report through the existing
`replaycoverage` resolver return (`bool`, `detail string`) every sibling family
guard already uses, and the ledger's finding rendering
(`materializedEdgeFinding`) is untouched. An operator sees the same
materialized-edge coverage report shape as before, with two families whose
waiver reasons now say more.

## Mutation evidence

Each case substitutes exactly one production expression, runs `go vet` on the
mutant FIRST so a non-zero test exit is behavioural rather than a compile
failure, runs the target assertion, restores, and re-runs. Exit codes captured
directly, never `$?` after a pipe.

| id | mutated expression | subs | mutant `go vet` | mutant test | restored test |
| --- | --- | ---: | ---: | ---: | ---: |
| K1 | `namespaceEnvironmentFromLabels` returns `normalized` instead of `environment.Canonical(normalized)` | 1 | 0 | 1 | 0 |
| K2 | `if !environment.IsKnownToken(normalized)` short-circuited so it never skips | 1 | 0 | 1 | 0 |
| K3 | `retractKubernetesNamespaceStaleTargetsEnvironmentCypher` retracts `[rel:TARGETS_ENV]` | 1 | 0 | 1 | 0 |
| I1 | `ExtractIAMInstanceProfileRoleEdgeRows` iterates `roleARNs[:1]` | 1 | 0 | 1 | 0 |
| I2 | `roleOK` forced true, so an unresolved role ARN still emits an edge | 1 | 0 | 1 | 0 |
| I3 | `retractIAMInstanceProfileRoleEdgesCypher` retracts `[rel:HAS_ROLE_X]` | 1 | 0 | 1 | 0 |

K1, K2, I1 and I2 ran against
`TestGuardedDirectFamiliesResolveTheirOduCovered` in
`go/internal/ifa/materializededges`; K3 and I3 against
`TestMaterializedEdgeFamilyRegistryMatchesItsRetract` in
`go/internal/storage/cypher`.

What each red proves:

- **K1** — the guard compares the environment name the extractor actually
  produced. This mutation SURVIVED the first version of the guard, which
  re-canonicalized the row value before comparing and so repaired the drift it
  exists to report. The guard was fixed and the mutation re-run; that fix is why
  this row is in the table rather than a line of prose.
- **K2** — the two deliberately-unbound namespaces in the Odù are load-bearing.
  Without the known-token gate, `platform-team` binds and arrives as an EXTRA
  `TARGETS_ENVIRONMENT` edge to an `Environment` node the graph must never gain.
- **I1** — the fan-out is real. One instance profile attached to two scanned
  roles must produce two edges; truncating to the first leaves one MISSING.
- **I2** — the unresolved-target negative control is real. The orphan profile's
  role ARN matches no `aws_iam_role` fact, and an extractor that stopped
  checking produces an edge to a fabricated endpoint.
- **K3, I3** — each family's registered edge-type set is pinned to what its
  retract actually reaps, so a family cannot declare a type its retract would
  leave behind as stale truth.

## A limitation found by mutating, not assumed

Changing the WRITE template's relationship type
(`MERGE (n)-[env_rel:TARGETS_ENVIRONMENT]->(env)` → `TARGETS_ENV`; subs=1,
mutant `go vet` exit 0) reds no materialized-edge registry check. It reds
`TestKubernetesNamespaceNodeWriterBoundRowCreatesEnvironment`, the writer's own
unit test, and nothing else in `go/internal/storage/cypher`.

That is the registry contract behaving as designed rather than a gap to paper
over. `TestMaterializedEdgeFamilyRegistryMatchesItsRetract` pins declared types
against the RETRACT, and `TestSingleTypeFamilyIdentityMatchesWriteCypher` reads
the write template only for its MERGE property map. The write template's type
is corroborated by three other things — the retract alternation, the canonical
`edgetype` registry, and the writer's own test — but it is not pinned by this
registry, and a reader should not believe it is. Recorded because assuming
otherwise is the shape of mistake #6181 was reported over.

## What is still missing for a coverage row

Both families need the live half before either waiver can be retired:

1. A committed cassette per family under `testdata/cassettes/`, plus the
   compiled-catalog/cassette lockstep test the other families carry.
2. A row in `scripts/lib/ifa_family_registry/rows/`, a drive/assert function
   pair, and an entry in `materializedEdgeFamilyTriggerStems`.
3. Workflow path triggers so `ifa-determinism` and `ifa-fault-injection` re-run
   when the family's Odù, fixture, extractor, or writer changes.
   `TestEveryCoveredFamilyTriggersBothLiveGates` requires this the moment a
   coverage row appears.
4. A green live run of both matrices with the family driven and its exact set
   asserted.

None of that was done here, and neither live matrix was run for this change.
Several agents shared this machine for the whole session, and the live gates
hold a cross-worktree mutex precisely so they are not self-scheduled alongside
other work — see
`docs/internal/agent-guide.md#live-gate-serialization-and-contention`. Running
them needs an explicit hand-over of the machine.

## Registry-shape blocker for part of the remaining 26

`TestMaterializedEdgeFamilyRegistryMatchesItsRetract` extracts a `[rel:TYPE]`
alternation from each registered family's retract template. A family whose
retract names no relationship type cannot supply one, so it cannot be
registered in the table's current shape however good its fixture is. Six of the
remaining families are in that position: `code_taint_evidence` and
`incident_routing_evidence` retract by `DETACH DELETE` on their evidence node,
`semantic_entity_containment` retracts by a node-label alternation, and
`cloud_resource`, `observability_coverage` and `security_group_sg_rule` retract
through a deliberately bare `[rel]` scoped by `evidence_source` because their
type vocabularies are open or derived and an alternation would be an
ever-growing allowlist. Covering those needs a decision about the registry
contract, recorded in an ADR, before any fixture work.

Refs #6228, #6181, #5589
