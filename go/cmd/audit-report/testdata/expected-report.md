# Competitive Audit Report

3 findings — link_existing_issue: 2, no_issue: 1

## Findings

| Competitor | Feature | Gap class | Recommendation | Detail | Competitor files | Eshu evidence |
| --- | --- | --- | --- | --- | --- | --- |
| codegraphcontext | semantic retrieval embedder | already tracked | `link_existing_issue` | already tracked; link #2676 | `src/embed/index.py` | `go/internal/searchretrieval/doc.go` |
| gitnexus | commit history timeline | missing | `link_existing_issue` | missing but a similar issue exists; link #1841 | `src/timeline.ts` | `go/internal/query/contract_changed_since.go` |
| graphify | symbol relationship lookup | foundation exists | `no_issue` | foundation exists; surface or document the existing capability instead of a new build | `src/graph/symbols.ts` | `go/internal/query/code.go` |
