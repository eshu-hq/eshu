## Graph Analysis Compose Fixture

This fixture corpus exists for the Chunk 4 compose-backed graph-analysis gate.

Each top-level directory is treated as one repository by the local filesystem
source mode. The current corpus intentionally contains one small Go repository:

- `graph-analysis-go`
  - `main` calls `entrypointGraphProof`
  - `entrypointGraphProof` calls `dispatchGraphProof`
  - `dispatchGraphProof` calls `persistGraphProof`
  - `deadAlphaGraphProof` and `deadBetaGraphProof` are intentionally unused

The compose verification lane proves that a fresh full-stack bootstrap can:

- search for a known function by exact name
- resolve direct callers from canonical `CALLS` edges
- resolve transitive callers with the expected depth values
- resolve a shortest call chain path
- return only the intentionally unused functions from dead-code analysis
