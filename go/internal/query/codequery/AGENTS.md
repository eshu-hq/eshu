# AGENTS.md — `go/internal/query/codequery`

Scope: the code-family query handlers, split out of root package `query`
(#6060 lane A). Agents working here MUST read the package
[README](README.md) and [doc.go](doc.go) first.

## Ownership

- This package owns `*CodeHandler`, its `Mount` route table, and the
  NornicDB/postgres readers behind `/api/v0/code/*`. It MUST NOT
  construct `language.Handler` (the `language` leaf imports this
  package, so importing it back would be a cycle) or
  register capabilities (root owns the router and the contract
  matrix).
- Root `code_alias.go` owns the `CodeHandler` alias; root
  `code_seam.go` owns the exports staying callers name. When the next
  lane moves a staying caller, delete the corresponding seam entry --
  never extend the seam for new callers.
- `codemodel` owns read-model builders and response shapers;
  `querycontract` owns envelopes, capabilities, and the access
  filter; `auth` owns request auth bounds. This package MUST NOT
  re-declare those -- qualify to them.

## Import discipline (cardinal)

NEVER import root package `query` -- not in production code, not in
tests (not even `package codequery_test`: the external test package
may name root only for seam assertions that cannot live anywhere
else, e.g. `seam_crosspackage_test.go`). A future agent breaks
this blind: `go build ./...` is the tripwire, and it stays green
only while this rule holds. Allowed: stdlib plus the leaves named in
[doc.go](doc.go).

## Repository selector errors (`repository_selector.go`)

- `ApplyRepositorySelectorForAccess` MUST keep the order
  `WriteGraphReadError` (503/504), then `selector.WriteLookupFailure` (500,
  fixed body, span error), then 400. Never write `err.Error()` of a
  `selector.LookupError` to a body: it carries backend text (#7626).
- Not-found stays 400 in this family. Moving it to 404 is a separate
  contract change, not part of a lookup-failure fix.

## Bundles read (`registry_bundles.go`)

- `searchRegistryBundlesCypher` MUST stay anchor-only: no `OPTIONAL MATCH`,
  no `WITH ... count()`. The pinned NornicDB silently ignores the `ORDER BY`
  and `LIMIT` that follow a `WITH p, count(v)`, so the count comes from
  `registry.VersionCountsByPackageID` for the returned page (#5167).
- A scoped caller gets `p.visibility = 'public'`; an empty grant returns an
  empty page with no graph call. Do not add a repository-id predicate: a
  `Package` node carries no repository key.
- `package/registry` is imported for that one exported function only. It
  imports no `codequery`, so the direction stays legal; do not import more.

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

## Failed reads (#7674)

Every store or graph read failure goes through `writeCodeReadFailure`
(`read_failure.go`): `querycontract.WriteGraphReadError` first (fence 503,
graph 503/504), then `tracing.WriteServerFailure` with a fixed message
constant for that route step (500, or 499 for a client cancel). A
configuration-state sentinel goes through `writeCodeUnavailable` before that,
which answers 503 with the sentinel's own text. Declare a new step's message
as a `const` in `read_failure.go`. Never write `err.Error()` or a message
formatted with `err` to a 4xx/5xx body; the query error-text guard holds
every file in this package at zero sites. Only client-built text (selectors,
candidate ids with their paths and start lines, locally computed counts) may
reach a 400/422 body; wrap a
store error that shares a return path with such text, as
`callChainLookupError` does. When a capability is computed by a call
(`relationshipCapability`), call `WriteGraphReadError` inline rather than
through the helper: the root capability sweep resolves a helper parameter
across all its callers with one visited set, so a second computed argument
reads as unresolvable. `server_failure*_test.go` pins each family.

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
  batch 1). Shared helpers hoist to `testutil` as exported
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

## find_code rank walk (#7746)

- `handleRankWalk` (`rank_walk.go`) MUST pick its lane through
  `selectCodeSearchLane`, the same graph-first choice an offset request
  makes (fork-6 ruling). Never force a lane: the cursor records the lane, and
  a lane change between pages must surface as 409 `basis_changed`.
- A `rank_cursor` request never falls back to an offset page: every refusal
  goes through `rank.Answer` with a `rank.Reason*` constant.
- Keep walk logic in `rank_walk.go` or the generic
  `go/internal/query/search/rank`, not in `handler.go`.
