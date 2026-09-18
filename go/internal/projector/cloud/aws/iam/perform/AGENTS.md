# AGENTS.md — IAM CAN_PERFORM projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../../../../AGENTS.md` and `../../../../README.md` for projector-wide invariants.
3. `../../../../intent/AGENTS.md` for the neutral builder contract.
4. `../../../../scope_generation_intents.go` for root-owned assembly order.
5. `../trust/AGENTS.md` for the sibling CAN_ASSUME builder this family sits
   beside (same entity key, same trigger fact kind, disjoint `policy_source`
   predicate).
6. `docs/internal/design/1134-iam-can-perform.md` for the CAN_PERFORM MVP
   design and the node-before-edge readiness gate this intent participates in.

## Invariants

- Import `internal/projector/intent`, never the root projector package.
- `BuildIAMCanPerformMaterializationReducerIntent` anchors to the earliest
  fact in original input order, across BOTH `aws_iam_permission` and
  `aws_resource_policy_permission`, that qualifies (`FirstAcrossKinds`):
  an `aws_iam_permission` fact whose payload decodes and carries
  `policy_source` `"inline"` or `"attached_managed"`, or any decodable
  `aws_resource_policy_permission` fact. A trust statement
  (`policy_source == "trust"`) never qualifies — that fact kind's trust
  statements are the sibling `trust` package's trigger, not this one's. Do not
  widen the predicate to accept `"trust"`, narrow it to only one kind, or turn
  a decode failure into a returned error; each changes `FactID` or drops a
  valid generation.
- The payload decode goes through this package's own
  `factschema_decode_iam.go` (`decodeIAMCanPerformAWSIAMPermission`,
  `decodeIAMCanPerformAWSResourcePolicyPermission`; `sdk/go/factschema` plus
  `internal/factenvelope` directly), never root's classified decode wrapper or
  the sibling `trust` package's `decodeAWSIAMPermission` copy. This package's
  wrappers are named distinctly from `trust`'s on purpose:
  `scripts/verify-payload-usage-manifest.sh` requires every decode seam under
  `go/internal/projector` to have a globally unique function name (it keys
  seams by bare identifier, not by (package, identifier)), so reusing
  `decodeAWSIAMPermission` here fails the gate with a
  "declared in both ... factschema_decode_iam.go" error. Root imports this
  package to dispatch, so the reverse import cycles. Keep the file name on the
  `factschema_decode*.go` convention and keep the
  `factschema.FactKindAWSIAMPermission` /
  `factschema.FactKindAWSResourcePolicyPermission` references inside their
  function bodies; `scripts/verify-payload-usage-manifest.sh` globs that name
  and AST-scans the body to recognize the decode seam, and dropping either
  silently removes the fact kind from the projector's manifest coverage.
- `iamCanPerformIdentityPolicySources` duplicates the reducer's
  `iamCanPerformPolicySourceInline` / `iamCanPerformPolicySourceAttachedManaged`
  constants on purpose so the projector does not import the reducer package
  for two strings. Change both together.
- The entity key is `aws_resource_materialization:<scope>` on purpose. It is
  NOT a family-distinct key: the reducer's edge handler resolves the
  `GraphProjectionPhaseCanonicalNodesCommitted` row the AWS resource node
  builders publish under exactly this key, so CAN_PERFORM edges never project
  before the IAM principal/resource nodes commit. Renaming the key silently
  removes that gate.
- Do not resolve grants, evaluate the closed action catalog, classify target
  resource types, or pick edge labels here. The reducer's
  `DomainIAMCanPerformMaterialization` handler owns the bounded join, the
  readiness check, and the `iam_can_perform_edge_writer` call.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.
- This is a `risk:schema` graph-write-adjacent surface (design doc §Status);
  do not widen the CAN_PERFORM trigger scope or honesty boundary without
  reading the design doc's Open Forks section first.

## Verification

Use TDD. Run the focused child test, the root ordered fan-out parity and
probe-count tests, package-doc verification, the payload-usage manifest gate,
the projector package tree, and the golden-corpus gates selected by the
changed paths.
