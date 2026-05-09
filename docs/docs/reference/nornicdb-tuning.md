# NornicDB Tuning Reference

This page is the operator map for Eshu's NornicDB-specific environment
variables. Use it when `local_authoritative` indexing is correct but a
repo-scale run exposes a bounded write timeout, slow phase, or compatibility
gate.

For the complete Eshu environment-variable catalog, including non-NornicDB
collector, queue, database, telemetry, and Compose settings, see
[Environment Variables](environment-variables.md).

NornicDB is Eshu's supported default graph backend. Tune from evidence: first
identify the phase, label, row count, grouped statement count, and timeout shape
in the structured logs, then change the narrowest matching knob. Do not lower
broad defaults because one chunk looked scary.

## Validation Ladder

Do not use the full corpus as the first debugging loop for a timeout. When a
run names a specific repo, scope, phase, label, and row count, validate in this
order:

1. Re-run only the failing repo with a fresh `ESHU_HOME`, rebuilt Eshu binaries,
   and the exact NornicDB binary under evaluation.
2. If the timeout is the only blocker and the statement is plausibly correct,
   raise `ESHU_CANONICAL_WRITE_TIMEOUT` for that correctness-validation lane
   (for example `120s`) so the pipeline can finish and reveal later semantic or
   query-truth failures.
3. After the single repo drains with `pending=0`, `in_flight=0`, and no
   dead letters, run a medium corpus of 15-20 representative repos.
4. Run the full corpus only after the focused and medium lanes pass end to end.

This ladder separates correctness from performance. A larger timeout is allowed
to prove the graph, queue, and query surfaces finish correctly; it must not be
treated as the final tuning answer without later phase timing and write-shape
analysis.

Latest checkpoint: the 2026-05-04 latest-main full-corpus proof rebuilt Eshu and
NornicDB `main`, indexed the full corpus, drained `8458/8458` queue rows in
`878s`, kept pending, in-flight, retrying, failed, and dead-letter rows at `0`,
and passed API/MCP relationship-evidence drilldowns. Treat earlier focused and
medium runs as the debugging ladder that led here. The remaining promotion work
is Neo4j parity research: find the Neo4j bottleneck, tune the smallest proven
adapter slice, and rerun a terminal comparison. Another NornicDB-only
full-corpus proof is regression evidence, not a substitute for that decision.

Follow-up checkpoint: Eshu `c598000d` then passed a targeted five-repo lane that
combined the prior small semantic regressions with the two noisy PHP stress
repos. It drained healthy in `854s`; the largest projections were
`php-large-repo-a` at `148,948` facts in `166.496305644s` and
`php-large-repo-b` at `176,201` facts in `521.49982913s`; their semantic
reducers completed in `6.33473887s` and `15.762956452s`; and the run ended
with `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. Use this as
the current problem-repo proof before moving to a larger representative subset.

Representative subset checkpoint: Eshu `5c9b169a` with the same NornicDB
`86e78f1` binary drained a 50-repo subset from `/home/ubuntu/eshu-e2e-full` in
`884s` with final `Health: healthy` and queue
`pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. The log scan found
no graph write timeout, semantic failure, acceptance-cap, retry, dead-letter,
panic, or fatal lines. The slow path was not reducer semantic correctness:
`php-large-repo-b` source-local projection held the queue while writing
`131,977` `Variable` entities and `28,926` `Function` entities. During that
phase, `Variable` entity chunks progressed from small subsecond executions to
a label summary of `102,654` rows, `13,200` statements, and `130.161796981s`
total label time before the repo drained. Treat this as the current evidence
that the remaining performance target is high-cardinality source-local
canonical entity writes and noisy repo input shape, not another semantic
batch-cap tweak.

