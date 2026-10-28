# Runner image version as a readiness profile separator (#9411)

## Question

The v2 readiness weights refresher required one runner profile (image name,
image version, Go version, Postgres digest) across every shard of every
source run. Since #9365, every green `main` push run of
`live-postgres-readiness.yml` mixes two hosted-runner image versions,
`20261012.334.1` and `20261025.353.1`, across its five shards. GitHub rolls
hosted images over several days, and standard runners cannot pin a version,
so no run passed the gate. This note measures how large the image-version
effect is next to run-to-run noise.

Classification: diagnostic-only. The refresher change prices nothing
differently, and this note claims no wall time.

| Candidate | Cheapest proof | Result | Disposition |
| --- | --- | --- | --- |
| Gate on image name, report the version | Pooled ratio over six green runs, like-for-like pairs | Bounded offset, below same-image noise | diagnostic-only |

## Runs and scripts

- Set A: 40663856066, 40672571941, 40687668044, 40696592633, 40697064528.
  These are the runs for the pre-declared pooled gate.
- Set B: Set A with 40686743748 in place of 40697064528. 40686743748 has a
  different inventory and is outside the refit pair rule; it is recorded as
  a sensitivity check.

Each run directory holds `run.json`, `jobs.json`, and `shard1.log` through
`shard5.log`, as fetched with `gh api` (run, jobs, and job logs). Three
scripts parse the `Image:`/`Version:` header, every
`<Test>: PASS elapsed=` line, and job wall time from `jobs.json`. Each one
takes the run IDs as arguments:

- `imgcmp.py RUN...` prints the pooled sum of per-test means by image, over
  tests that ran under both images with a mean of at least 1 s on the old
  image.
- `imgsep.py RUN...` covers every same-shard pair of runs with an identical
  selected test set. It prints the symmetric max/min ratio of the test-elapsed
  sum and of the job wall, marked same-image or between-image.
- `signed.py RUN...` covers the same pairs. It prints the signed geometric-mean
  ratio: new/old for between-image pairs, later/earlier for same-image pairs.
  It also prints the standard deviation of the log ratio.

## Image version per shard

| Run | Shard 1 | Shard 2 | Shard 3 | Shard 4 | Shard 5 |
| --- | --- | --- | --- | --- | --- |
| 40663856066 | 20261025.353.1 | 20261012.334.1 | 20261025.353.1 | 20261012.334.1 | 20261012.334.1 |
| 40672571941 | 20261025.353.1 | 20261012.334.1 | 20261025.353.1 | 20261025.353.1 | 20261012.334.1 |
| 40686743748 | 20261012.334.1 | 20261025.353.1 | 20261012.334.1 | 20261025.353.1 | 20261012.334.1 |
| 40687668044 | 20261012.334.1 | 20261025.353.1 | 20261012.334.1 | 20261025.353.1 | 20261012.334.1 |
| 40696592633 | 20261025.353.1 | 20261025.353.1 | 20261025.353.1 | 20261025.353.1 | 20261012.334.1 |
| 40697064528 | 20261012.334.1 | 20261025.353.1 | 20261012.334.1 | 20261025.353.1 | 20261012.334.1 |

Every run mixes both versions, and the version on a given shard changes from
run to run. All six runs share image name `ubuntu-24.04`, Go `1.26.10`, and
one Postgres image digest.

The patched refresher's provenance line for the refit pair 40672571941 and
40687668044 is below. It was built from the shard log headers with the
module's `_profile` and `ImageProvenance.line()`; no fit was run on the pair:

```text
weights refresh v2: runner image ubuntu-24.04 versions run 40672571941 shard1=20261025.353.1 shard2=20261012.334.1 shard3=20261025.353.1 shard4=20261025.353.1 shard5=20261012.334.1; run 40687668044 shard1=20261012.334.1 shard2=20261025.353.1 shard3=20261012.334.1 shard4=20261025.353.1 shard5=20261012.334.1; distinct 20261012.334.1, 20261025.353.1
```

## Pooled gate

The one pre-declared gate: the pooled sum-of-means ratio new/old must be
within [0.85, 1.15].

| Set | Tests | Old / new (s) | Ratio | Gate |
| --- | --- | --- | --- | --- |
| A | 422 | 2881.2 / 2587.8 | 0.898 | PASS |
| B | 422 | 2918.1 / 2553.1 | 0.875 | PASS |

## Like-for-like record

This table is recorded with no pass or fail. Ratios compare pairs on the
same shard with an identical selected test set: 26 same-image and 20
between-image pairs in each set.

| Measure | Set A | Set B |
| --- | --- | --- |
| Max same-image test-sum ratio | 1.517 | 1.533 |
| Max between-image test-sum ratio | 1.642 | 1.693 |
| Max same-image job-wall ratio | 1.480 | 1.485 |
| Max between-image job-wall ratio | 1.432 | 1.555 |
| Test sums: image effect, geometric mean new/old | 0.879 (abs log 0.129) | 0.857 (abs log 0.155) |
| Test sums: same-image sd(log) | 0.210 | 0.188 |
| Job wall: image effect, geometric mean new/old | 0.933 (abs log 0.069) | 0.910 (abs log 0.094) |
| Job wall: same-image sd(log) | 0.217 | 0.188 |

The image version is not a clean separator from run-to-run noise. On test
sums, the largest between-image pair exceeds the largest same-image pair. The
image offset (about 12% on test sums, about 7% on job wall) is smaller than
one standard deviation of same-image noise (about 21%). In Set A, both
extreme pairs contain 40672571941 shard 3 (443.6 s, the fastest shard-3 job).
That makes the max-vs-max comparison a measure of that one job, not of the
image. In Set B, the between-image extreme also contains that job.

An earlier comparison, "1.51 >= 1.43", set a same-image test-sum ratio
against a between-image job-wall ratio. It compared unlike metrics and is
withdrawn.

## Pricing is unchanged

The image version is never priced: `build_document` prices from per-test
values and row residuals, and the weights document has no profile field.
With the patched module at b888b5f3d4, the committed
`specs/live-postgres-readiness-weights.v2.json` regenerates byte-identical
from its source runs, 40603534686 and 40585622864. The unpatched module does
the same. The refresher modules did not change between b888b5f3d4 and the PR
base.

At the PR base and at this change's HEAD, the refresher rejects those same
source runs with
`weights refresh v2: source shard 2 row selected test set differs from source weights`.
`source_rows` re-splits each source SHA's weights with the current splitter,
and #9365 changed that splitter after those runs. The rejection predates this
change, which neither introduces nor fixes it.

## Not checked

- Runs outside the six listed above, and other hosted image versions.
- Per-package residual (row overhead) differences by image. Only test
  elapsed sums and job wall are compared.
