# #7128 Documentation Facts Active-Generation Binding Evidence

`GET /api/v0/documentation/facts` and MCP `list_documentation_facts` share
`buildDocumentationFactsSQL`. Without `generation_id` the statement did not
constrain `fact_records.generation_id`, so it read every retained generation of
the scope. That is an accuracy defect (superseded facts served as current facts
under an `exact`/`fresh` truth envelope) and the cause of the fixed latency
floor (a top-N sort of every retained generation). The fix binds the default
read to the scope's active generation.

The sibling builders `buildDocumentationFindingsSQL`,
`buildDocumentationTargetFactsSQL`, and `buildSemanticEvidenceSQL` have the same
gap and are deliberately not touched here; they are tracked in #7164.

## Behaviour contract

| Request | Read | Label |
| --- | --- | --- |
| `scope_id`, no `generation_id` | `generation_id = (SELECT active_generation_id FROM ingestion_scopes WHERE scope_id = $n)` reusing the scope parameter | `generation_binding.mode=active`, bound generation from the returned rows |
| anchor-only (`repo`, `source_id`, ...), no `generation_id` | one INNER JOIN on `ingestion_scopes.active_generation_id = fact_records.generation_id` | `mode=active`, `generation_id` empty, `is_active=true` |
| `fact_kind=source` alone, no scoped token | ordered `documentation_source` scan plus a per-row primary-key probe of `ingestion_scopes` (`active_probe`) | as anchor-only |
| `generation_id` set | unchanged `generation_id = $n` | `mode=explicit`, `is_active` from `scope_generations`; `truth.freshness` `fresh` (active), `stale` (superseded, completed, failed), `building` (pending), `unavailable` (unknown id) |
| scope with no active generation | zero rows, never a latest-generation fallback | states `no_active_generation`; freshness `unavailable`/`dead_lettered_domain` for a failed scope, `building`/`pending_repo_generation` otherwise |
| unknown `scope_id` | zero rows | state `scope_not_found` |
| scoped token, scope or generation outside its grants | zero rows (the facts authorization predicate) | the label of an unknown id: `scope_not_found`, or `unavailable` for a named generation; no generation id, lifecycle state, or owning scope |

The cursor stays an integer offset that names no generation.

`documentationFactBindingForm` is the single pure function that picks the form
(`explicit`, `active_scope`, `active_join`, `active_probe`); the SQL builder, the
read model, and the handler span attribute all call it and nothing else chooses
a form. The handler records the span attribute from the filter after the
scoped-token access filter is applied, which is the filter the SQL is built
from. `TestDocumentationFactBindingFormForEveryFilterClass` covers every
filter class, including scoped-token callers, and
`TestDocumentationFactsSQLFollowsBindingForm` pins the SQL each form produces.

## Deviation from the arbiter ruling

The ruling put every anchor-only read on one INNER JOIN and named an EXISTS
semi-join as the fallback if shape (a) lost its early stop. The measurements
below show both forms lose it, so `fact_kind=source` alone with no scoped token
binds through a per-row primary-key probe (`active_probe`) instead. That form is
not an index and keeps the ordered scan the ruling wanted. The team lead
approved it against the measured data. The span attribute therefore has a fourth
value, `active_probe`, beyond the ruling's `active_scope`, `active_join`, and
`explicit`.

## Before: measured floor

Diagnosis on ops-qa, one Confluence-style scope with eight retained generations
(one active, seven superseded, about 56,000 rows): the unbound statement ran a
top-N sort over 56,149 index entries at every limit, about 74,000 buffers,
median about 1.4 s and a 0.67 s to 6.0 s range, independent of `limit`. The API
and MCP surfaces run the same statement; the gap between them in the sweep sat
inside the statement's own run-to-run spread.

## After: local plan proof (same data, before and after)

Performance Evidence: `TestDocumentationFactsActiveScopeReadUsesKeysetIndexLive`, PostgreSQL 16 in a
disposable container, one scope, eight generations of 7,000 documentation facts
each (56,000 rows), production SQL from `buildDocumentationFactsSQL`. "Before"
is the same test with the active binding removed from the builder.