Promoted batched-containment checkpoint: a 2026-04-27 isolated
`php-large-repo-b` rerun on Eshu `dcb5e466` with
`ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT=true`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`,
`ESHU_REDUCER_WORKERS=2`, and the `#119 + #120` NornicDB binary drained the
main queue cleanly with no graph timeout, retry, dead-letter, panic, or fatal
lines. The repo discovered `74,475` files and persisted `176,201` facts;
collection/emission took `161.706108907s`. Source-local projection reached
`Variable=131,977`, `Function=28,926`, `Class=6`; canonical `Variable` used
the intended `batch_across_files=true` shape and completed `131,977` rows as
`13,198` statements / `2,640` grouped executions in `301.798956955s` with no
singleton fallbacks. A later Elasticsearch Tier 3 run showed file-scoped
containment over-fragmented `Variable` writes on million-entity Java input, so
batched containment is now the default NornicDB canonical entity shape. The
next optimization target remains NornicDB file-anchor and relationship
existence lookup behavior before adding more Eshu batch caps.

Variable row-cap checkpoint: follow-up 2026-04-27 focused reruns on
`php-large-repo-b` showed the earlier narrow `Variable=10` default was too
conservative after file-scoped entity batching. The same `131,977` canonical
`Variable` rows completed in `196.713s` at `10` rows, `130.082s` at `25`,
`118.136s` at `50`, and `102.820s` at `100`, with zero singleton fallbacks,
zero retries, zero dead letters, and max grouped execution `0.607s` at the
`100`-row cap. A small control run on `terraform-module-karpenter` also
drained healthy with queue `pending=0 in_flight=0 retrying=0 dead_letter=0
failed=0`. This promotes `Variable=100` as the built-in default. Raise beyond
`100` only after a focused run shows max grouped execution remains comfortably
below `ESHU_CANONICAL_WRITE_TIMEOUT`; lower it again only if timeout summaries
name `Variable` and the discovery advisory confirms the rows are authored
source that should remain in the graph.

Content-store checkpoint: after worker-parallel pre-scan and graph-write
tuning, the same repo showed Postgres content persistence as the largest
single source-local stage. On Eshu `318c83e4`, `prepare_entities` took `0.117s`
but `upsert_entities` took `158.293s` for `160,909` rows / `537` batches. Use
`ESHU_CONTENT_ENTITY_BATCH_SIZE` (`300` default, `1..4000` valid range) for
focused A/B runs only after this exact content-writer ledger points at Postgres
entity upserts; it is not a NornicDB graph-write knob and should not be used to
respond to `graph_write_timeout`.

The first focused A/B proved batch size was diagnostic, not causal, for the
`php-large-repo-b` stress repo: `ESHU_CONTENT_ENTITY_BATCH_SIZE=600`
reduced statements to `269`, but `upsert_entities` stayed flat at `158.814s`.
A direct Postgres microbench isolated the real cost to the trigram index over
large entity snippets: copying the same `160,909` rows took `1.661s` without
indexes, `2.827s` with the btree lookup indexes, and `132.174s` with
`content_entities_source_trgm_idx`. The repo's `Variable` entities alone
carried about `1.108 GB` of `source_cache`, mostly generated/vendor-style
assignments. Eshu now bounds oversized `Variable` entity snippets at `4 KiB`
and records `source_cache_truncated`, `source_cache_original_bytes`, and
`source_cache_limit_bytes` metadata. Exact full-source search remains available
through `content_files`; entity search is a snippet surface.

The follow-up runtime proof on Eshu `f8322c41` drained the same repo healthy with
projector `1/1`, reducer `8/8`, and no retry, dead-letter, failed, timeout, or
panic/fatal rows. `upsert_entities` fell to `31.956s`, total content write fell
to `43.762s`, and total source-local projection fell to `165.604s` while
canonical graph write stayed comparable at `120.248s`. Persisted content still
contained `160,909` entities; `37,288` oversized `Variable` rows had truncation
metadata, total entity `source_cache` was `164 MB`, and function/class snippets
were unchanged. Treat this as the current evidence that source-cache shaping,
not `ESHU_CONTENT_ENTITY_BATCH_SIZE`, is the right fix for this bottleneck.

