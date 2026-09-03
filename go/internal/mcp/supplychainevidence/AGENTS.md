# AGENTS.md — MCP supply-chain evidence route guidance

## Read first

1. `README.md` and `doc.go` in this directory.
2. `../AGENTS.md` for MCP routing, authorization, and transport rules.
3. `routes.go` and `routes_test.go` for the child request-selection contract.
4. `../dispatch_supply_chain.go` and
   `../dispatch_supply_chain_evidence_contract_test.go` for the root adapter
   and the production-boundary proof.
5. `../dispatch_repositories.go` for the repository switch this family is
   answered ahead of.
6. `../routecontract/README.md` for the dependency-neutral request contract.
7. `../supplychainimpact/README.md` to see why that sibling family stays
   separate: it selects requests for reducer-derived supply-chain-impact
   findings, not source-only evidence.

## Invariants

- Keep only supply-chain evidence family membership and pure
  argument-to-request selection here. Global route fanout, the private
  adapter, and execution stay in the parent MCP package and `internal/query`.
- Keep the package clause as `package supplychainevidencetools`; the root
  imports it with an explicit alias.
- Preserve the five tool names and their exact method, path, and query keys:
  `/api/v0/supply-chain/vulnerability-scanner/contract`,
  `/api/v0/supply-chain/advisories/evidence`,
  `/api/v0/supply-chain/sbom-attestations/attachments`,
  `.../attachments/count`, and `.../attachments/inventory`, all `GET` with no
  body.
- Preserve the `limit` default of 50 on `list_advisory_evidence` and
  `list_sbom_attestation_attachments`, and of 100 on
  `get_sbom_attestation_attachment_inventory`; preserve the `offset` default
  of 0 on the inventory route.
- Preserve `count_sbom_attestation_attachments`' austerity: it carries the
  same SBOM filter keys as the listing and inventory routes and never a
  `limit`, `offset`, or `group_by`. Adding one for symmetry with its siblings
  advertises a bound the endpoint does not honor.
- Preserve the `group_by` default of `"attachment_status"` on
  `get_sbom_attestation_attachment_inventory` alone; no sibling route reads
  `group_by`.
- Return the zero request and `handled=false` for unrelated tools, including
  near-miss names that share a prefix with an owned tool.
- Selection stays pure: no HTTP call, no query, no clock, no environment read.

## Common changes

- Change a route only with the exact child request tests, the root adapter
  parity test, the shared HTTP contract, and applicable golden-corpus proof.
- Change a `limit` or `offset` default only with the handler's own bound
  check, because a mismatch silently changes a client-visible page size.
- Add a tool here only after confirming the root repository switch no longer
  answers it, so the two never both claim a name.

## Failure modes

- Importing the MCP root creates a parent-child cycle. Use `routecontract`
  only.
- Giving `count_sbom_attestation_attachments` a `limit`, `offset`, or
  `group_by` compiles and reads like a consistency fix. The count handler
  reads none of them, so one sent there is inert and only advertises a bound
  the endpoint does not honor. The child and root count tests exist to catch
  exactly that edit.
- Dropping the `group_by` default on the inventory route changes its grouped
  response shape for every caller that omits the field.
- Executing a request here would split authorization, timeout,
  response-budget, envelope, summary, and telemetry ownership.
- Claiming a name the repository switch still answers makes resolution depend
  on which check runs first.
- Returning a shared map would let one caller's mutation leak into a later
  request.

## Anti-patterns

- Do not add global fanout, HTTP execution, query, storage, authorization,
  summary, or telemetry helpers.
- Do not reintroduce the root `str` and `intOr` helpers; use
  `routecontract.Arguments`, whose coercions they match exactly.
- Do not widen `Route` to a prefix or regular-expression match. Family
  membership is an explicit list of names.
- Do not merge this package with `supplychainimpact`. The two select requests
  for different domains that happen to share the `supply-chain` path prefix.

## Verification

From `go/`, run:

```bash
go test ./internal/mcp/supplychainevidence ./internal/mcp -count=1
go vet ./internal/mcp/supplychainevidence ./internal/mcp
```

Run `scripts/verify-package-docs.sh` from the repository root. An intentional
MCP query-shape change also requires the golden-corpus gate described by the
parent package.
