# Runner image version as a readiness profile separator (#9411)

## Question

The v2 readiness weights refresher required one runner profile (image name,
image version, Go version, Postgres digest) across every shard of every
source run. Since #9365, every green `main` push run of
`live-postgres-readiness.yml` mixes two hosted-runner image versions,
`20261012.334.1` and `20261025.353.1`, across its five shards. GitHub rolls
hosted images over several days and standard runners cannot pin a version,
so no run passed the gate. This note records whether the image version
separates readiness timings more than run-to-run noise does.

Classification: diagnostic-only. The refresher change prices nothing
differently, and no wall time is claimed.

## Method

The inputs are the shard logs and job metadata of green `main` push runs.
`imgcmp.py` parses the `Image:`/`Version:` header, every
`<Test>: PASS elapsed=` line, and job wall time. It reports two things: the
pooled per-test means by image over tests that ran under both images (mean
old at least 1 s), and per-shard old-vs-new pairs with an identical selected
test set. A companion script with the same parsing compares every same-shard
pair of runs with an identical selected test set. For each pair it reports
the symmetric ratio (max/min) of the test-elapsed sum and of the job wall,
classified as same-image or between-image.

Two five-run sets were checked:

- Set A: 40663856066, 40672571941, 40687668044, 40696592633, 40697064528.
  This set reproduces the ruling's pooled figures exactly.
- Set B: 40663856066, 40672571941, 40686743748, 40687668044, 40696592633.
  This is the set named in the lane brief.

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

## Results

| Measure | Set A | Set B |
| --- | --- | --- |
| Tests compared (under both images) | 422 | 422 |
| Pooled sum of means, old / new (s) | 2881.2 / 2587.8 | 2918.1 / 2553.1 |
| Pooled ratio new/old | 0.898 | 0.875 |
| Pooled ratio within [0.85, 1.15] | PASS | PASS |
| Max same-image test-sum ratio | 1.517 (shard 3) | 1.533 (shard 1) |
| Max between-image test-sum ratio | 1.642 (shard 3) | 1.693 (shard 3) |
| Max same-image job-wall ratio | 1.480 (shard 3) | 1.485 (shard 1) |
| Max between-image job-wall ratio | 1.432 (shard 3) | 1.555 (shard 3) |

The largest pairs in Set A:

| Shard | Run pair | Class | Test-sum (s) | Sum ratio | Job ratio |
| --- | --- | --- | --- | --- | --- |
| 3 | 40663856066 / 40672571941 | same-image (353.1) | 673.1 / 443.6 | 1.517 | 1.480 |
| 1 | 40687668044 / 40697064528 | same-image (334.1) | 469.4 / 709.7 | 1.512 | 1.450 |
| 3 | 40672571941 / 40687668044 | between-image | 443.6 / 728.7 | 1.642 | 1.342 |
| 3 | 40672571941 / 40697064528 | between-image | 443.6 / 687.5 | 1.550 | 1.432 |

## Reading

The newer image is about 10 to 12% faster per test (pooled 0.875 to 0.898).
Run-to-run noise on one image, on one shard, with the same tests, reaches
about 1.52 times. The largest between-image ratio (1.64 on test sums) is
about what that noise times the image effect predicts (1.52 x 1.1 = 1.67).
So the version adds a modest, consistent offset on top of noise. It does not
create a separate timing regime.

The ruling's criterion ("1.51 >= 1.43") compared a same-image test-sum ratio
with a between-image job-wall ratio. Like for like, it holds only for job
wall on Set A. It fails on test sums for both sets and on job wall for
Set B. The refresher change does not rest on this criterion alone: the image
version is never used in pricing, and the committed v2 weights regenerate
byte-identical with the patched refresher (see the PR).

## Not checked

- Runs outside the six listed above, and other hosted image versions.
- Per-package residual (row overhead) differences by image. Only test
  elapsed sums and job wall are compared.