When local-authoritative bulk-load proofs still show content trigram index
maintenance as the long pole, `ESHU_LOCAL_AUTHORITATIVE_DEFER_CONTENT_SEARCH_INDEXES=true`
can defer the `content_files.content` and `content_entities.source_cache`
trigram indexes during initial writes. The local Eshu service rebuilds those indexes
after the discovered filesystem repo set reaches a clean
projector/reducer/shared-intent drain, so content rows and search semantics are
preserved while write-heavy startup avoids per-batch GIN maintenance. Treat
this as a local-authoritative proof/load knob, not a deployed Postgres schema
default.

Medium-corpus source-cache checkpoint: Eshu `a7078ddf` with NornicDB `v1.0.43`,
`ESHU_REDUCER_WORKERS=8`, `ESHU_CANONICAL_WRITE_TIMEOUT=120s`, and
`ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT=250000` drained the
`/home/ubuntu/eshu-test-repos` corpus of `23` repos healthy in about `3m11s`.
Final durable state was projector `23/23`, reducer `184` succeeded work items,
and queue `pending=0 in_flight=0 retrying=0 dead_letter=0 failed=0`. The large
PHP repo `php-large-repo-a` wrote `138,712` content entities in
`21.196s`, then spent `78.490s` in canonical graph write. Across the run Eshu
persisted `182,305` content entities, with only `1,463` truncated `Variable`
rows and `24 MB` total `Variable` source cache. This promotes the source-cache
shaping rule from focused proof to medium-corpus proof and moves the next tuning
target back to canonical graph Cypher shape and NornicDB lookup behavior.

Variable grouping checkpoint: the follow-up focused run on
`php-large-repo-a` showed that the proven `Variable=100` row
batch and the proven `Variable=5` grouped-statement cap are separate controls,
not conflicting evidence. `ESHU_NORNICDB_ENTITY_LABEL_BATCH_SIZES=Variable=100`
keeps each Variable statement large enough to avoid excessive fragmentation.
`ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS=Variable=5` keeps each
grouped execution bounded to roughly `100 * 5 = 500` Variable rows. Raising the
grouped-statement cap to `10` was safe but only marginally faster on that repo;
raising it to `25` made early Variable chunks clearly slower, so `5` remains
the best proven default and `10` is only a focused experiment candidate.

Edge-index checkpoint: the NornicDB direct edge-between index proof reran the
same large PHP repo after patching relationship lookup from outgoing-edge scans
to indexed `(start,end,type)` existence checks. The run drained healthy with no
retry, failed, or dead-letter queue rows. Canonical files completed in `1.496s`,
`Function` completed in `6.053s`, and `Variable` completed `118,768` rows in
`62.340s` with max grouped execution `0.437s`. Keep `Variable=100` plus
`Variable=5` as the ESHU-side default while the NornicDB patch moves through
upstream review; do not compensate for missing relationship indexes by lowering
Eshu row caps.

Medium-corpus edge-index checkpoint: the same patched NornicDB binary drained
`23` repos from `/home/ubuntu/eshu-test-repos` healthy, with projector `23/23`,
reducer `184` succeeded work items, and queue `pending=0 in_flight=0
retrying=0 dead_letter=0 failed=0`. The large PHP tail still spent `69.750s`
inside `Variable` entity writes, but file relationship writes stayed bounded:
the canonical file phase completed `52` statements in `1.683s`. This confirms
the edge index fixes relationship-existence slope; it does not remove the need
to tune high-cardinality entity volume from measured label summaries.

## Backend Selection

| Variable | Default | Scope | Use |
| --- | --- | --- | --- |
| `ESHU_GRAPH_BACKEND` | `nornicdb` | API, MCP, ingester, reducer, local Eshu service | Selects the graph adapter. Set to `neo4j` for the explicit Neo4j path. Invalid values fail startup. |
| `ESHU_NORNICDB_BINARY` | unset | local Eshu service / install / tests | Points Eshu at an explicit NornicDB binary. This wins over managed `${ESHU_HOME}/bin/nornicdb-headless` and `PATH`. |
| `ESHU_NORNICDB_INSTALL_TIMEOUT` | `30s` | `eshu install nornicdb` | Extends remote download timeouts for slow links. |

## Canonical Write Budget

