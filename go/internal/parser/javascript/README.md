# JavaScript Parser

## Purpose

This package parses JavaScript, TypeScript, and TSX into Eshu parser payloads.
It owns tree-sitter traversal, import/export rows, TypeScript declaration
evidence, call metadata, `package.json` public-surface roots, tsconfig alias
resolution, and syntax-grounded dead-code root evidence.

## Ownership Boundary

The package stays below the parent parser engine. It accepts a `ParserFactory`
from the caller and must not import `internal/parser`. Registry dispatch,
grammar caching, worker orchestration, parse timing, fact emission, graph
projection, and query truth live outside this package.

## Exported Surface

See `doc.go` and source comments for the godoc contract. Main exports include
`Parse`, `PreScan`, `ParserFactory`, tsconfig resolver helpers, package manifest
helpers, and `ExpressServerSymbols`.

## Telemetry

This package emits no metrics, spans, or logs. Parent parser runtime
instrumentation records parse timing and failures.

## Gotchas / Invariants

- tsconfig files are JSONC; comments and trailing commas are accepted before
  unmarshalling.
- Alias resolution is repository-bounded. Absolute or escaping candidates return
  no result.
- Package helpers use the nearest `package.json`; a workspace root manifest
  must not claim files under a nested package.
- Declaration targets ending in `.d.ts` can map back to authored source
  candidates such as `src/index.ts`.
- Dead-code roots are evidence rows, not guesses. Public-surface walks stay
  static, repository-bounded, and depth-capped.

## Focused Tests

```bash
cd go
go test ./internal/parser/javascript -count=1
go run ./cmd/eshu docs verify ../go/internal/parser/javascript --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related Docs

- `docs/public/contributing-language-support.md`
- `docs/public/architecture.md`
