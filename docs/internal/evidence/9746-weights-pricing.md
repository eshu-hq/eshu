# Weights pricing for the two #9468 proofs (#9746)

The #9468 lane (`drive/9468-gates`, closed unmerged) enrolls two live proofs
that run on default prices because no merged pool contains them:

- `./internal/query/supply/chain/impact|TestDerivedFindingAuthorityOpensBehindCurrentPublicationLive`
- `./internal/storage/postgres|TestScannerResultCommitAdmitsAnalyzesCompletesLive`

Before this change both keys were absent from
`specs/live-postgres-readiness-weights.v1.json` (779 priced) and
`.v2.json` (763 priced).

## Samples

Exactly one CI sample per proof exists, from the lane's green six-shard
`call-live-postgres-readiness` legs on run 41248317646 (head `7a56616231`,
`pull_request`, all six readiness shards success):

| Proof | Shard job | Elapsed |
| --- | --- | --- |
| `TestScannerResultCommitAdmitsAnalyzesCompletesLive` | 124676586988 (shard 3/6) | 1.690 s |
| `TestDerivedFindingAuthorityOpensBehindCurrentPublicationLive` | 124676587027 (shard 2/6) | 3.180 s |

Same-shape disposable-PG proof times on the lane (2.03 s / 2.60 s, PR #9748
body) agree to within a second; the CI shard values above are the priced ones.

## Pricing rule

The standalone-refit rule (#9745) prices one test's samples at the pool p75
(linear inclusive), which degenerates to the single value with one sample
(`PricingRule.statistic`), rounded to a tenth (`_checked_tenth`); v1 prices
the mean of the samples seen, identical for one sample. Applied here:

- result-commit proof: `round(1.690, 1)` = **1.7 s**
- finding-authority proof: `round(3.180, 1)` = **3.2 s**

Both weights files gain exactly these two entries (v1: 779 -> 781,
v2: 763 -> 765); every scalar and existing price is byte-identical, and both
defaults recompute unchanged (v1 4.4 s, v2 4.9 s). Entries were written
through the committed `render()` so the files stay in canonical form
(the v2 two-per-line pairing reflows past the insertion points).
`source_runs` is intentionally untouched: no pool fit ran, so the files do
not claim one. The next full refit over a pool with at least four samples per
proof will reprice (and relocate) these entries; until then they stand on the
single lane samples above.

## Drift sim

`refit_evidence.py plan` on main's live plan (801 tests), plus the same plan
with both proofs inserted package-consecutively (803 tests), re-measured on
the rebased head (branch `drive/9746-price-9468` on `origin/main`):

| Weights | Plan | Defaulted | Stale | Drift errors | Budget errors |
| --- | --- | --- | --- | --- | --- |
| v2 | 801 (main) | 38 | 2 | [] | [] |
| v2 | 803 (enrolled) | 38 | 0 | [] | [] |
| v1 | 801 (main) | 38 | 2 | [] | [] |
| v1 | 803 (enrolled) | 38 | 0 | [] | [] |

Neither proof is defaulted on the enrolled plan under either file.
(The stale 2 on main's plan are these entries; stale is reported, not an error.)

## Checks

- `test-live-postgres-readiness-shards.py`: 77 tests OK.
- `git diff --check`: clean.
- NOT_CHECKED: `make pre-push`, full CI on this branch, out-of-sample scoring.
