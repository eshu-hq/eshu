# #11186: projector workers poll at the base interval while the pool is active

Evidence for the change that keeps every projector worker in a process polling at
the base interval while any worker of that process has claimed within the last
5 seconds. The per-source backpressure replay (#10882) showed that a burst of
healthy work waits behind one awake worker when the idle backoff (#10869) has put
its siblings to sleep. This note records the design, the three builds that were
measured, the registered run, and what is still not proven.

## Design

`processActivity` (`go/internal/projector/idle_wait.go`) is one atomic word that
holds the time of the last claim any worker of the process made. An empty poll
resets its backoff counter to 1 (the base interval) when that claim is less than
`maxProjectorPollInterval` (5 s) old; otherwise it keeps #10869's 1, 2, 4, 5 s
sequence. The first claim after the whole process was idle also wakes the sleeping
workers once per poll interval (`idleWake`). The word is the time since a base
that keeps Go's monotonic reading, and a clock that reads earlier than the last
claim counts as idle, so a wall-clock step cannot hold the pool "active". The claim
SQL, the worker count and the length of a worker's run of claims are unchanged. The
sequential `Workers <= 1` path is not gated.

## Why this and not the other two builds

Three builds were run against the same registered check: B's projector queue wait
in arm T2TTL (source A paced, TTL 5 s) compared with the same build's arm C2
(control), p50 and p95, tolerance as `check_arm.py --kind t2` computes it. Each set
is three C2 reps and three T2TTL reps unless noted. Values are seconds.

| Build | Change | C2 p95 (mean) | T2TTL p95 per rep | Registered result |
|---|---|---|---|---|
| Baseline (fix build 9e63e08c11) | none | .937, 1.028, .924 (.963) | 1.457, .919, 1.155 | rep 1 FAIL (tolerance .209) |
| Flat 1 s backoff (throwaway af6e541c8b, never merged) | `return base` in `poll_backoff.go` | .835, .859, .721 (.805) | .830, .845, .927 | PASS 3 of 3 against the original control; p50 INCONCLUSIVE once the load-breached control rep is replaced by its rerun |
| Sibling wake alone (d1370c6938) | wake sleepers on the first claim after the worker's own empty poll | .902, .946, .897 (.915) | 1.075, .973, 1.088 | reps 1 and 3 FAIL (tolerance .097) |
| This change (835ce351a0) | activity gate plus wake after idle | see the table below | see the table below | PASS 3 of 3 valid reps |

The flat backoff is refused as the fix: it is the polling pattern #10869 removed.
With four workers an idle process polls 0.8 times a second with the backoff and 4
times a second without it, 3.2 more queries a second per process, forever. The
sibling wake alone left the burst to the one worker that was already awake when it
was in a run of claims, and woke siblings that then went back to a long sleep.

## Registered run on this change (835ce351a0)

The check, the tolerance rule, the worker count (4) and the push time are the ones
the earlier builds used. Control mean: p50 0.4375 s (tolerance 0.1190, 27% of the
mean), p95 0.8354 s (tolerance 0.2955, 35% of the mean). The load gate is start
load1 below 15 and run maximum below 15 on the host.

| Run | B p50 | B p95 | Start load1 | Run max load1 | Result |
|---|---|---|---|---|---|
| C2 rep 1 | .452 | .934 | 5.46 | 5.9 | control |
| C2 rep 2 | .460 | .786 | 8.27 | 11.3 | control |
| C2 rep 3 | .400 | .787 | 4.40 | 11.3 | control |
| T2TTL rep 1 | .481 | .967 | 5.01 | 10.8 | p50 PASS, p95 PASS |
| T2TTL rep 2 | .392 | .896 | 6.01 | 7.2 | p50 PASS, p95 PASS |
| T2TTL rep 3 | .377 | .887 | 10.21 | 24.5 | VOID: run maximum above 15 |
| T2TTL rep 3 rerun | .463 | .948 | 5.17 | 9.8 | p50 PASS, p95 PASS |

Rep 3 ran while other jobs compiled on the host, so it is void under the gate and
was rerun on the same build. Its values are listed only for the record; the verdict
uses the rerun. The checker printed 0 FAIL and no INCONCLUSIVE line for reps 1 and
2 (146 PASS lines over the control set and both reps) and 0 FAIL, 35 PASS for the
rerun. In every T2TTL run the fleet-summed engaged counter was 1.0 and the
claim-grant consult count was 176 to 178.

Arm against the control mean: p95 is above it in all three valid reps (by .132,
.061 and .113 s) and p50 is above it in two (by .044 and .026 s) and below it in
one (by .046 s). The p95 tolerance is wide because the three control reps spread
(.786 to .934), not because the arm has headroom: the earlier sibling-wake values
1.0754 and 1.0876 would also pass under this tolerance. This note claims no margin.

Reported, not gated: the 504 B items of the three original T2TTL reps include 6 above
1 s with a slowest of 1.13 s; the C2 reps include 6 above 1 s with a slowest of
1.79 s. The rerun has 168 items, none above 1 s, slowest 0.98 s.

## Idle cost

A projector process on a reset replay database, four workers, no feed, 120 s measured
10 s after start (`eshu_dp_queue_claim_duration_seconds_count{queue="projector"}`
difference). Host load1 was 8 to 14.

| Build | Run 1 | Run 2 |
|---|---|---|
| Backoff unchanged (fix build 9e63e08c11) | 96 | 96 |
| This change (835ce351a0) | 96 | 96 |

An idle process polls at 0.80 claims a second with and without the change.

## Claim counts

Same metric, whole run, per rep (C2 control; T2TTL arm):

| Build | C2 | T2TTL |
|---|---|---|
| Baseline | 323, 266, 349 | 515, 467, 457 |
| Flat 1 s backoff | 815, 805, 831 | 1216, 1183, 1118 |
| Sibling wake alone | 605, 481, 499 | 627, 599, 655 |
| This change | 731, 661, 683 | 850, 810, 804 (rerun 864) |

While the pool is active the change polls more than the baseline and less than the
flat backoff.

## Cost while active

Extra claim polls per second per process while active: `(Workers - 1) / PollInterval`.
The replay used 4 workers (3 per second). `Workers` defaults to `min(NumCPU, 8)`
(`go/cmd/ingester/README.md`), up to 7 per second. Cost per process:
`(Workers - 1) / PollInterval x mean hand-out time`, with the mean read from the claim
histogram.

Measured on the replay host, per claim (mean of `_sum / _count`): 3.9 to 11.4 ms at the
C2 shape and 74 to 108 ms at the T2TTL shape. The T2TTL mean includes the per-source
probe and is not split into empty and non-empty polls, so it bounds the cost of an empty
poll from above and does not measure it. At 4 workers that is 12 to 34 ms of query time
a second at the C2 shape and 0.22 to 0.32 s a second at the T2TTL shape; at 7 extra polls
the figures are 28 to 80 ms and 0.52 to 0.76 s.

On QA the projector runs inside one ingester process (one replica). Its
`ESHU_PROJECTOR_WORKERS` is not set, so `Workers` is the default `min(NumCPU, 8)`; the CPU
count of that node was not read, so the worker count is NOT_CHECKED (at most 8). The QA
hand-out mean was not measured; the replay means above are the only figures.

## Claims

| Claim | Test | Seeded violation | Role | N |
|---|---|---|---|---|
| Every worker polls at the base interval while one claimed within 5 s | `TestActivePoolKeepsEveryWorkerPollingAtTheBaseInterval` | gate removed: "a worker slept 4s at 3s while a sibling was claiming every second, want at most 1s" | n/a (no privilege surface) | 4 workers |
| An idle process polls exactly as before (#10869) | `TestIdleProcessPollsExactlyAsBefore`, `TestBackoffResumesAfterTheProcessGoesQuiet`, plus 96 claims per 120 s run above | gate always on: "claim polls in 60 idle simulated seconds = 244, want 56" | n/a | 4 workers |
| The first claim after idle wakes sleepers and no other claim does | `TestWakeFiresOnlyOnTheFirstClaimAfterTheProcessWasIdle`, `TestIdleWorkersWakeWhenASiblingClaimsAfterAnIdleSpell`, `TestIdleWorkerWakeIsDebounced`, `TestRecordIdleWakeCountsOnePerCall` | wake on every claim: "wake counter = 20, want 2"; never wake: counter 0 and "timed out waiting for three or more workers projecting" | n/a | 4 workers |
| The window is strictly under 5 s | `TestProcessActivityWindowEdges` (0, 4.999 s, 5 s, 5.001 s, no claim yet) | `<=` instead of `<`: "at the cap: active() = true, want false" | n/a | 1 |
| A backward clock does not hold the pool active | `TestProcessActivityTreatsABackwardClockAsIdle` | `age >= 0` dropped: "active() = true after the clock stepped back an hour" | n/a | 1 |
| Registered B queue wait does not degrade | `check_arm.py --kind t2`, three valid T2TTL reps | baseline build: rep 1 p95 1.457 FAIL | n/a | 3 reps |
| Race clean on the shared timestamp | `go test -race` on the projector package | not run on the final tests | n/a | 4 workers |

The seeded violations were run on a throwaway copy of this tree; the real tree was not
mutated. The simulated-clock tests drive the real `Service.Run` loop with an injected
`Service.Now` and `Service.Wait`.

## Not proven

- The registered replay ran on build 835ce351a0 (44e8a5909c after the rebase onto the
  current main; same patch). The clock-step hardening in 8a0631d984 changed how the activity
  word stores time (monotonic base, a backward reading counts as idle); the replay was not
  repeated on it. The hermetic tests cover that change; the replay figures above are for the
  code before it.
- The 30-minute diagnostic of why a first graph write is slow on a near-empty graph (the
  "M3" item in the replay ruling) was NOT done. The ruling owes it before #11186 closes.
- `go test -race` on the final test set was not run: host load1 was above 8 when it was
  due. An earlier race run of the first version of the activity tests (20 and 5
  repetitions) was clean.
- Poll cost on QA: not measured (see above).
- The idle-run, claim-count and cost figures come from one lab host with other jobs running
  (load1 8 to 18); they are counts, not timings, except the hand-out means.
- The result is for the replay shape: 4 workers, one feed per second, source A paced.

## Close condition

The registered condition for #11186 (the replay ruling, decision 3, repeated in the
iteration ruling) is a T2TTL x3 pass by the same check and tolerance rule against a
same-build C2 x3, with a load-breached rep voided and rerun. This change meets it: p50 and
p95 PASS in 3 of 3 valid reps above. The issue also owes the M3 diagnostic, so this PR uses
`Refs #11186` and leaves the close to the coordinator.
