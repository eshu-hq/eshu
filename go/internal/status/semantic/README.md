# Semantic Extraction Status

## Purpose

`internal/status/semantic` owns the optional LLM-assisted semantic-extraction
family of the status report: whether extraction can run, why not when it
can't, its bounded queue/budget/audit snapshots, and the redacted provider
profile roster behind it. It exists so the root `internal/status` package
(which aggregates every family into `RawSnapshot` and `Report`) has one place
that owns "is semantic extraction available, and does that affect anything
else" — the answer to the second half is always no, and this package's own
doc comments say so on every non-available state.

## Ownership boundary

This package owns extraction liveness derivation, the queue/budget/audit
snapshot shapes, and provider-profile *type* normalization. It does not own
attaching a live provider-profile roster to a status read —
`WithSemanticProviderProfiles` wraps the root `Reader`/`RawSnapshot`
contract and stays in root, because this package cannot depend on those root
types. It does not own any other status family.

The root aggregates every family leaf into `RawSnapshot` and `Report`, so the
root imports the leaves. That makes the dependency direction one-way: leaves
and root may import `semantic`; `semantic` may import neither the root nor a
sibling leaf.

## Exported surface

- `ExtractionStatus`, `ExtractionSupportedStates`, `DefaultExtractionStatus`
  — extraction liveness, its closed state enum, and the zero-key default
- `ExtractionQueueSnapshot`, `ExtractionProviderProfileQueueCount`,
  `ExtractionDecisionCount` — bounded queue lifecycle aggregates
- `ExtractionBudgetSnapshot`, `ExtractionBudgetDecisionCount` — redacted
  token/cost budget totals
- `ExtractionAuditSnapshot` — audit-safe actor/ACL class counts
- `ProviderProfileStatus`, `ProviderProfileSupportedStates` — the redacted
  provider-profile view and its closed state enum
- `ExtractionStatusJSON`, `ProviderProfilesJSON`, and the queue/budget/audit
  JSON projections — the wire shapes

See `doc.go` for the full godoc contract.

## Dependencies

- `internal/status/shared` — `NamedCount`, `NamedCountJSON`, `CountMap`,
  `FormatTotals`

## Telemetry

None. This package performs no I/O; it derives and normalizes status from
data the root's status reader already gathered, and never probes a provider
directly — provider health is asserted by an external source and normalized
here.

## Gotchas / invariants

- Every non-available `ExtractionStatus` state carries a detail string that
  explicitly says deterministic indexing, reducer projection, API reads, MCP
  tools, and documentation fact verification are unaffected. Preserve that
  language (or its substance) in any new state's default detail — it is what
  stops an operator from misreading an optional-provider outage as a
  platform outage.
- `WithSemanticProviderProfiles` (root `internal/status`) attaches a static
  profile roster to a `Reader` and mutates `RawSnapshot.SemanticExtraction.
  ProviderProfiles` on read. It depends on the root `Reader`, `RawSnapshot`,
  and `SnapshotSelection` types and therefore cannot move here; it calls this
  package's profile-clone normalizer, which must be exported (for example
  `CloneProviderProfiles`) for root to reach it — it is unexported today.
- `normalizeSemanticExtractionStatus` derives `ProviderConfigured`,
  `DocumentationObservationsEnabled`, and `CodeHintsEnabled` from the profile
  roster when profiles are present, and forces the two enablement flags to
  `false` whenever the derived state is not `available`. A caller that skips
  normalization and reads the raw snapshot fields directly can see stale
  `true` flags next to a non-available state.
- `safeSemanticExtractionDetail` scrubs any provided `Detail` string
  containing `prompt`, `response`, `secret`, `credential`, `token`, or
  `api key` (case-insensitive) back to empty rather than passing it through.
  Do not bypass this when constructing a status from a new evidence source.
- This package does not probe provider health; `ProviderProfileStatus.State`
  reflects whatever an external health source last reported, normalized
  against the closed state enum by `normalizeSemanticProviderProfile`.
- `renderSemanticExtractionLine` and `semanticExtractionStatusJSON` (called
  from root `status.go`/`json.go`) are unexported today and must be exported
  on the move.

## Evidence

No-Regression Evidence (#6949 batch 5, semantic family): this change moves
the 22 `semantic.*` compat entries' Go importers (8 type aliases, 11 consts,
and 3 funcs, reached as both `status.*` and `statuspkg.*` plus 3 bare
same-package uses in root `status.go`) off the transitional compat spellings
onto the destuttered `semantic.*` names, and deletes the emptied
`compat_semantic.go`. Locals named `semantic` in `status_governance.go` (5
params) and `semantic_extraction_test.go` (8 sites) are renamed to
`extraction` so the leaf import is never shadowed; wire keys, log keys,
comments, and failure-message text are byte-identical. No type shape, wire
field, SQL text, or executable statement changes: across 23 code files,
every production hunk requalifies an identifier, renames a local, or edits
an import path only (80 changed non-import production lines per side
verified identical modulo the mapping plus the documented local rename; the
other 9 changed lines are import adds/removes), every other hunk is a
package-doc rewording, a ledger row, or the compat file's own deletion, and
the build resolves with no dangling reference.
Measurement: identical before/after outcomes (ledger:6949-semantic-batch5-before, ledger:6949-semantic-batch5-after). The command is `go test -count=1` over
the 7 affected package targets (`./internal/status/...`,
`./internal/query/...`, `./internal/storage/postgres/...`,
`./internal/mcp/...`, `./cmd/eshu/...`, `./internal/semanticpolicy/...`,
`./internal/semanticprofile/...`) on baseline `f41526bf8a` vs measurement
commit `acdba0a226` (this Evidence section and the two ledger rows are the
only later changes): 159 packages ok, 0 fail on both sides, with the
ok-package set byte-identical after timing strip. `go test -list`
inventory is identical on both sides (11197 tests). Backend/version:
go1.26.2 linux/amd64, in-memory; no backend touched. Input shape: n/a
(no runtime input). Terminal queue/row counts: none — no queue, lease,
Cypher, or SQL path is touched. Contract gates green on the branch:
`verify-openapi.sh` (261/261 routes), `verify-contracttest.sh`,
`verify-package-docs.sh`, and `verify-performance-evidence.sh`. The change
is safe because it cannot alter runtime behavior: the compiler resolves
the same types through their new paths, and the compat deletion is
compile-enforced total — any missed caller would fail the build.

No-Observability-Change (#6949 batch 5, semantic family): this package
carries no instrumentation (see Telemetry above) and the move adds,
removes, or renames no metric, span, structured log, or status field in
any touched package. The status readers, query handlers, governance
routes, and postgres readers that use these types are untouched;
operator signals are identical before and after.

## Related docs

- `docs/internal/naming.md` — the nesting rules this leaf was created under
- Issue #6775 — the `internal/status` nest that introduced this package
