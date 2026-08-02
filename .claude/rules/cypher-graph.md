---
paths:
  - "go/internal/storage/cypher/**/*.go"
  - "go/internal/relationships/**/*.go"
---

# Cypher and graph writes

**Load `cypher-query-rigor`.** Add `eshu-performance-rigor` for any change on a
hot path, and `eshu-correlation-truth` when the change decides what a correlation
means.

Read [Cypher Performance](../../docs/public/reference/cypher-performance.md)
before changing hot-path Cypher, graph writes, or schema DDL. For NornicDB
behavior, [NornicDB Pitfalls](../../docs/public/reference/nornicdb-pitfalls.md)
documents where the backend diverges from what the query text implies — including
cases that return wrong counts rather than failing.
