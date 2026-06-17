### Competitor source and local path

graphify (local clone /local/graphify): exposes a code-relationship graph with a
REST list endpoint for symbols.

### Eshu code evidence

Eshu already serves symbol lookups via `go/internal/query/code.go`
(`/api/v0/code/search`, `/api/v0/code/symbols`) and `find_symbol` in
`go/internal/mcp/tools_codebase.go`. The capability catalog lists
`code_search.symbol_lookup` as general_availability.

### Eshu docs evidence

docs/public/reference/http-api/code.md and the capability catalog
(docs/public/reference/capability-catalog.md) document the symbol surfaces.

### Eshu test or proof evidence

go test ./internal/query (code_call_graph_contract_test.go) and the matrix
verification rows for code_search.symbol_lookup.

### Existing issue duplicate search

Searched open issues for "symbol lookup", "code search" — covered by the
shipped capability; no new gap. Epic #2711 catalog makes this discoverable.

### Gap class

foundation exists

### Owner surface

api

### Verification plan

No new issue. If a surfacing gap is confirmed, attach to the existing code
search capability and run `go test ./internal/query`.
