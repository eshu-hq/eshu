# #7767: ApplicationSet that deploys from its own config repository

This note records the evidence behind the change that records a `DEPLOYS_FROM`
fact from the control repository when an Argo CD `ApplicationSet` renders a
template source that is also the repository its git generator reads config
from. It exists because the change touches hot files (the reducer deployment
binding, the Postgres ingestion commit path and the telemetry instruments) that
the performance-evidence gate guards.

## What runs, and where

The extractor (`go/internal/relationships`) is pure in-memory Go. It runs once
per ingestion batch in `DiscoverEvidenceWithStats`, called from the Postgres
ingestion commit path (`go/internal/storage/postgres/ingestion.go`). It issues
no SQL, no Cypher, no lease, no claim, and takes no lock. The single call added
to `ingestion.go` records the tally into an OpenTelemetry counter and sits
before the empty-evidence early return. It runs inside the open commit
transaction but issues no statement and takes no lock; the transaction also
carries the 86 added evidence rows from the measurement input (145 to 231). The
reducer edits register one provenance string in the existing Argo lists and one switch;
they add no query and no loop.

Metric boundaries: start is the call to `DiscoverEvidence`, end is its return;
correctness invariant is "no fact has the same repository as source and target,
and no resolved template source that is not the control repository is dropped
silently, except the case where the generator reads config from the control
repository itself, which stays a known silent skip pinned in the invariant test
and tracked in #7778"; the intended delta is one added `DEPLOYS_FROM` fact per matched
self-referencing ApplicationSet file; stop threshold is a regression above 10
percent of the baseline median.

## Measurement

Input shape: 190 `ApplicationSet` files from a private GitOps repository and the
config files their git generators read (396 envelopes in all), against a catalog
of 936 repositories. The numbers below are aggregates; repository names stay out
of this repository.

Method: a local-only harness (git-excluded, not committed) calls
`DiscoverEvidence` nine times per process on that input and reports the median.
Runs alternate between a worktree at `origin/main` (baseline) and a worktree at
the change (after); the order flips every round. There is no database backend
involved. Nine rounds ran on a laptop shared with other work: load1 was 20 to 25
in rounds 1 to 3 and fell to about 9 to 12 in rounds 6 to 9.

| Round | Baseline median | After median | After against baseline |
| --- | --- | --- | --- |
| 1 | 34.5 ms | 42.3 ms | +22.7% |
| 2 | 40.6 ms | 40.5 ms | -0.1% |
| 3 | 41.4 ms | 38.4 ms | -7.3% |
| 4 | 33.8 ms | 27.1 ms | -19.9% |
| 5 | 51.8 ms | 48.7 ms | -6.0% |
| 6 | 26.3 ms | 25.8 ms | -2.0% |
| 7 | 29.6 ms | 26.7 ms | -9.9% |
| 8 | 26.0 ms | 27.2 ms | +4.6% |
| 9 | 24.5 ms | 27.9 ms | +13.7% |

Mean of the nine round medians: baseline 34.3 ms, after 33.8 ms (-1.3%). In the
four quietest rounds (6 to 9) the means are 26.6 ms and 26.9 ms (+1.0%). Single
rounds swing from -19.9% to +22.7% with the order flipped. Two rounds exceeded
the 10 percent stop threshold in the adverse direction: round 1 (+22.7%, baseline
ran first) and round 9 (+13.7%, after ran first, one of the quiet rounds, with
the ranges not overlapping: after min 27.0 ms against baseline max 25.5 ms).
Three rounds (4, 7 and 3) went the other way by 19.9%, 9.9% and 7.3%. So the
sign is not stable and a small true cost is not excluded by the timing alone.
The conclusion rests on two things together: the means are within -1.3% to +1.0%
against a laptop noise floor that is larger on a 25 to 50 ms call, and the code
path is in-memory with no SQL, Cypher, lock, lease or queue change, so there is
no mechanism for a large cost beyond about one extra fact per matched
ApplicationSet file and a counter add. This is a no-regression result within
noise, not a speedup, and it is a laptop measurement, not a production wall
time. A quiet-host benchmark in CI or a dedicated run is the way to resolve a
cost below the noise floor.

Terminal counts, baseline against after, same input:

| Evidence kind | Baseline | After |
| --- | --- | --- |
| `ARGOCD_APPLICATIONSET_DISCOVERY` | 104 | 104 |
| `ARGOCD_APPLICATIONSET_DEPLOY_SOURCE` | 41 | 41 |
| `ARGOCD_APPLICATIONSET_TEMPLATE_SOURCE` | 0 | 86 |
| `ARGOCD_DESTINATION_PLATFORM` | 0 | 0 |
| Facts with source equal to target | 0 | 0 |

The 86 new facts all run from one control repository to 20 deployed
repositories (counted from the per-row output of the local harness). No existing row is removed or changed. Every real destination in
this input is templated, so the platform half of the change is inert here.

No-Regression Evidence: DiscoverEvidence on 190 ApplicationSets and 396 envelopes, nine interleaved rounds with the order flipped each round, mean of round medians 34.3 ms baseline against 33.8 ms after (-1.3%) and 26.6 ms against 26.9 ms (+1.0%) in the four quietest rounds, single rounds between -19.9% and +22.7% with two rounds above +10% (1 and 9, opposite orders) and three below -7% so the sign is not stable (see the measurement section), in-memory only with no SQL, Cypher, lock, lease or queue change; terminal counts 104 discovery and 41 deploy-source unchanged, 86 template-source facts added, 0 self-loops.

## Operator visibility

Observability Evidence: new counter eshu_dp_argocd_applicationset_template_source_total{outcome} with the closed outcome set deploy_source, template_source_self_reference, skipped_control_repo and skipped_templated_destination, recorded at the ingestion commit path, documented in docs/public/reference/telemetry/metrics-reducer-storage.md and the telemetry-coverage.md row; scripts/verify-telemetry-coverage.sh passes and the Postgres commit-path test asserts one data point per outcome.

An operator at 3 AM reads the counter by outcome: a `template_source_self_reference`
rate shows repositories gaining a deployment source, a sustained
`skipped_templated_destination` rate shows platform edges missing because
destinations are templated (the repository edge was still emitted), and
`skipped_control_repo` is a designed skip. The counter does not cover an
`ApplicationSet` whose generator reads config from the control repository
itself; that case is tracked in #7778.

## Why this is safe

- Accuracy: the new fact is emitted only inside the matched-template-source loop,
  so a repository that is only a generator target gets the discovery edge and
  nothing else (pinned by `TestApplicationSetConfigOnlyRepoGetsNoDeployOrPlatformEvidence`).
  A template source that is the control repository stays skipped. No fact has the
  same repository as source and target (invariant matrix, 16 rows).
- Concurrency: no shared state is added. The tally is a per-call value, and the
  counter add is safe for concurrent use by the OpenTelemetry SDK.
- Idempotency: facts are de-duplicated by (kind, control repository, deployed
  repository, file path) through the existing `seen` map, and the writer MERGEs
  one `DEPLOYS_FROM` per ordered repository pair.
- Rollback: reverting the commit removes the fact kind and the counter, but it
  does not remove persisted `TEMPLATE_SOURCE` evidence rows. An older binary
  reading those rows still turns them into `DEPLOYS_FROM` candidates, because the
  resolver aggregates candidates without filtering by evidence kind; only the
  reducer workload binding on an older binary ignores the kind. Graph edges
  already written stay until a later reconcile retracts them.
- Not measured here: the live Postgres commit path under load, the explicit-mode
  Neo4j end to end, and the golden-corpus and Ifa lanes. CI is the authority for
  those.
