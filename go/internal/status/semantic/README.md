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

## Related docs

- `docs/internal/naming.md` — the nesting rules this leaf was created under
- Issue #6775 — the `internal/status` nest that introduced this package