| Variable | Default | Scope | Use |
| --- | --- | --- | --- |
| `ESHU_CANONICAL_WRITE_TIMEOUT` | `30s` on NornicDB | ingester, reducer graph writers | Bounds each NornicDB graph execution with a client deadline and Bolt transaction timeout. Shorten for diagnostics; lengthen only with evidence. |
| `ESHU_NORNICDB_PHASE_GROUP_STATEMENTS` | `500` | canonical writes | Broad grouped-statement cap for phases without a narrower phase-specific cap. |
| `ESHU_NORNICDB_FILE_PHASE_GROUP_STATEMENTS` | `5` | canonical `files` phase | Limits how many file-upsert statements share one grouped Bolt transaction. |
| `ESHU_NORNICDB_FILE_BATCH_SIZE` | `100` | canonical `files` phase | Limits rows inside each `phase=files` statement. Use when file groups are narrow but one statement still carries too many rows. |
| `ESHU_NORNICDB_ENTITY_PHASE_GROUP_STATEMENTS` | `25` | canonical `entities` and `entity_containment` phases | Limits grouped statement count for canonical entity phases. |
| `ESHU_NORNICDB_ENTITY_BATCH_SIZE` | `100` | canonical entity rows | Limits rows inside normal entity upsert statements before label-specific caps apply. |
| `ESHU_NORNICDB_ENTITY_LABEL_BATCH_SIZES` | `Function=15,K8sResource=1,Struct=50,Variable=100` | canonical entity rows | Overrides row caps for specific canonical labels, for example `Function=15,Variable=100`. |
| `ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS` | `Function=5,K8sResource=1,Struct=15,Variable=5` | canonical entity grouping | Overrides grouped-statement caps for specific canonical labels. |

Two knobs often look similar but are different:

- `*_PHASE_GROUP_STATEMENTS` controls how many statements run in one grouped
  transaction.
- `*_BATCH_SIZE` controls how many rows are inside one statement.

The effective grouped row pressure is approximately:

```text
label row batch size * label grouped statement cap
```

For example, `ESHU_NORNICDB_ENTITY_LABEL_BATCH_SIZES=Variable=100` with
`ESHU_NORNICDB_ENTITY_LABEL_PHASE_GROUP_STATEMENTS=Variable=5` means each
Variable statement can carry up to `100` rows, and a grouped execution can
carry roughly `500` Variable rows. Increasing the grouped-statement cap to `25`
would push that pressure toward `2,500` rows per grouped execution, even though
the row-batch knob still says `Variable=100`.

Use the timeout summary and `nornicdb entity label summary` logs to decide
which dimension failed.

Eshu applies `ESHU_CANONICAL_WRITE_TIMEOUT` in two places on NornicDB: the
client context deadline and the Neo4j-driver Bolt `tx_timeout` metadata. Keep
both sides aligned so a timed-out reducer or ingester write does not merely
stop waiting while the database keeps executing the same mutation.

When that budget is exhausted, Eshu stores the queue failure as
`graph_write_timeout` and preserves the sanitized phase/label/row summary in
`failure_details`. Typed graph write timeouts are bounded-retry candidates: the
first timeout can be transient backend pressure or graph-write contention, but
the queue still dead-letters after the configured attempt budget. Deterministic
syntax, schema, and unsupported-query failures remain terminal because they do
not implement the retry contract.

## Semantic Write Budget

