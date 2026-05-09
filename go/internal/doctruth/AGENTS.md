# AGENTS.md — internal/doctruth guidance for LLM assistants

## Read First

1. `go/internal/doctruth/README.md`
2. `go/internal/doctruth/doc.go`
3. `go/internal/doctruth/extractor.go`
4. `go/internal/facts/documentation.go`
5. `go/internal/telemetry/README.md`

## Invariants

- Keep extraction conservative. Prefer structured hints, exact aliases, exact
  URLs, and bounded section text over broad prose inference.
- Do not write graph state, documentation sources, or external systems from this
  package.
- Ambiguous mentions may be emitted as evidence, but they must not produce exact
  claim candidates.
- Preserve section provenance and excerpt hashes on every emitted claim
  candidate.
- Record observability with existing telemetry helpers and bounded labels.
