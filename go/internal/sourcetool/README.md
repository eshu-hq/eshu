# sourcetool

## Purpose

`sourcetool` is the single source of truth for the canonical `source_tool`
vocabulary — the closed, lowercase-snake token set that identifies which tool
produced a graph edge (terraform, helm, kustomize, ansible, …) in epic
[#3997](https://github.com/eshu-hq/eshu/issues/3997). It exists so every
read-side consumer validates filter input against one enum instead of
hand-duplicating the list.

## Ownership boundary

Owns the canonical token list and membership check only. It does **not** classify
an `EvidenceKind` into a tool — that write-side mapping lives in
`go/internal/reducer` (`evidenceKindToSourceTool` + `sourceToolPrefixFallback`).
A reducer test asserts every token that mapping can produce is a member of
`Canonical`, so the write side and this read-side vocabulary cannot drift.

## Exported surface

- `Canonical []string` — the ordered set of all valid `source_tool` tokens
  (including the explicit `unknown` fallback).
- `IsValid(token string) bool` — true when `token` is a member of `Canonical`.
  Exact match; callers lowercase and trim first.

## Dependencies

Standard library only. No internal-package dependencies (deliberately a leaf, so
the reducer, query, MCP, and console-facing packages can all import it without a
cycle).

## Telemetry

None. This is a pure in-process vocabulary helper with no I/O, no goroutines, and
no metrics/spans/logs.

## Gotchas / invariants

- `Canonical` must never contain duplicates and must always include `"unknown"`.
- The list must stay in lockstep with the vocabulary table in
  `docs/public/reference/edge-source-tool-provenance.md`; a new tool is added in
  both places (and to the reducer classifier) in the same PR.
- The OpenAPI `source_tool` filter enum is asserted equal to `Canonical` by a
  query-package test, and the reducer classifier values are asserted ⊆
  `Canonical` by a reducer test — both fail on drift.

## Related docs

- [Edge Source-Tool Provenance](../../../docs/public/reference/edge-source-tool-provenance.md)
  — the vocabulary definition and the three provenance tiers.