| Case | Before ms / buffers / node | After literal ms / buffers | After generic-plan ms / buffers |
| --- | --- | --- | --- |
| default, limit 6 | 216.95 / 1743 / Sort | 0.053 / 5 | 0.064 / 5 |
| default, limit 30 | 226.97 / 1789 / Sort | 0.557 / 6 | 0.166 / 6 |
| default, limit 15, offset 500 | 214.95 / 1789 / Sort | 1.172 / 35 | 1.854 / 35 |
| `fact_kind=section` | 75.54 / 1743 / Sort | 0.123 / 9 | 0.091 / 9 |
| scoped token | 482.53 / 1744 / Sort | 0.110 / 7 | 0.088 / 7 |

After: `Index Scan Backward` on `fact_records_scope_generation_keyset_idx` with the
generation in the index condition and no `Sort` node (the `fact_kind` case
plans an Incremental Sort over the `(scope_id, generation_id, fact_kind,
observed_at)` index, also bounded by `LIMIT`; the test bans only a full `Sort`).
The generic plan is the one `database/sql` clients get: the test runs
`SET plan_cache_mode = force_generic_plan`, `PREPARE`, then
`EXPLAIN (ANALYZE, BUFFERS) EXECUTE`. A bare `EXPLAIN` does not show it (#6154).

## Anchor-only reads: ops-qa experiment (read-only)

Read-only `EXPLAIN (ANALYZE, BUFFERS)` through a session forced to
`default_transaction_read_only=on`, account confirmed with
`aws sts get-caller-identity --profile ops-qa` before the run. The production
SQL for each shape was generated by `buildDocumentationFactsSQL`; two to four
repositories were rotated and the variants interleaved with alternating first
mover. Repository and source identifiers are omitted. 11,960
`documentation_source` facts and 814 ingestion scopes exist.

| Shape (limit 10) | Unbound (before) | INNER JOIN | EXISTS | Correlated PK probe |
| --- | --- | --- | --- | --- |
| a. `fact_kind=source` | 0.19-0.35 ms, 9-14 buffers, ordered early stop | 26-76 ms, 4,915 buffers, Sort of all source rows | 25-32 ms, 4,915 buffers, same plan | 0.45-0.59 ms, 33-47 buffers, ordered early stop |
| b. `repo` | statement timeout at 45 s (2 of 2) | 2.8-8.7 s | not run | statement timeout at 45 s |
| c. `source_id`+`document_id`+section | statement timeout at 45 s (2 of 2) | 6.0-9.9 s | not run | statement timeout at 45 s |
| d. `q`+`source_id` | statement timeout at 45 s (2 of 2) | 7.2-20.5 s | not run | statement timeout at 45 s |
| e. (a) with scoped-token predicates | 85 ms and 2.7 s | 31-40 ms, 4,325-4,665 buffers | not run | 179 ms and 3.9 s |

Decision from the data:

- The planner flattens `EXISTS` into the same semi-join as the INNER JOIN, so
  the ruling's fallback form does not keep the early stop for shape (a).
- The correlated scalar probe keeps the early stop for (a) but is worse than
  the join wherever selective payload predicates or the scoped-token OR
  predicate drive the scan, so it is used only for the kind-only, unscoped-token
  read (the `active_probe` branch of `documentationFactBindingForm`).
- Everything else uses the single INNER JOIN. Shapes (b)-(d) were already
  timing out at 45 s unbound and now complete in seconds; they remain seconds
  because no index serves `source_id`/`document_id`/`section_id`/`q` payload
  predicates without a scope. That is pre-existing and no index is proposed
  here.

## Generic plans for the anchor-only forms

`TestDocumentationFactsAnchorOnlyReadsBindActiveGenerationLive` seeds the ops-qa
source scale (1,500 scopes x 8 generations, one `documentation_source` fact per
generation, 12,000 rows, one row in eight active) and plans each form as a
literal statement and as a `force_generic_plan` `PREPARE`/`EXECUTE`:

| Form / case | Literal ms / buffers | Generic ms / buffers | Plan |
| --- | --- | --- | --- |
| `active_probe`, `fact_kind=source` | 0.096 / 58 | 0.220 / 36 | `documentation_source` partial index, no Sort, `ingestion_scopes` primary-key probe |
| `active_join`, selective `source_id` | 4.5 / 478 | 2.2 / 478 | INNER JOIN through `ingestion_scopes` on `active_generation_id` |
| `active_join`, scoped token | 7.1 / 478 | 6.5 / 478 | same join |

This check found a pre-existing defect. Before this change, the unbound source
page under a generic plan already lost the partial index because the kind was a
parameter (`fact_kind = $n`): 32.4 ms, 405 buffers, with a Sort, against 0.043
ms and 3 buffers as a literal. With the probe added, the same generic plan got
worse (83.7 ms, 45,949 buffers) until the builder inlined the
`documentation_source` constant, a package constant and never caller input.
Under `plan_cache_mode = auto` the planner keeps the cheap custom plan, so
production was not seeing the generic plan; the forced check is the worst case
and is now clean.

## Residual and the issue's acceptance

The selective-anchor shapes (b)-(d) go from a statement timeout above 45 s to
2.8-9.9 s (up to 20.5 s for the `q` search shape). That is still above the
sub-second target and is a named residual, not a claim of success: no index
serves `source_id`, `document_id`, `section_id`, or `q` payload predicates
without a scope, so the join bounds the scan to active generations but still
reads it. No index is proposed here.

The issue's own acceptance is the sweep's `scope_id`-only argsets at limits 6,
15, and 30 (p95 warm under 1 s). Evidence so far:

- ops-qa diagnosis shim of the same statement shape, literal parameters,
  before the change: 0.67-6.0 s (median about 1.4 s); after (scalar-subquery
  form): 0.36-1.58 ms at limit 6, 0.75-1.05 ms at limit 15, 1.26-1.57 ms at
  limit 30, 16-77 buffers.
- local PostgreSQL 16, production SQL, 56,000 rows: 0.05-1.9 ms, 5-35 buffers,
  literal and generic plans.
- ops-qa SQL replay, below: the acceptance holds.

### ops-qa replay of the branch SQL

Read-only replay on ops-qa (session `default_transaction_read_only=on`,
`EXPLAIN (ANALYZE, BUFFERS)` only, generic-plan runs inside
`BEGIN`/`ROLLBACK`) of the SQL `buildDocumentationFactsSQL` renders at
`0269f185f0`, against the unbound form (the same statement with only the
`generation_id = (SELECT ...)` predicate removed). One Confluence documentation
scope from the sweep: 8 retained generations (1 active, 7 superseded), 56,149
non-tombstone rows, 7,013 in the active generation. Times are server-side
planning plus execution; each repeat uses a different `OFFSET`; 7 warm runs per
cell after the change (p95 with n=7 is close to the max), 3 runs before.

| Shape | Limit | Before, unbound: warm p50 | After: warm p95 (custom) | After: warm p95 (generic) |
| --- | --- | --- | --- | --- |
| `scope_id` only | 6 / 15 / 30 | 540 / 442 / 424 ms | 2.3 / 2.1 / 2.2 ms | 0.19 / 0.22 / 0.30 ms |
| `scope_id` + section kind | 6 / 15 / 30 | 107-110 ms | 15-18 ms | 14-38 ms |
| `scope_id` + scoped token | 6 / 15 / 30 | 431-454 ms | 2.4-3.6 ms | 50-69 ms |

- The acceptance holds: `scope_id`-only reads at limits 6, 15, and 30 have a
  warm p95 of 2.3, 2.1, and 2.2 ms with the custom plan and 0.19-0.30 ms with
  the generic plan. Both plans walk `fact_records_scope_generation_keyset_idx`
  backward with no Sort and touch 16-77 buffers. The unbound form ran 424-540 ms
  warm over about 73,800 buffers at every limit.
- The cold outlier is explained: the first unbound call (limit 6, working set
  evicted by other traffic) took 22.4 s with about 37,000 buffer reads, and the
  same work runs at every limit, so the "large limit" of the sweep outlier was
  whichever call happened to be cold. After the change a first call touches
  16-170 buffers and ran 10-12 ms for `scope_id`-only reads.
- Residual: the section-kind read is not an early-stop plan. It runs an
  Incremental Sort over the active generation's section rows (554 here, 2,350
  buffers), so its 15-18 ms p95 scales with the number of rows of that kind, not
  the limit.
