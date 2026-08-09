# #5988 — apply change-surface grants before the page limit

## Problem

The change-surface graph reads used to fetch `limit + 1` rows and apply the
caller's repository grants in Go afterward. Eleven denied rows could fill a
ten-row page before an authorized row reached the handler. The caller then saw
an empty or incomplete impact set with `truncated: true`.

The Go filter prevented row disclosure, but the result was still wrong. The
truncation bit also revealed that more graph paths existed outside the caller's
grant.

## Fix

Scoped outgoing reads now use one `CALL { ... UNION ... }` statement with two
ownership branches:

- Workload, WorkloadInstance, CloudResource, TerraformModule, and DataAsset
  nodes bind their `repo_id` to the caller's repository or scope grants.
- Repository nodes bind their canonical `id` to those grants.

Both branches filter before the shared `ORDER BY` and `LIMIT`. Incoming
`DEPENDS_ON` consumer reads also filter the impacted Repository id before their
limit. The existing Go checks remain as defense in depth. All-scope calls keep
their previous query bytes.

Pinned NornicDB v1.1.11 silently ignored the second arm of a top-level `UNION`,
so that form was rejected. A `CALL` subquery executed both arms. Separate
strict outgoing queries were correct but added more than 10% latency, and
label-aware predicates combined with `relationships(path)` silently returned
no rows. The accepted shape avoids those forms.

## Backend proof

On 2026-08-09, `TestLiveChangeSurfaceImpactTraversal` ran against the digest-
locked NornicDB v1.1.11 image. The fixture placed eleven denied Workloads and
eleven denied Repository consumers before the authorized rows in sort order.
The shipped queries returned only the authorized consumer, Workload, and
CloudResource, with `truncated: false`. The test also checked relationship
provenance and exact-node cleanup.

```bash
ESHU_OCI_PROVE_LIVE=1 \
ESHU_NEO4J_URI=bolt://127.0.0.1:41687 \
go test ./internal/query -run '^TestLiveChangeSurfaceImpactTraversal$' -count=1 -v; echo $?
```

Result: `PASS`, exit `0`. The isolated container was removed after the run.

## Performance

The theory test used the same pinned backend and seeded 200 outgoing Workload
paths plus 200 incoming Repository-consumer paths. One hundred warm HTTP
samples compared the old scoped outgoing statement with the accepted
`CALL`-subquery statement on the same graph and grant set.

| shape | mean | p50 | p95 |
| --- | ---: | ---: | ---: |
| old | 1.382 ms | 1.299 ms | 1.760 ms |
| accepted | 1.438 ms | 1.314 ms | 1.829 ms |
| change | +4.1% | +1.2% | +3.9% |

The all-granted result set was identical. In the denied-heavy case, the new
statement returned the authorized rows that the old page omitted. This is a
small synthetic graph measurement, not a repository-scale latency claim.

No-Regression Evidence: scoped calls still make one outgoing graph read and,
for Repository anchors, one incoming consumer read. Both traversals remain
label-anchored, depth-bounded, ordered, and limited. The measured all-granted
overhead stayed below the 10% acceptance threshold, while denied-heavy reads
stopped sorting and returning rows the caller could not use. Query-plan
fixtures bind the scoped and all-scope emitted bytes separately.

No-Observability-Change: the handler keeps the existing
`query.change_surface_investigation` span and graph-query timing. This change
adds no route, metric, log field, queue work, or runtime setting.
