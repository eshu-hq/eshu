# Code Query Family (`go/internal/query/codequery`)

The code-family query handlers: `*CodeHandler`, its `Mount` route table
for `/api/v0/code/*`, and the NornicDB/postgres readers behind search,
symbols, structural inventory, topics, secrets, imports, call graph,
flow, relationships, dead code, complexity, quality, call chain,
route-to-caller, cypher, visualize, and bundles.

It moved out of root package `query` (#6060 lane A) so the family can be
read, tested, and changed without pulling in the rest of the query
surface. The directory stays under the dirgate file cap.

## How it connects

- Root keeps `type CodeHandler = codequery.CodeHandler`
  (`go/internal/query/code_alias.go`) and the seam
  (`go/internal/query/code_seam.go`) for staying callers: content
  readers, the contract capability matrix, and both cmd wirings.
- `codemodel` (sibling leaf) owns the read-model builders, row
  decoders, and response shapers this family's handlers execute.
- Language-specific queries are NOT here: `language.Handler` lives in
  the sibling leaf `go/internal/query/language` (root aliases it as
  `LanguageQueryHandler`) and mounts from `APIRouter.Mount` via its own
  `Language` field. That leaf imports this package, so this package must
  not import it back. Both cmd wirings construct it with
  Neo4j/Content/Profile/Logger.
- Tests that pin production Cypher bytes drive handlers over HTTP
  (`httptest` + `handleSearch`-style direct handler calls), never
  over internal helpers: `handleSearch` probes with limit+1, so a
  body limit of 9 drives the graph builder with 10.
- Repository-scoped code-search fallback asks `ContentStore.SearchCodeCandidates`
  for language and exact-name predicates before each bounded content page.
  The unfiltered fallback keeps its existing name-then-source reads.

## Contracts (must hold)

- **Never import root package `query`.** Root imports this package;
  the reverse is an import cycle. Qualify to the leaves instead.
- **Relocated, not rewritten.** Queryplan-pinned builders keep their
  emitted Cypher byte-identical. Where the move forced a spelling
  change (a seam export, a leaf qualification), a same-named alias
  (`relationshipStoryRequest`, `callGraphMetricsEdgeScanLimit`)
  preserves the declaration bytes so `source_sha256` keeps matching.
  Never re-freeze a `cypher_sha256`.
- **Moving a builder moves its pin.** Update the `file:` path (and
  the `source_sha256` only when the declaration bytes changed for a
  sanctioned reason) in
  `go/internal/queryplan/testdata/{handler-hot-cypher,hot-cypher}.yaml`
  and re-run `go test ./internal/queryplan/`.
- **Selector failures map in one place.** `ApplyRepositorySelectorForAccess`
  serves every `repo_id` selector in the family, including
  `POST /api/v0/code/language-query`. A reader fence or graph verdict answers
  503/504, any other backend read failure answers 500 with the fixed
  `selector.LookupFailureMessage` body and a span error (#7626), and an
  unmatched or ambiguous selector answers 400.
- **Live proofs stay honest.** `live_nornicdb_reader_test.go` is the
  test-only live-backend reader (no production read policy); the
  `live_nornicdb_*` tag-gated files prove query text against a real
  backend, not the policy layer.

## Failed reads (#7674)

Every code route answers a failed store or graph read with a fixed message
per route step (for example `code search failed to read the graph`,
`relationship story target resolution failed`, `call chain endpoint
resolution failed`, `code-flow read failed`, or `dead-code candidate scan
failed`), never the backend error text. The order at each failure is: the
route's own 503 sentinel check, `querycontract.WriteGraphReadError` (reader
fence or graph verdict answers the retryable 503/504 with `Retry-After`), the
route's 400/404/422 answers, then `tracing.WriteServerFailure` with the step's
constant (500, or 499 on a client cancel). `read_failure.go` holds the
helpers and the message constants.

A configuration-state sentinel (index not ready, backend not configured)
answers 503 with the sentinel's own fixed text and no `Retry-After`, even
when the store wraps it with detail. The code-flow routes no longer answer
408 on a client cancel; a cancel answers 499 and a deadline the graph layer
does not classify answers the fixed 500. Call-chain endpoint resolution keeps
its 400 disambiguation answer, which quotes the caller's selector, the
repository id, and each candidate's entity id, path, and start line; a store
or graph failure during that lookup is a 500
(`call_chain_lookup.go`). The Cypher and visualize routes keep their
`internal_error` envelope through `tracing.ServerFailureEnvelope`.
`server_failure*_test.go` covers each route family with a canary fault, a
client cancel, a stale reader, and the wrapped sentinels.

No-Regression Evidence (#7674): the change runs only after a read has already
returned an error. No Cypher, SQL, query parameter, call count, row bound, or
success path changed. The queryplan `source_sha256` pins for
`handleCallChain`, `handleCypherQuery`, `handleVisualizeQuery`, and
`handleSearchBundles` were re-pinned from the gate's mismatch report because
only their error branches changed; no `cypher_sha256` moved. A failure now
costs one span `RecordError`/`SetStatus` and a fixed-string write instead of
formatting the error into the body. `go test ./internal/query/...
./internal/queryplan/... -count=1` and `go test -race
./internal/query/codequery/...` exit 0. No benchmark is claimed because the
success path has no runtime delta to measure.

Observability Evidence (#7674): a server fault records the backend error on
the handler span as an `exception` event and sets status Error with the step's
fixed message; a client cancel adds `eshu.request.client_canceled`, leaves the
status Unset, and answers `499`. A sentinel 503 records the error and sets
status Error with the sentinel text. The call-graph metrics and import routes
still record exactly one exception on their 422 scope and graph-verdict
branches. `server_failure*_test.go` asserts these span shapes with a
recording tracer.

## Verification

```bash
cd go && go build ./internal/query/...
cd go && go vet -gcflags=-e ./internal/query/...
cd go && go test ./internal/query/ ./internal/query/codequery/ -count=1
cd go && go test ./internal/queryplan/ -count=1
cd go && go test ./internal/query/ -run 'QueryplanManifestBindsProduction' -count=1
```
