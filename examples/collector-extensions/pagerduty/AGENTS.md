# AGENTS.md - PagerDuty reference component guidance

## Read first

1. `README.md` - package purpose, fixture-only boundary, and verification.
2. `doc.go` - godoc contract.
3. `pagerduty.go` - SDK result construction and redaction rules.
4. `manifest.yaml` - component package declaration.
5. `docs/public/reference/pagerduty-evidence.md` - PagerDuty evidence and redaction contract.

## Invariants

- This package is fixture-only. Do not add live PagerDuty API calls, tokens,
  hosted scheduling, workflow claims, reducers, graph writes, API reads, or MCP
  reads here.
- Facts use namespaced component fact kinds. The in-tree parity test owns the
  comparison to core PagerDuty fact families.
- Fixtures must stay synthetic and redacted. Do not commit private service
  names, incident titles, responder identities, URLs, tokens, routing keys,
  account IDs, source names, provider payloads, host paths, or IP addresses.
- Preserve `source_evidence_only:no_graph_truth` in the manifest unless a
  separate core-owned reducer/query issue changes the contract.

## Verification

- `go test ./...`
- `go -C ../../../go run ./cmd/eshu component inspect ../examples/collector-extensions/pagerduty/manifest.yaml`
- `go -C ../../../go run ./cmd/eshu component verify ../examples/collector-extensions/pagerduty/manifest.yaml --trust-mode allowlist --allow-id dev.eshu.examples.pagerduty --allow-publisher eshu-hq`
- `go -C ../../../go run ./cmd/eshu component conform ../examples/collector-extensions/pagerduty/manifest.yaml --fixture ../examples/collector-extensions/pagerduty/testdata/fixtures/complete-result.json --mode fixture`
