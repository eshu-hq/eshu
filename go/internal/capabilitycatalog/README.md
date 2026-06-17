# capabilitycatalog

`capabilitycatalog` reconciles Eshu capability truth into one deterministic,
auditable catalog so a contributor can answer, from a single source, whether a
capability is implemented, exposed, documented, proven, gated, or missing.

## Inputs

| Source | File | Provides |
| --- | --- | --- |
| Capability matrix | `specs/capability-matrix.v1.yaml` + `specs/capability-matrix/*.yaml` | capability ids, per-profile support and truth ceilings, declared tools, proof signals |
| Editorial overlay | `specs/capability-catalog.v1.yaml` | display names, owner packages, maturity overrides, known gaps, linked issues, docs, exemptions, non-MCP surfaces |
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

## Generated artifact

`data/catalog.generated.json` is the committed, deterministic artifact embedded
by `Load`. Regenerate and verify it with:

```bash
cd go
go run ./cmd/capability-inventory -mode generate
go run ./cmd/capability-inventory -mode verify
```

`TestVerifyAgainstRealSpecs` (in `cmd/capability-inventory`) is the drift gate:
the embedded artifact must reconcile with zero findings and match a fresh
regeneration from the specs and live registry.

## Related

- [Capability Conformance Spec](../../../docs/public/reference/capability-conformance-spec.md)
- [Capability Catalog](../../../docs/public/reference/capability-catalog.md)
- `specs/README.md`
