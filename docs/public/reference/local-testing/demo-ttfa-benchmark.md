# Demo Time-To-First-Answer Benchmark

This lane measures how long `eshu demo` takes to reach its first correlated
answer, and rejects a run that reached one without being able to say how.

It is the demo-mode companion to the
[First Five Minutes Benchmark](first-five-minutes-benchmark.md): that page
scores `eshu first-run`, this one scores `eshu demo`. Both read the same
`{data, truth, error}` envelope shape and share a scorecard vocabulary.

## What TTFA Means Here

**Time-to-first-answer is measured from command invocation to the first
successful graph-authoritative answer envelope.** `eshu demo up` spans exactly
that interval and reports it as `data.total_millis`, so the scorer reads the
command's own measurement instead of timing it from outside and drifting.

Two modes are measured, and they are **never averaged**:

| Mode | Meaning |
| --- | --- |
| `cold` | At least one demo image was missing and had to be built or pulled |
| `warm` | Every demo image was already present locally |

A blended number would understate what someone installing for the first time
actually waits through, which is the number the demo exists to be honest about.
The demo stack mostly *builds* its images rather than pulling them, so a cold
run pays build cost, not just download cost.

## Scored Criteria

| Criterion | Required | Source |
| --- | --- | --- |
| `first_answer_returned` | yes | `data.first_answer.answer` + no envelope error |
| `answer_has_truth_metadata` | yes | `truth` (or `data.first_answer.truth`) |
| `repository_indexed` | yes | `data.ready` |
| `phase_timings_complete` | yes | `data.phase_millis` + `data.total_millis` |
| `time_to_first_answer` | when a target is set | `data.total_millis` vs `--target` |
| `declared_mode_matches_observed` | when the cache was probed | `--mode` vs `--images` |

Two of these are worth explaining, because they are the rows that make the
number trustworthy rather than merely present.

**`phase_timings_complete` is required, not informational.** A total whose
composition is unknown cannot be attributed: there is no way to tell a slow
image build from a slow index, so there is nothing actionable to do about a
regression. An unattributable total is not a measurement, so a missing phase
fails the run rather than scoring the total anyway.

**`declared_mode_matches_observed` exists because a label is the weakest part
of the measurement.** Nothing downstream can tell a mislabelled warm run from a
genuine cold one by looking at the number — it just looks like good news. The
harness therefore probes the image cache **before** `demo up` and passes what
it saw; the scorer fails the run when the observation contradicts `--mode`.
Probing afterwards would prove nothing, since by then every image is present.

When the cache was not probed, that row records `not_measured` and says the
mode was taken on trust, rather than implying a check that never ran.

## Running It

```bash
cd go && go build -o bin/eshu ./cmd/eshu
```

```bash
scripts/measure-demo-ttfa.sh --mode warm --runs 3
```

```bash
scripts/measure-demo-ttfa.sh --mode cold --runs 3 --prune-build-cache
```

`--prune-build-cache` is what makes a cold run a first install rather than a
cached rebuild. It is opt-in because the build cache is shared with everything
else on the machine, and reclaiming it is not a measurement script's call to
make silently.

Each mode is a separate invocation. The harness tears the stack down between
runs, drops the demo's images first in `cold` mode, prints every run's total
alongside the median, and exits non-zero if any run fails its scorecard.

The image set comes from `docker compose config --images` rather than a
hardcoded list, so a new demo service cannot silently fall outside the
cold/warm classification.

To score an envelope you already captured:

```bash
eshu demo-benchmark --envelope /tmp/demo.json --mode warm --images present
```

## Measured Basis And Targets

All numbers below are medians of three runs, measured with
`scripts/measure-demo-ttfa.sh` on one recorded basis. Per the evidence rules a
number without its basis is not comparable to the next one and cannot support a
regression claim.

| Basis | Value |
| --- | --- |
| Graph backend | NornicDB (the demo default) |
| Hardware class | x86_64, 16 vCPU, 123 GB RAM |
| Docker | 29.3.1 |
| Corpus | 20-repository golden-corpus replay, as the `acme` org |
| Commit | `50b18bb68` |
| Machine state | dedicated host, no other containers running |
| Measured | 2026-08-09 |

| Mode | Runs (ms) | Median | Target |
| --- | --- | --- | --- |
| warm | 204164 / 204093 / 204154 | **3m24.2s** | 5m00s |
| cold, cached rebuild | 208916 / 208837 / 208628 | **3m28.8s** | — |
| cold, first install | 462874 / 456790 / 459152 | **7m39.2s** | 10m00s |

### Cold Has Two Shapes, And Only One Of Them Is A First Install

The middle row is the trap. Dropping the demo's image *tags* looks like a cold
run and is not one: the demo builds most of its images and BuildKit keeps every
layer, so the rebuild comes back out of cache. That run landed **2.3% above
warm** — a distinction that existed, passed its own checks, and discriminated
nothing.

Reclaiming the build cache as well (`--prune-build-cache`, 21.66 GB on the
measured host) moved the same mode to **7m39.2s, 2.27x warm**. That is what
someone installing for the first time actually waits through, and it is the
number the cold target is set from. Without the flag the harness now says out
loud that it measured a cached rebuild.

### Where The Time Goes

Measured with `build` recorded separately from `up`, which is the only way to
tell a slow image build from a slow bring-up:

| Mode | build | up | total |
| --- | --- | --- | --- |
| warm | 0 ms (skipped) | 203,866 ms (99.8%) | 204,347 ms |
| cold, first install | 252,818 ms (**55%**) | 207,182 ms (45%) | 460,319 ms |

An earlier revision of this page concluded that neither image acquisition nor
indexing was the cost. That was wrong, and it was wrong for a structural
reason worth keeping: it was read off a single `up` bucket that spanned image
build, container start, corpus bootstrap and the reducer drain at once. A
bucket that cannot separate those cannot support a claim about which of them
dominates.

With the phases split, a first install is **majority image build**. Warm skips
the build entirely — `up -d --wait` already builds whatever is missing, so the
explicit build step runs only when an image is absent. That guard is load
bearing: an unconditional `docker compose build` revalidates every build
context and cost 221,590 ms on an otherwise warm run, which the 5m warm target
caught.

### How The Targets Were Chosen

Both targets are regression detectors on the basis above, not aspirations. The
measured spread within a mode is under 2% (71 ms across three warm runs), so the
headroom — 47% warm, 31% cold — absorbs ordinary variance while still catching a
1.5x regression. A target that can never fail is a decoration; one that trips on
noise gets ignored.

They are basis-specific. A laptop with other Compose stacks running measured
**7m27s warm**, 2.2x the dedicated host, with no code change between the two —
which is exactly why the basis table above is not optional.

Once [#4589](https://github.com/eshu-hq/eshu/issues/4589) publishes the SLO
page, these rows belong there, with this page keeping the method and the
scorecard semantics.

## Not A Blocking CI Lane

This measurement is operator-gated and run per release, not on every pull
request. It needs Docker, several minutes per run, and a machine whose
performance class is recorded — none of which a shared CI runner provides in a
way that would make a threshold meaningful rather than flaky.

## Verification

```bash
cd go && go test ./cmd/eshu -count=1
```

```bash
scripts/measure-demo-ttfa.sh --mode warm --runs 3
```

The scorer is a pure function over the envelope, so every rejection above —
missing phase, over target, mislabelled mode, health-only run — is unit-tested
without Docker. The harness itself needs a real stack.
