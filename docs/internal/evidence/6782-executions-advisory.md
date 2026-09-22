# #6782: execution-count divergences are advisory at the gate

Owner direction 2026-09-21: fix the differential oracle permanently instead
of excusing one flapping statement per PR.

## Symptom

After 574566079 retired allowlist entry 57, the `differential nornicdb vs
neo4j` job was red on every main run and on every open PR whose paths select
it, each run on a different statement:

| head | run | reproduced divergence (quorum) | kind |
| --- | --- | --- | --- |
| main 4c4bc378 | 35654686333 | stale allowlist entry 36 (fails before quorum) | parse |
| main 788edd169 | 35662995322 | workload-cloud USES read, `ORDER BY name, id LIMIT` | results |
| main 965631995 | 35664395755 | `UNWIND $pairs ... INVOKES_CLOUD_ACTION ... RUNS_IN` write, nornicdb=8 vs neo4j=7 | executions |
| PR #6892 9270f6dc | 35661069105 | USES read + package-consumption DEPENDS_ON MERGE, nornicdb=2 vs neo4j=3 | results + executions |
| PR #6931 | 35659894831 | USES read + `UNWIND $pairs` write | results + executions |
| PR #6932 995a98f9 | 35661933201 | `UNWIND $pairs` write | executions |

The USES read is a real non-total `ORDER BY` fixed in #6932. Every other
reproduced divergence is the `executions` kind: identical statement and
parameters, every record `Failed=false`, agreeing (empty) result sets, only
the number of executions differs. From the #6892 captures (artifact
10668492757), the DEPENDS_ON row `package-consumption:repository:r_ea78e8bb->
repository:r_3eddcea1` ran 3 times on Neo4j and 2 on NornicDB in both
pairings.

## Root cause

`CompareRecordings` defines the `executions` kind as scheduling noise (drain
passes, retries, regrouped batches). Quorum was expected to drop it as
pairing-local, but NornicDB and Neo4j drain the same intents at
systematically different speeds, so the pass-count difference reproduces
across both pairings. The only outlet was a per-statement `tier: executions`
allowlist entry; 21 had accumulated and each new corpus edge or drain loop
added another. `entryMatches` requires tier == kind, so the DEPENDS_ON
statement's existing `tier: missing` entry did not cover its `executions`
divergence either.

## Change

- `backendconformance.AdvisoryKind` / `SplitAdvisory`: `executions` is
  advisory; `missing`, `results`, `failures`, `rowcount` stay required.
- Quorum phase: reproduced `executions` divergences go to a new
  non-required finding `nornicdb_vs_neo4j_executions` (`[WARN]`, counted as
  advisory-warn, first statement named); `nornicdb_vs_neo4j_quorum` fails
  only on the required kinds.
- Single-pair `capture.Compare`: prints advisory lines and returns nil for
  executions-only divergences.
- Allowlist: the `executions` tier is removed from the parser (an entry
  using it is a parse error) and the 21 entries are deleted; the staleness
  exemption they needed is gone with them. 71 -> 50 entries, tiers now
  `missing` and `results`.

Row truth is unchanged: every read is still compared through `results` and
`missing`, and a one-sided error still fails through `failures`.

What the change gives up: on a write statement, `executions` was the only
differential signal. Writes return no rows, so `results` compares two empty
digests, `rowcount` is unreachable once counts differ (the classifier
returns `executions` first), and `missing` fires only when the statement ran
on one backend alone. A write that one backend silently drops and the
reducer re-drives to convergence therefore shows here only as a count
difference, which is now advisory. Its consequence is still caught by two
layers this phase does not own: a later read in the same capture, where it
covers the written state, observes it and diverges on `results` or
`missing` (a MERGE that duplicates on one backend, or a drop that is never re-driven, still fails
there), and the B-12 snapshot's node and edge count tolerances on the
canonical backend. The advisory count itself has no ceiling yet; #6941
tracks bounding it.

## Proof

Seeded RED/GREEN pairs, all run with `go test -count=1`:

- `TestSplitAdvisorySeparatesExecutions` (backendconformance): partition
  and order pinned.
- `TestCompareReportsExecutionsOnlyAsAdvisory` and
  `TestCompareStillFailsOnResultsWithExecutionsNoise` (capture): the
  executions-only pair returns nil and is named as advisory; a results
  divergence beside it still fails and is counted alone.
