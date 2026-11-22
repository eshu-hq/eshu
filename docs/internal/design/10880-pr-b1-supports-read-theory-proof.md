# PR-B1 theory proof: a supports-table read for grant-scoped impact reads (#10880)

Refs #10880. Companion to
[the PR-B evidence record](10880-pr-b-reader-moves-and-scoped-fork.md) and
[the PR-B0 execution record](10880-pr-b0-execution-record.md). Governing rule:
the root AGENTS.md "Prove-The-Theory-First" section. This record states the
decision, the threshold, and the deadline before any measurement, and holds no
production code.

## Decision, threshold, deadline (stated before measuring)

- **Decision.** Whether PR-B1 builds a supports table (one row per support, with
  the filter columns, the payload, and the active-generation join kept) so that
  grant-scoped list, aggregate, and explain reads can leave the fact CTE
  (arbiter ruling: option B). The theory under test: a read over a typed supports
  table, with the grant applied to the support row and the same pick as the legacy
  CTE, is no worse than the legacy CTE on buffers and latency at the same row
  counts, and returns the same findings.
- **Threshold.** For each grant shape (scope grant, repository grant, mixed
  grant), at the same row counts:
  1. the supports read returns the same finding set and the same chosen
     support (by fact id) as the legacy read;
  2. its buffers (shared hit plus read) are at most the legacy read's, and its
     execution time is at most the legacy read's, measured as the median of seven
     warm runs; and
  3. it stays inside the capability matrix p95 (1000 ms local, 1500 ms
     production) at the largest row count measured.
  The unscoped winners route must not regress: its plan and timing with the
  supports table present equal its plan and timing without it (the table adds no
  index or join to that read).
- **Deadline.** 90 minutes wall clock from 05:58Z, so 07:28Z. A failed attempt does
  not restart the clock. If the deadline passes with a threshold unproved, this
  record says which part is unproved and PR-B1 does not build on it.
- **Out of scope here.** The same-transaction maintainer, the backfill, the
  contract version, the concurrency proof, and drift telemetry are PR-B1 work
  that follows a passing proof; none is measured here.
