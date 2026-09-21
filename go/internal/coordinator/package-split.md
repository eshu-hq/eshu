# Coordinator package split (#6781 Part A)

## What moved

`go/internal/coordinator` held 49 non-test files against the 40-file dirgate
cap, and #6627 pins that count by design, so the `internal/coordinator` ledger
row could not shrink under any other open issue. Part A moved every family that
owns its own types into a subpackage, leaving the root at 35 non-test files and
retiring the ledger row.

`schedule` holds the shared scheduling substrate. `vulnerability` and
`registry/package` hold their derivation and planner halves and import
`schedule`. `egress` holds collector and extension egress policy. `semantic`
holds the egress-gated provider worker. `environment` and `governance/audit`
hold the two root-private helper sets that more than one of the above needed.

What stayed is the `Service` type and its methods. Go pins a method to its
type's package, so a `*_service.go` half cannot leave without first decomposing
`Service` into composed sub-types, which this issue does not do. Root files in
that position carry a `//nolint:dirgate` marker naming the reason.

## Evidence

No-Regression Evidence (#6781): `cd go && go test ./internal/coordinator/...
./cmd/workflow-coordinator/... -count=1` passes. The split is a file and symbol
relocation, so the proof it owes is that the contract did not move with the
files: the discovered test-name set is byte-identical to `origin/main` at 279
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
normalization; `dispatch` differs only in one parameter's type name. Lease TTL
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
