# #11376 eco-anchored holder index: performance evidence

Refs eshu-hq/eshu#11376. The package-keyed holder read scanned the whole
affected-package partial index with the ecosystem as a residual heap
filter (measured 23.9 s vs the 10 s holder-read guard on QA). Migrations
347 (partial index on the folded ecosystem expression + scope/generation)
and 349 (extended statistics on the same expression) let the planner ride
an Index Cond instead. The shipped query text is unchanged and the
cve/advisory/member arms are untouched (H-3 no-build stands).

Performance Evidence: baseline shipped whole-index bitmap, 202 ms warm;
after eco-index bitmap, 101 ms warm (narrow `go` family 51 ms).
Backend/version: PostgreSQL 18.6, disposable proof database (same major
as QA). Input shape: throwaway shim, 960k target affected-package rows
plus 2.5M noise rows, many-scope layout with the queried family a
minority of the table. Row counts: 95,988 rows identical md5 between
shipped and indexed reads. Write cost: +26% on a narrow 20k-row batch
(single sample). Telemetry/log/status evidence: holder read timeouts
count on eshu_dp_supply_chain_impact_fanout_total{outcome="read_timeout"};
no new signals added. Why safe: additive CONCURRENTLY index (no writer
block), query text byte-identical, stats object affects only plan choice,
rollback is DROP INDEX CONCURRENTLY. Wall-clock numbers above are
lane-measured on the throwaway shim (since removed); the coordinator
independently verified the plan shape on disposable PostgreSQL 18 with
the committed live test (TestFindPackageHolderCandidatesEcoIndexPlanLive
green, 375/375 migrations applied) plus the Ifa contention suites green.

No-Observability-Change: no new metrics, spans, logs, status fields, or
dashboards; the existing holder-read timeout error path and its fan-out
counters are the only signals, unchanged.
