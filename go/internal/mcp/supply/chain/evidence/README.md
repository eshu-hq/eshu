# MCP supply-chain evidence route selection

## Purpose

This package owns family membership and pure internal-request selection for
the five MCP supply-chain evidence tools: the vulnerability-scanner read
contract, the advisory-evidence listing, and the SBOM/attestation attachment
listing, count, and grouped inventory.

## Ownership boundary

This package owns supply-chain evidence family membership and the pure
mapping from decoded arguments to a dependency-neutral internal request.
`internal/mcp` keeps tool registration and its client-visible order, global
route fanout, the private adapter, HTTP dispatch, authorization, timeouts,
response budgets, envelopes, summaries, and telemetry. `internal/query` owns
the bounded, source-only reads these paths reach, including advisory-anchor
derivation from reducer-owned impact findings and the SBOM attachment status
vocabulary.

This is a narrower, evidence-listing surface than the `supplychainimpact`
child: that package selects requests for reducer-derived
supply-chain-impact findings (list/count/inventory/explain), while this one
selects requests for source-only advisory evidence, the scanner read
contract, and SBOM attestation attachment evidence. The two are siblings; this
package does not import `supplychainimpact` and does not read its request
shapes.

## Exported surface

- `Route` selects the internal request for a supply-chain evidence tool
  without executing it, and reports `handled=false` for every other tool.

See `doc.go` for the godoc contract.

## Dependencies

- `internal/mcp/routecontract` owns the dependency-neutral decoded-argument
  and internal-request shapes used by `Route`.

## Telemetry

None. Route selection only constructs in-memory values. The parent MCP
package keeps transport and dispatch signals, while the HTTP handlers retain
the shared API request duration and error metrics.

## Gotchas / invariants

- The import path ends in `supplychainevidence`, while the declared package is
  `supplychainevidencetools`. The root uses an explicit import alias.
- `count_sbom_attestation_attachments` carries the same filter keys as
  `list_sbom_attestation_attachments` and
  `get_sbom_attestation_attachment_inventory` but never `limit`, `offset`, or
  `group_by`. It aggregates a scope, so there is no page to size and nothing
  to seek past; a key added here for symmetry would advertise a bound the
  endpoint does not honor.
- `get_sbom_attestation_attachment_inventory` substitutes `"attachment_status"`
  for an absent or empty `group_by`. No sibling route reads `group_by` at all.
- `limit` defaults to 50 on the two listings
  (`list_advisory_evidence`, `list_sbom_attestation_attachments`) and to 100
  on the inventory route; `offset` defaults to 0 on the inventory route only.
  These are the dispatcher's historical defaults, not handler defaults; the
  handlers still enforce their own bounds.
- Numeric coercion follows `routecontract.Arguments.IntOr`: `int`, `int64`,
  and `float64` are accepted, a `float64` truncates toward zero, and every
  other type — `float32` and a numeric string included — falls back to the
  default.
- `get_vulnerability_scanner_read_contract` forwards its `route` selector
  unchecked; the HTTP handler validates which contract section it names.
- `Route` returns a fresh query map per call, so a caller may mutate one
  result without changing a later one or the arguments it was given.

No-Observability-Change: this extraction moves only pure supply-chain
evidence route selection. The root adapter still feeds the same global
fanout, dispatch, authorization, budgets, envelopes, summaries, and transport
telemetry, and the same query handlers execute the requests.

## Related docs

- [MCP package](../README.md)
- [MCP route contract](../routecontract/README.md)
- [HTTP API reference](../../../../docs/public/reference/http-api.md)

## Verification

From `go/`, run `go test ./internal/mcp/... -count=1` and
`go vet ./internal/mcp/...`. From the repository root, run
`scripts/verify-package-docs.sh`.
