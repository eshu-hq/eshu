# Agent instructions: rows

Read `doc.go` and `README.md` first.

## Invariants

- Do not add a new querycontract-eligible symbol here just because it is
  convenient. This package exists ONLY for (a) code that type-switches on a
  graph-driver type and (b) `GraphSemanticMetadataProjection`'s Cypher
  fragment, which querycontract's authorization-only fragment carve-out does
  not cover. Anything else belongs in querycontract or a query-owning family
  package.
- `GraphPathNodeProps` and `RouteToCallerEntityFromChain` MUST keep decoding
  both the `neo4j.Node` driver shape and the `map[string]any` fallback.
  Dropping either branch silently breaks one backend while the other keeps
  passing tests.
- This package is a named exception in `go/.golangci.yml`'s
  `query-no-graph-driver` depguard rule. Do not add a second file here that
  imports the driver without adding it to that exception list too -- the
  linter will fail the build, which is the point of the rule.

## When you change GraphSemanticMetadataProjection's text

Its literal contains no `MATCH`/`RETURN`/etc. keyword, so `#6060`'s
`query-literals.py` tree-wide check does not track it. Grep every caller of
`GraphSemanticMetadataProjection` (`language/entities.go`'s
`graphSemanticMetadataProjection` forwarder and the code-family readers in
`codequery`, `codequery/deadcode`, `codequery/relationships`, and `entity`) and confirm the column list they build around it
still lines up.

## Verification

From `go/`: `go build ./... && go vet ./internal/query/... && go test ./internal/query/... -count=1`.
