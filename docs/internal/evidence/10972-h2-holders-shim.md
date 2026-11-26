#10972 H2 theory shim: holder-read shape proof on the QA primary

Refs #10972. Date: 2026-11-26. Lane: `drive/10972-h2-shim` (executor leaf).
Binding ruling: `arbiter-10972-h2shape.md` (session-local arbiter record, not
committed to the repo; essentials: Lane-0 four-arm shim spec, bars warm
per-statement p95 <= 2s and max <= 5s with byte-equal holder sets,
pre-committed winner rule with no second ruling needed).
Skills active: `performance-rigor`, `postgres-rigor` (read-only), `golang-engineering`
(generator helper only, removed after use).

## Method (read-only on the QA primary)

- Host: the QA primary database pod (`eshu-postgresql-0`, QA namespace), pod-local `psql`.
  Every statement ran in `REPEATABLE READ, READ ONLY` with
  `SET LOCAL statement_timeout = '10s'` — the production guard — inside the file.
  No DDL, no writes; only `SELECT` and `EXPLAIN (ANALYZE, BUFFERS)`.
- Every statement text derived from the shipped constants
  (`FindSupplyChainImpactHoldersQuery`,
  `FindSupplyChainImpactHolderKeyRangeQuery` /
  `FindSupplyChainImpactHolderOpenKeyRangeQuery`, `HolderKeyRanges()`,
  `HolderMemberTokens()`), dumped by a temporary Go generator and fed to `psql`
  via `PREPARE` + `EXECUTE` (A0/A1 default plan mode, one `EXECUTE` per session,
  i.e. custom plan; A2 pinned `force_custom_plan` exactly like the shipped
  key-range read). No hand-copied SQL. Statements and census queries:
  [`10972-h2shim-raw/statements/`](10972-h2shim-raw/statements/).
- Warm protocol: discard run 1 as cold, then >=5 warm repeats per arm per set.
  Diagnostic-only raised timeouts (read-only, labeled): census scans
  (300s, then 1800s), per-id counts (60s), shipped-reference `SELECT`s (120s),
  1% member sample (600s). All warm arm repeats stayed at the 10s guard.
- Raw plans (one representative file per arm/set, 21 files):
  [`10972-h2shim-raw/`](10972-h2shim-raw/).

## Census (a): id sets

Table: ~13,037,546 rows (reltuples), 131 GB total / 26 GB heap,
`shared_buffers = 4GB`.

