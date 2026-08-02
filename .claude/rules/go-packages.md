---
paths:
  - "go/**/*.go"
---

# Go package work

**Load `golang-engineering`.**

**Read the `AGENTS.md` in the directory you are editing.** This is the rule that
matters most here, because Claude Code cannot see those files on its own: it
reads `CLAUDE.md`, not `AGENTS.md`, and this repository carries a scoped
`AGENTS.md` in every Go package directory under `go/`. Codex loads the one for
the directory it is working in; Claude does not. If you are editing
`go/internal/reducer/foo.go`, `go/internal/reducer/AGENTS.md` is written for you
and you have not read it.

Its siblings serve different audiences and are worth the read for different
reasons: `doc.go` is the godoc contract, `README.md` is the human architecture
and operational context.

Surfaces that need a second skill on top of `golang-engineering`:

| If the file touches | Also load |
| --- | --- |
| Cypher, graph reads/writes, indexes | `cypher-query-rigor` |
| Postgres SQL, DDL, migrations, queue queries | `eshu-postgres-rigor` |
| workers, leases, conflict keys, retries, queue ordering | `concurrency-deadlock-rigor` |
| correlation, materialization, deployment tracing | `eshu-correlation-truth` |
| MCP or API tool calls, bounded graph-backed queries | `eshu-mcp-call-rigor` |
| benchmarks, query/index optimization, throughput | `eshu-performance-rigor` |
| fact kinds, payload shapes, `sdk/go/factschema` | `eshu-contract-rigor` |
