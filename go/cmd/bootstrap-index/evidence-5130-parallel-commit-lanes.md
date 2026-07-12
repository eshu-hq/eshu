# Evidence: scope-partitioned parallel commit lanes (#5130)

Issue [#5130](https://github.com/eshu-hq/eshu/issues/5130), from the #5122
under-20 attribution: the accepted 896-repo run
(`3624-post5108-main-20260712T064007Z`) measured the ingestion commit chain
busy 99.66% of the 1313.7s collection span with `upsert_facts` strictly
serialized (interval-overlap max_concurrency=1, 921.1s), while the 16
configured snapshot workers achieved parse parallel_factor of only 3.16 —
starved behind the single-consumer commit loop via stream backpressure.
`drainCollector` now runs a single-consumer dispatcher (the GitSource stream
contract) feeding `ESHU_BOOTSTRAP_COMMIT_LANES` concurrent commit workers.

 Performance Evidence: prove-theory shim recorded on #5122 before
 implementation — the production insert path (embedded schema with all
 fact_records indexes, exact batch upsert SQL, one transaction per scope)
 against postgres:18-alpine driving the accepted run's real 896-scope fact
 distribution (3,615,526 facts; p50 436, p95 15,768, max 772,875): 1 lane
 107.9s, 2 lanes 55.4s (1.95x), 4 lanes 48.6s (2.22x, plateau), 8 lanes
 49.2s — exact row counts on every run, zero deadlocks or serialization
 errors. The default of 4 lanes is that measured plateau (WAL/disk plus the
 largest-scope tail bound it), deliberately not CPU count. Same-machine
 relative proof per #5122's hardware rule; the reference-machine full-corpus
 wall-clock re-measure is tracked by issue #5122.

 No-Regression Evidence: full-pipeline bounded-corpus A/B via the B-7 golden
 corpus gate (`scripts/verify-golden-corpus-gate.sh`, 20-fixture corpus,
 NornicDB backend, full sync -> discover -> parse -> collect -> reduce ->
 query drain): `ESHU_BOOTSTRAP_COMMIT_LANES=1` — 416 pass / 0 fail / 0 warn
 (34s); `ESHU_BOOTSTRAP_COMMIT_LANES=4` — 416 pass / 0 fail / 0 warn (32s).
 Both runs drain every queue to terminal and match the B-12 golden snapshot,
 the byte-identity oracle for collector facts, reducer/projector graph
 output, and query/MCP response shapes — commit order changes under lanes,
 projected truth does not. Lane unit tests pin exactly-once commits with
 real overlap (high-water >= 2 at 4 lanes), single-lane source-order
 preservation, fatal commit-failure semantics, and the env-knob contract,
 all green under -race.

 Observability Evidence: the "bootstrap collection complete" log gains a
 `commit_lanes` attribute; every pre-existing per-repo log message and
 metric ("bootstrap scope collected", "bootstrap collection progress",
 FactsEmitted/FactsCommitted/CollectorObserveDuration,
 ContentEntityEmitted) is emitted attribute-compatibly from the lane
 workers — same message names and structured fields; the commit-failure
 and progress logs now additionally carry the cycle span's trace
 correlation (they previously logged on the root context). Discovery
 advisory reports are appended in commit-completion order rather than
 source order: each report self-identifies by scope and generation id and
 the discovery-advisory playbook filters by repository, so ordering is
 not a consumer contract. No new metric instruments or pipeline stages.

Concurrency contract: scopes are independent conflict domains — one
transaction per scope, per-repo deferred-maintenance shared-lock keys, a
catalog cache designed for concurrent commit goroutines (#3481, #5129), and
idempotent ON CONFLICT fact upserts. The work channel is unbuffered so
dispatcher backpressure semantics are unchanged from the serial loop. The
first commit failure cancels the dispatcher and fails bootstrap exactly as
before (bootstrap has no retry/dead-letter path). The collector Service.Run
loop (ingester runtime) is intentionally untouched: its commit serialization
was not measured as a bottleneck and it carries different retry/dead-letter
semantics.

PR #5135 review (P1, four findings) hardened the coordinator; all four are
fixed and pinned by deterministic barrier tests (all -race, stable across
-count=20):

1. Admitted siblings finish atomically: a lane failure cancels only the
   ADMISSION context (source pulls + dispatch); admitted commits run under
   the parent context and are never canceled mid-transaction
   (`TestDrainCollectorLaneFailureLetsAdmittedSiblingsFinish`, barrier-
   coordinated: fails one lane while three siblings are held mid-commit,
   asserts zero mid-commit cancellations, all three complete, no
   post-failure admissions).
2. No stranded producers: a generation received but never dispatched has
   its fact stream drained to exhaustion, and lanes reject-and-drain any
   cycle handed off in the same instant admission stopped (closing the
   send-versus-cancel race). Source-side producers unwind through the
   admission context exactly as GitSource's snapshot workers do
   (`TestDrainCollectorDrainsUndispatchedGenerationsOnFailure`, real
   blocking-send producer goroutines, asserts every producer exits).
3. Keyed conflict admission: the dispatcher holds active ScopeID and
   PartitionKey sets and admits a generation only when both are free,
   preserving full concurrency for independent domains
   (`TestDrainCollectorSerializesConflictingScopeKeys`: interleaved
   same-scope generations never overlap per key, all commit, independent
   scopes still overlap). Bootstrap emits unique scopes, so this is a
   guard for future sources, not a hot path.
4. Pool-aware lane bound: `effectiveCommitLanes` clamps requested lanes to
   the measured 4-lane plateau AND to `ESHU_POSTGRES_MAX_OPEN_CONNS`
   headroom after reserving max(2, projectionWorkers+1) connections for
   the concurrent projector, never below one lane
   (`TestEffectiveCommitLanes`, incl. the pg96 reference profile, the
   30-conn default, and starved-pool shapes). Snapshot, parser, projector,
   content, and graph concurrency are untouched.

 No-Regression Evidence (post-hardening rerun): golden corpus gate at
 `ESHU_BOOTSTRAP_COMMIT_LANES=1` — 416 pass / 0 fail (32s) — and at `=4` —
 416 pass / 0 fail (34s) — identical B-12 snapshot and terminal queue
 drain on the reworked coordinator.

Second review round (P1: production drain invariant) — the reviewer was
right that the prior producer test was a FALSE GREEN: it modeled fact sends
as cancellation-aware, but production `factStreamWriter.send` is
unconditional and every generation's producer starts eagerly at build time,
with the stream buffering up to the snapshot worker count. Fixes:

- After a failure stops admission, `drainCollector` consumes the source to
  exact exhaustion under the parent context and drains every remaining
  generation's fact stream (parent cancellation is the one non-drainable
  path — process teardown, already reported as an error).
- `GitSource` snapshot workers that abandon a stream hand-off on
  cancellation now drain their own generation's facts instead of dropping
  a stuck producer (pre-existing leak on the serial failure path too).
- The regression test now models production honestly: PREBUFFERED
  generations whose producers start eagerly and send UNCONDITIONALLY.
  Negative proof recorded: with the exhaustion drain disabled the test
  fails with stranded producers; restored, it is green under -race.
- Hardening this surfaced a real teardown deadlock in the lane
  coordinator itself: outstanding completion signals can reach
  commitLanes+1 (in-flight at the last per-iteration drain plus one
  admission that reused a freed worker), overflowing the laneDone buffer
  exactly at exit — a worker blocked on its final send against a
  dispatcher stuck in Wait. Teardown now drains laneDone concurrently
  with the worker wait. Reproduced deterministically at -count=20 -race
  before the fix; green at -count=100 -race after.

P2 proof gaps closed: generations with different ScopeIDs sharing one
PartitionKey serialize
(`TestDrainCollectorSerializesSharedPartitionAcrossScopes`); blank
PartitionKeys are never treated as a shared conflict domain — unrelated
scopes keep concurrency (`TestDrainCollectorBlankPartitionKeysDoNotSerialize`);
the startup log now reports `commit_lanes_requested`, effective
`commit_lanes`, `postgres_max_open_conns`, and `commit_lane_reserve`.

 No-Regression Evidence (post-drain-hardening rerun): golden corpus gate
 at `ESHU_BOOTSTRAP_COMMIT_LANES=1` — 416 pass / 0 required-fail / 0
 advisory-warn (34s) — and at `=4` — 416 pass / 0 required-fail / 0
 advisory-warn (34s) — identical B-12 snapshot and terminal queue drain.
 One earlier lanes=4 attempt failed in the cassette-replay settle stage
 ("only 7 credentialed collector source(s) landed facts" after the 20s
 settle window) — that stage runs standalone cassette collectors after
 bootstrap-index has already completed and is independent of commit
 lanes; the rerun passed clean and both counts are recorded here rather
 than deferred to PR logs.

Round-3 review caught a real P1 in the round-2 exhaustion drain: its guard
also fired on the SOURCE-error path, where the stream has already closed
and GitSource has reset itself for the next discovery cycle — so the
drain's first Next call would relaunch full corpus discovery and
re-snapshot during error cleanup. The guard is now gated on the
commit-failure path only (the sole path with a still-open stream holding
buffered generations); GitSource's own cancel plus the processRepo
hand-off drain unwind an errored stream's in-flight producers. Pinned by
`TestDrainCollectorSourceErrorDoesNotReinvokeSource` with a recorded
negative proof: with the broad guard restored, the test fails with
"source.Next called 2 times"; narrowed, it is green under -race.
