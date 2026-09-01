# AGENTS.md — IAM CAN_ASSUME projector intent guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` and `../README.md` for projector-wide invariants.
3. `../intent/AGENTS.md` for the neutral builder contract.
4. `../scope_generation_intents.go` for root-owned assembly order.
5. `docs/internal/design/1134-iam-can-assume-trust-graph.md` for the trust
   graph design and the node-before-edge readiness gate this intent
   participates in.

## Invariants

- Import `internal/projector/intent`, never the root projector package.
- `BuildIAMCanAssumeMaterializationReducerIntent` anchors to the earliest
  `aws_iam_permission` fact in original input order whose payload decodes and
  carries `policy_source == "trust"` (`FirstOfKindMatching`). Identity-policy
  statements and undecodable facts are skipped, not errors: a later valid
  trust statement in the same generation still anchors the intent. Do not
  widen the predicate, add a cross-kind scan, or turn a decode failure into a
  returned error; each changes `FactID` or drops a valid generation.
- The payload decode goes through this package's own `factschema_decode_iam.go`
  (`decodeAWSIAMPermission`, `sdk/go/factschema` plus `internal/factenvelope`
  directly), never root's classified decode wrapper. Root imports this package
  to dispatch, so the reverse import cycles. Keep the file name on the
  `factschema_decode*.go` convention and keep the
  `factschema.FactKindAWSIAMPermission` reference inside the function body;
  `scripts/verify-payload-usage-manifest.sh` globs that name and AST-scans the
  body to recognize the decode seam, and dropping either silently removes
  `aws_iam_permission` from the projector's manifest coverage.
- `iamCanAssumePolicySourceTrust` duplicates the collector's
  `IAMPolicySourceTrust` string on purpose so the projector does not import
  the collector package for one constant. Change both together.
- The entity key is `aws_resource_materialization:<scope>` on purpose. It is
  NOT a family-distinct key: the reducer's edge handler resolves the
  `GraphProjectionPhaseCanonicalNodesCommitted` row the AWS resource node
  builders publish under exactly this key, so trust edges never project
  before the IAM role/user nodes commit. Renaming the key silently removes
  that gate.
- Do not resolve principals, expand `assume_principals`, or pick edge labels
  here. The reducer's `DomainIAMCanAssumeMaterialization` handler owns the
  bounded join, the readiness check, and the `iam_can_assume_edge_writer`
  call.
- Do not move lookup construction, assembly, queue writes, retries, graph
  writes, or telemetry into this package.

## Verification

Use TDD. Run the focused child test, the root ordered fan-out parity and
probe-count tests, package-doc verification, the payload-usage manifest gate,
the projector package tree, and the golden-corpus gates selected by the
changed paths.