- Residual: a scoped token under a forced generic plan loses the ordered scan
  and sorts all 7,013 active rows (49-69 ms, about 6,400 buffers). It is still
  under 1 s, and the custom plan it gets by default keeps the ordered scan.
- Caveats: one scope only, and an SQL replay against ops-qa, not a rebuilt
  binary; application-side costs (decode, the label lookups) are not in these
  numbers. The replay ran the SQL at `0269f185f0`; the later label
  authorization fix changes only the label lookups, not the facts statement.

## Row-set equivalence and accuracy proof

`TestDocumentationFactsBindActiveGenerationLive` (PostgreSQL 16, disposable
database, real bootstrap DDL) seeds a superseded and an active generation whose
facts tie on `observed_at`, plus a dead-lettered scope, a pending scope, an
active scope with no facts, and a second active scope. Reference statements are
hand written. Subtests: default read equals the reference restricted to the
active generation in `(observed_at DESC, fact_id DESC)` order; an explicit
superseded generation equals its reference; a selective `fact_kind` filter;
offset paging with no duplicates or gaps; a scoped token (granted and denied);
anchor-only source reads across scopes; the kind-only source read; the three
empty-scope states; and explicit-generation labelling. With the active binding
removed from the builder, seven of the nine original subtests fail (the two that
do not are the explicit-generation ones, which must not change); with it, all
ten pass. The tenth subtest runs the kind-only source read through the probe form
and through the same statement rewritten with the INNER JOIN, and requires the
same ordered rows, including the paged walk one row at a time. The seed makes
that comparison bite: source facts of every scope tie on `observed_at` (`fact_id`
breaks the tie), two scopes are active, one has a superseded generation, and two
scopes have a NULL `active_generation_id` (one dead-lettered, one pending) whose
facts both forms must exclude.

