# #6915 Eshu half: Go-side complexity re-sort evidence

## Problem

`listMostComplexFunctions` (`go/internal/query/codequery/complexity_queries.go`)
asked for `ORDER BY complexity DESC, e.name, e.id LIMIT $limit` (with
`limit = requested + 1`) and truncated backend delivery order in Go via
`trimComplexityResults`. The #6782 capture legs proved NornicDB mis-sorts
multi-key ORDER BY on distinct keys, so the same rows arrived in different
order per backend and the wrong members survived truncation into the API
answer (user-visible). The upstream sort fix stays tracked in #6915; this
note covers the Eshu hardening only.

## Fix

Stable re-sort by the query's documented keys (complexity DESC, name ASC,
entity_id ASC for `e.id`) in Go after row mapping and before trimming. The
entity_id tiebreak keeps the order total (ids are unique), so the order of
the returned window is identical across backends. Residual (#6915 upstream):
the backend `LIMIT` still applies to backend order first, so when matching
functions exceed limit+1, true top-N rows can be dropped backend-side before
Go sees them — full determinism needs the upstream sort fix. No Cypher text
change, no response-shape change (same fields, same truth envelope).

## Proof

- RED: `TestListMostComplexFunctionsResortsBackendDelivery` failed pre-fix
  (backend-misordered rows, limit 2: `function-low`/complexity 3 survived
  over `function-mid`/complexity 9); GREEN post-fix with correct
  top-2 membership and order.
- `TestListMostComplexFunctionsBreaksNameTiesByEntityID` pins the
  deterministic tiebreak.
- `go test ./internal/query/codequery/ -count=1` PASS (full package);
  `go vet` clean; `gofumpt -l` clean; `git diff --check` clean.

No-Regression Evidence: this is a read-path ordering hardening, not a
throughput change — there is no latency delta to bench. The no-regression
case is: (a) the sort runs over at most `limit + 1` rows (max 101: clamp is
10 default / 100 max in `NormalizeComplexityListLimit`), each a small
string/int map — microseconds against the graph round trip that dominates
this handler; (b) the Cypher statement, its parameters, and the response
shape are byte-identical, so backend plan, index use, and downstream
consumers (HTTP + MCP `code_quality.complexity`) are unaffected; (c) full
`codequery` package green plus the pre-existing complexity handler tests
unchanged and passing. Backend sort behavior itself is upstream (#6915);
unit delivery-order proof suffices here because the hardening makes no
backend claim — it removes the handler's dependence on delivery order.

No-Observability-Change: no metric, span, log field, or status contract is
added or renamed. The handler's existing `truncated`/`limit` response fields
already expose the truncation outcome; row order within the existing shape
needs no new signal.
