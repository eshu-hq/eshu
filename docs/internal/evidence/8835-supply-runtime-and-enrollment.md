# Supply runtime filter proof and enrollment (#8835)

The fresh-store enrollment proof exposed missing fixture bootstrap, stale
parameter bindings, and a normalized runtime-filter plan above its 500 ms
budget. The fix preserves the ten original enrollment files and four roots.
It adds disjoint normalized fallback branches and five partial indexes while
retaining raw indexed branches, scope/grants, active generations, tombstones,
provenance, repository precedence and normalization semantics.

## Performance Evidence:

The theory replay used Go 1.26.10 with the race detector, PostgreSQL 18.6,
100,000 facts per runtime dimension, JIT enabled, and unchanged EXPLAIN ANALYZE
with BUFFERS and timing. Each invocation used a fresh disposable database.
The unchanged limits are 500 ms per plan and 120 seconds for the 300,000-row
fixture write. These are fixture budgets, not API or end-to-end capacity claims.

Two counterbalanced original-corpus pairs failed at baseline: legacy combined
505.689 ms in the first pair and winners combined 509.985 ms in the second.
Both scratch candidate runs passed all four roots. Candidate legacy combined
plans took 1.651 and 1.615 ms; winners 1.427 and 1.340 ms; aggregate 1.340 and
1.300 ms. The paired writes remained below 120 seconds. Two noisy samples do
not establish negligible write overhead or an end-to-end speedup.

A separate 100,000-row padded-entity probe returned exactly one row for a hit
and zero for a miss. Candidate execution was 215.121 and 207.802 ms, versus
420.880 and 403.193 ms at baseline. The entity partial index scanned 100,002
qualifying entries: its work is proportional to the padded subset. It is not
a selective normalized-value lookup. Candidate writes were 21.949 and 21.871
seconds, versus 20.974 and 20.889 seconds at baseline, about 4.7% higher in
those two local samples. This cost is disclosed rather than called free.

The production plan test keeps its original canonical seed and cases. Its
added padded phase updates the existing identity rows only after those cases;
that phase has different physical storage from the fresh scratch seed and is
not a paired timing comparison with it.

## Integrated production replay

At `203f383f3cde7a72b6bcb512dd1ae3ce23a8aae9` on main `093143b350`,
all four original roots passed with Go 1.26.10, race detection and no selected
skips/failures on a fresh pinned PostgreSQL 18.6 database. The unchanged
500 ms plan and 120 second write assertions include all original canonical
cases and the added padded phase. Sixteen measured plans passed: canonical
legacy/winners/aggregate combined execution was 1.546/1.292/1.279 ms;
padded hit/miss was 357.867/333.151 ms with exactly one/zero result rows.
The 300,000-row write took 21.004985909 seconds. These are declared fixture
observations; the update-based padded phase is not comparable to fresh seed
samples above. Process wall was 40.496 seconds; load1 start/max/end was
4.42/4.88/4.24. Cleanup verified the database absent.

## Exactness

A scratch differential derived baseline SQL from the shipped source and built
both query variants with the race detector. Both read the same persisted
fixture at a fixed read-as-of time. All 17 complete output cases matched,
including legacy and winners arrays, aggregate counts, inventory and
explanation. The unfiltered CVE case returned nine ordered findings in each
list and count nine. Cases include grants, current/stale generations,
tombstones, ambiguity, provenance, raw/padded selectors and misses.

Baseline runtime SQL SHA256:
`ae8735590c8bb06d24ff81456232cbe5730a8f19cfabc309cde03bd7f2bb150a`.
Candidate runtime SQL SHA256:
`f0edaf4a9a92b8f657ce51cf6f871c17555e0f6dcbc5f17a950cdb238b1cca56`.
The differential is evidence for its declared fixture, not a universal
semantic proof or a copied SQL golden standard.

## Migration and execution proof

The five unshipped index migrations were renumbered to 211–215 during
integration, preserving shipped migration 210 byte for byte. Each new index
has its own immutable concurrent-build migration. The bootstrap
mirror, definition order, DDL parity and migration-manifest contracts passed
focused verification. The index contract first failed for all five missing
migrations, then passed. A cold compilation timeout was not counted as RED.

The dedicated restart proof passed with Go 1.26.10 and race detection on
PostgreSQL 18.6. It uses the real tracked bootstrap path and an explicit
disposable administrative DSN. On 30,000 populated facts, five builds took
326.916 ms while 40 insert/normalize/tombstone/delete cycles completed; the
maximum writer cycle was 14.458 ms. First application, stable repeat/restart,
invalid-build recovery and rollback/reapplication passed. Process wall was
7.104 seconds; load1 start/max/end was 3.61/3.64/3.64, and no proof database
survived. These are local fixture observations, not deployment capacity.

The new root joins the existing `disposable_storage` runner group with its
explicit admin DSN and disposable opt-in. It adds no legacy scheduled
exemption. [Compact proof receipt](8835-runtime-proof-summary.json) binds the
checkpoint, source hashes, binaries, exactness cases and lifecycle command.

No-Observability-Change: no metric, span, log or status schema changes. Existing
PostgreSQL EXPLAIN ANALYZE/BUFFERS remains the query diagnostic surface; the
live proof records execution, rows, buffers, write and migration observations.

NOT_CHECKED: final enrollment promotion gates, independent final review and
hosted acceptance. Helper mappings and generated metadata receive their own
focused seeded proof before publication.
The scratch theory and output differential do not substitute for these checks.