| Variable | Default | Scope | Use |
| --- | --- | --- | --- |
| `ESHU_PROJECTOR_WORKERS` | `NumCPU` on NornicDB local-authoritative | ingester source-local projector | Runs source-local projection at the developer or host CPU count. Lower only when graph-write telemetry shows backend saturation or conflict-key contention. |
| `ESHU_NORNICDB_SEMANTIC_ENTITY_LABEL_BATCH_SIZES` | `Annotation=5,Function=10,ImplBlock=10,Module=10,TypeAlias=5,TypeAnnotation=50,Variable=10` | reducer semantic entity materialization | Overrides NornicDB row caps for semantic labels after parser-enriched semantic metadata proves expensive. |
| `ESHU_REDUCER_WORKERS` | `NumCPU` on NornicDB | reducer graph writers | Overrides reducer work concurrency. Leave unset for normal NornicDB runs; lower only when conflict-domain fencing still shows graph write conflicts or backend saturation. |
| `ESHU_REDUCER_BATCH_CLAIM_SIZE` | `workers` on NornicDB | reducer queue claim window | Limits how many reducer intents one claim cycle leases before workers start them. Keep this near worker count so queued-but-not-started items do not expire their leases. |
| `ESHU_REDUCER_SEMANTIC_ENTITY_CLAIM_LIMIT` | `1` on NornicDB | reducer semantic entity materialization | Caps concurrent semantic entity reducer claims after the source-local drain gate opens. Raise only in focused proofs after the active NornicDB binary proves semantic `MATCH SET` writes stay bounded. |
| `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` | `250000` | reducer code-call projection | Bounds how many code-call shared intents one accepted repo/run may scan or load before failing safely. Raise only when a real repo has more CALLS intents than the default and memory headroom is known. |

Semantic materialization is a reducer-owned phase. Do not copy canonical caps
blindly; semantic labels should be narrowed only after timeout summaries name
the semantic label and row count.

NornicDB semantic writes use a merge-first explicit row template instead of the
older `MATCH File` before `MERGE node` row-map shape. The older shape can still
use schema lookup, but trace probes showed it misses NornicDB's generalized
`UNWIND/MERGE` batch hot path. Treat semantic timeouts as query-shape evidence
first, then tune label caps only after confirming the statement is already on
the intended template. The merge-first writer is now validated through focused,
medium, and full-corpus latest-main lanes. A future timeout in this phase
should be treated as regression or comparison evidence and narrowed to the
label, row count, graph size, and query shape before changing caps.

Code-call projection is also reducer-owned, but its scan limit is a correctness
guard rather than a graph-write tuning knob. The runner retracts repo-wide
CALLS edges and then rewrites the accepted repo/run slice, so it must load the
whole acceptance unit before marking intents complete. If
`ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` is exhausted, use the
discovery advisory report first to confirm the repo is not dominated by
generated or vendored code before raising the limit.

Increase `ESHU_CODE_CALL_PROJECTION_ACCEPTANCE_SCAN_LIMIT` only when all of the
following are true:

- The reducer log names `code call acceptance scan reached cap` or
  `code call acceptance intent scan reached cap`.
- The discovery advisory shows the repo's high code-call volume comes from
  authored source you intentionally want in the graph, not checked-in bundles,
  generated output, archives, or third-party vendor trees that should be
  filtered with `.eshu/discovery.json`.
- The host has memory headroom for loading the full accepted repo/run slice in
  one reducer cycle. The guard exists to prevent partial CALLS truth, not to
  make unbounded in-memory projection safe.

Do not increase it for `graph_write_timeout`, slow canonical phases, semantic
label timeouts, or queue backlog by itself. Those failures belong to the
phase/label/write-shape controls above, the discovery advisory workflow, or a
deeper reducer/code-call projection design change. If a real authored repo
needs more than the default repeatedly, record the advisory evidence and
consider redesigning code-call projection to page a complete acceptance unit
safely instead of growing the cap indefinitely.

When `ESHU_GRAPH_BACKEND=nornicdb`, Eshu defaults reducer intent execution to a
bounded CPU-sized worker pool and relies on durable conflict-domain keys for
safety. Code-graph reducer domains for one repo serialize with one another,
platform graph reducer domains for that repo serialize with one another, and
unrelated families can still run concurrently. The claim window defaults to the
worker count so each claimed item can enter heartbeat-protected execution
promptly instead of sitting in the local worker channel until `claim_until`
expires.

For `ESHU_QUERY_PROFILE=local_authoritative` plus `ESHU_GRAPH_BACKEND=nornicdb`,
reducer claims also wait while source-local projector work is outstanding. This
is not a row-size tuning knob: it removes the unsafe overlap where
first-generation canonical projection and reducer graph writes contend for the
same local NornicDB runtime. Neo4j keeps the existing production concurrency
path, and NornicDB operators should tune worker count only from post-drain
queue tail, graph-write timeout, CPU, disk, and NornicDB profile evidence.

