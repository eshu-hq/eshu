# iamescalation

## Purpose

Projects merged `aws_iam_permission` facts into conservative IAM
`CAN_ESCALATE_TO` privilege-escalation edges: an edge asserts that a scanned
principal holds a curated escalation primitive against exactly one scanned
target. This is the `CAN_ESCALATE_TO` slice of #1134; the design of record is
`docs/internal/design/1134-iam-privilege-escalation-catalog.md`. This package
moved out of the flat `internal/reducer` root under issue #6061.

## Ownership boundary

This package owns the escalation *primitive vocabulary*
(`iamEscalationCatalog`, `iamEscalationPrimitive`, `iamEscalationTargetKind`),
the arm/deny/incomplete fold, the target-resolution ladder, and the handler.

It does **not** own the IAM statement/grant shapes (`iampolicy`), the
CloudResource join index or node uid (`cloudjoin`), the `aws_iam_permission`
decoder (`schemadecode`), quarantine (`factdecode`), the scoped fact read
(`factload`), or readiness phases (`gpphase`). Registration stays in the
reducer root (`defaults_additive_domains_cloud_posture.go`), and the Cypher
writer is `internal/storage/cypher/iam_escalation_edge_writer.go`.

## Exported surface

| symbol | consumer |
|---|---|
| `MaterializationDomainDefinition` | the root additive-domain registry (`defaults_additive_domains_cloud_posture.go`) |
| `IAMEscalationMaterializationHandler` | the root additive-domain registry |
| `IAMEscalationEdgeWriter` | `cmd/reducer` (implemented by `sourcecypher.IAMEscalationEdgeWriter`) |
| `IAMEscalationNodesNotReadyFailureClass` | `internal/storage/postgres`' readiness claim gate -- a storage contract literal, not just a Go identifier |
| `IAMEscalationResult` | no cross-package caller today; kept exported for symmetry with the sibling extraction seams below |
| `ExtractIAMEscalationEdges` | no cross-package caller today; kept exported for the same reason |

`IAMEscalationResult` and `ExtractIAMEscalationEdges` mirror the identical,
already-merged extraction seams `iamcan.IAMCanPerformResult` /
`iamcan.ExtractIAMCanPerformEdges`, `iamcan.ExtractIAMCanAssumeEdgeRows`, and
`secretsiam.ExtractSecretsIAMGraphRows`. Demoting them here would break that
family symmetry for no consumer gain.

See `doc.go` for the godoc-rendered contract.

## Dependencies

`reducer/contract`, `reducer/cloudjoin`, `reducer/factdecode`,
`reducer/factload`, `reducer/gpphase`, `reducer/iampolicy`,
`reducer/payloadcore`, `reducer/schemadecode`, `internal/facts`,
`internal/telemetry`, `internal/truth`, `pkg/log`. Never `internal/reducer`.

## Telemetry

| signal | dimensions |
|---|---|
| `eshu_dp_iam_escalation_edges_total` | -- |
| `eshu_dp_iam_escalation_skipped_total` | `skip_reason` over the bounded set `skipped_ambiguous`, `skipped_unresolved`, `skipped_deny`, `skipped_conditioned`, `skipped_not_action_resource`, `skipped_incomplete`, `deferred_can_assume` |

Every `skip_reason` is recorded even at zero so the series exists and a rising
skip rate charts from zero. Span `reducer.iam_escalation_materialization`; the
completion log carries per-stage `load` / `extract` / `retract` /
`graph_write` / `total_duration_seconds`.

No-Regression Evidence: #6061 relocates this family's production logic
without changing it. Almost every hunk in the moved production files is a
package clause, an import requalification, or an identifier requalification:
symbols the reducer root supplied as one-line forwarders are now imported
directly from the leaf that already owned them. There is one hunk of a fourth
kind: `iam_escalation.go:114` gains an empty `case iamPrimitiveArmed:` arm so
the switch lists all three members of a closed enum. An empty Go case breaks
out of the switch, which is what an unmatched value did before -- there is no
`default` and no `fallthrough` -- so behaviour is unchanged. It exists because
this move deleted a stale `go/.golangci.yml` exclusion that had been
suppressing the `exhaustive` finding on the old path.

The one deletion is `payloadStringSlice`, an inverse misfiling with zero family
callers and three unrelated root test callers, repointed to
`payloadcore.PayloadOrderedStrings`. The two are not equivalent functions --
`PayloadOrderedStrings` trims and drops empties on its `[]string` arm and
accepts a bare `string` where the deleted helper returned nil -- but they are
equivalent at these three call sites, each of which trims the value and skips
empties immediately after the call. Measured on this branch: `go build ./...`
exits 0, `go vet ./...` exits 0, `go test ./internal/reducer/iamescalation
-count=1` passes, `go test ./internal/reducer/... -count=1` passes, and the
three repointed convergence tests
(`go test ./internal/reducer -run
'PartitionConvergence|RationaleEdgeMaterializationPartition' -count=1`) pass.

No-Observability-Change: #6061 adds no queue domain, worker, lease, graph, or
storage contract. The two counters above and the span/log are the same before
and after the move.

## Gotchas / invariants

- Deny beats Allow, always.
- A bare `*` resource is `skipped_ambiguous`, not an edge.
- Self-escalation is dropped **without** a skip count -- it carries no
  escalation truth.
- An unscanned principal costs one `skipped_unresolved` for the whole
  principal, not one per primitive.
- `sts:AssumeRole` is counted once per principal regardless of statement
  count, and deferred to the separate `CAN_ASSUME` edge (#1134 PR2) -- never
  emitted here.
- Catalog actions are lowercase because `aws_iam_permission` normalizes them
  (loss-free -- IAM actions are case-insensitive); a non-lowercase entry
  silently never matches, which is why `TestIAMEscalationCatalogIsWellFormed`
  exists.
- The type gate in `iamResourceTypeForTarget` is what stops a policy-target
  primitive resolving to a role node that shares a glob.
- Adding a catalog entry widens what the graph asserts about real
  permissions -- it carries a security review.
- Test helpers are duplicated from `iamcan`/root by design; do not export a
  root test helper to reach it.

## Related docs

- `docs/internal/design/1134-iam-privilege-escalation-catalog.md`
- `docs/internal/design/1134-iam-can-perform.md`
- `go/internal/reducer/README.md`
- `go/internal/reducer/iampolicy/README.md`
- `go/internal/reducer/iamcan/README.md`
- `go/internal/reducer/cloudjoin/README.md`
- `docs/public/observability/telemetry-coverage.md`
