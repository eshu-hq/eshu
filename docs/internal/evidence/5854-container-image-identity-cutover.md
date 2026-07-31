# #5854 — outcome-independent container-image identity cutover

## Result

`reducer_container_image_identity` now has one durable logical identity per
`(scope_id, generation_id, image_ref)`. Outcome remains payload, so a
reclassification collides on the same `fact_id`; an authoritative demotion
writes a tombstone at that key. Both paths carry the evidence-read fencing
token introduced by #5847. A late stale worker therefore cannot overwrite a
fresher classification or resurrect a fresher tombstone.

The change also retires exact outcome-keyed rows written before #5854. It does
not use the rejected generation-wide DELETE. The planner derives only the
legacy ID that the old writer could have produced for each evaluated
reference, and publication plus exact cleanup commits atomically.

Collector-declared incompleteness is fail-closed:

- `tag_list_truncated` holds affected tag references.
- `config_blob_unavailable` holds manifest digests mapped from the warning.
- `missing_manifest_digest` holds the named repository conservatively.
- malformed or unreadable warning state stops a destructive pass.

An all-canonical pass skips the warning read because it cannot demote a
canonical publication.

## Rolling-upgrade fence

The new logical-key token cannot conflict with an old binary that derives a
different outcome-keyed `fact_id`. Migration 088 therefore adds a temporary
format-compatibility fence:

1. New rows carry `payload.identity_format = image_ref_v2`.
2. The first new writer creates one durable
   `container_image_identity_cutovers` marker for its scope generation in the
   same transaction as publication and cleanup.
3. The marker trigger locks the exact stable reducer work item, marks it
   `container_image_identity_v2_required`, and normalizes it to
   `status='running'` with
   `container_image_identity_v2_authorized_status='running'`.
4. A statement-level transition-table trigger rejects or suppresses legacy
   outcome-keyed fact writes after the marker. It serializes only writers for
   the same scope generation; unrelated keys remain concurrent.
5. The queue claim trigger is defined as `BEFORE UPDATE OF
   container_image_identity_claim_epoch` with a domain predicate. Every target
   claim increments the epoch and authorizes `running`; unrelated claims do not
   execute the trigger body.
6. ACK, retry, and failure SQL bind the exact claim epoch and write the matching
   authorized terminal status. The row constraint requires
   `status = container_image_identity_v2_authorized_status` whenever the v2
   requirement is set. Old SQL cannot update that authorization and therefore
   cannot certify a post-cutover transition.
7. Replay and recovery write matching authorization state. An old callback
   carrying a stale epoch cannot ACK, retry, or fail a reclaimed item, including
   when the literal lease owner is reused.

The marker is durable, so later writers do not reacquire the first-cutover
compatibility lock. A partial index proves whether any active legacy row still
exists for the scope generation:

```sql
CREATE INDEX fact_records_container_image_identity_legacy_cleanup_idx
ON fact_records (scope_id, generation_id, fact_id)
WHERE fact_kind = 'reducer_container_image_identity'
  AND is_tombstone = FALSE
  AND COALESCE(payload->>'identity_format', '') <> 'image_ref_v2';
```

The lookup orders by `fact_id` with `LIMIT 1`, forcing a bounded index-only
probe in the zero-legacy steady state. When the marker and zero-legacy proof
both hold, a single-chunk pass uses the exact-claim publication-only statement;
a multi-chunk pass locks the exact claim once and retains one transaction across
bounded publication chunks. If any held legacy row remains, exact cleanup stays
enabled until that row becomes eligible.

## Prove-the-theory-first evidence

All fixtures use public synthetic values such as
`registry.example.com/performance/team-api` and placeholder SHA-256 digests.

### Correctness and concurrency theories

- A live token 9 row, token 10 tombstone, stale token 5 live write, and fresh
  token 11 live write proved the conflict fence: the stale write affected zero
  rows and the fresh write revived the tombstone.
- A cutover held for one scope generation blocked same-key old and new writers
  while an unrelated scope committed. After cutover commit, the old writer
  inserted zero rows and the fresher logical-key writer won.
- Injecting a later-chunk failure rolled back all new rows, exact cleanup, and
  the marker. A retry then converged to the complete v2 set.
- A failed migration under a held fact-table lock left no partial table,
  column, function, trigger, or constraint. Retry through `ApplyBootstrap`
  preserved rows and remained idempotent.
- The cleanup probe used the partial index in both the zero-legacy and
  one-legacy-row cases, touching only a few buffers.

### Queue and claim performance