- `TestAllowlistRejectsExecutionsTier`,
  `TestAllowlistStatementTierMatchesAdvisoryKind`,
  `TestAllowlistTierScopesExcuseToKind` (now on `failures`).
- `TestRunBackendDiffQuorumReproducedExecutionsIsAdvisory` and
  `TestRunBackendDiffQuorumResultsBesideExecutionsFails`
  (golden-corpus-gate): the real phase over real sink files.

Shim, before and after, on the four real CI capture artifacts with the
committed allowlist (`golden-corpus-gate -phase=backend-diff` in quorum
mode; baseline binary built from main 965631995, after binary from this
branch):

| captures | before | after |
| --- | --- | --- |
| PR #6892 (35661069105) | 1 required-fail (2 reproduced: USES results + DEPENDS_ON executions) | 1 required-fail (USES results only), 67 executions advisory |
| main 965631995 (35664395755) | 1 required-fail (`UNWIND $pairs` executions) | 0 required-fail, 56 executions advisory |
| PR #6932 (35661933201) | 1 required-fail (`UNWIND $pairs` executions) | 0 required-fail, 70 executions advisory |
| PR #6931 (35659894831) | 1 required-fail (USES results + `UNWIND $pairs` executions) | 1 required-fail (USES results only), 56 executions advisory |

The two residual reds are the USES read, which #6932 fixes; main and #6932
themselves go green. The advisory counts are the noise the 21 entries had
been excusing plus the unexcused tail.

No-Regression Evidence: the files the perf-evidence gate names as hot
(`backendconformance/differential_kinds.go`, `capture/allowlist.go`,
`capture/diff.go`) run only inside the gate's offline backend-diff phase,
never on a service path. Baseline binary built from main 0f1a7e2864 (the
branch base when measured) versus the head binary built at a1c7535d97
(commits after it on this branch touch no Go file: `git diff a1c7535d97..HEAD
-- '*.go'` is empty), `-phase=backend-diff` in quorum mode over the PR #6892
capture set (10,820 records across both pairings) with the committed
allowlist, same host, three consecutive runs each, every run reported
(`/usr/bin/time -p`): baseline real 1.27 / 0.24 / 0.25 s, user 0.23 / 0.23
/ 0.23 s; head real 1.27 / 0.25 / 0.31 s, user 0.23 / 0.22 / 0.23 s. The
first run of each binary is a cold launch of a fresh build, which is why
its wall time differs while CPU does not; runs 2-3 are the comparable
figures, and the 0.31 s is one wall-clock outlier with unchanged CPU. Input
shape and terminal verdicts as in the table above; the only added work is
one O(n) partition over an already-materialized slice and per-pairing and
advisory report lines bounded by `capture.MaxReportedDiffs`.

No-Observability-Change: no new metrics, spans, or log keys. The gate report
gains one finding line per quorum run.

## Advisory ceiling (#6941)

The executions-kind advisory finding above has no upper bound: a backend
regression that tripled drain passes would still report as an advisory `WARN`
and the gate would still pass green. #6941 adds a ceiling,
`-diff-executions-advisory-max` (quorum mode only), that fails the gate with a
required `nornicdb_vs_neo4j_executions_ceiling` finding when the reproduced
advisory total exceeds it; 0 (the default) disables the check, and within the
ceiling the existing advisory finding is unchanged.

### Calibration

