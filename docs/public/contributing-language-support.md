# Contributing Parser Support

Parser support lives in the Go parser registry, parser implementation, and
tests. Public language pages document that runtime contract; they are not a
separate source of truth.

Primary files:

```text
go/internal/parser/registry.go
go/internal/parser/*.go
go/internal/parser/*_test.go
docs/public/languages/
```

## Contract Model

Every parser claim needs three anchors:

- registry metadata in `go/internal/parser/registry.go`
- implementation in the language-specific parser path
- parser-level and integration tests that prove the emitted rows and query
  surface

Status words mean:

| Status | Meaning |
| --- | --- |
| `supported` | Extracted, surfaced end to end, and covered by tests. |
| `partial` | Only the documented subset is promised. |
| `unsupported` | Intentionally not claimed. |

Parse-only behavior must not be documented as `supported`.

## Workflow

1. Add or update Go parser tests first.
2. Implement or adjust parser behavior.
3. Add integration or query coverage for surfaced graph behavior.
4. Update the affected language page and, when needed, the feature or support
   matrix.
5. Run focused Go tests and the docs build.
6. Back support-maturity promotions with real indexing or compose-backed proof,
   not parser fixtures alone.

## Writing Language Pages

Keep pages short and factual:

- identify the parser, entrypoint, fixture, and main tests
- summarize supported surfaces without duplicating the full matrix
- name the dead-code maturity and exactness blockers
- link to canonical references for deep detail

Use [Parser Feature Matrix](languages/feature-matrix.md),
[Parser Support Matrix](languages/support-maturity.md), and
[Dead Code Language Maturity](reference/dead-code-language-maturity.md) as the
canonical summaries.

## Verification

```bash
cd go
go test ./internal/parser ./internal/collector ./internal/content/shape -count=1
```

Then build docs:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

If YAML, tests, generated docs, and public pages disagree, fix the disagreement
before merging.
