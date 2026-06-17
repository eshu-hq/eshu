# serviceintel

## Purpose

`serviceintel` composes existing Eshu answer evidence into a deterministic,
operator-ready **service intelligence report**. It is the Eshu-native answer to
single-artifact graph reports: instead of one LLM-written narrative, it arranges
already-produced, truth-labelled answer evidence into a fixed set of sections an
operator can read top to bottom and trust.

The report ties every section to code-to-cloud evidence, preserved truth labels,
explicit missing-evidence reasons, and recommended bounded next calls with any
non-sensitive arguments required to execute them.

## Ownership boundary

This package owns **composition only**. It does not:

- query a graph, content, or relational store,
- call an LLM or any provider,
- re-derive or reclassify truth,
- hydrate citations, or
- expose an HTTP route or MCP tool.

Callers gather evidence from existing answer routes and pass it in. Truth
classification and the no-confident-summary-on-unsupported-answers invariant are
delegated to `query.NewAnswerPacket`, so the report inherits the proven answer
honesty contract rather than duplicating it.

## Report shape

A `Report` always carries the same ordered sections regardless of which inputs
were supplied:

1. `identity` — canonical service identity (the truth anchor)
2. `code_to_runtime` — entrypoints, network paths, source-to-runtime trace
3. `deployment_config` — deployment lanes and configuration influence
4. `supply_chain` — image, dependency, and build-provenance evidence
5. `incidents_support` — incident, support, and runbook evidence

Each section embeds a canonical `query.AnswerPacket`, so it preserves the source
truth envelope, evidence handles, limitations, and recommended next calls. A
section's `status` is `supported`, `partial`, or `unsupported`, derived from the
packet — never reclassified from the source truth.
Missing evidence marks the section partial and adds the section fallback, so
the report never reads as complete while unresolved handles remain.

The report is `partial` whenever any present section is partial or unsupported,
so a report never reads as complete while sections are missing. Report-level
`truth` and `truth_class` are copied from the identity section.

## Guided investigations

`Compose` also derives a bounded list of `SuggestedInvestigation` values from the
report's own signals, so an operator gets a "what to look at next" list grounded
in real gaps rather than free-form prompts. Each suggestion is derived from one
closed `InvestigationBasis`:

| Basis | Signal | Bounded next call |
| --- | --- | --- |
| `missing_evidence` | requested evidence handles did not resolve | `build_evidence_citation_packet` |
| `stale_freshness` | section stale/building with a proven cause | the section's freshness next check |
| `ambiguous_target` | source route could not pick one subject | `resolve_entity` (never guesses a winner) |
| `unsupported_lane` | section's evidence lane is unavailable | the section's fallback call |
| `high_impact_relationship` | caller flagged a high-impact relationship | `get_relationship_evidence` |

Each suggestion carries a reason, an evidence basis (the concrete signal it came
from), a bounded next call, and an expected truth class sourced from the section
truth or the linked playbook — never invented. The list is de-duplicated by a
stable id, bounded, and empty when no section carries a supporting basis.

## Determinism

`Compose` is a pure function. The same `ReportInput` yields a byte-identical
`Report`: sections are emitted in catalog order, the first input for a kind
wins, and aggregated limitations and next calls are de-duplicated in stable
encounter order. No timestamps, randomness, or map-iteration order leak into the
output.

## Exported surface

See `doc.go` for the godoc contract. The package exports `Compose`, the
`Report` / `ReportSection` / `ReportInput` / `SectionInput` shapes, the
`SectionKind` and `SectionStatus` enums, `ReportSubject`, `NextCall`, the
`SuggestedInvestigation` / `InvestigationBasis` guided-investigation surface,
`FromServiceStory` (adapts a `get_service_story` dossier into a `ReportInput`:
subject + identity / code_to_runtime / deployment_config sections), and
`FromSupplyChainInventory` (adapts a `get_supply_chain_impact_inventory` response
into the `supply_chain` section), so callers build reports from real route
evidence.

## Dependencies

- `github.com/eshu-hq/eshu/go/internal/query` for `AnswerPacket`, `TruthEnvelope`,
  `EvidenceCitationHandle`, and the truth-class enum.

## Verification

```bash
(cd go && go test ./internal/serviceintel -count=1)
scripts/verify-package-docs.sh
```