First-generation semantic materialization skips stale retract because there is
no prior semantic graph state to clean up. Refreshes and retries still retract;
on NornicDB those retracts run one semantic label per statement. The Neo4j
adapter keeps its broad multi-label retract, but NornicDB's syntax and cost
profile make the label-scoped shape the safer repo-scale cleanup path.

## Compatibility And Conformance Switches

| Variable | Default | Scope | Use |
| --- | --- | --- | --- |
| `ESHU_NORNICDB_CANONICAL_GROUPED_WRITES` | unset / `false` | canonical writes | Conformance-only switch that exposes Neo4j-style grouped canonical writes on NornicDB. Leave unset for normal laptop runs. |
| `ESHU_NORNICDB_REQUIRE_GROUPED_ROLLBACK` | unset / `false` | test gates | Makes rollback conformance mandatory in opt-in NornicDB grouped-write tests. |
| `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT` | unset / `true` | canonical entity writes | Cross-file batched entity containment. Set to `false` only for focused fallback comparisons against the older file-scoped shape. |

Do not disable `ESHU_NORNICDB_BATCHED_ENTITY_CONTAINMENT` just because one run is
slow. The default row-scoped containment shape has repo-scale correctness proof;
fallback comparisons should capture statement count, label summaries, retries,
dead letters, and terminal drain state.

## NornicDB Runtime Diagnostics

| Variable | Default | Scope | Use |
| --- | --- | --- | --- |
| `NORNICDB_ENABLE_PPROF` | unset / `false` | NornicDB process | Enables NornicDB profiling when a run is progressing linearly and Eshu logs no longer identify a ESHU-side batching mistake. |

## Adding New Knobs

Phase-specific tuning is deliberately narrow and evidence-driven. Before
adding another `ESHU_NORNICDB_*` variable:

1. Capture a timeout or slow-phase log that names the phase, label, row count,
   grouped statement count, and duration.
2. Prove whether the failure is statement width, row width, query shape,
   missing NornicDB functionality, or machine/resource pressure.
3. Prefer fixing NornicDB when Eshu is missing a Neo4j-equivalent primitive and
   the feature belongs in the database.
4. Add the narrowest Eshu adapter seam only when the evidence shows a ESHU-side
   shape or bounded budget is the right fix.
5. Update this page, the active NornicDB ADR, and the local testing runbook in
   the same PR.

If a one-row or very-low-row statement is still slow, do not immediately lower
global graph-write concurrency. First confirm whether NornicDB is taking the
intended hot path or falling back to a generic executor. The compatibility
workflow prefers adding performant NornicDB support for Neo4j-equivalent query
shapes before Eshu gives up useful cross-repo parallelism.
For canonical entity writes, ordinary one-row file-scoped batches should still
use the `UNWIND $rows AS row` hot path. The execute-only singleton fallback is
reserved for rows containing the known `shortestPath` / `allShortestPaths`
parser hazard; broad singleton logs for normal symbols usually mean a writer
shape regression, not a reason to lower global concurrency.
If a correctly grouped `MERGE (n:<Label> {uid: row.entity_id})` statement is
still slow at one row, check schema preconditions before tuning workers:
NornicDB needs the matching `<Label>.uid` uniqueness constraint to use its
schema-backed merge lookup instead of a generic label scan.
File-phase writes have the same rule. Eshu's NornicDB schema includes explicit
property indexes for `Repository.id`, `Directory.path`, and `File.path` because
NornicDB's `MERGE` lookup path checks property indexes before falling back to a
label scan. If `phase=files` chunks grow steadily slower as the graph grows,
verify these indexes were created before changing file batch sizes or write
timeouts.

Watch future heavy write families such as call edges, infra edges, and other
shared reducer domains. If they need different treatment, add phase metadata
and tuning only after repo-scale evidence proves the existing canonical or
semantic controls do not describe the bottleneck.
