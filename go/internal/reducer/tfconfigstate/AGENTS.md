# Agent instructions: internal/reducer/tfconfigstate

Scoped rules for this directory. The root `AGENTS.md` still applies.

## What this package is

The `config_state_drift` reducer intent handler and Postgres writer:
Terraform config (parsed HCL) reconciled against Terraform state, one
state-snapshot scope at a time (issues #5442, #5572, #5594). Moved out of the
reducer root in issue #6061 after hoisting its one blocking symbol
(`nonNilMapSlice`) to `payloadcore`.

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/AGENTS.md`
- `go/internal/reducer/tfconfigstate/README.md`
- `docs/internal/design/package-restructure.md`

## Invariants

- **No import of the reducer root, ever.** This package is a leaf below
  `internal/reducer`: the root imports it for `DriftHandlers` wiring and
  `TerraformConfigStateDriftHandler` construction, never the reverse.
- **Exactly one write outcome per `TerraformConfigStateDriftWrite` call.**
  `Candidates`, `AmbiguousOwners`, and `UnresolvedOwner` are mutually
  exclusive; see the type's doc comment for which reducer-level outcome
  ("exact"/"derived", "ambiguous", "unresolved") each maps to.
- **Ambiguous- and unresolved-owner write failures are NOT handled the same
  way.** Ambiguous is logged and swallowed; unresolved is returned as a
  retriable error. Do not "fix" one to match the other — read the comment on
  `writeUnresolvedOwner` before touching either path.
- **Retire always runs in the same write as the insert.** The
  generation-authoritative retire query deletes every prior finding for
  `(scope_id, generation_id)` not among the rows just written. Splitting
  retire into a separate, later write reopens the stale-finding bug #5442
  fixed.
- **Do not import the reducer root's shared batch-insert test doubles.** This
  package keeps its own scoped copy
  (`terraform_config_state_drift_batch_test_helpers_test.go`) of the
  `fakeWorkloadIdentityExecer` / `decodeBatchedVersionedFactCalls` shapes
  instead of the root's `workload_identity_writer_test.go` /
  `reducer_fact_batch_insert_test_helpers_test.go` versions — those are still
  shared by 17 files in other families that have not moved out of the root.
  Keep the two copies structurally identical if you change the wire shape
  (`factwrite.BatchInsertVersionedQuery`'s argument order/count); do not let
  them silently drift apart.

## Common changes

Adding a new drift outcome kind: extend
`terraform_config_state_drift_writer.go`'s `terraformConfigStateDriftOutcome*`
constants and `TerraformConfigStateDriftWrite`'s mutually-exclusive field set
together — do not add a new field without also deciding which existing
outcome constant it maps to.

Changing the retire query: update
`terraform_config_state_drift_writer_retire.go` and its tests together with
any change to the insert's `(scope_id, generation_id)` shape; the two must
stay in exact agreement about what counts as "this write's rows."

## Failure modes to avoid

- Confusing this package with `internal/correlation/drift/tfconfigstate` —
  they share a package name (`tfconfigstate`) but are different packages at
  different import paths. This package is the reducer intent handler and
  writer; the other builds correlation candidates. A file in this package
  that imports the other refers to it simply as `tfconfigstate.X`, which is
  correct Go (the current package's own symbols never need a qualifier) but
  easy to misread — do not "fix" the import by renaming or removing it.
- Treating an ambiguous-owner write failure as fatal, or an unresolved-owner
  write failure as swallowable. See the Invariants section above.
- Adding a batch-insert-shaped test helper to this package's
  `terraform_config_state_drift_batch_test_helpers_test.go` that silently
  diverges from the root's copies other families still use. If the shared
  shape changes, change both.

## Do not change without ADR review

- The mutual exclusivity of `Candidates` / `AmbiguousOwners` /
  `UnresolvedOwner` on `TerraformConfigStateDriftWrite`.
- The asymmetric handling of ambiguous- vs. unresolved-owner write failures.
