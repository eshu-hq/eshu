# internal/reducer/tfconfigstate

## Purpose

Reconciles Terraform config facts (parsed HCL) against Terraform state facts
to detect and durably publish drift for one state-snapshot scope at a time.
This is the reducer intent handler and Postgres writer for the
`config_state_drift` domain (issues #5442, #5572, #5594).

It exists as its own package (issue #6061, epic #6053) because the family's
only real blocker was `nonNilMapSlice`, a root-owned helper that had not yet
been hoisted to `payloadcore` the way its sibling `nonNilStrings` already had.
Everything else the family referenced — the batch-insert primitives, the
intent/result/domain aliases — already resolved to a leaf (`factwrite`,
`contract`) before this move.

## Ownership boundary

**Owns:** `TerraformConfigStateDriftHandler` (the intent handler),
`DriftEvidenceLoader` (the evidence-loading port), `PostgresTerraformConfig
StateDriftWriter` and `TerraformConfigStateDriftFindingWriter` (the writer and
its port), the unresolved-owner durable-write path, and the
generation-authoritative retire query that clears stale findings.

**Does not own:** the correlation candidate-building logic
(`internal/correlation/drift/tfconfigstate.BuildCandidates` — a different,
pre-existing package that happens to share this name) or the backend-owner
resolution (`internal/relationships/tfstatebackend.Resolver`). Both are ports
this package calls through, not logic it duplicates.

## Exported surface

| symbol | what it is |
|---|---|
| `TerraformConfigStateDriftHandler` | the reducer intent handler for `config_state_drift` |
| `DriftEvidenceLoader` | port: loads joined per-address drift rows for one state scope |
| `DriftRejection` | structured-log payload for a non-fatal drift rejection |
| `TerraformConfigStateDriftFindingWriter` | port: persists admitted findings and rejections |
| `TerraformConfigStateDriftWrite` / `...WriteResult` | the durable-write request/response shapes |
| `PostgresTerraformConfigStateDriftWriter` | the Postgres implementation of the writer port |

The reducer root wires `DriftHandlers.DriftEvidenceLoader` and
`DriftHandlers.DriftWriter` to this package's `DriftEvidenceLoader` and
`TerraformConfigStateDriftFindingWriter` interfaces
(`defaults_handlers.go`), and constructs `TerraformConfigStateDriftHandler`
directly in `defaults_additive_domains_correlation.go`. `cmd/reducer`
constructs the concrete `PostgresTerraformConfigStateDriftWriter`.

## Dependencies

`internal/correlation/drift/tfconfigstate` (candidate building — a separate
package despite the shared name), `internal/correlation/engine` and
`internal/correlation/rules` (explain-trace recording and the correlation rule
pack), `internal/relationships/tfstatebackend` (backend-owner resolution),
`internal/reducer/contract` (the `Intent`/`Result`/`Domain` shapes, aliased
`reducercontract`), `internal/reducer/factwrite` (the batch-insert
primitives), `internal/reducer/payloadcore` (the nil-to-empty-slice
substitution), `internal/facts` and the generated `sdk/go/factschema`
packages (fact-kind identity and payload encoding), and
`internal/storage/postgres/pgarray` (the retire query's array binding). No
dependency on the reducer root, and none of the root's other family
subpackages.

## Telemetry

`eshu_dp_correlation_rule_matches_total{pack, rule}` and
`eshu_dp_correlation_drift_detected_total{pack, rule, drift_kind}` (emitted by
the shared correlation engine, labeled with this domain's rule pack),
`eshu_dp_drift_unresolved_owner_write_failed_total` (unresolved-owner durable
write failures), and `eshu_dp_drift_ambiguous_owner_write_failed_total`
(ambiguous-owner durable write failures, swallowed rather than returned — see
`doc.go`). Writer execution is covered by the reducer's standard
`eshu_dp_reducer_executions_total` / `eshu_dp_reducer_run_duration_seconds`
and `eshu_dp_postgres_query_duration_seconds`. Unchanged by this move: same
metric names, same emission sites, only the package that owns the code moved.

## Gotchas / invariants

- **Exactly one of `Candidates`, `AmbiguousOwners`, or `UnresolvedOwner` is
  populated per `TerraformConfigStateDriftWrite` call.** See the type's doc
  comment in `terraform_config_state_drift_writer.go` for which outcome each
  maps to. Do not populate more than one; the writer does not merge them.
- **Ambiguous- and unresolved-owner write failures are handled asymmetrically
  on purpose.** An ambiguous-owner write failure is logged and swallowed; an
  unresolved-owner write failure is returned as a retriable `Handle()` error.
  The reasoning is in the doc comment on `writeUnresolvedOwner`
  (terraform_config_state_drift_unresolved_owner.go) — a permanently
  unresolved backend produces no future generation to retry the lost write
  against, unlike a resolvable ambiguity.
- **Retire is generation-authoritative, not scope-authoritative.** The retire
  query (terraform_config_state_drift_writer_retire.go) removes every prior
  finding for `(scope_id, generation_id)` not among the rows the current pass
  just wrote — it must run in the same write as the insert, or a resolved
  backend's stale finding survives.
- **This package's test doubles for the shared batch-insert primitives are a
  scoped, package-local copy**, not the reducer root's
  `reducer_fact_batch_insert_test_helpers_test.go` / `fakeWorkloadIdentity
  Execer` (defined in `workload_identity_writer_test.go`). Those root helpers
  are still shared by 17 files across other families that have not moved out
  of the root yet (`aws_cloud_runtime_drift`, `multi_cloud_runtime_drift`,
  `supply_chain_impact`, `workload_identity`, `package_correlation`,
  `cloud_inventory_admission`); duplicating rather than sharing avoided
  touching a file several other in-flight #6061 moves also depend on. See
  `terraform_config_state_drift_batch_test_helpers_test.go`. If a future move
  consolidates that root-shared test scaffolding into an exported package
  (e.g. under `factwrite`), this copy should be replaced with an import of it.

## Related docs

- `go/internal/reducer/README.md` — the root package and its subpackage inventory
- `docs/internal/design/package-restructure.md` — the #6061 restructure and this move's no-regression evidence
- `docs/public/observability/telemetry-coverage.md` — the coverage rows for this domain
- `go/internal/correlation/drift/tfconfigstate/doc.go` — the candidate-building package this handler calls through (shares this package's name; do not confuse the two)
