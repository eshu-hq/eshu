# CLI: Analysis & Search

Commands for extracting insights from indexed code.

## Code Analysis

### `analyze callers`

Find every function that calls a given function. Use this before refactoring to
understand who depends on it. Under the hood this routes to
`POST /api/v0/code/relationships` with `direction=incoming` and
`relationship_type=CALLS`.

```bash
eshu analyze callers process_payment
```

Add `--transitive` to walk indirect callers and `--depth` to cap the traversal:

```bash
eshu analyze callers process_payment --transitive --depth 7
```

### `analyze calls`

The reverse — show what a function calls (its callees). Under the hood this
routes to `POST /api/v0/code/relationships` with `direction=outgoing` and
`relationship_type=CALLS`.

```bash
eshu analyze calls process_payment
```

Use the same flags for indirect callees:

```bash
eshu analyze calls process_payment --transitive --depth 7
```

### `analyze chain`

Find the execution path between two functions. Useful for understanding how
data flows from one entry point to another. Use `--depth` to raise or lower
the traversal bound the Go API uses for shortest-path lookup.

```bash
eshu analyze chain handle_request process_payment --depth 5
```

### `analyze deps`

Show imports and dependencies for a module.

```bash
eshu analyze deps payments
```

### `analyze tree`

Show the class inheritance hierarchy for a given class.

```bash
eshu analyze tree BaseProcessor
```

### `analyze complexity`

Show relationship-based complexity metrics for a specific entity. The broader
threshold-based quality-gate flow is still tracked separately in the parity
matrix and is not yet part of the Go CLI contract.

```bash
eshu analyze complexity
```

### `analyze dead-code`

Find graph-backed dead-code candidates after the current default exclusions for
Go entrypoints, direct Go Cobra/stdlib-HTTP/controller-runtime framework roots,
Go exported public-package symbols, test files, and obvious generated code are
applied. Exported Go symbols remain candidates under `internal/`, `cmd/`, and
`vendor/`; only public-package exports are treated as default roots. The result
is intentionally `derived` today until broader framework, public-API, and
reflection root models land. Use `--repo` to scope the scan to one repository
by ID, name, slug, or path. `--repo-id` remains as a compatibility alias for
canonical IDs. Use `--exclude` to skip decorator-owned entry points such as
route handlers, `--limit` to raise or lower the bounded result window, and
`--fail-on-found` to turn the command into a CI gate. The response includes
`truncated=true` when more dead-code candidates existed than the bounded result
set returned.

```bash
eshu analyze dead-code --repo payments --limit 200 --exclude "@route" --fail-on-found
```

### `analyze overrides`

Show methods that override parent class methods.

```bash
eshu analyze overrides PaymentProcessor
```

### `analyze variable`

Find where a variable is defined and used across files.

```bash
eshu analyze variable MAX_RETRIES
```

---

## Discovery & Search

These commands search the graph index, not the raw filesystem. They operate on what Eshu has already parsed.

### `find name`

Find code elements by exact name.

```bash
eshu find name PaymentProcessor
```

### `find pattern`

Fuzzy substring search. Use this when you don't know the exact name.

```bash
eshu find pattern payment
```

### `find type`

List all nodes of a given type: `class`, `function`, or `module`.

```bash
eshu find type class
```

### `find variable`

Find variables by name across the graph.

```bash
eshu find variable config
```

### `find content`

Full-text search across source code and docstrings.

```bash
eshu find content "shared-payments-prod"
```

### `find decorator`

Find functions with a specific decorator.

```bash
eshu find decorator @app.route
```

### `find argument`

Find functions that accept a specific argument name.

```bash
eshu find argument user_id
```
