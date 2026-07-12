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
