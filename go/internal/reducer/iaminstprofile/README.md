# iaminstprofile

## Purpose

Projects IAM instance-profile `aws_resource` `role_arns` into canonical
`HAS_ROLE` edges from instance-profile `:CloudResource` nodes to the IAM
role `:CloudResource` nodes they attach. This is issue #1299. The family is
the middle edge in Eshu's EC2 blast-radius chain (EC2 → instance-profile →
role → `CAN_ESCALATE_TO`): the profile target nodes it resolves against are
materialized by `DomainAWSResourceMaterialization` (#805), and the source
profile nodes are the same `aws_iam_instance_profile` resources the
`ec2usesprofile` edge points at (#1146 PR-B). This package moved out of the
flat `internal/reducer` root under issue #6061.

## Ownership boundary

This package owns the HAS_ROLE edge-row extraction
(`ExtractIAMInstanceProfileRoleEdgeRows`, its tally, its skip vocabulary,
and the role ARN join index), the additive domain definition and its
handler, the edge contract (one edge per profile/role pair, never a
fabricated endpoint, never a dangling node), the evidence-source constant
(plus its exported alias for the Ifá materialized-edge offline guard,
#6309), and the readiness-miss failure class.

It does **not** own the `aws_resource` decoder (`schemadecode`), the
`role_arns` typed-attribute accessor (`sdk/go/factschema/aws/v1`, #4631),
the CloudResource uid derivation (`cloudjoin`), quarantine (`factdecode`),
the scoped fact read (`factload`), readiness phases (`gpphase`), or the IAM
role resource-type token (`reducer/iampolicy`, a shared IAM-vocabulary leaf
per the `iamcan` precedent). Registration stays in the reducer root
(`defaults_additive_domains_cloud_posture.go`), the domain alias stays in
the root (`ec2_profile_domains.go`, shared with the `ec2usesprofile` slice),
the `DefaultHandlers.IAMInstanceProfileRoleEdgeWriter` field type lives in
the root (`defaults.go`), and the Cypher edge writer lives under
`internal/storage/cypher`.

## Exported surface

- `MaterializationDomainDefinition` — the root additive-domain registry
  (`defaults_additive_domains_cloud_posture.go`)
- `IAMInstanceProfileRoleMaterializationHandler` — the root
  additive-domain registry
- `IAMInstanceProfileRoleEdgeWriter` — `cmd/reducer`; `defaults.go`
  declares `DefaultHandlers.IAMInstanceProfileRoleEdgeWriter` with this type
  directly — no root compat file
- `ExtractIAMInstanceProfileRoleEdgeRows` — no cross-package caller today;
  kept exported for symmetry with the sibling extraction seams
  (`ec2usesprofile.ExtractEC2UsesProfileEdgeRows`,
  `rdsposture.ExtractRDSPostureRows`)
- `IAMInstanceProfileRoleEvidenceSource` — the Ifá materialized-edge
  offline guard (#6309); an alias of the unexported constant so the two
  cannot drift
- `IAMInstanceProfileRoleNodesNotReadyFailureClass` —
  `internal/storage/postgres`'s readiness claim gate — a storage contract
  literal, not just a Go identifier — called directly, no root compat alias

See `doc.go` for the godoc-rendered contract.

## Dependencies

`reducer/contract`, `reducer/cloudjoin`, `reducer/factdecode`,
`reducer/factload`, `reducer/gpphase`, `reducer/iampolicy`,
`reducer/payloadcore`, `reducer/schemadecode`, `internal/facts`,
`internal/graph/edgetype`, `internal/telemetry`, `internal/truth`,
`pkg/log`. Never `internal/reducer`, never a sibling family package
besides the shared `iampolicy` vocabulary leaf.

## Telemetry

Two package instruments: `eshu_dp_iam_instance_profile_role_edges_total`
(dimensioned by `resolution_mode`) and
`eshu_dp_iam_instance_profile_role_skipped_total` (dimensioned by
`skip_reason`). Both tally maps are map-driven — `resolved` emits only
observed resolution modes and `skipped` emits only nonzero skip reasons
(`source_unresolved` / `target_unresolved`) — so a generation with no
projectable edges emits no per-mode series; the completion log is the
always-present record. Each handler run's completion log carries
`resource_fact_count`, `edge_count`, `resolved_by_mode`,
`skipped_by_reason`, `skip_retract`, and per-stage `load_facts` / `resolve`
/ `retract` / `graph_write` / `total_duration_seconds` fields — the same
fields the pre-move root file logged. The run is also covered by the shared
`reducer.iam_instance_profile_role_materialization` span.

No-Regression Evidence: #6061 relocates this family's production logic
without changing it. Every hunk in the moved production files is a package
clause, an import requalification, or an identifier requalification: symbols
the reducer root exposed as one-line forwarders
(`loadFactsForKinds`→`factload.LoadFactsForKinds`,
`partitionDecodeFailures`/`recordQuarantinedFacts`/`inputInvalidSubSignals`/`quarantinedAttributeShapeFact`→`factdecode.*`,
`derefString`/`formatTally`/`anyToString`→`payloadcore.*`,
`decodeAWSResource`→`schemadecode.DecodeAWSResource`,
`cloudResourceUID`→`cloudjoin.CloudResourceUID`,
`graphProjectionPhaseStateForIntent`→`gpphase.StateForIntentValue`,
readiness keyspace/phase/lookup→`gpphase.*`,
`PriorGenerationCheck`/`Intent`/`Result`/`DomainDefinition`/`OwnershipShape`/`IsRetryable`→`reducercontract.*`)
are now called on the leaf package directly. `iampolicy.ResourceTypeRole`
is untouched: it is a shared IAM-vocabulary leaf (the `iamcan` precedent),
not reducer-root substrate, so no hoist applies. No handler branch,
extraction rule, readiness gate, retract/write ordering, dedupe key, row
shape (`profile_uid` / `role_uid` / `relationship_type` /
`resolution_mode`), evidence source, failure class, or quarantine isolation
changed.
`go test ./internal/reducer/iaminstprofile -count=1` passes with the moved
test suite unchanged in assertion content (8 tests: 5 extraction, 3
handler; only package clause, import paths, leaf requalification, and
unexported symbol duplication for the package boundary).

No-Observability-Change: the move adds no route, graph query shape, queue
table, worker, lease, runtime knob, metric instrument, or metric label; the
shared span and the completion log keep the same names, labels, and key set
at the new import path.
