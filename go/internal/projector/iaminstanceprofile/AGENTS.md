# AGENTS.md — IAM instance-profile-role projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order; this probe
   runs after the EC2 USES_PROFILE probe and before the EC2 internet-exposure
   probe.
5. `docs/internal/design/1299-iam-instance-profile-role-edge.md` for the
   HAS_ROLE edge design, the no-role retraction rationale, and the readiness
   contract this intent encodes.
6. `go/internal/reducer/iam_instance_profile_role_materialization.go` for what
   the reducer does with the intent this package enqueues: the profile/role
   join over aws_resource facts, the retract-first edge lifecycle, the
   canonical-nodes readiness gate, and the
   `IAMInstanceProfileRoleEdgeWriter` calls.

## Invariants

- Import `internal/projector/intent`, never the root projector package. Root
  imports this package to dispatch, so the reverse import cycles.
- `BuildIAMInstanceProfileRoleMaterializationReducerIntent` triggers on an
  `aws_resource` fact whose DECODED `resource_type` is
  `aws_iam_instance_profile`, anchored to the earliest such fact in original
  input order (`FirstOfKindMatching`). A no-role profile (empty `role_arns`)
  still triggers: the reducer's retract pass must run in a generation whose
  profile dropped its roles, or the prior HAS_ROLE edge leaks.
- A fact that fails the typed decode is not a trigger — the predicate returns
  false. Do not turn a decode failure into an error or an enqueue.
- The entity key is `aws_resource_materialization:<scope>` on purpose. It is
  NOT a family-distinct key: the reducer handler gates on the
  canonical-nodes-committed phase row the AWS resource node builder publishes
  under exactly this key on the CloudResource keyspace, so HAS_ROLE edges
  never resolve against uncommitted profile or role nodes. Renaming the key
  silently removes that gate.
- `SourceSystem` is the shared two-tier `projectorintent.SourceSystem`: a
  trimmed `SourceRef.SourceSystem`, falling back to a trimmed
  `CollectorKind`. The pre-extraction root helper
  (`awsCloudRuntimeDriftSourceSystem`) had the identical two-tier body, and
  the child tests pin both tiers.
- The decode wrapper in `factschema_decode_aws.go` stays package-local against
  `sdk/go/factschema` (the `ec2`/`s3` pattern). Do not import root's
  classified wrapper, and keep the wrapper name unique across all
  `factschema_decode*.go` seam files — the payload-usage manifest gate
  requires it.
- Do not read `role_arns` here, join profiles to roles, or add a readiness
  gate. The reducer handler owns the join, the retract-first lifecycle, and
  the edge write.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Common changes

- **Changing the reason string or the entity key.** Both are asserted
  verbatim by the package tests and by the root fan-out parity fixture
  (`../scope_generation_intents_fanout_parity_test.go`); change them
  together, and remember the entity key is a cross-domain readiness contract,
  not a label.
- **Changing the trigger predicate.** The `resource_type` filter and the
  no-role trigger are correctness decisions, not cleanups: the package tests
  and the root fan-out fixture (`aws-resource-iam-profile-1` in
  `../scope_generation_intents_fanout_test.go`) pin them, and the design
  doc's retraction section explains why presence of a profile — not presence
  of roles — is the signal. Update the design doc in the same change.

## Failure modes

- **Path citations outside this package.** The MCP kind-disclosure ledger
  (`go/internal/mcp/kind_disclosure_ledger.go`) cites this package's
  `materialization_intents.go` by full path and names its
  `iamInstanceProfileRoleResourceTypeInstanceProfile` constant in an
  evidence note, and the telemetry contract doc
  (`docs/public/observability/telemetry-coverage.md`) cites both `.go` files
  by full path — a rename here dangles the ledger note and breaks
  `scripts/verify-telemetry-coverage.sh`. No route-serves-data registry entry
  cites this family (verified with a positive control against the registry's
  projector citations), and `sdk/go/factschema` has none either.
- **The reducer keeps its own constant of the same name.**
  `go/internal/reducer/iam_instance_profile_role_edge_rows.go` defines its own
  `iamInstanceProfileRoleResourceTypeInstanceProfile`; the two are separate
  per-package copies of the wire literal, not a shared symbol. Changing the
  literal means changing both, plus the collector emitting it.
- **A trigger fact with a blank source ref AND blank collector kind** yields
  an empty `SourceSystem` rather than dropping the intent. That is the
  preserved pre-extraction behavior, not a bug to patch in passing.

## Anti-patterns

- Do not add a package-local source-system helper; the two-tier
  `projectorintent.SourceSystem` IS the pre-extraction behavior here (unlike
  the code-taint/interproc families, whose single-tier labels must not use
  it).
- Do not import the root `projector` package. Root imports this package to
  dispatch, and the reverse direction is an import cycle.
- Do not widen the export surface past
  `BuildIAMInstanceProfileRoleMaterializationReducerIntent`. Every sibling
  family in this series exports exactly one builder and no types.

## Changes needing ADR review

- Changing `reducer.DomainIAMInstanceProfileRoleMaterialization`, the
  instance-profile trigger predicate, the shared
  `aws_resource_materialization:<scope>` entity key, or the two-tier
  source-system label. All are contract surface the reducer handler, the
  readiness gate, and the fan-out parity fixture assert against; the trigger
  and key are load-bearing halves of the #1299 retraction and
  node-before-edge designs.
- Replacing the package-local decode seam with a shared one. The per-family
  decode copy is a deliberate design decision (matching `internal/reducer`'s
  per-package copies), not duplication to clean up.

## Verification

Use TDD. Run the focused child tests, the root ordered fan-out parity and
probe-count tests, package-doc verification, the projector package tree,
telemetry coverage, and the golden-corpus gates selected by the changed
paths.
