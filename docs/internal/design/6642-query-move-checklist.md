# Query move landing checklist (#6642)

Tracks the #6642 / #6597 moves as they land. It lives apart from
[the target tree](6642-query-target-tree.md) on purpose: it gains a row per PR,
and that doc sits 46 lines under the 500-line markdown cap, so a growing table
there would fail the cap on some later PR for a reason unrelated to its change.

## The moves

One destination directory per PR. A row ticks when its PR is merged, not when
it is opened. `querycontract` starts at 56 non-test files and retires its
`//nolint:dirgate` when what stays is under 40.

The `querycontract` leaves land **nested under the current name**
(`querycontract/<leaf>`), not at `contract/<leaf>`: today's `contract/` is a
different package and the name only frees itself once its 40 rows drain, which
is why the rename is sequenced late. The rename carries the leaves with it.
`querycontract/rowvalue/` is the existing precedent.

| # | destination | files | PR | state | `querycontract` after |
| --- | --- | ---: | --- | --- | ---: |
| 1 | `capability/` | 2 | [#6985](https://github.com/eshu-hq/eshu/pull/6985) | **merged** `2d68f1cbb` | — |
| 2 | `querycontract/kubernetes` | 2 | — | open | 54 |
| 3 | `querycontract/code` | 2–3 | — | not started | |
| 4 | `querycontract/language` | 4 | — | not started | |
| 5 | `querycontract/entity` | 2–3 | — | not started | |
| 6 | `querycontract/evidence` | 3 | — | not started | |
| 7 | `querycontract/visualization` | 2 | — | not started | |
| 8 | `querycontract/answer` | 3 | — | not started | |
| | rename `querycontract` -> `contract` | — | — | blocked on `contract/` draining | |

Order is not free. `evidence` is a **base**, not a peer leaf: `answer` and
`visualization` both use `EvidenceCitationHandle` as a field, parameter and
map-key type, so it lands before either of them. Everything else is
independent. The `code` and `entity` counts differ by one between this doc and
[the move-cost doc](6642-query-move-cost.md); measured inbound inside
`querycontract` is zero for every candidate file in both groups, so it is a
labelling choice for those PRs rather than a coupling question.

### Alias retirements

The issue's no-retained-aliases rule is tracked by count, and the count is a
**family** count, not a `*_alias.go` glob: 21 non-test root files at
`c31655ede`, which is 19 `*_alias.go`, plus the build-tagged
`entity_alias_live.go`, plus `envelope_aliases.go` (plural, and per the
move-cost doc "not an alias file" — 52 real type aliases with 1022 callers).
All 21 carry a disposition in
[the alias ledger](6642-query-move-cost.md#the-alias-ledger); audited at
`c31655ede`, nothing uncovered.

Anyone updating this number must use the family rule. A `*_alias.go` glob
returns 19 and makes the docs' correct "Twenty-one" look like a miscount.

| retired by | file | remaining |
| --- | --- | ---: |
| — | (baseline at `c31655ede`) | 21 |
| the `kubernetes` leaf | `k8s_match_alias.go` | 20 |

## Performance and observability evidence for the `kubernetes` leaf

The `perf-evidence` gate selects this PR because
`go/internal/query/impact/trace_deployment_resources.go` is on its hot-file
list, and that file is touched. It is worth saying exactly what the touch is
rather than producing a benchmark shaped like proof.

No-Regression Evidence: the change to every hot file in this PR is an
identifier repoint. `trace_deployment_resources.go` changes two tokens on one
line — `querycontract.NewK8sWorkloadMatchTarget` becomes
`kubernetes.NewWorkloadMatchTarget` and `querycontract.K8sSelectMatchInputFromEntity`
becomes `kubernetes.SelectMatchInputFromEntity`. No call site, argument,
allocation, loop bound, batch size, transaction scope or query text changes.

Baseline and after are the same program. That is not an assertion from reading
the diff: normalising each moved file through the intended rename map, the
base-symbol qualification and the package/import lines, then diffing against the
moved file, yields **0 residual lines** for both files. The check is not blind —
seeding a single behavioural mutation (`strings.TrimSpace` to `strings.ToLower`)
into the pre-move file yields 2 residual lines. So the compiled behaviour is
identical by construction, and a before/after measurement would be measuring the
same instructions twice.

The diff adds **no** Cypher: `git diff <base>...HEAD -- '*.go'` matched zero added
lines containing `MATCH `, `MERGE `, `UNWIND `, `CREATE (`, `DETACH DELETE` or a
parameterised `SET`. No graph write, worker claim, lease, batching knob or
runtime Compose/Helm setting is touched, so there is no backend, input shape,
row count or terminal queue depth for this change to move. Backend version is
therefore not a variable here.

No-Observability-Change: no span, metric, log or status field is added,
removed or renamed. `LogSelectMixedVintageDrop` keeps its name, its
`DebugContext` level and its fields; it moved packages and lost its `K8s`
prefix, which changes the Go identifier and not the emitted record.

Why it is safe: `go vet ./...` exit 0; `go test ./internal/query/... -count=1`
exit 0 across 54 packages; `go test -list` still discovers all 44 `K8s`-named
tests in `internal/query` and `internal/query/impact`, so the suite that covers
this path still runs rather than merely still compiling.
