# cvss package instructions

## Read first

- Repository-root `AGENTS.md`
- `go/internal/reducer/supplychain/core/AGENTS.md`
- `go/internal/reducer/supplychain/core/cvss/README.md`
- `docs/internal/design/advisory-intelligence-phases.md`, "Item 1.2 details"

## Invariants

- Keep this package a pure leaf: standard library only, no logging, I/O,
  clock, or global mutable state. Query and storage packages import it, so an
  Eshu import here would create a cycle or a layering break.
- `BaseScore` never panics and never returns a score alongside an error. A
  vector it cannot fully validate returns `ErrMalformedVector` or
  `ErrUnsupportedVersion`.
- A label never produces a score. `DeriveSeverity` returns a zero score and an
  empty score source for a label-only record.
- A derived score is always marked `ScoreSourceDerivedFromVector`. Never
  return a computed score marked as reported.
- `v4_lookup.go` and `v4_max.go` are copied FIRST data. A scoring change needs
  new digests in `exhaustive_test.go`, generated from FIRST's reference code
  over the full base-metric space, not from this package.
- `bucketThresholds`, `labelBuckets`, `Bucket`, `LabelBucket`, `DeriveSeverity`,
  and `BucketSQL` change together. Update the tests beside each, the winners
  recompute and read-time parity tests, and the live parity test.
- Keep expected values in tests sourced from FIRST or NVD, with the source
  cited in a comment. Do not generate an expected score from this package.
