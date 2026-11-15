# #10707 repository ambiguity candidate fields: no-regression evidence

Scope: branch `drive/10707-selector-fields`. A repository-selector
ambiguity candidate gains `repo_name` and `match_basis` on top of the
`{repo_id}` object #10655 introduced. The gate flags
`go/internal/query/selector/selector.go` as a hot file by path; this
note records why the change cannot regress runtime cost.

No-Regression Evidence (candidate enrichment + fallback RETURN): the
catalog branch runs the same `MatchRepositories` call and derives
`repo_name` and `match_basis` in memory inside the existing switch
(one string comparison per field per candidate, O(n) in match count,
no I/O, lock, retry, or enqueue). The graph fallback's ordered read
adds `r.name` (already loaded on the matched node) and a six-branch
CASE to its RETURN; the CASE re-evaluates the WHERE predicates on the
matched row only, so it is constant work per row and changes neither
the row set, the `ORDER BY`, nor the plan. `ResolveExactForAccess`
stays a single-key `keyed_support` non-hot path (source digest
re-pinned in `queryplan/testdata/source-coverage/second.yaml`); no
hot-path statement, lock, claim, batch size, or order is added,
removed, or reordered. The success (0/1 match), 404, and 500 paths
keep their order and bodies.

Observability Evidence: the change adds no spans, metrics, or logs.
The operator-visible delta is two additive JSON fields (`repo_name`,
`match_basis`) on each ambiguity candidate, documented with a wire
example in `docs/public/reference/http-api/selector-and-read-failures.md`.
