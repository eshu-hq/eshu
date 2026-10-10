# #7164 documentation related reads: active generation proof

## Root-Cause Evidence

The production SQL for documentation findings, target-related facts, and semantic
observations/code hints read retained `fact_records` without binding a default
read to `ingestion_scopes.active_generation_id`. A default page could therefore
mix active and superseded facts or report facts from a scope whose latest
attempt failed. An explicit `generation_id` remains a historical read.

The disposable PostgreSQL regression seeds one scope with superseded and active
generations, one scope with no active generation, and all four related fact
kinds. It was RED before the fix: default pages returned retained rows and the
no-active scope returned failed-generation rows. It is GREEN after the fix.
The same test compares production SQL row IDs with an independent active-scope
join for unscoped pages and a generation-filtered reference for scope pages.
It also checks explicit historical reads, source-only coverage, lifecycle
labels, truth freshness, and grant-safe empty states.

## Performance Evidence

Read-only QA PostgreSQL 18 observations on 2026-09-26 found 57,909 claim
candidate rows across 368 scopes and 139,666 entity mentions across 620
scopes. One busy scope held 10,705 target rows across 20 generations: 568
active and 10,137 superseded. On a 51-row scoped target read, the retained
statement took 41.869 ms with 1,046 buffer hits and 1,436 reads; the active
join took 1.762 ms with 142 hits and no reads. In reverse order, the active
join took 1.177 ms and the retained statement 12.195 ms. The active result
excluded the 10,137 superseded rows, with zero active rows missing from the
retained set. These are single EXPLAIN ANALYZE samples, not a p95 claim.

For an unscoped target page, a per-candidate active probe took 14,693.743 ms
and 655,659 buffer hits, slower than the retained statement's 10,966.093 ms.
The active scope join returned the same 11 active rows in 216.337 ms with
12,890 hits and 533 reads. Thus the implementation uses the join for anchored
or otherwise filtered unscoped pages. The unfiltered findings page keeps the
existing bounded page probe shape. This plan comparison is not a deployed
before/after measurement: the QA environment was read-only and the new binary was not
installed there.

The endpoint sweep named `post-518f9dd` is context, not a comparable
before/after baseline: documentation facts API cold was 24.2228 s and warm
p95 was 0.575 s; MCP list_documentation_facts cold was 30.0902 s and warm
p95 was 25.6142 s with one timeout. These routes were fixed under #7128.
The QA environment contained no documentation findings or semantic rows, so it cannot
establish a deployed p95 for those routes. The requested cold and warm p95
below one second remains unproved until deployment and a matching sweep.

## Observability Evidence

The handlers keep their existing query spans and PostgreSQL operation spans.
The response now exposes `generation_binding` and empty-page `states`, and the
truth envelope reports stale or unavailable freshness for explicit historical
or no-active reads. No new metric or span attribute was added; the changed
behavior can be identified from the route, query span, returned binding, and
truth freshness without disclosing an ungranted scope.
