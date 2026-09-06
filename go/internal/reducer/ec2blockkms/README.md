# ec2blockkms

## Purpose

Derives conservative EC2 block-device KMS encryption posture -- `encrypted` /
`not_encrypted` / `mixed` / `unknown`, with a specific reason -- and writes it
as reducer-owned properties on existing EC2 `:CloudResource` nodes. This is
issue #1304. This package moved out of the flat `internal/reducer` root under
issue #6061.

## Ownership boundary

This package owns the block-device KMS posture extraction
(`ExtractEC2BlockDeviceKMSPostureRows`), the additive domain definition and its
handler, and the volume/KMS-key join index used to resolve each instance's
posture.

It does **not** own the `aws_resource`/`aws_relationship`/`ec2_instance_posture`
decoders (`schemadecode`), the CloudResource uid derivation (`cloudjoin`),
quarantine (`factdecode`), the scoped fact read (`factload`), or readiness
phases (`gpphase`). Registration stays in the reducer root
(`defaults_additive_domains_cloud_posture.go`), and the Cypher node writer
lives under `internal/storage/cypher`.

## Exported surface

- `MaterializationDomainDefinition` — the root additive-domain registry
  (`defaults_additive_domains_cloud_posture.go`)
- `EC2BlockDeviceKMSPostureMaterializationHandler` — the root additive-domain
  registry
- `EC2BlockDeviceKMSPostureNodeWriter` — `cmd/reducer`; `defaults.go` declares
  `DefaultHandlers.EC2BlockDeviceKMSPostureNodeWriter` with this type
  directly -- no root compat file
- `ExtractEC2BlockDeviceKMSPostureRows` — no cross-package caller today; kept
  exported for symmetry with the sibling extraction seams
  (`iamescalation.ExtractIAMEscalationEdges`,
  `secgroup.ExtractSecurityGroupReachability`)
- `EC2BlockDeviceKMSPostureNodesNotReadyFailureClass` —
  `internal/storage/postgres`'s readiness claim gate -- a storage contract
  literal, not just a Go identifier -- called directly, no root compat alias

See `doc.go` for the godoc-rendered contract.

## Dependencies

`reducer/contract`, `reducer/cloudjoin`, `reducer/factdecode`,
`reducer/factload`, `reducer/gpphase`, `reducer/payloadcore`,
`reducer/schemadecode`, `internal/facts`, `internal/telemetry`,
`internal/truth`, `pkg/log`, `sdk/go/factschema/aws/v1`. Never
`internal/reducer`.

## Telemetry

| signal | dimensions |
|---|---|
| `eshu_dp_ec2_block_device_kms_posture_decisions_total` | `outcome` (`encrypted`/`not_encrypted`/`mixed`/`unknown`), `reason` |
| `eshu_dp_ec2_block_device_kms_posture_skipped_total` | `skip_reason` (`source_unresolved`/`tombstone`) |

Every decision outcome/reason and skip reason is recorded even at zero so the
series exists and a rising skip rate charts from zero. Span
`reducer.ec2_block_device_kms_posture_materialization`; each handler's
completion log carries per-stage `load` / `derive` / `retract` / `graph_write`
/ `total_duration_seconds`.

No-Regression Evidence: #6061 relocates this family's production logic without
changing it. Every hunk in the moved production files is a package clause, an
import requalification, or an identifier requalification: symbols the reducer
root supplied as one-line forwarders (`loadFactsForKinds`,
`partitionDecodeFailures`, `recordQuarantinedFacts`, `inputInvalidSubSignals`,
`decodeAWSResource`, `decodeAWSRelationship`, `decodeEC2InstancePosture`,
`cloudResourceUID`, `derefString`, `uniqueSortedStrings`, `formatTally`) are
now called directly against the leaf package that already owned them
(`factload`, `factdecode`, `schemadecode`, `cloudjoin`, `payloadcore`).
Measured on this branch: `go build ./...` exits 0, `go vet ./...` exits 0,
`go test ./internal/reducer/ec2blockkms -count=1` passes, and
`go test ./internal/reducer/... ./cmd/reducer ./internal/storage/postgres
-count=1` passes.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph, or
storage contract. The two counters above and the span/log are the same before
and after the move.

## Gotchas / invariants

- The dual readiness gate (EC2 instance-node phase, EBS/KMS CloudResource
  phase) MUST both be committed before a write; a miss on either is retryable
  via `EC2BlockDeviceKMSPostureNodesNotReadyFailureClass`, enrolled in
  `nonCountingReducerRetryFailureClasses` so it never erodes the retry budget.
- The handler never creates a CloudResource node -- it matches by uid only.
  A posture whose EC2 instance node does not exist in this scope generation
  never fabricates one.
- Ambiguous evidence (two conflicting volume facts for the same id, two
  conflicting KMS relationships for the same volume) MUST resolve to
  `state=unknown`, never to a guessed value.
- An unattached, detached, or attachment-mismatched volume is excluded from
  the encrypted count via `ec2BlockDeviceKMSAttachmentReason`, not silently
  treated as encrypted.
- Test helpers (`stubFactLoader`, `ec2BlockDeviceKMSDualKeyLookup`) are
  duplicated from the reducer root by design; do not export a root test
  helper to reach it.

## Related docs

- `go/internal/reducer/README.md`
- `go/internal/reducer/cloudjoin/README.md`
- `go/internal/reducer/gpphase/README.md`
- `go/internal/reducer/schemadecode/README.md`
- `docs/public/observability/telemetry-coverage.md`
