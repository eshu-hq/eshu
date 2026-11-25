# #10032 round-1 go.mod filter seed: performance evidence

Refs eshu-hq/eshu#10032 (arbiter-approved shape A).

The round-1 evidence filter dropped every advisory branch for a git scope
holding only go.mod files, so the holder pass ran a full no-op. Seeding
canonical Go identities from the scope's own manifest dependencies lets the
package_id and purl probes fire with anchor Index Conds.

Performance Evidence: baseline round-1 filter derived only repository IDs
(0 advisory rows loaded; probes skipped by RunWhen); after the fix the
fixture scope derives 78 modules, and on live QA PostgreSQL 18 the
package_id key probe returns 370 advisory rows and the purl probe 759 rows,
both via anchor Index Cond (no heap-filter walk). Input shape:
git-repository-scope:repository:r_efdfb126 at generation dcbbaa0f (2
go.mod files). Terminal row counts: new unit tests 3/3 pass;
supplychain/core suite green; Ifa saturation 5/5 green. Telemetry/log/
status evidence: no new signals; the pass reports through the existing
holder-read timeout and fanout counters. Why safe: additive, go-ecosystem
only, fail-closed on unparseable names, bounded by existing per-branch
chunking (the package_id/purl arms carry no per-branch bound, unlike the CVE/alias arms at 120; the fixture's 78 values ride whole in a single ANY probe); findings still require version/range
match, so over-seeding cannot invent truth. The wall-clock numbers above
were measured read-only on QA by the implementing lane and re-verified in
the plan shape by the coordinator's live EXPLAIN review.

No-Observability-Change: no new metrics, spans, logs, status fields, or
dashboards; the existing holder fan-out timeout and dead-letter counters
are the only signals, unchanged.
