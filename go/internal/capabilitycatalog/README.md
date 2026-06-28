# capabilitycatalog

`capabilitycatalog` reconciles Eshu capability truth into one deterministic,
auditable catalog so a contributor can answer, from a single source, whether a
capability is implemented, exposed, documented, proven, gated, or missing.

## Inputs

| Source | File | Provides |
| --- | --- | --- |
| Capability matrix | `specs/capability-matrix.v1.yaml` + `specs/capability-matrix/*.yaml` | capability ids, per-profile support and truth ceilings, declared tools, proof signals |
| Editorial overlay | `specs/capability-catalog.v1.yaml` | display names, owner packages, maturity overrides, known gaps, linked issues, docs, exemptions, non-MCP surfaces |
| Authorization catalog | `specs/authorization-catalog.v1.yaml` | built-in roles, explicit grants, data classes, permission families, bootstrap-owner posture |
| Live signals | injected `Signals` | the MCP tool registry (`mcp.ReadOnlyTools`) |

The package never imports `mcp` or `query`; the generator
(`go/cmd/capability-inventory`) injects the registry through `Signals` so the
catalog stays dependency-light and can be embedded into the API, MCP, and
console.

## Output

`Build` returns a `Catalog` (entries sorted by capability id) and a list of
`Finding`s. An empty findings list means the catalog fully reconciles. Findings
flag:

- `orphan_mcp_tool` — a registered MCP tool with no capability and no exemption.
- `unmatched_surface` — a declared tool with no MCP match and no non-MCP-surface
  declaration.
- `stale_overlay_capability`, `stale_tool_exemption`, `stale_non_mcp_surface` —
  overlay entries that no longer match the matrix or registry.
- `missing_maturity_reason`, `invalid_overlay_maturity` — malformed overlay
  maturity overrides.
- `missing_authorization_grant`, `invalid_authorization_reference`,
  `stale_authorization_family` — missing or malformed role/grant/data-class
  metadata from the authorization catalog.

Every catalog entry carries an `authorization` block with its matched permission
family, action, data classes, scope levels, default roles, and sensitive-data
marker. The top-level `authorization` block carries the built-in role and
data-class catalog. Planned families may exist before their runtime routes land;
live capability rows must match a permission family before the generator passes.
Each family default role must also explicitly grant the family action with the
family data classes and scope levels, so role tables cannot drift away from
advertised defaults.

Each profile also carries the matrix-declared `p95_latency_ms` and
`max_scope_size` budget claims. `CheckBudgetProof` verifies an operator-supplied
public artifact that binds every supported budget row to measured API/MCP
evidence, scope/truncation proof, freshness, backend/version, sanitized artifact
handle, and retry/dead-letter invariants.

## Maturity

Maturity is derived from the matrix support statuses
(`general_availability`, `experimental`, `preview`, `not_implemented`). Some
matrix rows omit `status` and declare only a truth ceiling; those are inferred as
supported unless the ceiling is `unsupported`. The overlay may override maturity
with the operational states the matrix cannot express (`gated`, `degraded`); each
override requires a reason. Entries record both effective and derived maturity.

## Docs freshness guard

`ParseDocClaims` scans markdown for `<!-- capability-state: id=<id> state=<state>
[issue=<n>] -->` markers and `CheckDocFreshness` flags any marker that names an
unknown capability, uses an invalid state, or contradicts the catalog maturity.
The marker is an HTML comment, so it is invisible in rendered docs. Run it with
`go run ./cmd/capability-inventory -mode docs`. See
[Capability Catalog](../../../docs/public/reference/capability-catalog.md).

## Product claim ledger

`LoadProductClaimLedger` reads `specs/product-claims.v1.yaml`, and
`CheckProductClaims` verifies broad public claims that are too wide for a single
`capability-state` marker. Each guarded source line must carry
`<!-- product-claim: id=<claim-id> -->`, and each row must name the same source
line, a whole-line quote, capability ids and maturity, owner packages,
implementation paths, API/MCP/console surfaces, deterministic evidence source,
semantic-output posture, proof command or artifact, generated surface-count
expectations, catalog proof-signal references, and issue state for partial or
gated claims. The check is part of
`capability-inventory -mode docs`, so README and public docs cannot drift into
broader product claims without a matching proof ledger update.

