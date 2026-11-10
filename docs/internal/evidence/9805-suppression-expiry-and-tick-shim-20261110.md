# #9805 suppression-expiry proof and slow-tick theory shim (2026-11-10)

Refs #9805. Two arbiter-ordered items: the unconditional time-driven
suppression-expiry test (criterion 4), and the prove-theory-first shim for
the slow `build_active_facts` ticks on QA (criterion 5). No production code
changed: the expiry theory was disproven by test, and the shim verdict gates
the tick fix design.

## Suppression expiry: no resweep fix needed (theory disproven)

Theory: a suppression whose `expires_at` passes with no fact write creates
no dirty-log row, so the keyed table goes stale until the audit repairs it.

Disproof, from source and by test:

- The winner select stores `suppression_state` and `suppression_expires_at`
  verbatim from payloads and has no time predicate:
  `supplyChainImpactWinnerBodySQL` in
  `go/internal/storage/postgres/supply_chain_impact_canonical_winners_store.go`
  (state at line 208, expiry at lines 209-219), the operator predicate at
  line 122, and no `NOW()` anywhere in the select, rebuild, or keyed apply
  SQL. A fresh recompute after an expiry therefore produces byte-identical
  rows, so there is nothing for a tick to repair.
- Readers derive the effective (expired) state at read time from the stored
  pair: `supplyChainImpactEffectiveDecisionStateSQL` over the winners table
  in `go/internal/query/supply/chain/impact/findings_queries.go:164-168`,
  and `advisoryGroupWinnerEffectiveStateSQL` in
  `go/internal/storage/postgres/supply_chain_impact_advisory_group_winners.go:56-66`.

Proof: subtest `time-driven suppression expiry passes with no fact write`
in `supply_chain_impact_winners_keyed_parity_live_test.go`. It seeds an
operator override expiring 3s out (database clock), ticks, lets the expiry
pass with no write, and asserts dirty depth 0, a zero-write tick, parity
with a fresh recompute, and a stored row whose `suppression_expires_at <=
now()`. GREEN on current code (8.90s full parity suite, disposable
Postgres 18). Sensitivity: with a deliberate mutation making the operator
predicate time-dependent (`expires_at > NOW()`), the same subtest goes RED
at parity (incremental `accepted_risk` + expiry vs fresh `active` + null).
The rejected fix (union time-expired keys into every tick) would recompute
identical rows and add `build_active_facts` volume — the phase already
over budget below.

## Slow-tick shim: candidate volume times cold I/O (QA primary, read-only)

Method: one `BEGIN READ ONLY` session on the QA environment's primary
Postgres pod, `statement_timeout=30s`, count-only mirror of the lateral probe
in `buildWinnersKeyedActiveFactsSQL`
(`supply_chain_impact_winners_keyed_sql.go:115-146`), triples taken from
the winners table so the probe is the session's first `fact_records`
touch. Script: `/tmp/9805-shim.sql` on the agent host; exits 0.

Measured (2026-11-10 ~11:00Z):

| Probe | Candidate rows | Probe buffers (hit+read) | Buffers/row | I/O read share |
| --- | ---: | ---: | ---: | ---: |
| Top-25 triples | 43,081 | 15,072+34,245 | 1.144 | 267ms of 512ms |
| Mid triples (offset 1000) | 7,902 | 2,286+11,832 | 1.79 | 2,622ms of 2,804ms |

- `fact_records`: 129.0M live, 1.94M dead (1.50%), last autovacuum
  2026-11-10T02:49Z. Dead-entry waste (12.2-46 buffers/row pre-#10119)
  stays disproven; buffers/row is at post-vacuum health.
- The read fraction (69% hot, 84% mid) did **not** drop on an immediate
  rerun (34,174 reads again): probe pages churn out of shared buffers
  between ticks, so slow ticks pay cold random I/O per fetched row.
- Fetched vs kept: the top-25 triples fetch 43,081 candidate rows for
  1,275 distinct keys (33.8 fetched per key; the probe keeps a whole
  triple per dirty key by design). Winners per triple across 15,338
  triples: p50=2, p95=18, p99=51, max=145 (71,023 winners).
- Dirty-log depth at shim time was 10,482 rows, so real ticks regularly
  face the volume the synthetic 1%-churn scale run (358.9ms at 1x) did
  not: ~865 keys p95 per tick on QA can mean hundreds of thousands of
  candidate heap fetches, mostly cold.

Verdict per the arbiter's pre-named decision rule: volume branch. Bound
the candidate rows (distinct triples) one tick fetches — chunked consume
in `supply_chain_impact_winners_keyed.go`/`_keyed_sql.go` driven from
`winners_keyed_maintainer.go` — while never stamping a fresh watermark
with consumed-but-unapplied keys outstanding. That fix is NOT in this
change; it needs its own failing scale/plan test first (bounded
searches/buffers and tick bound at QA-like hot-triple volume). Neither
branch supports an index/probe rewrite, lease renewal, or serialization.

NOT_CHECKED: per-tick Prometheus correlation of phase seconds vs keys
(existing samples only); apply-phase split (second phase, p95 4.04s);
host CPU throttling; QA re-measurement over a >=3h window (needs the
chunking fix on a rolled build first).

Performance Evidence: this change adds one live subtest and this note;
no runtime SQL, index, batch, or concurrency change. The QA numbers
above are the before-measurement for the chunking fix.

No-Observability-Change: no new metric, span, log, or knob. The tick
phase histogram (`build_active_facts`) remains the operator signal.
