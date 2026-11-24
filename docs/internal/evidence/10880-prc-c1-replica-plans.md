# QA replica plan measurements for PR-C C1 (T1, T2, T4 read parts), recorded 2026-11-24

Source: read-only scan agent, the QA replica only (PG 18.6, hot_standby_feedback off, max_standby_streaming_delay 30s), 2026-11-24 08:54Z to ~09:03Z. The raw plans and the exact SQL were kept outside the repository and are not committed; the numbers below are copied from them. Statements rendered from branch `drive/10880-prc-c1` at `8898e4657c`. No primary connection, no writes. Single run per statement, cache state not controlled, times include client overhead. Not a verdict on PR-C.

QA schema on the replica at the time: supports table 10.69M est rows, 96 GB; `fact_records` 96.9M est rows, 551 GB. Supports has 8 indexes (pkey on fact_id, scope_generation, repository, finding_id, generation, image_ref partial, operator partial, precise_key_ordered partial). It has NO btree on package_id, cve_id, advisory_id or subject_digest: migrations 345 and 346 (B2c) and 350/351 (C1) are not on QA yet. Parameters used: scope S = 14,123 supports rows (largest found; no 20k scope; one active generation); package axios est 2.02M rows (exact count timed out); keyed tuple (fast-xml-parser, one repository, CVE-2026-33349) 1,616 rows. The 83,642-row keyed tuple was NOT found.

## Results (exec ms; r1 cold-ish, r2 warm; supports vs legacy twin)
- T4 stored read (S, 14,123 rows): supports 11,608 / 878; legacy 807 / 798. Supports shape: scope_generation index plus 14,123 `fact_records_pkey` probes (the twin join), 19,351 shared reads cold. Warm it is within 10% of legacy; cold it is about 14 times slower, from the random reads.
- T4 recovery reads, 2-leg (61 rows): cveids 1,206 / 541 vs 351 / 350; groupids 372 / 362 vs 347 / 345. Index scans only. The 2-leg still includes a facts leg, so this is not supports-only. The 3-leg with the retracted-keys table was not run (migration 351 absent).
- T2 public id: supports 191 / 23 (uses `finding_id` idx). Legacy plain plan only.
- T2 image: 0.70 / 0.67 ms (0 rows, synthetic image; supports has zero non-empty image_ref rows).
- T2 granted: supports 1,805 / 548 (BitmapOr repository_idx plus scope_generation).
- T2 ungranted (CVE plus package): supports plain plan only: scope-driven probes over every active scope, est cost 7,629 (no cve or package btree on supports). Legacy: r1 cancelled at 24,049 ms; r2 19,545 ms warm (4.28M shared hits). So the legacy statement the product runs today is already near the replica's 24 s limit; supports without a package index has the same scope-driven shape.
- T1 keyed scope idents (S): supports 2,722 / 686; legacy 7,432 / 674. Index scans both.
- T1 keyed active_facts (IDENT, 1,616 legacy rows): legacy 1,289 / 59 using `fact_records` winners_keyed_idx. Supports: plain plan only: Parallel Seq Scan on supports, est cost 3,047,857 (no supports index on package/repository/digest/cve/advisory). This is the proven index gap for set g.
- T1 operator facts: 0.13 / 0.21 vs 0.11 / 0.10 (0 rows).
- T1 winners full select: plain plans only. Est cost 7,614.81 supports vs 7,619.14 legacy (estimates, both nested-loop scope probes).
- Seq scans executed on supports or `fact_records`: none.
- Errors: exact `count(*)` on supports and a whole-table package group-by hit the 24 s limit; the legacy ungranted r1 was cancelled at 24,049 ms (r2 completed in 19.5 s).

## What this changes (coordinator reading, to be confirmed by the arbiter at the C1 review)
1. Index set g is PROVEN needed for the keyed active_facts statement (supports seq scan, est cost 3.05M vs a 1,289 ms legacy index lookup). It also depends on whether 345 (package_id) alone narrows a 2M-row package enough: axios-size packages would still need a composite key. Re-measure on QA after the roll that carries 345, 346, with candidate composite btrees, before fixing the set.
2. The cold stored read through the fact_records twin join costs about 11.6 s for a 14k-row scope; rows whose `stable_fact_key` was never filled (all rows written before 350, and unchanged rows because the fill is outside the write-on-change compare) keep paying it on a cold cache. Warm cost is near legacy. After C2 new rows carry the key. This needs a decision at the C1 review: accept as a one-time cold cost per big scope, or add a one-shot key fill.
3. The ungranted lookups (CVE plus package) have no index on supports today; 345 adds package_id. The legacy statement is already slow (19.5 s warm); the measurement after the roll is what decides.
4. The 3-leg recovery and the stored read with the key column are untested on real data: they need migrations 350 and 351, which only exist after the C1 roll, so they stay owed to D3a.
5. An index build on the 96 GB table adds WAL and I/O, and its build time on the QA table is not measured: it must not land inside W1b. Index set g belongs with C1 (applied at the D3a roll), after W1b.

NOT_CHECKED: the 20,000-row scope, the 83,642-row keyed case, exact row counts, cold versus warm under controlled cache, any write path, the primary.