Observed reproduced advisory execution-count totals, same quorum invocation
and committed allowlist, read from the `[WARN] nornicdb_vs_neo4j_executions`
line of each run's "Compare backend recordings" step. Every completed
`golden-corpus-gate.yml` run since #6942 merged (2026-09-22, oldest first):
57 (35711167105, main d4b50d1348), 54 (35711318028, main 1f777b5e48),
55 (35715309080), 63 (35715863614), 58 (35715941325), 12 (35720266323),
64 (35720279505, main 9208c2f575), 58 (35723358515), 14 (35723589651).
Every completed run that printed an advisory total is listed; runs that
were cancelled or failed before the compare step print none. Four earlier
capture artifacts replayed locally with this branch's binary
(`-diff-executions-advisory-max=200`, no ceiling finding on any): 56
(run 35664395755, main), 70 (run 35661933201, PR #6932), 56 (run
35659894831, PR #6931),
21 (run 35695605570, PR #6892). Observed range 12-70. CI now passes
`-diff-executions-advisory-max=200` (`.github/workflows/golden-corpus-gate.yml`,
"Compare backend recordings" step): about 2.9x the observed max. This is a
systemic-regression tripwire, not a tuning target -- it exists to catch a
gross behavioral change (e.g. drain passes tripling), not to track the
corpus's normal scheduling-noise band. Narrowing it toward the observed
band would turn ordinary run-to-run variance into gate flakes; the point is a
wide backstop, not a calibrated alarm.

The advisory finding's detail changed shape (#6941). It previously named
the first recorded divergence with its per-statement execution counts
(`nornicdb=N, neo4j=M`), which says how far one statement's counts differ
but not whether the total is spread across many statements (scheduling
noise, the expected shape) or concentrated on one or two (a possible
regression). It now names the top 3 statements by reproduced-divergence
count (`backendconformance.TopAdvisoryStatementReports`); a statement over
120 runes is elided in the middle and suffixed with an 8-hex SHA-256
digest, because the corpus has a 116-statement UNWIND family sharing head
and tail that diverges at rune 142, inside any fixed cut (census over the
nornicdb leg of the run 35664395755 pairing-1 capture: 634 distinct
statements give 634 distinct labels with the digest, worst family 1;
the elided text alone gives 490, worst family 116). The ceiling finding names the same top
statements. The per-statement execution magnitude (`nornicdb=N, neo4j=M`)
is no longer on the summary line, and on the passing path it is on no
surface: the per-pairing dump prints at most `capture.MaxReportedDiffs`
(20) divergences per pairing, and the capture artifact is uploaded only
when the job fails. That trade is deliberate:
the ceiling and the detail both count reproduced fingerprints, because a
drain-pass regression shows up as more statements diverging, not as one
statement's counts drifting further apart.

### Performance and observability markers (#6941)

No-Regression Evidence: the change is gate verdict logic over already-captured recordings, not a runtime path. Baseline main's `golden-corpus-gate` binary (391a69b892) vs this branch's, `-phase=backend-diff` in quorum mode over the run 35664395755 capture (634 distinct statements, 56 reproduced advisory divergences, committed allowlist), one warm-up each then eight warm runs each, interleaved with alternating first-mover on the same host: main min 234 / median 245 / max 269 ms, branch min 228 / median 248 / max 258 ms, median delta +2 ms (a separate reviewer's interleaved run: medians 298 vs 294 ms, delta -4 ms). Both exit 0 with identical findings apart from the widened advisory detail, and the branch adds no ceiling finding at `-diff-executions-advisory-max=200`. An earlier sequential measurement (main three runs then branch three runs) read 295-297 vs 247-255 ms; that gap was page-cache order, not the change, and is not evidence. The ranking helper is O(n log n) over at most the advisory total (12-70 observed), so it cannot dominate a phase that already loads and compares every recording.

No-Observability-Change: no metric, span, log key, or status field changes; the only new operator-visible text is the advisory finding's top-3 detail and the ceiling finding, both on the gate's stdout.

### RED/GREEN proof commands run

Unit level (`go/internal/backendconformance`, pure ranking/truncation helper):

```bash
cd go && go test ./internal/backendconformance -run TestTopAdvisoryStatementReports -count=1 -v
```

Gate level (`go/cmd/golden-corpus-gate`, real sink files through the quorum
phase, `-diff-executions-advisory-max` under test):

```bash
cd go && go test ./cmd/golden-corpus-gate -run \
  'TestRunBackendDiffQuorumExecutionsWithinCeilingPasses|TestRunBackendDiffQuorumExecutionsAboveCeilingFails|TestRunBackendDiffQuorumExecutionsCeilingDisabledNeverFails|TestRunBackendDiffQuorumAdvisoryDetailNamesTopStatements' \
  -count=1 -v
```

Both RED (flag/finding undefined, `TopAdvisoryStatementReports` undefined)
before the implementation and GREEN after; full package runs
(`go test ./internal/backendconformance ./cmd/golden-corpus-gate -count=1`)
stayed green with no regressions in the existing #6782 quorum/advisory tests.
