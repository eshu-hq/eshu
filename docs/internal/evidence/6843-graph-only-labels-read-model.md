# #6843: unscoped /infra/resources from Postgres fact truth

## Performance Evidence: graph-only labels moved off whole-label Cypher scans

Baseline (issue record, read-API latency gate at 150k graph nodes per
infra label): unscoped `GET /api/v0/infra/resources/count` and
`/infra/resources/inventory` ran one whole-label Cypher scan per
graph-only label (`CloudResource`, `TerraformStateResource`), with an
8.28s cold first execution and one gate run ending in a 504. The scan
cost is linear in label size with no usable anchor: the only
selective predicate (`evidence_source`) has no NornicDB index the
planner takes on these shapes.

After (this change): the same gate at the same corpus (800 scopes,
150k nodes per infra label across 8 labels, 150k IaC facts plus 150k
graph-only facts per label, NornicDB v1.3.3, Postgres 18-alpine, 20
counted iterations per route) reports `GET
/api/v0/infra/resources/count` p95 707ms (3.0 calls, 221,024 shared
buffers, 44 rows) and `GET /api/v0/infra/resources/inventory` p95
680ms (3.0 calls, 221,022 shared buffers, 8 rows), inside the 2s
latency ceiling and the committed work budgets, with gate exit 0 and
all 65 exercised routes within budget. The gate binary bound to
c922fbf45; the only later change is comment-only (split-artifact
comments outside function bodies, callsite digests re-verified green),
so the numbers stand for this tree.

Backend/version and input shape for the mechanism proof: the fact
CTEs (`admission`, `state`, `ec2_instance`, `bindings`) pre-aggregate
with DISTINCT ON over current-generation, non-tombstone facts
(partitioned by row identity, ordered by source-order rank) and join
to the scope/generation fence, replacing the earlier quadratic
LATERAL-join shape found by EXPLAIN during development. No new index
was added: unscoped reads touch every label, `infra_resource_entities`
carries no label index, and an index cannot help a read that selects
the whole table; the btrim-expression sorts are inherent to
normalizing payload values at read time, and per index doctrine a
covering index follows only with its own EXPLAIN. Terminal counts:
gate exit 0; the live parity battery asserts bucket-identical counts,
dimensions, and filters between the fact path and the replaced
per-label scans across base, stale-scalar, adversarial, no-match, and
cross-kind rows; scoped, pre-ready, readiness-failed, table-failed,
and both-legs-failed paths are pinned by fake-leg unit tests. The
change is safe because selection semantics are unchanged (same scope
filter, same validity predicates, same bucket mapping through the
one shared merge helper), graph hydration is untouched, and the P2-2
fallback reports source graph whenever it serves.

At 1M nodes/label the same gate breaches (count p95 5.24s, inventory
504 on the mixed-writer graph leg; ~6.7x buffers for 6.67x rows on
both legs): unscoped aggregates are full scans at this shape, so 1M
needs persisted per-label rollups as follow-up work, not claimed here.

## Observability Evidence: existing instruments cover the new path

No new telemetry was added. Reads keep reporting through
`eshu_dp_infra_inventory_reads_total` (same route/source labels), the
truth envelope names the serving store (`read_model`, `hybrid`,
`graph`), handler spans are unchanged, and the gate's per-route
Postgres work meter distinguishes the paths (221k buffers table leg
versus the former multi-second graph scans). An operator pages the
route's latency and errors exactly as before; a fallback event shows
up as a `graph`-sourced read point alongside the attempted
read-model point (known P3-W4 wart: the fallback double-emits the
counter; rare by construction since it fires only on table-leg
failure) plus the unchanged error counters if both legs fail.
