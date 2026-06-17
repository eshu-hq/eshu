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
- `profiles` — per-profile status, truth ceiling, and required runtime
- `proof_signals` — deduplicated verification signals from the matrix
- `known_gaps`, `linked_issues`, `docs`, `console`

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

## Reconciliation findings

`go run ./cmd/capability-inventory -mode verify` fails when the catalog does not
reconcile. Findings include orphan MCP tools (a registered tool with no
capability and no exemption), unmatched surfaces (a declared tool with no MCP
match and no non-MCP-surface declaration), and stale overlay entries. Intentional
gaps are recorded in the overlay with a reason so the gate stays green and the
gap stays auditable.

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

## Related

- [Capability Conformance Spec](capability-conformance-spec.md)
- [MCP Tool Contract Matrix](mcp-tool-contract-matrix.md)
- `specs/README.md`
- `go/internal/capabilitycatalog/README.md`
