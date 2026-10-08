# Exact code-topic retrieval design gate (#7033)

Status: proposal for review. This document approves no data copy, schema change,
production implementation, deployment, or latency claim. The issue remains open.
It concerns the PostgreSQL content read behind
`POST /api/v0/code/topics/investigate`; Neo4j remains the canonical graph
backend. No NornicDB proof is proposed.

## Decision to make

The unscoped 16-term full-response read on deployed ops-qa measured 1.232 s
median and 1.526 s p95 over ten clean timed requests after two warm-ups. The
goal's under-1-s budget is not met. The relative-path trigram index (#7305)
and parallel read (#7519) are already merged. The remaining entity-name/source
probe is costly, but the endpoint also pays reader setup, another slow probe
partition, assembly, and response time. A faster isolated SQL term is not
endpoint acceptance.

The current read applies repository grants and language before each per-term
cap. It tests entity name or raw source with PostgreSQL `ILIKE`, and gives file
path matches priority over file-content matches. Each lane uses `LIMIT` without
`ORDER BY`. Final rank counts distinct matched terms, then sorts by score and
several row fields. Therefore capped membership, scores, and some ties can
change with a plan change. The public `truncated` page flag and internal
`candidate_pool_truncated` flag describe different boundaries.

The recommended architecture to test is a **PostgreSQL-owned, versioned
compressed-posting lane with a transactional exact-delta/tombstone overlay**.
Immutable posting segments would store sorted content identities per searchable
gram and field, in bounded shards. Each source mutation would record its exact
search delta or tombstone in the same transaction. A read would merge the
active segments and snapshot-visible overlay, fetch source rows, and verify
raw PostgreSQL `ILIKE` **before** counting a match toward the cap or its
cap-plus-one sentinel. Candidate grams are only a superset: false positives
must not consume quota. A manifest switch
would publish rebuilt segments only after validation, while old segments and
covered deltas remain until active readers finish. This is an **unproven
candidate**, not a selected implementation: the fixture must establish a
complete candidate superset, ordering, crash behavior, and acceptable cost
before any production schema or code is written. The tested row-per-trigram
table, curated BM25 documents, and plain asynchronous postings are not
substitutes. PostgreSQL documents that patterns with no extractable
trigrams can require a full trigram-index scan; GIN itself does not supply the
required candidate rank order ([pg_trgm](https://www.postgresql.org/docs/current/pgtrgm.html),
[index types](https://www.postgresql.org/docs/current/indexes-types.html)).

## Proposed result contract

1. Keep unscoped 16-term requests and the current explicit-term `ILIKE`
   pattern semantics, including `%`, `_`, backslash, case, Unicode, and short
   patterns. Changing these to literal-only input would be a separate API
   contract change, not a hidden #7033 optimization.
2. For an uncapped request, preserve the current matched entity/file set,
   distinct-term scores, path-before-content exclusion, SQL collation, and
   ranked results. Existing final-order ties are not fully specified; test
   them separately, and do not silently add a new wire-visible tie-breaker.
   The acceptance oracle compares full response data, not only counts or a
   few top rows.
3. Before every candidate cap, resolve the caller's grants, repository scope,
   language, and active source version. Never take a global top-N and then
   remove unauthorized rows. Do not send source text to an external service.
4. Make capped selection repeatable under one source version. The provisional
   policy is a stable `(repo_id, relative_path, entity_id)` order within each
   authorized per-term entity lane and `(repo_id, relative_path)` within each
   file lane, with file path before file content. The final SQL order and
   collation are unchanged. This selection key is a prototype, subject to
   fixture exactness and product approval.
   This is **not** a promise of global top-K by final score: capped results are
   explicitly partial and `candidate_pool_truncated` must be true only when a
   cap-plus-one sentinel proves an omitted candidate. This changes today's
   `bool_or(term_count >= cap)` saturation heuristic, which can report a pool
   as truncated when the cap is reached but no candidate was omitted. The final
   page still ranks the selected pool; its separate `truncated` flag is based on a
   page-limit-plus-one sentinel. The exact key and any wire-visible delta need
   product-contract approval before code. A proposed global exact top-K policy
   instead needs a proven complete scoring algorithm under the same budget.
5. Keep each request on one source/index snapshot. The current offset API does
   not promise stable cross-request pages when content or grants change. A
   future stable cursor would require retained source rows as well as retained
   index versions and a full rank key; this design does not claim that feature.
   A version token must never preserve authorization after a grant is revoked:
   recheck current grants on every page, then reject or restart a stale cursor.
   The current offset API remains unchanged until a separate wire-contract
   decision.
6. On index lag, partial publication, checksum failure, or unknown version,
   do not return a silently incomplete answer. Use the existing exact read if
   it can finish within the deadline; otherwise return an explicit retrieval
   failure/partial state defined by the API contract. No automatic change to
   the requested repository scope or candidate cap is allowed.

## Source/index visibility design gate

The current `content_entities` rows have no generation key, and content-store
batch upserts/deletes can commit as separate statements. A detached asynchronous
posting index with only read-time verification prevents false positives but
cannot repair missing postings, deletions, or renamed content. A PostgreSQL
snapshot makes the current rows internally consistent at one instant; it does
not prove that all files and entities represent one completed collector run.

The recommended overlay protocol is:

1. Inventory **every** source writer, direct batch method, reaper, and
   retention delete. In the same PostgreSQL transaction as each source-row
   change, write the replacement delta or tombstone keyed by stable source
   identity and version. Preserve concurrent writes to unrelated identities.
   A plain outbox or change-data-capture tail without a complete read overlay
   is not sufficient.
2. Build immutable compressed posting segments from one consistent source
   snapshot. Use a manifest that records the covered committed delta boundary
   and a durable validation checksum. A raw sequence number is not a safe
   commit-completeness watermark: PostgreSQL sequences are nontransactional
   ([transaction isolation](https://www.postgresql.org/docs/current/transaction-iso.html)).
3. In one read snapshot, merge active segments with all uncovered exact deltas
   and tombstones, authorize/filter, then iterate in candidate order and
   `ILIKE`-verify against source rows until the cap-plus-one **verified** match
   or exhaustion. Only verified matches enter the selected pool; a rejected
   candidate cannot consume its quota. A missing segment, incomplete overlay,
   or stale manifest must
   fail closed or use the existing exact route—not return incomplete success.
4. Publish a new manifest atomically only after source/index coverage proof.
   Keep old manifests/segments for existing readers; retire only deltas known
   covered and only after reader retention. Rebuilds must be idempotent and
   reject late stale publishers.

Two other architectures remain comparison controls, not parallel
implementations:

- A fully transactional same-store posting table can be screened if the
  compressed segment overlay fails; it may carry unacceptable per-write
  amplification, which must be measured rather than presumed.
- A full per-repository generation read model can offer stronger repo-wide
  publication, but duplicates content and changes the current visibility
  model. It requires a separate contract and capacity decision.

All alternatives require an idempotent rebuild path, bounded recovery and
retirement, an operator-visible index state and lag, and a rollback that
restores the current exact PostgreSQL read. The existing parallel route shares
one exported read snapshot; a new lane must preserve that guarantee. Do not
use a global repository barrier, single writer, or stale-index best effort to
manufacture latency.

## Budget and proof admission

These are proposed **gates**, not measured capacity or approved spend:

| Property | Admission gate before implementation |
| --- | --- |
| Accuracy | Exhaustive uncapped row/rank parity on the owned corpus; approved, deterministic capped delta; wildcard, Unicode, tie, empty, grant and file-priority cases. |
| Endpoint latency | Identical source snapshot and backend, with only the candidate index as the intended storage difference; stable data/storage within each variant during interleaved full-body A/B. Under 1 s on the 16-term unscoped request, with median and p95 reported. A deployed ops-qa check remains separate. |
| Storage | Record source heap, indexes, TOAST, new structures and peak dual-version/backfill bytes. Set a measured bytes-per-repository and total-space ceiling from actual corpus distribution before choosing an engine. |
| Writes | Measure sustained and burst mutations, index bytes and CPU per mutation, WAL amplification, contention, and collector/reducer throughput. No new write bottleneck at the target ingest rate. |
| Freshness | Set a maximum source-to-search visibility lag, plus rebuild/retirement deadline, from a measured collector cadence and an owner-approved SLO. Failed/stale state is explicit. |
| Scale | Model 100,000 repositories from row-count, source-length, term-frequency and repository-size distributions, concurrent readers, and measured mutation rate; report skew and confidence interval, not a linear extrapolation from one average repository. |
| Operations | Trace probe, verification, rank, snapshot setup and response stages; expose low-cardinality lag/build/failure metrics and a status path without terms, source, repo IDs, or credentials in metric labels. |

The first cheap shim is an owned small, bounded sample to reject infeasible
candidate generators and validate exact predicates. It cannot prove the
endpoint or the 100,000-repository model. Only a surviving candidate proceeds
to a representative fixed content copy and built-binary A/B. The already
tested generated row-per-trigram shape had poor storage and ordered-query
results; that rejects that physical shape, not every index architecture.

## Costed fixed-corpus test plan (not authorized to launch)

The last read-only ops-qa sample estimated 2.67 million entity rows and
184,745 file rows, with about 10.23 GB of table-plus-index relation size for
the selected content slice. Heap/TOAST data for three selected relations was
about 4.23 GB before compression. These are point-in-time sizing inputs,
not an export size or disk upper bound. The prior logical-copy attempt stopped
on reader replay lag; its 6.6 MB partial archive is unusable. Peer-owned
preserved test volumes are excluded.

| Tier | Purpose and destination | Resource/cost envelope | Required approval and stop gate |
| --- | --- | --- | --- |
| Sample | New isolated local database using at most 150,000 sampled entities and 512 MiB export, no network listener; reject-only exactness/plan screen. The development filesystem had 829 GiB free at this plan's inspection. | Proposed 8 CPU, 16 GiB RAM, 4 GiB writable cap, 120 s source-export bound; incremental cloud storage cost $0. Local time and source load still count. | New destination-specific consent for source data; finish and independently review hard export/storage bounds, cancellation and source-health watcher. No launch from the existing NO-LAUNCH scaffold. |
| Representative | Task-owned, isolated same-account PostgreSQL physical copies with current schema/content and no upstream replication link; two exact-SHA API binaries outside the live Service. The old read-only scheduling copy plan is **not** an index-ready test target. | Historical us-east-1 pricing inputs: each 3,072 GiB gp3 volume $345.76/month, snapshot upper-bound $153.60/month, example r7a.4xlarge host $1.2172/hour. At a 30-day month, **one volume/host** is about $45.86/24 h but cannot run the index A/B. **Two volumes/one host** are about $57.38/24 h; **two volumes/two hosts** about $86.60/24 h. Double for 48 h. All exclude root disk, extra IOPS/throughput, initialization, build headroom, taxes, transfer, retention beyond the window, and failed cleanup. These are not quotes or hard caps. | Separate approval of account, two volumes, host topology, expiry, dollar ceiling and teardown. Verify current pricing/quotas, encryption/key policy, source and destination identity, capacity, backup/restore method and owner isolation first. Never detach or replace the live reader PVC. |

For a physical candidate-index bakeoff, take one immutable source snapshot,
then recover two isolated standalone copies from it. Keep one as the baseline;
apply candidate DDL/backfill only to the other. The copies start with
the same source snapshot, PostgreSQL version, schema and collation; the
candidate index is the intentional storage difference. Run interleaved calls
on both after equivalent hydration, and reverse binary/host assignment where
possible to detect placement bias. Prove the clone-only recovery and snapshot
procedure in a disposable fixture before touching copied data. Never promote
the live standby, reuse its read-only copy as a writable source, or invoke
`pg_resetwal`. PostgreSQL prohibits index DDL on a hot standby
([hot-standby restrictions](https://www.postgresql.org/docs/18/hot-standby.html)).

The representative read run fingerprints table row versions, index definitions,
schema, collation, database/image/source SHAs and content counts before and
after. It records identical source data and stable storage within each variant;
the candidate index is the controlled difference. Two warm-ups
per variant precede interleaved ABBA full-body calls, with a bounded control
window and invalidation rules for load, replication, writes or restarts. It
compares exact uncapped responses and the separately approved capped policy,
then reports median, p95, tail errors, CPU, memory and read I/O. After this
fixed read window, a **separately authorized mutation replay** runs the same
bounded insert/update/delete stream against both isolated writable copies.
It compares committed-row truth, WAL bytes, ingest throughput, CPU, I/O,
contention, recovery and index/delta lag, with no collector or ops-qa writer
connection. Recreate both copies from the immutable snapshot before any
repeat of the read A/B; a post-replay database is not the fixed read corpus.
The 24/48-hour estimates assume both phases finish inside that window; price
more hours before approval if the replay/backfill bound exceeds it.
This storage replay does **not** establish collector/reducer throughput. A
separate task-owned integration run through those services is required before
implementation acceptance and needs its own topology, duration and cost
approval; it is not included in this clone estimate.
Finally, a read-only deployed ops-qa measurement checks the candidate on the
live Neo4j/PostgreSQL topology. Neither the fixed-copy result nor a local
sample alone closes #7033.

Execution packet for a separately approved run: freeze the exact snapshot ID,
source Pod/volume identity, image digest, schema and row fingerprints, target
account/AZ, two target volume IDs, host IDs, owners, TTL, quoted prices and
maximum bill. Start from the isolated copies only after health and capacity
preflights; deny outbound replication and writer routes to ops-qa. Run a
bounded baseline control, build the candidate index with disk/WAL/CPU and
source-health stop gates, then measure the interleaved A/B. On any guard or
accuracy failure, stop only task-owned API/database instances and retain logs
without source text. Verify instance, volume and snapshot teardown by exact
IDs after the agreed retention window; a failed teardown escalates immediately
because hourly charges continue. The prior source-dump cancellation defect
must be fixed and independently reviewed if a logical copy is selected
instead of physical copies.

## Current stop state

No existing exact ordered primitive or owned fixed representative corpus has
passed admission. Do not start implementation, a source copy, DDL, or a PR
claiming the under-1-s target from this document. First obtain the capped
policy and freshness/storage/write budgets, prove a candidate with the cheap
shim, and get destination-specific consent for any content copy. The issue
remains open until an independently reviewed change passes the fixed-corpus
and deployed measurements.

Evidence: `go/internal/query/content_reader_code_topic.go`,
`go/internal/query/codetopicparallel/reader.go`,
`go/internal/query/content_reader_search_page.go`,
`go/internal/query/codequery/topic.go`,
`go/internal/storage/postgres/schema_content_store.go`,
`go/internal/storage/postgres/content_store_writes.go`,
`docs/internal/evidence/7033-exact-code-topic-parallel.md`, and the private
admission/copy-preflight records under run `eshu7033-20261005-NAsTAI`.
