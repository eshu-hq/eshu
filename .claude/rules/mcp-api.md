---
paths:
  - "go/internal/mcp/**/*.go"
  - "go/internal/query/**/*.go"
  - "docs/public/reference/http-api.md"
---

# MCP tools and the HTTP query surface

**Load `eshu-mcp-call-rigor`.** Add `cypher-query-rigor` when the handler issues
graph reads.

OpenAPI stays in lockstep with `go/internal/query/openapi*.go`, the handler
tests, and [HTTP API Reference](../../docs/public/reference/http-api.md). These
are a wire contract: a shape change that lands in one and not the others is
visible to callers before it is visible to the tests.
