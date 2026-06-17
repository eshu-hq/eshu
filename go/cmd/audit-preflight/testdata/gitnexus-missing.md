### Competitor source and local path

GitNexus (local clone /local/gitnexus): offers a natural-language commit-history
timeline view that Eshu does not expose.

### Eshu code evidence

Searched `go/internal/query` and `go/internal/mcp` for commit-timeline surfaces:
freshness/changed-since exists (`get_changed_since`) but no narrative commit
history timeline. No matching capability in the catalog.

### Eshu docs evidence

docs/public/reference/ has freshness and changed-since pages but no
commit-timeline capability documentation.

### Eshu test or proof evidence

No tests cover a commit-history timeline; only changed-since contract tests
exist (contract_changed_since.go).

### Existing issue duplicate search

Searched "commit timeline", "commit history" in open issues — no existing
issue or PR. Not covered by the freshness epic.

### Gap class

missing

### Owner surface

api

### Verification plan

Create a new child issue under the relevant epic. Closing requires a query
handler, MCP tool, capability-matrix row, and focused tests.
