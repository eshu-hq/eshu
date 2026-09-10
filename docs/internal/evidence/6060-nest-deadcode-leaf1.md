# Deadcode Leaf 1 — Evidence (#6060 naming follow-up)

Moves the dead-code scan bodies out of `codequery/` into the `deadcode`
leaf per the #6618 naming rules (rule 3: split glued compounds into
nested directories): `dead_code_scan.go` helpers to `deadcode/scan.go`,
`dead_code_candidate_entity.go` helpers to `deadcode/entities.go`, with
the thin `*CodeHandler` methods collected in `analyzer.go` (methods must
live in their type's package) and the misnamed `dead_code_verdicts.go`
(compat aliases, zero verdicts) renamed to `aliases.go`.

## Performance and observability

No-Regression Evidence: behavior-preserving by construction. All four
moved functions keep byte-identical bodies including their load-bearing
NornicDB comments; the two `*CodeHandler` methods change only by
qualifying same-package calls to `deadcode.*`. Anchors, cardinality,
bounds, and indexes are untouched: the candidate scan still anchors on
`(r:Repository)` / `(f:File)` with `SKIP $skip LIMIT $limit` after the
`WHERE`, and the incoming-edge probe keeps its `count(*)` aggregation
shape. The two queryplan `source_sha256` pins
(`deadCodeCandidateRows`, `deadCodeResultsWithGraphIncomingEdges`) were
recomputed with the repo's own go/parser extraction and validated by
`go test ./internal/queryplan/`; the 12 grandfathered digests verify
12/0/0. Package tests green: query, codequery, deadcode, queryplan,
entity, golden-corpus-gate unit contract.

No-Observability-Change: no span, metric, tracer, or pprof identifier
is added or renamed. No handler route, capability string, or response
shape changes.
