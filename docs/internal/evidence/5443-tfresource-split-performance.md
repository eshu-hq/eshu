# #5443 TerraformResource / TerraformStateResource split — performance and observability evidence

## Change summary

Splits Terraform-state-observed resources off the shared `TerraformResource`
label onto a new `TerraformStateResource` label
(`go/internal/storage/cypher/tfstate_canonical_writer.go`,
`tfstate_canonical_writer_retract.go`, `tfstate_state_match_edge.go`). Adds,
per tfstate materialization cycle: one migration relabel statement (batched
by uid), two generation-gated `DETACH DELETE` retraction statements, and one
batched `MATCHES_STATE` edge-write statement scoped to rows with a resolved
config-repo owner. Wires the real ownership resolver into every cmd/*
canonical-writer wiring site: `go/cmd/projector/runtime_wiring.go`,
`go/cmd/ingester/wiring_canonical_writer_open.go`, and
`go/cmd/bootstrap-index/wiring.go`.

**Post-review correction (P0 fixes):** a separate-context review found two
defects this change's own live test could not catch, both fixed in the same
branch:

1. `terraformStateResourceRetractStatements` guarded only
   `mat.FirstGeneration`, not `mat.DeltaProjection`. A file-scoped delta cycle
   unrelated to Terraform state carries an empty
   `mat.TerraformStateResources`, so the retract statements would DETACH
   DELETE the entire existing population with nothing to recreate it. Fixed
   by skipping retraction outright under `DeltaProjection`, matching
   `buildRepositoryCleanupStatements`'s existing precedent.
2. `buildTerraformStateStatements` ran retraction BEFORE the resource upsert.
   Every existing node still carried the PREVIOUS cycle's `generation_id` at
   the moment retraction's `generation_id <> $generation_id` predicate
   evaluated it, so retraction deleted the ENTIRE population every cycle, not
   just genuinely stale nodes, and the upsert immediately recreated
   everything. Fixed by reordering to migration -> REMOVE -> upsert ->
   retract, matching this writer's own "entities" -> "entity_retract"
   precedent (`canonical_node_writer.go`'s `buildPhases`).

The "No-Regression Evidence" section below is the ORIGINAL evidence from
before this correction. The retraction cost-class claim in that section was
wrong as implemented (see "Retraction cost class re-measured" below for the
corrected claim and the real measurement that replaces it).

## No-Regression Evidence

Baseline: `docker-compose.yaml`'s pinned NornicDB
(`eshu-nornicdb-pr261:149245885258`, source commit
`1492458852588c884c32f70d27ea2ee07086769c`), the golden-corpus gate's minimal
fixture corpus (`scripts/verify-golden-corpus-gate.sh`), `nornic` database,
no prior warm state (fresh compose volumes).

After measurement (this change, full run,
`COMPOSE_PROJECT_NAME=eshu5443gate`, `GATE_COLLECTOR_SETTLE_SECONDS=45`,
unique ports, torn down after):

```
summary: 449 pass, 0 required-fail, 2 advisory-warn
[PASS] pipeline_wall_time: elapsed=1m9s, ceiling=30m0s (2.0x baseline 15m0s)
[PASS] phase_bootstrap: observed=7.0s, baseline=5.0s, ceiling=10.0s
[PASS] phase_first_drain: observed=9.0s, baseline=75.0s, ceiling=86.2s
[PASS] phase_maintenance_drains: observed=7.0s, baseline=5.0s, ceiling=10.0s
[WARN] phase_collect: observed=46.0s, baseline=20.0s, ceiling=25.0s (advisory)
[WARN] phase_graph_query: observed=9.0s, baseline=3.0s, ceiling=8.0s (advisory)
=== PASS: B-7 golden corpus gate green (elapsed 69s, budget ceiling 1800s) ===
```

The two advisory (non-blocking) phase-timing warnings are `phase_collect` and
`phase_graph_query`, not `phase_reduce` or `phase_second_drain` -- neither is
the phase this change's new Cypher statements execute in (canonical graph
projection runs inside the reduce/drain phases, which stayed within budget).
This machine was running multiple other concurrent agent sessions' Docker and
Go processes at measurement time (visible via `docker ps` showing sibling
NornicDB stacks from other worktrees), which is the far more likely
explanation for generic collection/query-phase slack than four additional
Cypher statements against a corpus with 3 Terraform state resources. This is
a single full-pipeline run, not a controlled isolated A/B benchmark of the
new statements against a scaled corpus; treat the advisory timing as
unconfirmed rather than dismissed.

The new statements' own cost characteristics, stated honestly rather than
measured in isolation:

- Migration and attribute-REMOVE: `MATCH (r:TerraformResource) WHERE r.uid
  IN $uids ...` / `MATCH (r:TerraformStateResource) WHERE r.uid IN $uids
  ...` -- anchored on `uid`, which carries a uniqueness constraint on both
  labels (`graph/schema_tables.go`'s `uidConstraintLabels`), so this is an
  indexed per-uid lookup, not a label scan.
- Retraction: `MATCH (r:TerraformStateResource) WHERE r.scope_id = $scope_id
  AND r.evidence_source = 'projector/tfstate' AND r.generation_id <>
  $generation_id DETACH DELETE r` (and the legacy-label twin) filter on
  `scope_id`, which has no backing index on either label. **This claim was
  false as originally implemented and is corrected below** ("Retraction cost
  class re-measured"): as shipped, this statement ran BEFORE the resource
  upsert, so it matched and deleted the SCOPE'S ENTIRE TerraformStateResource
  population on every cycle (not just genuinely stale rows), which the
  upsert then fully recreated -- a materially worse cost class than
  `canonicalNodeRetractEntityTemplate`'s steady-state behavior, not the same
  one. Once reordered to run AFTER the resource upsert (this writer's own
  "entities" -> "entity_retract" precedent), the claim of an equivalent cost
  class to the existing entity retraction becomes true, and is now backed by
  a real measurement instead of an unmeasured assertion.
- `MATCHES_STATE` edge write: anchored on `TerraformStateResource.uid`
  (indexed) and `TerraformResource.{repo_id, name}`, backed by the new
  `tf_resource_name` index (`graph/schema_tables.go`) rather than the
  existing `(name, path, line_number)` composite constraint, which does not
  serve a 2-property `{repo_id, name}` lookup. **No query-plan trace was
  captured for this statement at original ship time -- see "MATCHES_STATE
  query-plan evidence" below for the corrected status.**

## MATCHES_STATE query-plan evidence (P2 fix)

A P2 review finding required a `PROFILE`/`EXPLAIN` trace for
`canonicalTerraformStateMatchesConfigEdgeCypher`
(`tfstate_state_match_edge.go`), since it is generated hot-path Cypher using
the new `tf_resource_name` index. This was attempted against the same
standalone NornicDB container (`eshu-nornicdb-pr261:149245885258`) used for
the P0-2 measurement above, seeded with one `TerraformResource` node
(`repo_id: "profile-repo", name: "aws_instance.web", path:
"envs/a/main.tf"`) and one matching `TerraformStateResource` node.

Attempted via two paths:

1. HTTP `tx/commit` with a literal `EXPLAIN` prefix on the statement text:
   the endpoint accepted the request (200, no error) but returned an empty
   result set with no plan payload in the response body.
2. Bolt driver `PROFILE`-prefixed `session.Run`, reading the returned
   `ResultSummary`: `summary.Plan()` and `summary.Profile()` both returned
   `nil` (`HasPlan=false HasProfile=false`). The statement itself executed
   correctly (the `MATCHES_STATE` edge was created), so this is not a
   statement-shape failure -- the pinned NornicDB build accepts `PROFILE`
   syntactically but does not surface plan/profile metadata through the
   Bolt driver summary for this statement.

**Deferred, stated plainly:** the pinned NornicDB build
(`eshu-nornicdb-pr261:149245885258`, `orneryd/NornicDB` commit
`1492458852588c884c32f70d27ea2ee07086769c`) does not return plan or profile
data for this query through either the HTTP transaction endpoint or the Bolt
`ResultSummary`, so no `PROFILE`/`EXPLAIN` trace can be captured for it on
this backend today. This matches existing precedent already recorded in
`docs/public/reference/cypher-performance.md` ("no live PROFILE was
available because no local NornicDB-New" -- multiple prior entries), and
`cypher-performance.md`'s own framing that a NornicDB "statement summary" is
used only "when available." The anchor/index reasoning above (uid uniqueness
constraint on the state side, `tf_resource_name` index on the config side)
remains the correctness argument for why this statement should not fall back
to a label scan; it is index-shape reasoning, not a captured plan, and is
labeled as such here rather than presented as PROFILE evidence.
`TestCanonicalNodeWriterSkipsAmbiguousMatchesStateEdgeLive` and
`TestProjectorTerraformStateConfigMatchResolverLive` (added for the P1 fix)
are this repository's real backend-level correctness proof for this
statement family; they are not substitutes for a query plan and are not
represented as one.

## Retraction cost class re-measured (P0-2 fix)

Baseline: a standalone NornicDB container running
`eshu-nornicdb-pr261:149245885258` (the pinned image, already built locally;
no schema/index bootstrap applied -- this measurement is about relative
before/after cost of the SAME statement shape at the SAME schema state, not
absolute index-backed throughput), `nornic` database, one container per run,
torn down and its `TerraformStateResource` population cleared between runs.

Theory: the original retract-before-upsert ordering makes every
steady-state cycle (no resources added, changed, or removed since the last
cycle -- the common case) DETACH DELETE the scope's entire
`TerraformStateResource` population and have the upsert immediately
recreate it, rather than the near-zero-row retraction the "entities" ->
"entity_retract" precedent produces. The originally shipped doc claimed
this was "the SAME cost class ... consistent with existing precedent" --
that claim was never measured. This section replaces it with a real
measurement at `n=500` synthetic resources per scope (the original claim's
corpus had 3), scaled up specifically to make full-population churn visible
against a null steady-state cost, run against the real backend via the
production `buildTerraformStateStatements` statement sequence (not a shim):

Procedure per run: seed `n=500` `TerraformStateResource` rows via a
`FirstGeneration=true` materialization (gen-1), tag every resulting node with
a `churn_probe: true` property, then run a STEADY-STATE materialization
(gen-2: the identical 500 rows, unchanged, only `generation_id` bumped) and
measure (a) wall time for `buildTerraformStateStatements`'s full statement
sequence and (b) how many of the 500 nodes still carry `churn_probe: true`
afterward (survived in place) versus how many do not (deleted and
recreated by the upsert, which never sets `churn_probe`).

| Statement order | Steady-state elapsed (n=500) | Survived in place | Churned (deleted + recreated) |
|---|---|---|---|
| OLD: retract before upsert (pre-fix) | 2.089s | 0 / 500 | 500 / 500 |
| NEW: retract after upsert (this fix) | 0.087s–0.093s (two repeat runs) | 500 / 500 | 0 / 500 |

Both runs used the identical Cypher statement text; the only variable was
the order `buildTerraformStateStatements` emits `terraformStateResourceRetractStatements`
in relative to the resource upsert (proven by toggling only that function's
call order and re-running, byte-identical Cypher templates otherwise).

Equivalence / expected delta: this is a behavior-changing correctness fix,
not a pure optimization, so the required proof is the expected delta, not
result identity with the old (wrong) behavior: the OLD order deleted and
recreated every node every cycle (0/500 survived in place); the NEW order
performs zero deletions in the steady-state case (500/500 survived in
place), matching `buildEntityRetractStatements`'s own steady-state behavior.
`TestTerraformStateResourceMigrationLive`'s ASSERTION 3 (identity
preservation via a pre-attached relationship) is the corresponding
permanent regression test for this exact property.

Honest scope note: this measurement used synthetic single-property rows at
n=500, not the "hundreds to low thousands of resources per Terraform state
backend" the original doc estimated for real deployments, and did not
re-run the full `verify-golden-corpus-gate.sh` pipeline (that would need a
fixture corpus with hundreds of real `terraform_state` facts, which does not
exist today). The n=500 steady-state result is sufficient to prove the
COST CLASS difference (full-population churn vs near-zero) the original doc
asserted without measuring; it is not a claim about absolute production
wall-clock cost at real corpus scale.

No new runtime stage, worker, queue, or metric was added. The new statements
execute inside the existing `terraform_state` canonical-write phase, which
already emits `canonical phase completed` / `canonical phase failed`
structured logs and the existing `CanonicalProjectionDuration` /
`CanonicalWrites` / `ProjectorStageDuration` instruments
(`canonical_node_writer.go`'s `Write`); `scripts/verify-telemetry-coverage.sh`
passed with no diff. The ownership-resolver query failure path logs a
structured `slog.WarnContext` on failure (backend_kind, locator_hash, error)
rather than silently swallowing it; all three wiring sites carry
byte-identical logging
(`cmd/projector/terraform_state_ownership.go`,
`cmd/ingester/terraform_state_ownership.go`,
`cmd/bootstrap-index/terraform_state_ownership.go`).
