# Code Model Leaf (`go/internal/query/codemodel`)

The code-model query leaf: read-model builders, row decoders, response
shapers, and Cypher/postgres query constructors behind the find_code,
code-graph, complexity, dead-code, code-flow, import-dependency,
relationship-story, and code-search reads. (codeowners-ownership lives in
`internal/query/codeowners` since #6060 lane A L2.)

It moved out of root package `query` (#6060 lane A) so the family can be
read, tested, and changed without pulling in the rest of the query
surface. May import only dependency-neutral leaves (`querycontract`,
`queryauth`, `queryspan`, the `internal/search*` hybrid ranking packages,
`internal/facts`, `internal/codeprovenance`) — never on root package
`query` itself, which would create an import cycle: root's
`family_code_shim.go` imports this package for the compatibility aliases
the staying code family still uses.

## Contracts (must hold)

- **Read-model only.** These builders shape query results. Nothing here
  registers HTTP routes or capabilities, widens a caller-authorized
  scope, or performs an unbounded read — the scope gates live with the
  staying handlers that call these builders. Capability strings that
  travel with responses are leaf-owned values the staying contract
  matrix already registers; adding a NEW capability means registering
  it in root, not here.
- **Relocated, not rewritten.** Every builder is byte-identical to its
  root predecessor except the package clause, import qualifications to
  already-leaf-owned symbols, and the renames the export list records.
  Queryplan-pinned builders (`BuildSearchGraphEntitiesQuery`, the seven
  import-dependency Cypher builders) relocate byte-identical apart from
  those renames; their emitted Cypher text is unchanged
  (`cypher_sha256` pins prove it).
- **Type splits run one way.** Shared request/row/filter types the leaf
  needs moved here from their staying method files with their
  value-receiver methods (Go requires methods to live with their
  declaration); `*CodeHandler` route methods stay in root. The staying
  names survive as root aliases/forwarders in `family_code_shim.go`.
- **Family-local copies are byte-identical twins.** A few helpers shared
  with staying root readers that cannot cross the package boundary are
  copied here verbatim (each carries a provenance comment naming its
  root source). Change both copies together, or better: promote the
  helper to `querycontract` so the twin can die.

## Layout

- `code_call_graph_metrics_{aggregation,response}.go` — metric ranking,
  stats, and the response envelope (plus the split request type).
- `code_complexity_{ambiguity,page}.go`, `code_cypher_validation.go` —
  ambiguity refusal, list paging, read-only Cypher guard.
- `code_dead_code_*.go` — policy core, classification, exclusions,
  maturity, and per-language entrypoint roots.
- `code_flow_postgres.go` — Postgres flow reader plus the flow store
  contract it implements.
- `code_graph_search_query.go` — bounded entity-search builder.
- `code_hybrid_rerank.go` — in-process hybrid re-ranker.
- `code_import_dependencies_{queries,response,rows}.go` — the seven
  investigation builders plus envelope and row shaping.
- `code_relationship_story_{evidence_state,provenance}.go`,
  `code_relationships_{graph_response,resolution}.go` — story contract,
  evidence classification, graph shaping, name resolution.
- `code_search_page.go` — code-search page envelope.
- `doc.go`, `AGENTS.md` — package contract and per-symbol export list.

## Verification

```bash
cd go && go build ./internal/query/...
cd go && go test ./internal/query/ ./internal/query/codemodel/ -count=1
bash scripts/verify-route-coverage.sh
bash scripts/verify-dirgate.sh --digest internal/query
```

Moving a builder also moves its queryplan pin: update the `file:` path
(and the `source_sha256` when the declaration bytes change) in
`go/internal/queryplan/testdata/{handler-hot-cypher,hot-cypher}.yaml`
and re-run `go test ./internal/queryplan/`.

## Performance evidence (code PR1 move)

No-Regression Evidence: this PR moves code-family builders from the query
root into this leaf via git mv and repoints manifests/shas; it changes no
Cypher text, thresholds, ranking, pagination, or query-planning logic.
Baseline is origin/main with green suites; after the move, on this branch,
`go test ./internal/query/... -count=1` passes 11/11 packages with 0
failures, and the B-7 golden-corpus gate passes (elapsed 139s of an 1800s
ceiling) with 594 corpus checks green, which includes the B-12
e2e-20repo-snapshot byte comparison. Backend/version is unchanged (same
NornicDB-first contract over the same driver path B-7 exercises live),
input shape is the full query unit suite plus the 20-repo golden corpus,
and the terminal counts are 11/11 packages ok plus 594/594 corpus checks.
The change is safe because behavior is preserved by construction (a
path-only move) and proven by the unchanged suites and corpus above.

No-Observability-Change: no spans, metrics, structured logs, status
fields, or pprof surface were added, removed, or renamed; the move adds no
new query path, so dashboards and 3 AM triage read exactly as before.
