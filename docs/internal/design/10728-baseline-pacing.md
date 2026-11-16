# NVD baseline pacing: bounded whole-corpus seed (#10728, criterion 2)

Status: **Draft**. This record approves no code, migration, contract change,
deployment, or QA enable. It answers criterion 2 of
[the #10728 ruling](../arbiter/10728-baseline-ruling-2026-11-16.md): the
pacing design for the bounded one-time baseline. Criterion 1 (whether to hold
the corpus) is settled YES by that ruling. Parent transport design:
[10671-nvd-feed-transport-fallback.md](10671-nvd-feed-transport-fallback.md).
Measurements:
[10671-nvd-feed-measurements.md](../evidence/10671-nvd-feed-measurements.md).

Refs #10728. Part of #9209.

## Hard constraints (from the ruling, decisions 2-4)

- Baseline claims **never arm or advance** the `nvd/_window` cursor CAS.
- Per-chunk caps: **10,000 facts / 2 MiB**, same as window claims.
- Same `nvd_envelope` and stable keys as the window path.
- Deterministic year order.
- Resumable checkpoint **distinct** from `nvd/_window`.
- QA trip line: kill switch off on **>15k NVD-proxy dirty rows/h in 2
  consecutive hourly samples** or **oldest dirty >96h**; re-enable <48h, 6
  hourly samples; #10706 p95 <= 15s / watermark <= 60s stay soak bars. No
  pausing or serializing OSV/GHSA.
- Pacing: no faster than **one chunk per planner run** until disposable-DB
  proof justifies otherwise.

## 1. Year order

Ascending, 2002 through 2026, one pass, no revisits. The 25 year files span
2002 to 2026 (measurements M3,
`10671-nvd-feed-measurements.md:56-58`). Ascending order is the only order
that keeps the resume stamp monotone (section 3): the checkpoint
`(year, lastModified)` advances lexicographically, so a crash replays from a
prefix that is always older than uncommitted work.

Within a year, records emit in ascending `(lastModified, cveId)` order. This
mirrors the satellite's emitted-prefix order, ascending `lastModified` with
CVE ID tiebreak (`nvd_window_split.go:84-89` in
`eshu-collector-vulnerability-intelligence`), so the baseline chunker and the
window-path splitter agree on what a "group" is.

Year files are rewritten only when a record changes (M4,
`10671-nvd-feed-measurements.md:64-71`); the baseline streams them and never
holds the 2.7 GiB decompressed set whole (parent design section 5,
`10671-nvd-feed-transport-fallback.md:184`).

## 2. Chunk boundaries within caps

Each baseline chunk is a prefix of the remaining ordered stream cut at whole
`lastModified`-group boundaries such that the chunk holds at most 10,000
facts and 2 MiB. The caps are the existing satellite constants
`nvdMaxOutputFacts = 10000` and `nvdMaxOutputBytes = 2 << 20`
(`nvd_output_budget.go:14-15`); the byte measure is marshalled size plus one
separator byte per fact (`nvd_output_budget.go:22-28`), so the baseline
chunker and the window path agree on what fits.

Group atomicity follows the window path: groups are maximal runs of equal
stamps in emitted-prefix order (`nvd_window_split.go:161-163`), the prefix
accumulates whole groups ascending (`nvd_window_split.go:171-178`), and a
window whose first group alone exceeds the caps is detected, not split
(`nvd_feed_slice.go:181-184`, `nvd_feed_errors.go:52-56`).

Dispositions at a boundary:

- Prefix fits, remainder exists: commit the prefix; the remainder replans
  from the next group stamp (same rule as the window path, parent design
  section 3, `10671-nvd-feed-transport-fallback.md:139-143`).
- First group exceeds the caps: the chunk routes to the API walk with class
  `nvd_feed_group_oversize` (`nvd_feed_selector.go:45`,
  `nvd_feed_collect.go:51-52`), exactly as the window path does (parent
  design, `10671-nvd-feed-transport-fallback.md:145-152`). It never loops
  silently and never sheds. On the measured sample this route is unreachable
  for fact count: the densest single record emits 2,048 facts (M5,
  `10671-nvd-feed-measurements.md:77-78`), under the 10,000 cap. The byte
  side is NOT_CHECKED (per-fact byte sizes unmeasured).

Scale check from first-hand inputs (proof script `/tmp/10728_baseline_pacing_proof.py`,
`PROOF OK` 2026-11-16): 425,770 records (ruling:31) at the measured mean 7.07
facts/record (M5) give ~3.01M facts, ~301-302 claims at the fact cap, ~151h
at one chunk per 30-minute planner run (ruling:46-48 says ~301 claims,
~150h). The arithmetic reproduces the ruling.

## 3. Resumable checkpoint, distinct from `nvd/_window`

The baseline checkpoint is `(baseline_run_id, year, resume_last_modified,
year_sha256)` in its own store (new table or keyspace; implementation lane
chooses, following the `NVDWindowCursorStore` pattern in
`go/internal/storage/postgres/vulnerability/nvd_window_cursor.go:70-76`).
It is **not** a row in `vulnerability_nvd_window_cursors` (DDL at
`go/internal/storage/postgres/vulnerability/nvd_window_cursor.go:21-28`,
migration `275_vulnerability_nvd_window_cursor.sql:22`), and baseline claims
**never** carry a `GenerationStateWriter` for that table.

The mechanism that enforces this is already in the commit path: the host
arms the cursor writer only for window scopes
(`go/internal/collector/extensionhost/nvd_window_cursor.go:49-50`,
`IsNVDWindowScope` check) and only when the generation's snapshot verdict
says should-advance (`nvd_window_cursor.go:56-63`). Baseline claims use a
scope family **outside** the `nvd/_window/` prefix (prefix constant at
`go/internal/vulnsource/nvd_window_scope.go:21`), so the arming function
returns nil for them by construction, and the CAS query (`NVDWindowCursorAdvanceQuery`,
`nvd_window_cursor.go:40-59`) never sees a baseline candidate. A wrong
scope prefix is therefore a fail-closed shape error (the scope parser
rejects non-window ids at `nvd_window_scope.go:46-78`), not a cursor
corruption.

Resume rule: after a crash, the next baseline claim re-reads the checkpoint
and restarts at the recorded `(year, resume_last_modified)` group stamp.
Committed prefix groups replay idempotently under stable keys (section 6);
uncommitted groups were never checkpointed, so they re-emit whole.

## 4. Planner-cadence interaction

The window planner is pure planning math with no I/O and no locks
(`go/internal/coordinator/vulnerability/nvd_window_planner.go:12-18`): one
bounded window per call, windows tile, next start is the committed cursor
(`nvd_window_planner.go:43-70`). The baseline lane mirrors this shape with
its own planner over the checkpoint instead of the cursor: one chunk per
call, next start is the checkpointed group stamp, at most one chunk per
planner run (ruling decision 2), which the parent design budgets as at most
10,000 facts per 30 minutes
(`10671-nvd-feed-transport-fallback.md:177`).

Cadence is per-target config (`refresh_interval`,
`go/internal/coordinator/vulnerability/intelligence_scheduler.go:105-108`;
targets planned inside their interval skip at
`intelligence_scheduler.go:226`), so the one-chunk-per-run bound must be
enforced **in the baseline loop** (skip while a chunk for the current stamp
is already claimed or in flight), not assumed from the tick. Lease fencing
covers the overrun case: a feed attempt that outlives its lease is fenced by
the claim-row lock and the `fencing_token` filter (parent design section 7,
`10671-nvd-feed-transport-fallback.md:213-219`), and the commit-transaction
writer model means a fenced claim's state write rolls back with its
generation (`go/internal/collector/generation_state.go:22-29`).

The window stream is unaffected: it keeps planning from the cursor
(`nvd_window_derivation.go:61-67`, one deterministic scope per tick), and
cutover back after catch-up needs no flag (parent design section 7).

## 5. Trip-line wiring and operator flip

The QA baseline run obeys the same governed trip line as the window-path
enable (parent design section 8,
`10671-nvd-feed-transport-fallback.md:223-236`):

- Off (>15k NVD-proxy dirty rows/h in 2 consecutive hourly samples, or
  oldest dirty row >96h): the operator sets `nvd_feed_transport` to off.
  The key is the satellite instance-config switch
  (`nvd_feed_config.go:10`); absent or false keeps the API-only transport
  and is the kill switch (`nvd_feed_config.go:12-23`,
  `runtime.go:63-68`). "NVD-proxy rows" means dirty-log rows whose
  `advisory_id` starts with `CVE-` (parent design,
  `10671-nvd-feed-transport-fallback.md:231-233`); OSV also carries CVE ids,
  so the proxy over-counts and trips early, which is the safe direction.
- The 96h age trip is a **page, not an automatic flip**: the OSV/GHSA
  backlog is already ~58h old at measurement (M6,
  `10671-nvd-feed-measurements.md:94-101`), so the operator compares
  against the pre-enable baseline before acting (parent design,
  `10671-nvd-feed-transport-fallback.md:234-236`).
- Re-enable under 48h; record 6 hourly samples after enabling. The #10706
  bars (p95 <= 15s / watermark <= 60s) stay soak bars.

No pausing or serializing OSV/GHSA to make room (ruling decision 3).

## 6. Idempotency argument

1. Same input, same facts. Every NVD field is an identity translation with
   no contract change (parent design section 4,
   `10671-nvd-feed-transport-fallback.md:156-166`): feed records carry all
   `cpeMatch` ids, all seven `vulnStatus` states, and all CVSS families
   (M2), and 20/20 sampled records are deep-equal to the API (M8).
2. Same record, same keys. One record emits one `vulnerability.cve`, one
   `vulnerability.affected_product` per distinct
   `(criteria, matchCriteriaId)` identity with repeats folded, and one
   `vulnerability.reference` per distinct sanitized URL with tags unioned,
   so one stable key never carries two payload shapes inside a claim
   (`nvd_envelope.go:33-48` in the collector).
3. Same chunk, same scope. Same window bounds mint the same scope id on
   every run (`go/internal/vulnsource/nvd_window_scope.go:13-21`,
   mint at `:30-44`); a refusal, skip, or crash re-plans the same scope and
   the commit path fences it as a same-scope successor
   (`go/internal/coordinator/vulnerability/nvd_window_derivation.go:58-60`).
   The baseline lane must mint its chunk scopes with the same determinism
   (bounds in, same id out) so replays converge instead of superseding.
4. Same scope, one row set. State writes run inside the generation's own
   commit transaction (`go/internal/collector/generation_state.go:22-29`),
   so cursor/checkpoint state advances with its facts or not at all; the
   cursor CAS keeps the max on strictly-greater candidates only
   (`go/internal/storage/postgres/vulnerability/nvd_window_cursor.go:40-59`).
5. Proof required before QA: the R6-style convergence test (two baseline
   passes = one row set) from the ruling's proof gate (ruling:81), plus
   facts/record under satellite folding and dirty-log rows per committed
   fact on a disposable DB (ruling:79-83).

## NOT_CHECKED (must be measured before any QA baseline run)

- Per-fact byte sizes and folded facts per record under the satellite
  (upper-bound M5 only; proof script flags the byte side NOT_CHECKED).
- Dirty-log rows per committed NVD fact (unmeasured; M6,
  `10671-nvd-feed-measurements.md:99-101`).
- Per-year record counts (M3 measures sizes only).
- Whether the satellite runtime on the QA path carries the intended
  `nvd_feed_transport` value (M10 caveat,
  `10671-nvd-feed-measurements.md:198-201`).

## Implementation lanes (not this record)

- Satellite: baseline chunker over the year-set seed store (collector#100
  seed + manifest + guard), chunk-scope minting, checkpoint write.
- Core: baseline checkpoint store (section 3 shape), baseline commit policy
  (refuse any baseline generation that arms a window-cursor writer),
  trip-line counters and telemetry.
- Proof gates per the ruling: disposable-DB run, R6-style idempotency, the
  "how much of NVD held" count query inside a 15s statement timeout.
