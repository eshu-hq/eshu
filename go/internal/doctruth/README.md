# Doc Truth

## Purpose

`doctruth` extracts documentation mentions and checkable claim candidates, then
verifies explicit claims against caller-supplied truth sources. It powers
documentation evidence packets and the `eshu docs verify` workflow without
treating prose as operational truth.

## Ownership Boundary

The package is deterministic extraction and verification code. It does not call
Confluence, GitHub, databases, graph stores, LLMs, or the filesystem on its own.
Callers provide section text, hints, links, command trees, OpenAPI inventories,
environment-variable inventories, telemetry dependencies, and resolvers for
paths, images, and Terraform addresses.

## Exported Surface

See `doc.go` and `go doc ./internal/doctruth`. The active surface includes
extractors, verifier inputs/results, finding and evidence-packet fact builders,
local-path, command, HTTP endpoint, environment-variable, container-image, and
Terraform claim checks, plus drift analysis for service deployment claims.

## Telemetry

`observability.go` records bounded verifier counters and durations. Section
IDs, claim IDs, file paths, and excerpts stay in payloads or logs, not metric
labels.

## Gotchas / Invariants

- Claim candidates are documentation evidence only; they never override runtime
  graph or deployment truth.
- Ambiguous or unmatched subjects suppress candidate emission.
- Findings use explicit statuses: `valid`, `contradicted`,
  `missing_evidence`, and `unsupported_claim_type`.
- Generic examples such as globs, placeholders, home paths, optional config
  paths, and bare filenames are not local repository truth claims.
- HTTP template parameter names are normalized; concrete examples can match one
  path segment per placeholder.

## Focused Tests

```bash
cd go
go test ./internal/doctruth -count=1
go run ./cmd/eshu docs verify ../go/internal/doctruth --limit 1000 \
  --fail-on contradicted,missing_evidence
```

## Related Docs

- `docs/public/reference/local-testing.md`
- `docs/public/reference/telemetry/index.md`
