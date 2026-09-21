# Coordinator package split (#6781 Part A)

## What moved

`go/internal/coordinator` held 49 non-test files against the 40-file dirgate
cap, and #6627 pins that count by design, so the `internal/coordinator` ledger
row could not shrink under any other open issue. Part A moved out every planner
half that had no subpackage home yet, leaving the root at 35 non-test files and
retiring the ledger row.

`schedule` holds the shared scheduling substrate. `vulnerability` and
`registry/package` hold their derivation and planner halves and import
`schedule`. `egress` holds collector and extension egress policy. `semantic`
holds the egress-gated provider worker. `environment` and `governance/audit`
hold the two root-private helper sets that more than one of the above needed.

What stayed is the `Service` type and its methods. Go pins a method to its
type's package, so a `*_service.go` half cannot leave without first decomposing
`Service` into composed sub-types, which this issue does not do.

Some root files also keep types and free functions that Go would let them
move. The three `service_*_freshness.go` files each carry trigger-resolution
helpers — `service_gcp_freshness.go` has three types and five free functions at
lines 181-363, `service_incident_freshness.go` (9 methods, 6 movable unexported
declarations) and `service_aws_freshness.go` (7 methods, 4 movable unexported
declarations) have the same shape — whose only callers are the `Service`
methods in the same file. Counts exclude the exported Service-wired
interfaces (`IncidentFreshnessTriggerStore`, `AWSFreshnessTriggerStore`,
`AWSFreshnessPlanner`), which stay regardless of any split; including them
the free-declaration totals are 7 and 6. They stayed because moving them removes no file from the root: the
methods pin each file in place regardless, so the count is 35 either way
against a cap of 40.

That is also why the approved tree's `integration/observability/`,
`integration/incident/` and `cloud/` were not created. Each would take its
family's planner half, and #6057 already moved those into `planner/grafana`,
`planner/loki`, `planner/tempo`, `planner/prometheus`, `planner/jira`,
`planner/pagerduty`, `planner/gcp` and `planner/aws/*`. A new `cloud/` would
put provider code in a second namespace beside `planner/gcp/`, leave the
`*_service.go` file in the root, and shrink the count by zero. If the GCP
freshness helpers should leave the root on their own merits, the destination
that matches existing structure is `planner/gcp/`, where the AWS family already
keeps the equivalent logic (`planner/aws/freshness/planner.go`).

A root file in that position needs a `//nolint:dirgate` marker only when its
name matches a sibling subpackage, which is the case `go-dir-gate` flags.
`vulnerability_intelligence_service.go` carries one for that reason, as
`sbom_attestation_service.go`, `security_alert_service.go` and the other
pre-existing seams already do. `package_registry_service.go` sits in the same
architectural position but its name does not match a sibling directory, so the
gate does not flag it and it carries no marker. Adding one there would be an
unused directive.

## Evidence

No-Regression Evidence (#6781): `cd go && go test ./internal/coordinator/...
./cmd/workflow-coordinator/... -count=1` passes. The split is a file and symbol
relocation, so the proof it owes is that the contract did not move with the
files: the discovered test-name multiset is identical to `origin/main` at 306
names, compared with `go test -list '.*' ./internal/coordinator/...
./cmd/workflow-coordinator/...` on both trees and an empty `comm` diff in both
directions. That check exists because a moved test file can compile clean and
register nothing. `go vet ./...` is clean module-wide and caught eight
test-compilation breaks that `go build ./...` reported clean, since build
ignores `_test.go` entirely.

Each moved file was also checked differentially: apply the branch's rename map
to the `origin/main` version, strip the package clause and import block from
both sides, and diff the remaining body. The only surviving differences were
package qualification, gofumpt struct-tag column realignment, and two
deliberate decouplings in `semantic`. The tag strings themselves are identical
across the moved files that carry them, which matters because
`DerivedTargetSkipEvidence` is serialized into the durable
`requested_scope_set` on a workflow row.

The semantic-provider worker was checked method by method, because it is the
one moved thing with a security-relevant ordering contract: it must re-check
egress fail-closed before any provider dispatch, under a lease fence. `Run`,
`handleClaim`, `skipDenied`, `terminateProviderDisabled`, `recordEgressAudit`,
`recordClaim`, `client` and `now` are byte-identical to main under rename
normalization; `dispatch` differs only in two renamed type references, the
`cli` parameter (`SemanticProviderClient` to `ProviderClient`) and the
composite literal in its body (`SemanticDispatchRequest` to `DispatchRequest`). Lease TTL
stays `time.Minute`, `MaxClaimsPerPass` stays 32, and the lease owner stays
`svc:semantic-provider-worker`. No test name would reveal a reordering here, so
it was checked directly rather than inferred from a green suite.

`audit.Hash` and `audit.CorrelationID` are byte-identical to the root's former
`governanceAuditHash` and `governanceAuditCorrelation`, and every call site
passes the same arguments in the same order, so no audit row's `scope_id_hash`
or `correlation_id` changes value.

No-Observability-Change (#6781): the split adds or renames no metric, span, log
field, status field, queue, worker, lease, or runtime setting. The
`ESHU_SEMANTIC_PROVIDER_*` environment variable strings are unchanged; only
their Go constant names dropped the `Semantic` prefix.
`eshu_dp_workflow_coordinator_semantic_provider_claim_total` keeps its name and
dimensions. `MetricPrefix` is now exported so `cmd/workflow-coordinator` can
pass it to `semantic.NewProviderWorkerMetrics`, which keeps the instrument in
the same namespace without the subpackage importing this one. `minInt` was
replaced by the `min` builtin at one capacity hint, which is the same function.

## Related docs

- `go/internal/coordinator/README.md`
- `go/internal/coordinator/AGENTS.md`
- `docs/internal/naming.md`
