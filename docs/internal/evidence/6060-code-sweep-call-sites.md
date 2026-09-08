# Code Sweep Call Sites — Evidence (#6060 PR 2)

Covers the sweep PR that exports `CallGraphMetricsData`, relocates three
white-box test cases, and ports `codeGrantScopedAuthContext` to `querytestutil`.

## Performance and observability

No-Regression Evidence: the export is a rename — `callGraphMetricsData` becomes
`CallGraphMetricsData` with no change to its Cypher, its parameters, its bounds
or its call graph. The queryplan entry keeps `entry_ids: [QP-CALL-GRAPH-HUBS,
QP-CALL-GRAPH-RECURSIVE]` byte-for-byte; only `symbol` and `source_sha256`
change, because that manifest keys on `file:symbol` and pins the function's
source text. `grandfathered_non_hot.go` is untouched. The tree-wide query
literal multiset is unchanged from the base commit.

One behavior change is deliberate and is not a regression: the method now
enforces the caller's repository grant itself instead of relying on the HTTP
route to have done it. Exporting the method put callers on the other side of
`applyRepositorySelectorForCapability`, so a scoped caller holding a grant for
one repository could name another and read its metrics. The method now refuses
that case the same way it refuses a grantless caller, which means an ungranted
repository is indistinguishable from one with no metrics and the caller learns
nothing about a repository it cannot see. This adds a guard ahead of the graph
read; it removes no result any authorized caller could previously obtain.

That guard is mutation-proved rather than merely covered:
`TestCallGraphMetricsDataRefusesARepositoryOutsideTheGrant` fails with the guard
removed ("CallGraphMetricsData ran the graph query for a repository outside the
caller's grant") and passes with it present. The fake graph reader returns a row
for any query, which is what makes the test able to fail — an empty fake passes
either way, and the first version of this test did exactly that until the
mutation check caught it.

No-Observability-Change: no span, metric, log line or status field is added,
removed or renamed. The exported method starts no new span and the existing
`eshu.query.call_graph.*` attributes are unchanged.

## Verification

```
go build ./...                                       exit 0
go vet ./internal/query/...                          exit 0
go test ./internal/query/... -count=1                exit 0
go test ./internal/queryplan -count=1                exit 0
go vet -tags <each of the 8 code_* tags>             exit 0 (all 8)
git diff --check                                     exit 0
```

Test functions balance across the three relocated white-box cases: 33 before,
33 after, plus the new grant regression above.
