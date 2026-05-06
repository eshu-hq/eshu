# Contributing Parser Support

Parser support in Eshu lives in the checked-in parser
packages:

```text
go/internal/parser/registry.go
go/internal/parser/*.go
go/internal/parser/*_test.go
```

The public parser pages under `docs/docs/languages/`, plus
`feature-matrix.md` and `support-maturity.md`, are checked-in documentation for
that runtime. Keep them aligned with the implementation and tests when a
parser contract changes.

## Contract Model

Every parser contract should remain explicit in three places:

- registry metadata in `go/internal/parser/registry.go`
- implementation in the language-specific parser files
- verification in parser-level and integration tests

Framework and ecosystem semantics follow the same runtime contract. When you
expand semantic coverage, update the relevant parser implementation and tests,
then update the public language pages to match what the runtime actually emits
and what the graph surfaces end to end.

### Status semantics

- **supported** — extracted, surfaced end-to-end, covered by unit and integration tests
- **partial** — only the documented subset is promised
- **unsupported** — intentionally not claimed, but documented so the absence is explicit

Parse-only features must not remain `supported`.

## Workflow

1. Add or update Go parser tests first.
2. Implement or adjust the Go parser/runtime behavior.
3. Add or update integration coverage for the surfaced graph behavior.
4. Update the affected language page, feature matrix, or support-maturity page.
5. Run the relevant Go tests and the docs build.
6. When support-maturity claims change, back them with a real indexing run or
   compose-backed proof rather than fixture-only evidence.

## Writing Good Parser Docs

A reviewer should be able to answer:

- What does the Go parser claim to extract?
- What does the graph actually expose?
- Which Go test proves the parser behavior?
- Which integration test proves the end-to-end indexed behavior?
- What is intentionally partial or unsupported?

## Verification

Parser changes should normally include:

```bash
cd go
go test ./internal/parser ./internal/collector ./internal/content/shape -count=1
golangci-lint run ./...
```

Then rebuild the docs:

```bash
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

## Testing Rules

For `supported` capabilities:

- one Go unit or package test validates extraction and required fields
- one integration or compose-backed proof validates persisted or queryable
  end-to-end behavior

For support-maturity promotions:

- use at least one real repository run or compose-backed indexing proof
- document the evidence in the relevant language page or support notes
  `end_to_end_indexing: supported`

**For `partial` capabilities:**

- The spec describes only the part that is truly supported
- Tests match that narrower claim
- The rationale explains the missing or deferred parts

**For `unsupported` capabilities:**

- Keep it in the checklist instead of silently omitting it
- Add negative coverage proving the parser and graph surface do not overclaim

## Review Checklist

Before approving parser-support changes:

- [ ] Parser behavior changed under test-first discipline
- [ ] Capability YAML matches actual parser output
- [ ] Graph/query surface matches claimed `supported` capabilities
- [ ] Generated docs were regenerated, not hand-edited
- [ ] Unit and integration references point to real tests
- [ ] `partial` and `unsupported` entries have concrete rationales
- [ ] framework-pack YAML changes are validated and still ship in package builds

If the YAML, tests, and generated docs disagree, fix the disagreement before merging.