## Observability

Observability Evidence: span attribute `eshu.documentation.generation_binding`
(`active_scope`, `active_join`, `active_probe`, `explicit`) on
`query.documentation_facts`, recorded from the filter the SQL is built from, and
a `documentation.empty_page` event (`reason` = `scope_not_found`,
`no_active_generation`, `no_rows`; `scoped_grant` = whether the lookup was
restricted to a scoped token's grants) on the `postgres.query` span
(`db.operation=list_documentation_facts`) when a zero-row scope read runs its
scope-state lookup. `eshu_dp_postgres_query_duration_seconds` by `db.operation`
carries the before/after drop. Operator question at 3 AM: filter the handler
span by `generation_binding` and watch the `list_documentation_facts` duration
histogram; an empty page carries its reason on the span. The telemetry coverage
row in `docs/public/observability/telemetry-coverage.md` is updated.

## Label authorization

The empty-page scope lookup and the explicit-generation lookup read scope and
generation rows, not facts, so they carried no authorization predicate at first.
Review found that a scoped token could then learn an ungranted scope's
existence, its dead-letter or pending state, its active generation id, and the
owning scope of a named generation. Both lookups now apply the scope-level half
of the facts authorization predicate (`appendDocumentationScopeGrantClause`: the
scope id, or the scope payload's `repo` or `repo_id`, is granted), shared with
`appendDocumentationAuthorizationClause`, so the facts SQL text is unchanged. An
ungranted scope or generation is not found, so its label is the label of an id
that does not exist. The fact-payload predicates describe single facts, not a
scope, so they do not grant a label. The one exception is an explicit read that
returned rows: those rows passed the full facts predicate and carry their own
scope and generation ids, so that generation is labelled normally. A token with
no grants never reaches the store and gets the same unknown-id label
(`documentationFactUngrantedPageState`). Shared-key and unauthenticated callers
have no grant clause and keep the full labels.

`TestDocumentationFactsLabelsHonorScopedGrantsLive` (PostgreSQL 16, the row-set
seed above) promotes the reviewer's reproduction. A token granted only scope E,
by scope id and by repository id, asks for scopes A, B, C, and D with an empty
page and for all five other seeded generations by id, and each label must equal
the label of a nonexistent scope or generation and contain none of their ids or
states. On the unfixed head all 18 of those subtests failed: scope B returned
`unavailable`/`dead_lettered_domain`, scope C `building`/`pending_repo_generation`,
scopes A and D their active generation ids, and generation A-1
"superseded for scope scope:docfacts-a". After the fix all pass, as do the
granted-token, shared-key, and admin cases that must keep the full labels, and a
fact-payload grant that reads a row of generation A-1 (labelled `stale`) while
the same token with no visible row gets the unknown-id label. Removing the
rows-returned exception fails that last subtest.
`TestDocumentationFactsSpanBindingMatchesExecutedFormForScopedToken` failed on
the unfixed head (`active_probe` recorded, `active_join` executed) and passes
now. `TestDocumentationFactsNoGrantTokenMatchesUngrantedScope` pins the no-grant
labels.

The grant clause adds at most one primary-key join from `scope_generations` to
`ingestion_scopes` to a lookup that already ran; it runs only for an empty scope
page or an explicit read, never for a non-empty active page.

The page statement and its label lookup are separate reads. If the scope's
active generation changes between them, an empty scope page can carry the newer
active generation id. A non-empty page takes its generation id from its own rows
and cannot be mislabelled this way.

## Concurrency

Read-only statements. No lease, claim, or queue path is touched, so no
concurrency proof beyond the plan check is claimed.

## Golden snapshot tightening

`testdata/golden/e2e-20repo-snapshot.json` now requires `generation_binding` in
the documentation facts entries (the HTTP route and the `list_documentation_facts`
MCP tool), and one entry description names the binding. The edit only tightens
the snapshot: no count bound, required field, or expectation is loosened or
removed. Changing the golden standard is an irreversible-class act, so it went to
an arbiter, which approved it as a strengthening with three conditions:

- the CI B-7 golden gate must pass on the PR head, since the local Neo4j gate run
  (567 pass, 0 required-fail) predates the rebase onto main;
- this note and the PR body record the tightening and the approval;
- any further edit to these snapshot entries needs a new ruling.

## Not proven here

- NOT_CHECKED: the sweep against a rebuilt binary on ops-qa. The SQL replay
  above covers the statement on the ops-qa cache state for one scope; it does
  not cover application-side cost or other scopes.
- Golden-corpus gate: run on Neo4j (`neo4j:2026-community`,
  `ESHU_GRAPH_BACKEND=neo4j`, all gate ports moved off the defaults) at
  `1724ba6cf6`, the label authorization fix, and passed: 567 pass, 0
  required-fail, 5 advisory-warn (phase wall-time bands only, with three other
  gate runs on the machine). Its documentation facts query shapes require
  `generation_binding`; `GET /api/v0/documentation/facts?fact_kind=source`
  returned 12 facts and MCP `list_documentation_facts` 1. An earlier run on the
  first head also reported 567 pass and 0 required-fail; a first attempt before that started on
  NornicDB and was stopped and discarded under the owner's Neo4j-only
  directive. No graph-backed evidence here came from NornicDB; every
  measurement in this note is PostgreSQL only.
- The 26-39 s cold-call outlier from the diagnosis is a cold-cache read of the
  fixed 74,000-buffer unbound scan; the ops-qa replay reproduced one such call
  (22.4 s). Caches were not dropped (the replay was read-only), so a cold read
  after the change is one observation of a first call (10-12 ms, 16-170
  buffers), not a forced cold-disk measurement.
