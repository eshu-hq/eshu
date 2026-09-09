# AGENTS.md — `go/internal/query/codequery/deadcode`

Scope: dead-code analysis for the code-family queries, split out of
`codequery` (#6060 lane A). Agents working here MUST read the package
[README](README.md) and [doc.go](doc.go) first, plus the parent
[codequery AGENTS.md](../AGENTS.md) for the family-wide digest and
export disciplines.

## Ownership

- This package owns dead-code candidate scanning, cross-repo consumer
  evidence, investigation packets, downgraded-root verdicts, and the
  default reachability policy behind `/api/v0/code/dead-code*`.
- `codequery` owns `*CodeHandler`, its `Mount` route table, and the
  two pinned row readers (`deadCodeCandidateRows`,
  `deadCodeResultsWithGraphIncomingEdges`); this package MUST NOT
  re-declare those -- call them through `Dependencies` func fields.
- `codemodel` owns read-model builders and response shapers;
  `querycontract` owns envelopes, capabilities, and the access
  filter; `queryauth` owns request auth bounds. Qualify to them.

## Import discipline (cardinal)

NEVER import package `codequery` or root package `query` -- not in
production code, not in tests. Both import this package; the reverse
is an import cycle. `go build ./...` is the tripwire. Allowed:
stdlib plus the leaves named in [doc.go](doc.go).

## Analyzer discipline

- `Analyzer` is stateless. `codequery` delegates build one per call
  from the handler's own fields; add no request state and no caching.
- A staying `codequery` dependency enters only as a `Dependencies`
  func field with a comment naming the helper. When that helper moves
  to a leaf, repoint the field and drop the `codequery` reference.
- New exports need a staying caller named in the doc comment
  (delegate, seam, grant proof, staying test). Unexported-migrated
  helpers stay unexported.

## Test discipline

- Tests that pin production Cypher bytes drive the `Analyzer`
  methods or stay in `codequery` driving the delegates over HTTP --
  never over a copied implementation.
- A test that needs both `deadcode` internals and the `codequery`
  handler cannot live in either package; keep it in `codequery`
  against the delegate surface and qualify `deadcode` exports.
- Fixture paths are package-depth sensitive: this package sits two
  levels below root, so `tests/fixtures/...` needs five `..`
  segments, not three.

## Verification (paste all)

```bash
cd go && go build ./internal/query/...
cd go && go vet -gcflags=-e ./internal/query/...
cd go && go test ./internal/query/ ./internal/query/codequery/ ./internal/query/codequery/deadcode/ -count=1
cd go && go test ./internal/queryplan/ -count=1
git diff --check
gofmt -l <touched files, repo-relative>
```
