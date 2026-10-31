# cvss

Pure CVSS scoring and severity-bucket helpers for the supply-chain impact
reducer family (`go/internal/reducer/supplychain/core`). Added for #9693, where
a finding whose source published only a CVSS vector or only a severity label
was bucketed `none` because the bucket came from `cvss_score` alone.

## Ownership

`cvss` owns three things and nothing else:

- `BaseScore(vector)` computes the base score of a CVSS v3.0, v3.1, or v4.0
  vector. Threat, environmental, and supplemental metrics are validated and
  ignored, so the result is always the base score.
- `DeriveSeverity(reportedScore, vector, label)` returns the score, its
  source marker, and the severity bucket for one source's evidence.
- `BucketSQL(payload)` renders the SQL `CASE` expression that buckets a finding
  payload, built from the same threshold and label tables as `Bucket` and
  `LabelBucket`.

The reducer (`core`) calls `DeriveSeverity` on the advisory, product, and
provider-alert paths. The Postgres winners recompute
(`go/internal/storage/postgres`) and the read-time findings queries
(`go/internal/query/supply/chain/impact`) both embed `BucketSQL`.

## Precedence

For one source's evidence, `DeriveSeverity` uses the first of these that
applies:

1. A source-reported score above zero, used as published.
2. A valid vector that scores above zero: the computed base score, marked
   `derived_from_vector`.
3. A recognized label (`critical`, `high`/`important`, `medium`/`moderate`,
   `low`): the bucket only. A label never produces a score.
4. Nothing: bucket `none`.

Across sources the reducer ranks reported scores ahead of derived ones (see
the `core` README); `DeriveSeverity` only decides within one source.

A malformed or unsupported vector (CVSS v2, an unknown version, a missing or
repeated metric, an unknown metric or value) is ignored and the label, if any,
decides. A vector that scores 0.0 (no impact) also defers to the label.

## Scoring sources

The v3.0 and v3.1 equations are the "Base Metrics Equations" of the CVSS v3.1
specification. The two versions differ only in `Roundup`: v3.1 rounds on
integers, v3.0 takes the ceiling to one decimal. The v4.0 algorithm, the
MacroVector lookup table (`v4_lookup.go`), and the maxima tables
(`v4_max.go`) come from the CVSS v4.0 specification and FIRST's reference
calculator (`github.com/FIRSTdotorg/cvss-v4-calculator`, BSD-2-Clause,
copyright FIRST, Red Hat, and contributors).

Proof the scores are right, recorded in #9693:

- The unit tests pin 63 vectors from the FIRST examples page
  (`https://www.first.org/cvss/examples`) to the scores that page publishes.
- `TestBaseScoreMatchesFIRSTOverTheWholeBaseSpace` scores every base vector
  (104,976 v4.0 and 2,592 each for v3.0 and v3.1, about a second) and compares
  a SHA-256 of the sorted results with digests generated from FIRST's own
  code (`cvss_score.js`, `cvsscalc30.js`, `cvsscalc31.js`). It kills a changed
  table entry, a changed tie-break, and a changed weight. The digests must be
  regenerated from FIRST's code, never from this package.
- `TestBucketSQLGolden` pins the exact SQL text, so a changed label path or
  normalization fails without a database.

## Gotchas

- The tables are the contract. `v4_lookup.go` and `v4_max.go` are data copied
  from FIRST; do not edit them by hand. Regenerate from the FIRST source and
  rerun the differential check if the v4 specification revises them.
- Floating-point products are wrapped in explicit `float64(...)` conversions
  where the reference adds or subtracts them, so an arm64 compiler cannot fuse
  them into a fused multiply-add and change a rounded score.
- A reported score is trusted as published, including values above 10.
  `DeriveSeverity` only ignores scores that are zero, negative, NaN, or
  infinite.
- Changing `bucketThresholds` or `labelBuckets` changes the SQL bucket too.
  Run the live parity test
  (`TestSeverityBucketSQLMatchesGoBucketOnRealPostgres` in
  `go/internal/storage/postgres`) with `ESHU_POSTGRES_TEST_DSN` set.

See `doc.go` for the godoc contract and `AGENTS.md` for contributor rules.
