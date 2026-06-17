# AGENTS.md — internal/auditreport guidance for LLM assistants

## Read first

1. `go/internal/auditreport/README.md` — input, reconciliation, output.
2. `go/internal/auditreport/report.go` — `Generate` and the recommendation logic.
3. `go/internal/auditpreflight/README.md` — the shared gap-class/owner-surface
   taxonomy this package validates against.

## Invariants this package enforces

- **No issue creation, no scraping.** The generator is input-driven and pure;
  it recommends actions only. Do not add GitHub writes or repo crawling here.
- **Deterministic output.** Entries sort by competitor then feature; duplicate
  issue lists are deduped and sorted; renderers are stable. Keep new collections
  sorted.
- **Conflicts surface as review.** Invalid classification or missing-vs-exists
  must map to `RecReview`, never be silently dropped.
- **Taxonomy is shared.** Gap class and owner surface come from
  `auditpreflight`; do not fork the vocabulary.

## Common changes and how to scope them

- **New recommendation rule** → extend `recommend`; add a `report_test.go` case
  and, if it changes rendered output, run the cmd golden with `-update`.
- **New report field** → add to `ReportEntry`, thread through `buildEntry`,
  update `RenderMarkdown`/`RenderJSON` and the golden.

## Failure modes and how to debug

- Symptom: golden test fails after a catalog change → the referenced capability's
  presence changed; re-confirm the fixture capability still exists, then
  `go test ./cmd/audit-report -run Golden -update`.
- Symptom: duplicate detection misses an obvious issue → check
  `significantTokens` filtering and the two-token threshold.

## What NOT to change without an ADR

- The recommendation enum values — they are part of the report contract.