| set | ids (true `COUNT(*)`) |
|---|---|
| top-5 advisory | GHSA-7r86-cg39-jmmj 156,870; GHSA-23c5-xmqv-rm74 156,870; GHSA-3ppc-4f35-3m26 156,870; GHSA-g3hv-75wg-m249 156,868; GHSA-hxm2-r34f-qmc5 156,868 |
| worst CVE | CVE-2026-26996 156,870 (three-way tie at 156,870 with CVE-2026-27904, CVE-2026-27903); CVE-2025-27789 (#11430 id) now 102,394 |
| member-heavy | CVE-2026-42043: 48,221 direct + 48,216 member-arm rows |
| mixed 5-chunk | GHSA-7r86-cg39-jmmj (156,870/0), CVE-2026-42043 (48,221/48,216), CVE-2026-73569 (43,838/43,615), GHSA-4528-x5fx-h6hg (42,388 direct), CVE-2026-112943 (40,072/40,072). All true masses > 10k and all probe masses saturate at the 10,001 cap, so `planHolderReadStatements` plans each alone: 5 singleton statements. Chunk order above is the tested statement order |
| >1,024 B lookup | none found: direct max 19 B (cve) / 22 B (advisory); 1% `TABLESAMPLE` (~130k rows) zero member elements > 1,024 B. Full-table member unnest ground 30+ min on the throttled primary and was killed. Used synthetic `SYNTH-` + 1,094×`9` = 1,100 B, which routes to the fallback branch (shipped statement) |

Top advisory/CVE ids coincide in mass (same rows carry both). All hot ids carry
zero member-arm rows except the member-carrying CVE ids above.

## Arms (b): per-statement warm p95 / max, 10s guard

Bar (Decision 2): every warm statement p95 <= 2s AND max <= 5s, zero timeouts.
Warm n=5 statements per singleton set; A2 n=80 statements (5 reads x 16).

| arm / set | warm exec ms (discard-cold first) | p95 | max | timeouts | verdict |
|---|---|---|---|---|---|
| A0 adv5 (5-in-one) | 6466, 6365, 5273, 6157, 7679 (discard-cold 6339; first touch observed >10s timeout, raw overwritten by protocol reruns) | 7679 | 7679 | 0 | FAIL |
| A0 mixed (5-in-one) | 6/6 runs timeout; reference `SELECT` also exceeds 120s diagnostic timeout | — | >120s | 6 | FAIL |
| A1 adv1..adv5 | ~1222–1321 each (colds 1579–1779) | 1276–1321 | 1276–1321 | 0 | PASS |
| A1 cve (156,870) | 1237–1278 (cold 2817) | 1278 | 1278 | 0 | PASS |
| A1 mix4 (42,388 direct-only) | 414–440 (cold 5761) | 440 | 440 | 0 | PASS |
| A1 long (synthetic, fallback) | ~1 | 1 | 6 | 0 | PASS |
| A1 member (48k+48k) | 5/5 warm timeouts (cold also timed out, discarded per protocol) | — | >10s | 5 | FAIL |
| A1 mix3 (44k+44k) | timeout, timeout, 3680, 1860, 1803 | 3680 | 3680 | 2 | FAIL |
| A1 mix5 (40k+40k) | timeout x3, 6448, 1691 | 6448 | 6448 | 3 | FAIL |
| A1 4-parallel adv1–4 (waves 2–6, n=20) | 1418–1818 / 1418–1505 / 3193–3492 / 3088–3163 / 2348–2367 | 3452 | 3492 | 0 | FAIL (p95) |
| A2 adv1..adv5 union-of-16 | per-stmt 145–362 | 215–298 | 231–362 | 0 | PASS |
| A2 cve union-of-16 | per-stmt 147–287 | 271 | 287 | 0 | PASS |
| A2 member union-of-16 | per-stmt 99–167, phase ~1.7–2.0s | 156 | 167 | 0 | PASS |
| A2 mix3/mix4/mix5 union-of-16 | per-stmt 89–157 | 132–141 | 147–157 | 0 | PASS |
| A2 4-parallel adv1–4 (waves 2–6, n=320) | per-stmt p95 323 | 323 | 581 | 0 | PASS |
| A2 extra wall probe (adv1, outside matrix) | stmts 10–16 slowed to 2.2–6.6s under primary load, wall 36.9s vs typical phase 2.6–4.9s | — | 6654 | 0 | observed variance, reported |

A2 phase totals (sum of 16 warm statements): advisory sets ~2.6–4.5s,
cve ~2.7–3.9s, member/mix ~1.5–2.0s. Snapshot hold = phase wall (~3–5s typical).

## Per-statement shape (Decision 2 report fields)

- A0 warm: ~6.5s; top buffers `hit=17709 read=401887`; bitmap heap scan,
  `Heap Blocks: exact=375875`, lossy 0, recheck 0, temp 0. Custom plan
  (PREPARE+EXECUTE, default mode).
- A1 adv warm: ~1.22s; top buffers `hit=156726`; bitmap heap
  `exact=133830`, lossy 0, recheck 0, temp 0. Custom plan. The final
  scope hash join over the 17k-row `ingestion_scopes` seq scan dominates
  (~1.2s of the statement).
- A1 member/mix3/mix5: no plan rows — every attempt ends in 57014 at 10s,
  warm or cold. Members GIN bitmap arm over ~40–48k rows never completes.
- A2 warm: 99–362ms per statement; ~21–28k buffers; `Index Cond` carries the
  id plus BOTH `fact_id` bounds plus the token arm (`&& '{b:id,...}'`);
  no bitmap-heap lines, lossy 0, recheck 0, temp 0. Pinned
  `force_custom_plan`, one `REPEATABLE READ` snapshot per 16-statement read.
- Generic-plan diagnostic (`force_generic_plan`, same texts): A0 adv5, A1 adv1,
  A1 cve ALL time out at 10s. Generic shape = plain index scans with
  whole-table estimates (143k/140k/130k rows) — per-row heap fetch death.
  Production must keep custom plans on this path (A2 already pins it).

## Exactness

Holder sets byte-equal after `sort -u` (own scope `h2-shim-own-scope`,
same for all arms; live primary, no drift observed in these ids). Counts
below are session-asserted: no holder-list artifacts are committed, only
plans; Lane-1b re-proves exactness independently:

| comparison | holders | result |
|---|---|---|
| A0 adv5 == A1 adv1..5 (union == each singleton) | 234 | MATCH |
| A1 == A2 per id (adv1–5, cve, mix4) | 234 / 254 (mix4) | MATCH |
| A2 == shipped `SELECT` at 120s diagnostic (member, mix3, mix5) | 244 / 297 / 348 | MATCH |
| A1 long == A2 long (fallback == shipped) | 0 | MATCH |
| A0 mixed == shipped | — | unattainable (>120s); covered transitively — each member's A2 == its shipped read |

## A3 (c): covering index — NOT_RUN

No primary-identical clone exists in-session (local lab DB is 418 MB vs the
131 GB / 13M-row primary; restoring the primary would take hours and load the
throttled primary). DDL on the QA primary is barred during diagnosis. An index
verdict on lab-shaped data would not transfer, so A3 is recorded NOT_RUN
rather than measured on the wrong substrate.

## Decision 3: selected branch — A2 re-route (A1 fails, A2 passes, A3 unavailable)

- A1 does NOT pass: member-carrying singletons time out isolated (5/5, 2–3
  timeouts + 3.7–6.4s stragglers on mix3/mix5), and 4-parallel direct-only
  singletons breach p95 (3,452ms > 2s; max 3,492ms passes). Even dropping the
  two slowest waves as noise, p95 is 2,367ms — still over the bar.
- A2 passes per-statement isolated (warm max 362ms over 800 statements, zero
  timeouts, all 10 sets incl. member-heavy) and concurrent (p95 323ms,
  max 581ms over 320 statements, zero timeouts). Exactness holds everywhere
  it can be checked. A3 is not clearly better — it is unmeasured by design
  constraint.
- Therefore: re-route hot singletons to the key-range read above a
  true-mass gate (no timeout/serialization change, cold packing untouched).
  Gate data for the implementation lane: A1 passes at 156,870 direct-only
  rows (~1.3s) and at 42,388 direct-only rows (~0.4s); it fails with
  ~40k member-arm rows even warm. Halving the largest passing direct-only
  mass gives ~78k direct rows — but the failing dimension is member-arm
  mass, so the gate must key on the member-arm/probe signal, not direct
  mass alone (the probe already saturates the member arm above 100 rows).
  Lane 1 needs: failing regression first (routing pin), the F4 comment fix,
  and telemetry proof that per-statement durations land under the existing
  `holders` / `holders_key_range` kinds.

## Limits (NOT_CHECKED)

- A0 adv5 first-touch cold raw plan (observed >10s timeout; file overwritten
  by the protocol reruns; retained discard-cold is run1 6,339ms).
- Full-table member-length census (30m+ grind killed; 1% sample stands in).
- A3 covering index (NOT_RUN, see above). One extra A2 wall read showed
  primary-load variance (statements to 6.6s); the pre-committed matrix is the
  verdict basis, this is reported variance, not verdict.
- Replica numbers were not used; all timings are QA-primary `EXPLAIN ANALYZE`.

## Evidence markers

- Performance Evidence: baseline = shipped 5-in-one / singleton holder reads
  on the QA primary at true hot mass (A0 adv5 warm p95 7.7s; member-carrying
  singletons 10s+ timeouts warm; concurrent p95 3.5s); after = A2
  union-of-16 per hot singleton (warm per-statement p95 0.13–0.30s isolated,
  0.32s at 4-parallel, zero timeouts over 1,120 statements, byte-equal holder
  sets). Backend: QA primary `eshu-postgresql-0`, PG 18.x, 13M-row supports
  table, 4 GB shared buffers. The change is safe to select because A2 is the
  already-shipped, exactness-proven key-range read under its pinned custom
  plan — this shim selects the lane; it implements nothing.
- Observability Evidence: no code changed, so no new signals. Lane 1 must
  prove per-statement durations under the existing `holders` /
  `holders_key_range` kinds plus `split_chunks` / `hot_ids` firing on hot
  chunks.
