# CLI: Analysis & Search

Commands for extracting insights from indexed code. These commands read the API
query surface after repositories are indexed; they do not scan the filesystem
directly. For the full command inventory, see [CLI Reference](cli-reference.md).

## Code Analysis

| Command | Use it for | API route |
| --- | --- | --- |
| `eshu analyze callers <function>` | Direct or transitive callers of a function before refactoring. Add `--transitive` and `--depth` for indirect traversal. | `POST /api/v0/code/relationships` with incoming `CALLS` |
| `eshu analyze calls <function>` | Direct or transitive callees of a function. Add `--transitive` and `--depth` for indirect traversal. | `POST /api/v0/code/relationships` with outgoing `CALLS` |
| `eshu analyze chain <from> <to>` | Shortest call path between two functions. Use `--depth` to bound traversal. | `POST /api/v0/code/call-chain` |
| `eshu analyze deps <module>` | Import and dependency relationships for one module. | `POST /api/v0/code/relationships` with outgoing `IMPORTS` |
| `eshu analyze tree <class>` | Inheritance relationships for one class. | `POST /api/v0/code/relationships` with bidirectional `INHERITS` |
| `eshu analyze complexity` | Relationship-based complexity metrics. | `POST /api/v0/code/complexity` |
| `eshu analyze dead-code` | Graph-backed dead-code candidates. Use `--repo`, `--limit`, `--exclude`, and `--fail-on-found` to scope or gate the result. | `POST /api/v0/code/dead-code` |
| `eshu analyze overrides <name>` | Parent-method override implementations. | `POST /api/v0/code/relationships` with incoming `OVERRIDES` |
| `eshu analyze variable <name>` | Variable definitions and usage. | `POST /api/v0/code/search` |

Most analysis commands accept remote flags. Relationship commands also accept
`--repo` and `--repo-id`; `analyze variable` does not register repository
selector flags today.

Dead-code results are intentionally `derived` until broader framework,
public-API, and reflection root models land. The response includes
`truncated=true` when more candidates exist than the bounded result set
returned.

## Discovery & Search

These commands search the indexed graph or content store, not the raw
filesystem.

| Command | Use it for | API route |
| --- | --- | --- |
| `eshu find name <name>` | Legacy graph-domain name resolution. It sends the name unchanged and never widens a refused graph lookup into content search. Current servers fail closed for an untyped global name; use a typed/scoped `resolve_entity` request or `find pattern` when global content-name search is intended. | `POST /api/v0/entities/resolve` |
| `eshu find pattern <text>` | Substring search when the exact symbol is unknown. | `POST /api/v0/code/search` |
| `eshu find type <type>` | Nodes of a given type, such as `class`, `function`, or `module`. | `POST /api/v0/code/search` |
| `eshu find variable <name>` | Variables by name. | `POST /api/v0/code/search` |
| `eshu find content <text>` | Full-text search across source content. | `POST /api/v0/content/entities/search` |
| `eshu find decorator <name>` | Functions with a decorator. | `POST /api/v0/code/search` |
| `eshu find argument <name>` | Functions that accept an argument name. | `POST /api/v0/code/search` |