Fixed-schema paired tests compare the old queue table with migration 088
applied. Six trials, 100 warmups, and 1,000 paired iterations per operation
cover unrelated claim, ACK, failure, target scalar ACK, and target batch ACK.
The unrelated paths remain inside the repository's 5% median, 10% p95, and
25-microsecond absolute-p95 policies. Batch sizes 1, 16, and 64 preserve exact
row selection. The live claim-trigger catalog proof requires exactly one
user-defined `fact_work_items` trigger, the `UPDATE OF claim_epoch` column list,
and the target-domain predicate. Two competing claimers produce one successful
claim and one epoch increment.

### Production writer performance

The production-handler benchmark retains three distinct lanes:

- uncached full handler, representing cache-disabled or cap-exceeded fallback;
- cache-warm full handler, representing the default reducer wiring;
- writer-only, isolating publication from the common cross-scope evidence
  load.

The 99,500-reference writer-only comparison on the same Postgres instance
produced identical logical checksums:

| variant | median | p95 | write statements | attributable WAL |
| --- | ---: | ---: | ---: | ---: |
| old outcome-keyed writer | 6.281 s | 6.470 s | 199 | 451.8 MB/op |
| final v2 writer | 4.407 s | 4.770 s | 100 | 385.9 MB/op |

These are the medians of three alternating trials per variant. The v2 writer
was 29.8% faster at median, 26.3% faster at p95, and used 14.6% less
writer-local WAL in that isolated lane.

The cache-warm production handler also returned the same checksum:

| variant | median | p95 | queries/op |
| --- | ---: | ---: | ---: |
| old outcome-keyed writer | 6.825 s | 6.995 s | 5 |
| final v2 writer | 5.241 s | 5.297 s | 8 |

The final path was 23.2% faster at median and 24.3% faster at p95. The extra
three queries are the exact cutover marker, zero-legacy probe, and claim check.

The uncached lane performs 204 unchanged paginated evidence reads at this
cardinality for both variants. One paired run measured 11.530 s main versus
11.901 s head at median, while the head write statements themselves were about
one second faster per operation. Query-call time for the shared evidence reads
varied by more than two seconds per operation, so this lane is retained as a
fallback guard and not used to attribute writer cost.

Global-LSN WAL in the cache-warm lane was checkpoint-sensitive and is not used
for an improvement claim. The isolated writer-only lane is the attributable
WAL surface.

## Live acceptance proof

Focused live tests cover:

- mixed eligible and warning-held legacy rows, then warning-clear retirement;
- marker-present seeded legacy rows;
- first-cutover rollback atomicity;
- old INSERT and UPDATE before, during, and after marker commit;
- stale exact-claim rejection for single- and multi-chunk writes;
- same-owner reclaim and replay;
- later-chunk rollback and retry;
- exact batch sizes 1, 16, and 64;
- cleanup-probe index plans;
- migration failure, retry, backfill, and repeated bootstrap.

Primary commands:

```bash
cd go
ESHU_POSTGRES_DSN='postgresql://eshu:change-me@127.0.0.1:25432/eshu' \
  go test ./internal/reducer -run '^TestPostgresContainerImageIdentity' -count=1
ESHU_POSTGRES_TEST_DSN='postgresql://eshu:change-me@127.0.0.1:25432/eshu' \
  go test ./internal/storage/postgres -run '^TestContainerImageIdentity' -count=1
ESHU_POSTGRES_TEST_DSN='postgresql://eshu:change-me@127.0.0.1:25432/eshu' \
  go test -tags perf5854_ack ./internal/storage/postgres \
  -run '^TestContainerImageIdentity.*Performance.*Live$' -count=1
ESHU_5854_PERF_DSN='postgresql://eshu:change-me@127.0.0.1:25432/eshu' \
ESHU_5854_PERF_VARIANT=head \
  go test -tags perf5854_head ./cmd/reducer \
  -run '^TestContainerImageIdentity.*PerformanceLive$' -count=1
```

The final promotion sequence also requires the live golden-corpus gate after
the last edit, preliminary and final `eshu-code-review` verdicts with
P0=P1=P2=0, and `make pre-pr` between those reviews.

## Observability

`eshu_dp_container_image_identity_retirements_total` uses bounded `domain` and
`outcome` labels. Outcomes are `retirement_attempted`, `legacy_deleted`, and
`held_<reason>`. `retirement_attempted` is intentionally not named
`tombstoned`: a fresher row can reject an attempted publication at the conflict
fence. Existing Postgres query-duration metrics, reducer execution metrics,
status, and traces expose cutover or claim failures without high-cardinality
labels.