## Collector readiness guard

`ParseCollectorClaims` scans markdown for `<!-- collector-state: name=<collector>
lane=<readiness_lane> -->` markers and `CheckCollectorReadiness` flags any marker
that names an unknown collector, uses an invalid lane, contradicts the surface
inventory lane, or claims `implemented` without a linked promotion proof. It runs
in the same `-mode docs` pass against the embedded surface inventory, so a docs
page cannot claim a collector is production-ready unless the inventory agrees and
proof exists.

## Generated artifact

`data/catalog.generated.json` is the committed, deterministic artifact embedded
by `Load`. Regenerate and verify it with:

```bash
cd go
go run ./cmd/capability-inventory -mode generate
go run ./cmd/capability-inventory -mode verify
go run ./cmd/capability-inventory -mode budget-proof -budget-artifact ../capability-budget-proof.json
```

`TestVerifyAgainstRealSpecs` (in `cmd/capability-inventory`) is the drift gate:
the embedded artifact must reconcile with zero findings and match a fresh
regeneration from the specs and live registry.

## Surface inventory

The package also reconciles the **surface inventory**: a generated record of
every platform surface across six categories, so a contributor can see the full
fleet — and so a new surface cannot silently bypass the catalog.

| Category | Live source | Enumerated by the generator from |
| --- | --- | --- |
| `command` | command binaries | `go/cmd/*` directories |
| `collector` | collector families | `scope.AllCollectorKinds` |
| `reducer_domain` | reducer domains | `reducer.AllDomains` |
| `api_route` | HTTP API routes | `query.OpenAPISpec` method+path operations |
| `mcp_tool` | MCP tools | `mcp.ReadOnlyTools` |
| `console_page` | console pages | `apps/console/src/App.tsx` route elements imported from `./pages/*` |

`BuildSurfaceInventory(live, overlay)` merges the live surfaces (injected as
`LiveSurfaces` so the package stays dependency-light) with the editorial overlay
(`specs/surface-inventory.v1.yaml`) into a `SurfaceInventory` plus `Finding`s.
Each `SurfaceRecord` carries a `ReadinessLane` — `implemented`, `partial`,
`gated`, `foundation_only`, `fixture_only`, `research_only`, `not_implemented`,
or `unsupported`. Only `implemented` asserts production readiness, so
`ReadinessLane.RequiresPromotionProof` is true only for it. Non-collector
categories default to `implemented` (they exist because they are built and
served); collectors have no default and must be classified in the overlay.
Collector records may also carry `collector_contract`, a source-to-read-surface
manifest listing emitted fact kinds, projection/read consumers, proof gates,
fixture references, and a truth profile (`deterministic`, `provider_gated`, or
`optional_semantic`). The generator reconciles the live collector fact-kind map
against this contract so a new fact kind cannot ship without source provenance.

Surface findings:

- `unclassified_collector` — a live collector with no overlay readiness lane.
- `implemented_without_proof` — a collector declared `implemented` with no proof.
- `stale_surface_overlay` — an overlay row whose surface is absent from code.
- `invalid_readiness_lane` — an overlay readiness value outside the closed set.
- `collector_fact_kind_unmapped` — a live collector fact kind missing from that
  collector's `collector_contract.fact_kinds` manifest.

`data/surface-inventory.generated.json` is the committed artifact embedded by
`LoadSurfaceInventory`. `TestSurfaceInventoryDriftAgainstRealCode` and
`TestSurfaceInventoryGateCatchesSilentDrift` (in `cmd/capability-inventory`) are
the drift gate: a surface added or removed in code without regenerating the
artifact fails CI.

## Related

- [Capability Conformance Spec](../../../docs/public/reference/capability-conformance-spec.md)
- [Capability Catalog](../../../docs/public/reference/capability-catalog.md)
- [Authorization Catalog](../../../docs/public/reference/authorization-catalog.md)
- `specs/README.md`
