# Cloud resource page proof (#5563)

## Contract

`GET /api/v0/cloud/resources` has a 2-second interactive handler SLO. A request
selects at most `limit+1` current, authorized identities from
`graph_node_owner`, then hydrates at most `limit` exact `CloudResource.uid`
values from the graph. Authorization is part of the correlated active-fact
probe and therefore runs before the outer page limit.

API and MCP startup first run a one-time compatibility backfill for deployments
that already contain CloudResource graph nodes. The backfill walks the unique
`CloudResource.uid` key in 500-row pages, preserves each graph row in
`winning_row`, and seeds it with a year-1 order key. A normal reducer key always
sorts later, so a concurrent reducer write wins without a read-side snapshot
overwriting newer graph truth. The route is not mounted until the backfill has
either completed or been skipped through its durable completion marker.

The production variant family contains 64 SQL shapes: all 32 combinations of
provider, resource type, region, account, and cursor for both all-scope and
scoped access. `queryplan_production_variants_test.go` freezes the complete
family digest. `cloud_resource_list_store_live_test.go` executes every shape
against 20,000 resources split between allowed and denied scopes, compares the
exact identities with an independent Go reference, includes a no-match scoped
adversarial run, rejects an owner-ledger sequential scan, enforces the 2-second
SLO, and walks all 20,000 rows across page boundaries to prove strict ordering
with no gaps or duplicates.

## Theory results

The retained production symptom remains the user-facing baseline: about 17,100
resources returned HTTP 500 after 40.5 seconds. A later 112-resource control at
4.9 ms did not supersede it.

The theory gate used a 20,000-resource disposable corpus for same-data query
comparisons. The pinned NornicDB PROFILE response does not expose a reliable
db-hit count, so the table reports that field as unavailable instead of
inventing one.

| Shape | First execution | Warm execution | Backend work | Result |
| --- | ---: | ---: | --- | --- |
| Current graph label scan and order | 438 ms | about 0.86 ms | NornicDB db hits unavailable | Correct set; filtered order was not semantic in 7 variants |
| Candidate graph composite index | 399 ms | about 0.86 ms | NornicDB db hits unavailable | Rejected: no material win; equality reads could return zero rows on the pinned backend |
| Owner-ledger page, all-scope | 0.879 ms | 0.257 ms | cold: 238 shared hits + 44 reads; warm: 282 hits + 0 reads; 51 fact PK probes | Accepted |
| Owner-ledger page, scoped (all rows granted) | 0.862 ms | 0.200 ms | cold: 238 shared hits + 44 reads; warm: 282 hits + 0 reads; grant evaluated in every bounded fact probe | Accepted |

The exact checked-in 64-variant live gate completed with a slowest page of
63.504 ms, well below the 2-second SLO. That maximum includes an adversarial
scoped grant matching no owner row; every plan still used one of the four
`graph_node_owner_cloud_resource_*` ordered indexes.

The four exact concurrent-index bootstrap definitions also applied and
reapplied against a populated 20,000-row ledger. Catalog readback reported all
four indexes ready and valid, and the isolated proof schema was absent after
test cleanup.

The upgrade backfill seeded 20,000 existing graph-owner rows into an isolated
Postgres 18 schema in 254.485-497.976 ms across two runs on the shared local
host, including a post-rebase rerun while the machine was under load. The same
live test preloaded a real reducer owner for an overlapping uid and then applied
the minimum-key backfill row; the real key and fact id remained unchanged. The
final ledger contained all 20,001 expected rows, the completion marker read
back as true, and cleanup removed the isolated schema.

The exact graph enumeration also ran against an isolated NornicDB v1.1.11 image
at the repository-pinned digest. It backfilled 1,201 real `CloudResource` nodes
in 98.681 ms over three pages (500/500/201), with the expected continuation
cursor at each boundary and no missing, duplicate, or reordered uid. The test
removed its synthetic nodes, and the disposable container was removed after
the run. This is a one-time startup cost; the steady-state request remains the
indexed `limit+1` selection and bounded graph hydration measured above.

## Exactness

- Retained parity before implementation found 38 graph CloudResource nodes and
  38 matching owner-ledger identities, with no graph-only or ledger-only rows
  and an identical identity digest.
- The upgrade regression starts with a graph-only CloudResource row and an
  incomplete marker. It proves the row is seeded before completion, a partial
  seed never marks completion, a second startup skips the graph, malformed
  unattributable rows fail closed, and multi-page enumeration advances by the
  last uid without gaps. The opt-in NornicDB test repeats that page proof against
  the shipped Cypher and a real 1,201-node graph.
- Filtered current and candidate reads returned identical sets. The candidate
  corrects the old graph backend's non-semantic filtered ordering by enforcing
  `(resource_type, uid)` in Postgres.
- Handler tests cover zero, one, exact-limit, and limit-plus-one pages;
  provider/resource-type/region/account filters; stale and malformed cursors;
  empty scoped grants; store and graph failures; and shuffled graph hydration.
- Missing, duplicate, unexpected, ID-mismatched, or resource-type-mismatched
  graph rows fail closed. The handler never emits a partial page as exact truth.

## Rejected directions

- Adding an `id` graph index produced only a small cold change and no meaningful
  warm improvement.
- Materializing a graph list key was incompatible with the pinned backend's
  expression-write behavior and could not complete the representative backfill.
- The reducer cloud identity read model uses a different canonical identity and
  cannot safely hydrate these graph nodes.
- A naive owner-to-fact join scanned and sorted the whole fact set. The scalar
  active-fact probe's `LIMIT 1` is load-bearing and is pinned by query-shape tests.

No retained identifier, hostname, credential, or machine path is recorded in
this evidence.
