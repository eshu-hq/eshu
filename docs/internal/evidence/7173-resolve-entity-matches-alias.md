# resolve_entity `matches` Alias Removal Evidence (#7173)

`POST /api/v0/entities/resolve` (`resolve_entity`) returned `matches`, a
byte-identical copy of `entities`, next to it. #7170 removed the same alias from
the four code and content search producers and left this route because the
console read it. This change drops it here together with the console change.

## Consumers checked

Every reader of the field, found by a repo-wide search for `"matches"`,
`.matches`, and `matches:` outside test fixtures and unrelated words:

| Consumer | Reads `matches`? | Action |
| --- | --- | --- |
| `apps/console/src/api/entityResolution.ts` | fallback when `entities` is not an array | fallback and the `matches` field type removed |
| `apps/console/prototype/eshu-console/console/pages-explorer-parity.jsx` | fallback when `entities` is not an array | fallback removed |
| other console files (`rg` over `apps/console`, excluding `node_modules`) | no | none |
| `go/cmd/eshu/find.go` (`eshu find name`) | no; prints the raw JSON | none |
| `go/internal/serviceintel/suggestions.go`, `go/internal/ask/catalog` | route metadata only | none |
| OpenAPI (`paths/search/entities.go`) and the HTTP/MCP docs | never advertised `matches`; they document `entities`, `count`, `limit`, `truncated` | one sentence added recording the removal |
| `testdata/golden/e2e-20repo-snapshot.json` `resolve_entity` `required_response_fields` | listed `matches` | entry removed in the same change |
| Go tests that pinned or stubbed the alias | `entity_content_fallback_test.go` (asserted the alias), two MCP stub fixtures | flipped to assert absence / stub trimmed |

All four resolve producers (graph, canonical content handle, global content
name, workload) build their body through `resolvedEntityResponse`, so one edit
removes the field from every path.

## Contract note

This is a removal, not an additive change; the issue asks for it. A client that
read `matches` on a response that also carried `entities` (every response did)
must read `entities`. The console fallback only fired when `entities` was
missing, which the producer never allowed.

## Proof

- Regression first: `TestResolvedEntityResponseDoesNotEmitMatchesAlias` failed
  on the old producer (`emits the removed matches alias`), and the console test
  `reads only the canonical entities field, not the removed matches alias`
  failed with the old fallback restored (`expected [ { filePath: '', ... } ] to
  deeply equal []`). Both pass after the change.
- `go test` over `internal/query/...`, `internal/mcp/...`,
  `cmd/golden-corpus-gate/...`, `internal/goldengate/...`, `internal/demospec/...`,
  `cmd/api/...`, `cmd/mcp-server/...` pass; `scripts/test-verify-golden-corpus-gate.sh`
  passes; the console `entityResolution` tests pass.
- The live golden-corpus run (Docker, `golden-corpus-gate-neo4j`) was not run
  locally; it asserts the same `resolve_entity` field list this change edits,
  and CI runs it.

## No-Regression Evidence

No-Regression Evidence (#7173): response-shaping only, on a route that reads
the same rows as before. Dropping the copy roughly halves the body: a
100-row resolve response (the route maximum) measured 68,468 bytes before and
34,256 after, 34,212 bytes saved per full page, from a throwaway
`json.Marshal` of `resolvedEntityResponse` with and without the alias. Less to
marshal and write; no query, SQL, Cypher, index, or queue path changes.

## Observability Evidence

No-Observability-Change: no metric, span, log, or status signal is added or
removed. The route's `mcp_response_over_budget` accounting sees fewer bytes.
