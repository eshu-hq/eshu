# internal/doctruth

## Read First

1. `go/internal/doctruth/README.md`
2. `go/internal/doctruth/doc.go`
3. `go/internal/doctruth/extractor.go`
4. `go/internal/doctruth/verifier.go`
5. `go/internal/doctruth/verifier_claims.go`
6. `go/internal/doctruth/drift.go`
7. `go/internal/doctruth/observability.go`
8. `go/internal/facts/documentation.go`

## Package Rules

- Extraction and verification MUST stay deterministic and caller-supplied.
  Do not call LLMs, docs hosts, graph stores, databases, filesystems, or
  network services from this package.
- Documentation prose is evidence, not operational truth. Claim candidates MUST
  carry document, revision, section, and excerpt-hash provenance.
- Ambiguous or unmatched subject mentions MUST NOT produce exact claim
  candidates.
- A verification finding is valid only when an explicit caller-supplied truth
  source matched. Unknown claim families remain `unsupported_claim_type`.
- Keep local path, container image, Terraform address, command, endpoint, and
  environment-variable checks exact and bounded. Generic examples,
  placeholders, globs, home-directory paths, optional config paths, and bare
  filenames are not repo truth claims.
- Drift findings MUST be read-only. They never override graph truth and must
  preserve match, conflict, ambiguous, unsupported, stale, and building states.
- Metrics MUST use bounded labels only. Section IDs, claim IDs, excerpts, and
  high-cardinality values belong in payloads or logs, not metric attributes.

## Proof

- Add focused tests before changing extraction, verifier parsing, permission
  status, drift-state classification, or telemetry labels.
- Run `cd go && go test ./internal/doctruth -count=1` for package changes.
- Run `go run ./cmd/eshu docs verify ../go/internal/doctruth --limit 1400 --fail-on contradicted,missing_evidence`
  for docs changes in this package.
