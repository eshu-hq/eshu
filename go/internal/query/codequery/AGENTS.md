# AGENTS.md — `go/internal/query/codequery`

Scope: the code-family query handlers, split out of root package `query`
(#6060 lane A). Agents working here MUST read the package
[README](README.md) and [doc.go](doc.go) first.

## Ownership

- This package owns `*CodeHandler`, its `Mount` route table, and the
  NornicDB/postgres readers behind `/api/v0/code/*`. It MUST NOT
  construct `LanguageQueryHandler` (root-owned; would be a cycle) or
  register capabilities (root owns the router and the contract
  matrix).
- Root `code_alias.go` owns the `CodeHandler` alias; root
  `code_seam.go` owns the exports staying callers name. When the next
  lane moves a staying caller, delete the corresponding seam entry --
  never extend the seam for new callers.
- `codemodel` owns read-model builders and response shapers;
  `querycontract` owns envelopes, capabilities, and the access
  filter; `queryauth` owns request auth bounds. This package MUST NOT
  re-declare those -- qualify to them.

## Import discipline (cardinal)

NEVER import root package `query` -- not in production code, not in
tests (not even `package codequery_test`: the external test package
may name root only for seam assertions that cannot live anywhere
else, e.g. `seam_crosspackage_test.go`). A future agent breaks
this blind: `go build ./...` is the tripwire, and it stays green
only while this rule holds. Allowed: stdlib plus the leaves named in
[doc.go](doc.go).

## Export discipline (export-minimal)

Every export exists because a staying root caller, the seam, or a
cross-family manifest test names it. Do NOT export anything else; do
NOT unexport anything without moving its staying callers first.
Deliberate seam-adjacent exports: `SearchEntitiesForGrant`,
`RedactHardcodedSecretLine`, `NornicDBRelationshipStory{Graph,
InheritanceDepth,AnchorLookup}Cypher`, `CallGraphMetricsEdgesCypher`
(each carries a comment naming its cross-package caller).

## Digest discipline (no re-freezing)

- Queryplan-pinned builders relocate only. `cypher_sha256` NEVER
  changes; a `source_sha256` changes only when the declaration bytes
  changed for a sanctioned reason (a seam-forced export rename), and
  the new value comes from the gate's own mismatch report -- never
  invented.
- Prefer the alias technique over touching a digest: a same-named
  type/const alias (`relationshipStoryRequest`,
  `callGraphMetricsEdgeScanLimit`) keeps moved declaration bytes
  identical. Each alias carries a comment saying which pin it serves.
- `file:` paths in
  `go/internal/queryplan/testdata/{handler-hot-cypher,hot-cypher}.yaml`
  MUST track the builder's real location.

## Test discipline

- Pin production Cypher bytes over HTTP (`httptest` against the real
  handler or `Mount`), not over internals. Remember `handleSearch`
  probes with limit+1, and graph-empty falls through to the content
  fallback -- seed one graph row for graph-path baselines.
- Fixture paths are package-depth sensitive: this package sits one
  level below root, so `tests/fixtures/...` needs four `..`
  segments, not three.
- The `live_nornicdb_*` tag-gated proofs use the test-only
  `newLiveNornicDBReader` (no production read policy). Vet every tag
  after touching a live file; the default build skips them.
- `auth_scoped_*` files here are code-family grant proofs (#5167
  batch 1). Shared helpers hoist to `querytestutil` as exported
  non-test code; never twin a helper across the two packages.

## Verification (paste all)

```bash
cd go && go build ./internal/query/...
cd go && go vet -gcflags=-e ./internal/query/...
cd go && go test ./internal/query/ ./internal/query/codequery/ -count=1
cd go && go test ./internal/queryplan/ -count=1
git diff --check
gofmt -l <touched files, repo-relative>
```
