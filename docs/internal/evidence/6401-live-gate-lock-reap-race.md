# Live-gate lock: fresh-guard reap race

Root-Cause Evidence: on an unmodified checkout of `main`, the Docker-free
self-test `scripts/test-verify-golden-corpus-gate.sh` failed 2 of 8 runs. One
cause is the orphan-guard age gate in `scripts/lib/live-gate-lock.sh`, which
compares wall clock against the guard's embedded birth epoch. The case proving
a fresh guard is not reaped stamps a guard with the current epoch and then
calls `try_acquire`, which retries up to 50 times with a sleep and a
process-liveness probe per attempt. When that call outlasts the 60-second
window, the guard the case just created ages out mid-assertion and is reaped,
and the case reports a freshness violation the lock never committed. Observed
failure text: `a fresh orphan guard must not be reaped; got exit 0:
live-gate-lock: reclaimed stale lock from 44658 (/nonexistent/dead-worktree)`.

No-Regression Evidence: the change is confined to how the threshold is named.
Production keeps one fixed 60-second value assigned in-process; the gate's
comparison, its polarity, and every other code path are untouched, so there is
no new work on any run. Measured either side of the change on the same host,
the live lane in `make pre-pr` took 242s before and 242s after, inside its
1800s budget ceiling. Self-test stability went from 0 of 8 runs passing at the
first attempt (a structural assertion correctly rejecting the initial edit) and
2 of 8 failing on unmodified `main`, to 6 of 6 passing.

Observability Evidence: the failure path already prints the reclaim decision
through `live-gate-lock`'s own stderr line (`reclaimed stale lock from <pid>
(<worktree>)`), which is what identified this race from a failing run's log.
The fail-closed message names the fixture and git's real exit code so an
operator reading a red gate sees the cause rather than a downstream assertion.

## Why the threshold is not an environment variable

The first cut made it `: "${ESHU_LIVE_GATE_REAP_AGE_SECONDS:=60}"`. Review
caught that this hands any process able to export that name the ability to
shrink or zero the production window. Because the gate reaps only when
`age > threshold`, a LOWER threshold reaps sooner, so an inherited `0` would let
a transient `ps -p` false negative reap a guard created moments earlier — the
exact overlap the guard exists to prevent. A non-numeric inherited value would
also abort the `set -u` caller inside the arithmetic.

It is now a plain in-process assignment. The tests raise it by assigning the
variable after sourcing, which cannot arrive from the environment, and a
structural assertion pins the production value so a later edit cannot lower it
silently.
