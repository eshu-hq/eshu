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
scripts/measure-demo-ttfa.sh --mode cold --runs 3
```

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

## Targets

**No TTFA target is set yet.** With `--target` omitted the scorer records the
measured value and explicitly does not judge it — that is how a first
measurement run establishes a target rather than being failed by one that does
not exist.

A target is set from at least three measured runs per mode on a recorded basis,
never from an aspiration. Per the evidence rules, the basis must name the
graph backend, hardware class, corpus, and commit; a number without that basis
is not comparable to the next one and cannot support a regression claim.

Once [#4589](https://github.com/eshu-hq/eshu/issues/4589) publishes the SLO
page, the TTFA rows belong there, with this page keeping the method and the
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
