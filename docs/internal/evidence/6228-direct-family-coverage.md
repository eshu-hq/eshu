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

## What condition 4 actually requires

The fourth condition — "the live matrices actually run the family" — is not one
switch. Read against the two gate scripts it decomposes into five committed
artifacts plus a green run:

1. A committed cassette (a recorded collector output the gate replays instead
   of calling a real cloud or cluster) under `testdata/cassettes/`, and a
   committed expected-edge-set JSON. Both paths need a variable and a fail-fast
   existence check in `scripts/lib/ifa_family_fixtures.sh`
   (`ifa_family_fixtures_require`, lines 134-167). Both gates call that
   function before starting any container, so a missing file stops the run
   early.
2. A row file under `scripts/lib/ifa_family_registry/rows/` carrying the seven
   schema fields plus the dispatch metadata the gate loop needs: `drive_fn`,
   `assert_fn`, `cassette_var`, `expected_var`
   (`scripts/lib/ifa_family_registry.sh`, lines 55-158).
3. A family library supplying that row's two callbacks, in the shape
   `scripts/lib/ifa_shell_exec_live.sh` uses: replay the cassette, then call
   `eshu-ifa assert-edges -domain <family> -expected <file>`.
4. Fault-injection cells, workflow path triggers, and a
   `materializedEdgeFamilyTriggerStems` entry.
   `TestEveryCoveredFamilyTriggersBothLiveGates` demands the stem the moment a
   coverage row lands.
5. Both matrices green with the family driven and its exact edge set asserted.
   The determinism gate drives every `shared_cell=1` family into each of its
   N∈{1,2,4} cells and asserts each one after the drain
   (`scripts/verify-ifa-determinism.sh`, lines 339-345 and 390-396).

So it is the strict reading, not the loose one. A gate that merely executes
with the family's guard reachable does not satisfy it. The matrix corpus has to
contain a committed fixture that produces edges of that family, and the gate has
to assert that family's absolute expected set in every cell.

## The live path works — measured, not assumed

Before writing any of that wiring I proved the part that could have made all of
it pointless: whether a direct-materialization family can be driven through a
live stack at all. None ever has been. All fourteen families registered with
the gates are the shared-projection half, exactly
`reducer.MaterializedEdgeFamilies()`, and the registry's vocabulary is built
around that path.

One Postgres plus NornicDB stack, two scratch cassettes written to match the
two Odùs fact for fact, one `eshu-ifa drive` each, then a projector and reducer
drain:

```
fact_work_items after the drain (domain | status | count)
  iam_instance_profile_role_materialization | succeeded | 1
  kubernetes_namespace_materialization      | succeeded | 1
```

Both families reach the reducer through an ordinary work-item domain, which
also settles what the registry's `wait_stage=handler` and `wait_key` fields
would hold for them. Then:

```
ifa assert-edges: domain=iam_instance_profile_role expected=2 edges matched exactly
```

First run, live graph, committed expected-edge fixture unchanged. The fan-out
profile produced its two edges; the unresolved-role and empty-attachment
profiles produced none.

Those scratch cassettes are deliberately not committed here. A committed
cassette extends the golden standard, which needs the owner's agreement, and
that is where this change stops.

## The gap that would have blocked kubernetes_namespace_environment

The same run failed for the other family, and the failure was worth having:

```
ifa assert-edges: domain=kubernetes_namespace_environment materialized edge set
does not match the expected set exactly
  missing (2, in expected-set but not in graph):
    TARGETS_ENVIRONMENT|k8s-ns:eshu-fixture-cluster:payments-prod|prod|...
    TARGETS_ENVIRONMENT|k8s-ns:eshu-fixture-cluster:payments-staging|stage|...
  endpoint defects (2):
    ... target endpoint carries neither uid, id, nor (for a CodeownerTeam
    endpoint) ref ... — an unmaterialized endpoint node
```

The reducer was right and the gate was wrong. A graph dump showed exactly two
`TARGETS_ENVIRONMENT` edges carrying the correct `evidence_class` and
`evidence_source`, and exactly two `Environment` nodes, `{name: "prod"}` and
`{name: "stage"}` — the canonical forms of the fixture's `production` and
`staging` labels. Both deliberately-unbound namespaces produced nothing.

Those nodes carry `name` and nothing else, because the writer MERGEs them
`MERGE (env:Environment {name: row.environment})`. `endpointID`
(`go/cmd/ifa/assert_edges.go`) read `uid`, then `id`, then `ref` scoped to a
`CodeownerTeam` label. A name-keyed node resolved to the empty string, so every
edge of this family reported an unmaterialized endpoint. No fixture and no
writer could have satisfied condition 4 while that held.

This change adds a fourth fallback: `name`, scoped to the `Environment` label,
in the shape #6137 already established for `CodeownerTeam`'s `ref`. Scoping
matters more here than it did there. `name` is a common display property, so an
unscoped fallback would let any uid/id-keyed node that lost its real identity
pass as identified on its display name alone.

After the fix, rebuilt binary, same live stack, same committed fixtures:

```
ifa assert-edges: domain=kubernetes_namespace_environment expected=2 edges matched exactly
ifa assert-edges: domain=iam_instance_profile_role       expected=2 edges matched exactly
```

Mutation check on the new guard: replacing
`if hasLabel(labels, endpointEnvironmentLabel)` with `if true` (subs=1) leaves
`go vet` at exit 0 and turns `TestNameFallbackIsScopedToEnvironment` red, so the
scoping is load-bearing rather than decorative. Restored, exit 0.

The operator-facing defect message widened with it, and the test pinning that
message moved in the same edit.

## What is still missing for a coverage row

Both waivers stand, and neither family gained a coverage row. Neither live
matrix has been run with either family wired in, because none of the wiring in
items 1-4 above exists yet. What changed is that the remaining work is now
known to be reachable instead of assumed:

- The cassettes and expected sets are the next step. Committing them needs the
  owner's agreement, since they extend the golden standard.
- The fault-injection half needs no new machinery. A direct family fits the
  existing generic dispatcher as `blocker_kind=table_lock:fact_records` with
  `cell_kind=generic`, the same shape `codeowners_ownership_edges` already uses
  (`scripts/lib/ifa_fault_generic_cells.sh`, lines 415-419). The two
  shared-projection blocker kinds are unavailable to a direct family, and
  neither is required.
- Running both matrices needs an explicit hand-over of the machine. They bind
  fixed host ports and hold a cross-worktree mutex — see
  `docs/internal/agent-guide.md#live-gate-serialization-and-contention`.

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
