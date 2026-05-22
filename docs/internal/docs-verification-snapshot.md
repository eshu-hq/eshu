# Docs Verification Snapshot

This file records the verification gates used for the documentation cleanup PR.
Keep detailed command output in the terminal or CI logs; keep this page focused
on the durable proof shape reviewers need.

## Current Acceptance Gates

Run these before calling the branch ready:

```bash
cd go
go run ./cmd/eshu docs verify ../docs/public --limit 1400 \
  --fail-on contradicted,missing_evidence
go run ./cmd/eshu docs verify .. --limit 2400 \
  --fail-on contradicted,missing_evidence
go test ./cmd/eshu ./internal/doctruth ./internal/mcp ./internal/query \
  ./internal/storage/cypher -count=1
cd ..
scripts/verify-package-docs.sh
git diff --check
cmp -s AGENTS.md CLAUDE.md
uv run --with mkdocs --with mkdocs-material --with pymdown-extensions \
  mkdocs build --strict --clean --config-file docs/mkdocs.yml
```

Current checkpoint after the long-tail compression pass:

- Public docs verification: `173` documents, `1225` claims,
  `0` contradicted, `0` missing evidence.
- Full repository docs verification: `562` documents, `1513` claims,
  `0` contradicted, `0` missing evidence.
- Package docs verification passed for `go/internal` and `go/cmd`; fixture docs
  verification passed for `tests/fixtures`.
- Focused Go tests passed for the CLI docs verifier, documentation truth,
  MCP routing, HTTP query contracts, and Cypher writer package.
- Strict MkDocs build passed. The Material for MkDocs warning about MkDocs 2.0
  is upstream noise, not an Eshu docs failure.
- Package-doc gate passed; it reported no changed Go package source files.

## What The Gates Cover

The broad docs verifier checks:

- registered `eshu ...` CLI command claims
- OpenAPI-backed HTTP endpoint claims, including concrete route examples
- known `ESHU_*` environment variables
- explicit local repo paths in backticks or Markdown links
- tagged or digested container image refs found in local deployment manifests

Unsupported shell-command examples, such as `helm`, `kubectl`, and `terraform`,
remain `unsupported_claim_type`; they are not contradictions.

The focused Go tests cover:

- command registration and docs verification behavior in `cmd/eshu`
- command argument placeholders, endpoint templates, local path claims, and
  container-image claim extraction in `internal/doctruth`
- MCP tool count, route mapping, envelope handling, and the current 72-tool
  contract in `internal/mcp`
- query/OpenAPI read-surface contracts in `internal/query`
- Cypher writer phase order, retry behavior, timeout wrapping, and current
  canonical write shapes in `internal/storage/cypher`

## Historical Proof Kept Elsewhere

This branch already moved durable lessons out of old plans and ADR logs into
current docs:

- Docker Compose and local binary proof live in `docs/public/run-locally/`.
- Helm, Kubernetes, and collector render proof lives in
  `docs/public/deploy/kubernetes/`.
- Runtime, backend, Cypher, telemetry, and local testing proof lives in
  `docs/public/reference/`.
- Package-local proof obligations live in the package `README.md`,
  `doc.go`, and scoped `AGENTS.md` files.

Do not re-add long chronological terminal logs here. Add only the current gate
summary and move durable behavior into the relevant public or package-local
contract.
