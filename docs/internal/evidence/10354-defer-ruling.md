RULING: DEFER — eshu-hq/eshu#10354 (Phase 5 5.2) execution waits outside the current goal; the wait rests on 5.2 being an unbuilt Phase-5 lane needing a new baseline store and an isolated Neo4j projection run, not on an indefinite "no backend" wait.

Arbiter: arbiter-eshu (`muse-wonder-full-1.0` on Muse). Date: 2026-11-07.

> Copy note: environment identifiers in the verbatim ruling are redacted to
> generic labels here per repo policy (issue #7803); the unredacted original
> is preserved operator-local with its sha256 pinned in the companion
> evidence note `10354-cve-baseline-delta-tail.md`.
Subject: #10354 "Phase 5 5.2: CVE List baseline plus deltaLog.json tail"
(open; labels `type:task`, `collector:vulnerability`, `risk:supply-chain`;
Part of #9209; follow-up to #9961). This agent is read-only and files no
edits, pushes, merges, or shared-data changes; the coordinator records and
applies this ruling. No `make pre-push` / `make pre-pr` / `make pre-pr-full`
was run (forbidden in the brief, and unnecessary for a defer ruling).

## Verified facts (each checked by the arbiter, not taken from the brief)

1. **Issue and acceptance criteria.** `gh issue view 10354` returns state
   OPEN, created 2026-11-07T19:25:59Z, body: "Part of #9209. Follow-up to
   parent #9961 (Phase 5 5.1)." Acceptance criteria: (1) "Neo4j-backed
   projection PROFILE at full-shard volume", (2) "Committable T1-T4
   benchmarks carried from 5.1", (3) "Verdict on promoting 5.3 cursor work".
2. **Parent closed.** `gh issue view 9961` returns CLOSED
   2026-11-07T19:26:18Z; `gh pr view 10349` returns MERGED
   2026-11-07T19:23:50Z ("measure shard-scoped generations; reject signed
   CVE sequences"). PR #10349 body states verbatim: "NOT_CHECKED:
   Neo4j-backed projection PROFILE at full-shard volume (carried as 5.2
   acceptance criterion)".
3. **5.1 evidence scoped 5.2 as a filed follow-up, not as 5.1-lane
   execution.** The merged note `docs/internal/evidence/9961-shard-scoped-generations.md`
   (read via `gh api .../contents/...` because this checkout's HEAD,
   `52e0cbae93`, predates the merge) records: hermetic Go suites, "no live
   backend touched"; T1-T4 measured in memory (full shard = 1,000 records,
   budget share 0.010; ≈0.76-0.79 ms/shard; 2,000-CVE day → 2-12 dirty
   shards); the `BenchmarkScratch*` shims "were temporary worktree files,
   deleted before commit"; Residual: "No live Neo4j ran on this host, so no
   EXPLAIN/PROFILE of graph projection at full-shard volume is attached
   (NOT_CHECKED)… Recording a Neo4j-backed projection profile at
   full-shard volume is recommended as a 5.2 acceptance criterion, not a
   reason to redo 5.1." Filing verdict: "5.2 … FILE", "5.3 … DO NOT FILE"
   (needs 5.2's cursor-continuity proof first), "5.4 … DO NOT FILE".
4. **No live Neo4j on this host at ruling time.** `ss -tlnp` shows only
   `127.0.0.1:5432` listening among 7474/7687/5432; TCP probes to
   127.0.0.1:7687 and :7474 return "Connection refused" (exit 1), :5432
   exit 0; `docker ps` is empty; `docker ps -a` shows two
   `neo4j:2026-community` containers `Exited (137) 2 days ago`;
   `cypher-shell`/`neo4j` are not on PATH; no graph-backend env is set.
5. **5.2 has no implementation substrate yet.** The phase table defines 5.2
   as "CVE List baseline (midnight zip with sha256 and cutoff) plus the
   `deltaLog.json` tail" with proof "Cursor continuity; baseline checksum"
   and QA check "Mirror record count matches the baseline, and cursor lag
   is within the cadence" (`docs/internal/design/advisory-intelligence-phases.md:189`).
   The mirror ADR requires a **new baseline artifact version or store**:
   #603 artifacts are "one inline JSON read whole and capped at 16 MiB at
   cleanup, and the CVE List baseline is 643 MB", plus a new
   latest-plus-previous retention rule and stream-per-shard reads
   (`docs/internal/design/9872-new-advisory-sources-and-mirror-decision.md:142-151`).
   A search for `baseline.*artifact|midnight.*zip|deltaLog` under
   `go/internal/vulnsource` and `go/internal/collector` returns no matches:
   no baseline/deltaLog code exists to profile yet.

## Corrections to the brief (the DEFER does not rest on these as stated)

6. **"QA runs Postgres + API but not Eshu projection" is inaccurate
   as an infrastructure claim.** `kubectl config current-context` is
   the QA cluster context; a read-only `kubectl get pods -A` (2026-11-07)
   shows Running: `eshu-neo4j-79bf9b8db6-kwqqm` (21d),
   `eshu-postgresql-0` (21d), `eshu-api-…` (30h),
   `eshu-resolution-engine-…` (30h, the reducer), and
   `eshu-cx-vuln-intel-cx-<qa>-…`. Historical read-only Neo4j PROFILEs on
   the QA cluster are also recorded (`docs/internal/measurements.jsonl:77-89`).
   What is true is narrower: the QA cluster is a **shared** QA graph, not an
   isolated proof bed for a write-side projection PROFILE of an unbuilt
   mirror baseline, and 5.2's own QA check (record count vs baseline,
   cursor lag) presupposes 5.2 deployed and running on cadence first.
7. **"No live backend" alone is remediable and cannot justify an open-ended
   defer.** The pinned `neo4j:2026-community` image is present locally
   (2.31 GB), and the repo's established pattern starts a fresh pinned
   container per live run (e.g. `docs/internal/measurements.jsonl:94,98,103`;
   `cypher-query-rigor` requires an isolated, uniquely-named stack for
   probes). The coordinator MUST NOT record this DEFER as "wait until
   someone provides a backend". The re-entry condition below is charterable
   with resources already on this host.

## Decisions

1. **DEFER execution of #10354 from the current goal. Keep #10354 OPEN**
   under #9209 with its labels unchanged. Why it waits: criterion 1
   requires a Neo4j-backed projection PROFILE at full-shard volume, but
   there is (a) no 5.2 code to project (no baseline store, no deltaLog
   tail — fact 5), (b) no live Neo4j on this host and no isolated
   projection harness seeded through the real `cve_list` reducer path
   (fact 4), and (c) no deployed mirror against which the phase QA check
   (record count = baseline, cursor lag within cadence) could be observed.
   Building (a)-(c) is the 5.2 lane itself — a Phase-5 feature build with
   a new artifact store — not residue of the 5.1 measurement lane, whose
   own verdict was FILE 5.2 as a follow-up (fact 3). Forcing it into the
   current goal would either fake criterion 1 or expand the goal by a
   whole phase item. Accuracy forbids the first; scope discipline forbids
   the second.
2. **The acceptance criteria are not waived, downgraded, or substituted.**
   In-memory T1-T4 accounting, a NornicDB run, or a read-only PROFILE on
   the shared QA cluster do **not** satisfy criterion 1. Neo4j is the canonical
   backend, and a projection PROFILE is a write-path measurement bound to
   the exact commit, backend digest, cardinality, and index/constraint
   state (`eshu-performance-rigor`, `cypher-query-rigor`).

## Re-entry condition (all of R1-R3 charter the lane; then #10354 runs)

- **R1 — A dedicated 5.2 goal/worktree is chartered under #9209** that
  states its decision, thresholds, and wall-clock deadline up front and
  includes the baseline-store design required by the mirror ADR
  (`9872-…md:142-151`: new artifact version/store, stream-per-shard,
  latest-plus-previous retention). No external provisioning needs to
  happen first; chartering this goal is itself the trigger.
- **R2 — An isolated proof backend is part of that goal's plan:** a fresh
  pinned `neo4j:2026-community` container/stack per the repo live-test
  pattern (or an equivalently isolated sanctioned backend where
  projection writes are allowed). Writes against the shared QA graph
  are not proof.
- **R3 — The full shard is seeded through the real path:** 1,000-record
  shard collected/projected via the merged `cve_list` substrate
  (`cvelist_shard.go` from PR #10349) and the production reducer
  projection writer, so the PROFILE measures the shipped Cypher, not a
  stand-in.

## Required proof when #10354 executes

1. Neo4j `PROFILE` of the projection at full-shard volume (1,000 records):
   plan operators, rows, db hits, elapsed time, anchor + index/constraint
   state, backend image digest, Eshu commit, batch shape — plus the
   tracked `Performance Evidence:` / observability markers the
   performance-evidence gate requires for hot-path graph work.
2. Committable (non-scratch) T1-T4 benchmarks reproducing the 5.1
   thresholds, since the 5.1 shims were deleted before commit (fact 3).
3. Cursor continuity across baseline → `deltaLog.json` tail, baseline
   sha256/cutoff recorded, mirror record count equals baseline count, and
   cursor lag within cadence on the deployed/QA surface (phases file:189).
4. Focused contract tests for baseline streaming, checksum mismatch,
   empty/partial shard, duplicate delta replay, and retraction through
   shard re-collection. Concurrency: shard generations must stay
   idempotent under concurrent dirty-shard re-collection; serialization is
   not an accepted substitute.
5. An explicit 5.3 promotion verdict (criterion 3) recorded in the 5.2
   evidence note. 5.3 stays unfiled/unpromoted until that verdict.

## What will not be done, and why

- **Not executing #10354 inside the current goal** — it would require
  building the baseline store, the isolated PROFILE harness, and a
  deployed cadence observation inside a goal whose chartered item (5.1)
  is closed and measured.
- **Not closing #10354 or marking criterion 1 met** on 5.1's in-memory
  numbers — that trades accuracy for schedule.
- **Not running a write-side projection PROFILE against the shared QA Neo4j**
  Neo4j — it mutates shared QA truth and still would not prove the
  undeployed mirror path.
- **Not deferring indefinitely on backend availability** — the pinned
  image and live-run pattern already exist locally (correction 7); the
  only gate is chartering the lane (R1).
- **Not filing or promoting 5.3 (or 5.4)** on this ruling — the 5.1
  verdict and #10354 criterion 3 sequence that decision after 5.2 proof;
  5.4 additionally waits on the owner's catalog-scope decision.

## Coordinator mechanics

Record in the goal ledger, verbatim form:
`DEFER: #10354 /tmp/defer-10354-ruling.md`
`/tmp` is the path the brief assigned; if the ledger requires a durable
committed artifact, copy this file into the repo evidence location in the
same change that records the DEFER and update the path in the ledger line
(that copy is coordinator work; this arbiter edits nothing). A one-line
comment on #10354 linking the ruling is recommended so the issue reader
need not search for why it waits.

## Not verified (NOT_CHECKED)

- The current goal's charter text was not provided to the arbiter; this
  ruling takes the brief's framing (current goal = the lane that closed
  #9961 via PR #10349). If the charter in fact names 5.2 execution, R1 is
  already satisfied and the coordinator must charter the work rather than
  record a DEFER — the DEFER would be void on that fact alone.
- QA-cluster write-access policy and whether an isolated QA namespace could
  host the PROFILE instead of a local container (only the pod list was
  read; no cluster mutation or exec was attempted).
- Upstream `deltaLog.json` format/rate limits and midnight-zip
  availability at execution time.
- Whether baseline fetching belongs to this repo or a collector satellite
  repo (cross-repo split and same-named-worktree needs not checked).
- Cost/size estimate for the new baseline store beyond the ADR's 643 MB
  figure; no measurement was run for this ruling.

## Correction 2026-11-07 (criterion-1 premise)

Second arbiter ruling (arbiter-eshu, read-only, goal-01a1b869): no Neo4j
write path for vulnerability facts exists in-repo (verified zero
vulnerability-projection Cypher; projection is reducer-side Postgres via
`BuildSupplyChainImpactFindings` in
`go/internal/reducer/supplychain/core/impact.go` plus
`PostgresSupplyChainImpactWriter`). The "Neo4j-backed projection PROFILE"
wording in #10354 criterion 1 is therefore void; criterion 1 is re-scoped
to build-layer wall-clock over the 1,000-fact shard plus EXPLAIN
(ANALYZE, BUFFERS) and wall-clock of the writer transaction. Option (B) —
building a graph projection to satisfy the wording — is rejected as scope
manufacture. All other holdings of this ruling stand; R1 is satisfied
(5.2 worktree drive/10354-cve-baseline-52, goal-01a1b869). The isolated
Neo4j container stood down for criterion 1.
