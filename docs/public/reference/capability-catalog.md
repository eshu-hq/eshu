# Capability Catalog

The capability catalog is one auditable source that answers, per capability,
whether Eshu has implemented it, where it is exposed, what proves it, how mature
it is, what gaps remain, and which issues track it. It reconciles the capability
matrix, an editorial overlay, and the live MCP tool registry into a
deterministic artifact.

This is the repo-owned answer to "is this feature missing, or does the
foundation already exist?". It is the comparison point for the competitive-audit
preflight and the docs freshness guard.

## Sources

| Source | File | Provides |
| --- | --- | --- |
| Capability matrix | `specs/capability-matrix.v1.yaml` + `specs/capability-matrix/*.yaml` | capability ids, per-profile support and truth ceilings, declared tools, proof signals |
| Editorial overlay | `specs/capability-catalog.v1.yaml` | display names, owner packages, maturity overrides, known gaps, linked issues, docs, exemptions, non-MCP surfaces |
| Authorization catalog | `specs/authorization-catalog.v1.yaml` | built-in roles, data classes, permission families, bootstrap-owner posture, and per-capability grant metadata |
| Live MCP registry | `go/internal/mcp` (`ReadOnlyTools`) | the tool names exposed to MCP clients |

The matrix and Go contract (`go/internal/query/contract.go`) remain the source
of truth for runtime behavior. The catalog adds the editorial and reconciliation
layer; it never changes runtime truth. See
[Capability Conformance Spec](capability-conformance-spec.md).

## Generated artifact

The reconciled catalog is generated to
`go/internal/capabilitycatalog/data/catalog.generated.json` and embedded for the
API, MCP, and console to read. Each entry carries:

- `capability`, `display_name`, `owner_package`
- `maturity` (effective) and `derived_maturity`, with `maturity_reason` for
  overrides
- `surfaces` — each declared tool classified as `mcp`, `api`, `logical`, or
  `unknown`
- `authorization` — the matched permission family, action, data classes, scope
  levels, default roles, and sensitive-data marker
- `profiles` — per-profile status, truth ceiling, and required runtime
- `proof_signals` — deduplicated verification signals from the matrix
- `known_gaps`, `linked_issues`, `docs`, `console`

The top-level `authorization` block carries the built-in role catalog,
data-class catalog, permission-family rules, bootstrap-owner posture, and custom
policy posture. See [Authorization Catalog](authorization-catalog.md).

## Maturity

Maturity is derived from the matrix support statuses:

| Maturity | Meaning |
| --- | --- |
| `general_availability` | supported in the production profile |
| `experimental` | production profile marks it experimental |
| `preview` | supported in a local profile but not production |
| `not_implemented` | no profile supports it |
| `gated` | overlay-only: exists but withheld from a public surface (for example a pending chart) |
| `degraded` | overlay-only: exposed but operating below contract |

`gated` and `degraded` cannot be derived from the matrix; the overlay assigns
them and each requires a `maturity_reason`.

## Surfaces

The same embedded artifact is exposed through three surfaces, so they report the
same catalog state:

| Surface | Where | Notes |
| --- | --- | --- |
| HTTP API | `GET /api/v0/capabilities` | Bounded read with optional `maturity` and `owner` filters and `limit`/`offset` paging; exact truth, fresh freshness. |
| MCP tool | `get_capability_catalog` | Routes to `GET /api/v0/capabilities` with the same filters; prompt-ready. |
| Console | Capabilities page (System nav) | Capability matrix table with maturity, surfaces, proof, owner, and linked issues; truthful empty/unavailable state. |

The API serves the embedded artifact from `capabilitycatalog.Load`; the MCP tool
dispatches to the same route; the console fetches the same endpoint. Parity is
covered by `go/internal/query/capabilities_test.go` (API total equals the
embedded catalog), `go/internal/mcp/tools_runtime_test.go` (tool routes to the
endpoint), and `apps/console/src/pages/CapabilityMatrixPage.test.tsx`.

## Reconciliation findings

`go run ./cmd/capability-inventory -mode verify` fails when the catalog does not
reconcile. Findings include orphan MCP tools (a registered tool with no
capability and no exemption), unmatched surfaces (a declared tool with no MCP
match and no non-MCP-surface declaration), and stale overlay entries. Intentional
gaps are recorded in the overlay with a reason so the gate stays green and the
gap stays auditable.

## Docs freshness guard

Docs pages that assert capability state must bind the claim to a stable
capability id with a machine-checkable marker. The marker is an HTML comment, so
it is invisible in rendered MkDocs output:

```markdown
<!-- capability-state: id=component_extensions.diagnostics state=ga issue=2700 -->
```

`state` accepts any catalog maturity (`general_availability` or its `ga` alias,
`experimental`, `preview`, `gated`, `degraded`, `not_implemented`). The guard
flags any marker that names an unknown capability, uses an invalid state, or
contradicts the catalog maturity, naming the doc, line, capability, claimed
state, and expected state.

```bash
cd go
go run ./cmd/capability-inventory -mode docs        # fails on contradictions
```

`TestDocsFreshnessAgainstRealDocs` (in `cmd/capability-inventory`) runs this as a
CI gate. The contract is marker-based on purpose: prose is not machine-parsed, so
add a marker beside any capability-state claim you want guarded, and keep the
surrounding prose consistent with it.

## Workflow

```bash
cd go
# Inspect findings and entry count.
go run ./cmd/capability-inventory -mode report
# Regenerate the artifact after a matrix or overlay change.
go run ./cmd/capability-inventory -mode generate
# Drift gate (CI): findings empty and artifact fresh.
go run ./cmd/capability-inventory -mode verify
```

When you add an MCP tool or a matrix capability, run `verify`. If it reports an
orphan tool or unmatched surface, either map it to a capability or record an
exemption (with a reason and, where relevant, a tracking issue) in
`specs/capability-catalog.v1.yaml`, then regenerate.

If it reports a missing authorization grant, add or adjust a permission-family
prefix in `specs/authorization-catalog.v1.yaml`. Runtime enforcement still
belongs in the API/MCP/Ask/search implementation slices, but every capability
must have cataloged role, action, scope, and data-class metadata before it ships
as part of the v1 user-management model.

## Related

- [Capability Conformance Spec](capability-conformance-spec.md)
- [Authorization Catalog](authorization-catalog.md)
- [MCP Tool Contract Matrix](mcp-tool-contract-matrix.md)
- `specs/README.md`
- `go/internal/capabilitycatalog/README.md`
