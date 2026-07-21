# #5443 TerraformResource / TerraformStateResource split — performance and observability evidence

## Change summary

Splits Terraform-state-observed resources off the shared `TerraformResource`
label onto a new `TerraformStateResource` label
(`go/internal/storage/cypher/tfstate_canonical_writer.go`,
`tfstate_canonical_writer_retract.go`, `tfstate_state_match_edge.go`). Adds,
per tfstate materialization cycle: one migration relabel statement (batched
by uid), two generation-gated `DETACH DELETE` retraction statements, and one
batched `MATCHES_STATE` edge-write statement scoped to rows with a resolved
config-repo owner. Wires the real ownership resolver into
`go/cmd/projector/runtime_wiring.go`.

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
  `scope_id`, which has no backing index on either label. This is the SAME
  cost class as every other generation-gated entity retraction already
  shipped in this writer (`canonicalNodeRetractEntityTemplate` filters on
  `repo_id`, also unindexed, across ~50 entity labels) -- consistent with
  existing precedent, not a new class of risk this change introduces. It
  scales with the per-scope TerraformStateResource population, which is
  expected to be small to moderate (hundreds to low thousands of resources
  per Terraform state backend) in the deployments this feature targets.
- `MATCHES_STATE` edge write: anchored on `TerraformStateResource.uid`
  (indexed) and `TerraformResource.{repo_id, name}`, backed by the new
  `tf_resource_name` index (`graph/schema_tables.go`) rather than the
  existing `(name, path, line_number)` composite constraint, which does not
  serve a 2-property `{repo_id, name}` lookup.

## No-Observability-Change

No new runtime stage, worker, queue, or metric was added. The new statements
execute inside the existing `terraform_state` canonical-write phase, which
already emits `canonical phase completed` / `canonical phase failed`
structured logs and the existing `CanonicalProjectionDuration` /
`CanonicalWrites` / `ProjectorStageDuration` instruments
(`canonical_node_writer.go`'s `Write`); `scripts/verify-telemetry-coverage.sh`
passed with no diff. The ownership-resolver query failure path
(`cmd/projector/terraform_state_ownership.go`) logs a structured
`slog.WarnContext` on failure (backend_kind, locator_hash, error) rather than
silently swallowing it.
