### Competitor source and local path

CodeGraphContext (local clone /local/codegraphcontext): provides semantic
vector retrieval over code. Eshu's hybrid retrieval is partially built.

### Eshu code evidence

`go/internal/searchretrieval`, `go/internal/searchhybrid`, and the
`semantic_search.curated_retrieval` capability exist; embedder work is in
progress. The catalog lists semantic search as gated/preview where applicable.

### Eshu docs evidence

docs/public/reference/capability-conformance-spec.md and the curated search lane
docs describe the gated state.

### Eshu test or proof evidence

go test ./internal/searchretrieval and the search-bench evidence under
docs/public/reference/searchbench-evidence cover the partial state.

### Existing issue duplicate search

Searched "semantic retrieval", "embedder", "hybrid search" — already tracked by
the hybrid retrieval epic #2676 and its children. Duplicate.

### Gap class

already tracked

### Owner surface

search

### Verification plan

No new issue. Link the finding to epic #2676; verify against its acceptance
evidence.
