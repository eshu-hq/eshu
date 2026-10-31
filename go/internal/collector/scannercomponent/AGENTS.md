# AGENTS.md - scannercomponent guidance

Read these before editing this package:

1. `README.md`
2. `doc.go`
3. `go/internal/component/README.md`

## Invariants

- This package owns vocabulary and admission; it never executes analyzers.
  Do not add workflow writes, store handles, claim loops, runners, paging,
  or seeding here — execution lives in the `eshu-hq/eshu-scanner-worker`
  satellite.
- Never change a frozen vocabulary string. `vocabulary_test.go` asserts every
  value verbatim; a rename breaks the planner, the satellite, telemetry
  dimensions, and status classes. Route a vocabulary change through the
  #8878 epic with satellite coordination, never as a drive-by.
- Never interpolate image values (seeded refs, pins) into errors. Name the
  component ID and the defect, never the value.
- The digest pins, the `Scanner-Publish-SHA` anchor, and the chart
  `seed.image` entries move together in a reviewed re-pin PR. Never
  auto-update pins from a publish job.
- Manifests must declare every kind an analyzer can emit, including
  degraded-path warnings, or the host rejects real output at admission.
- No new SDK vocabulary from this package. An SDK addition is a separate
  versioned contract change.

## Common changes

- A re-pin PR bumps the `Scanner-Publish-SHA` anchor, the three digest pins,
  and the chart seed images together, with the digest-pins gate green.
- A new analyzer family needs its component ID, vocabulary entries, manifest
  kinds, and admission tests here first, with satellite execution landing
  alongside — never an in-tree executor.
