# Reducer Gotchas — Container Image Identity

Split from `gotchas-supply-chain-and-vulnerabilities.md` to keep reducer
documentation bounded. The detailed migration and performance record is
`docs/internal/evidence/5854-container-image-identity-cutover.md`.

## Digest-first admission

`ContainerImageIdentityHandler` writes `reducer_container_image_identity` facts
for explicit digest references or a tag resolved to one digest. Ambiguous,
unresolved, and stale tag outcomes remain diagnostic until stronger evidence
proves safe identity.

Git parser facts can expose image references through
`entity_metadata.container_images`; the reducer accepts the older
`metadata.container_images` fixture shape for compatibility. CI/CD
`container_image` artifacts can seed identity when they carry a digest. A
matching CI run contributes its repository anchor, and immutable digests outrank
mutable tags. Digest-only artifacts with multiple observed registry
repositories remain ambiguous.

## Logical identity and stale-writer fence

The durable logical key is `(scope_id, generation_id, image_ref)`. Outcome is
payload, not identity. A reclassification therefore collides on the same
`fact_id`; an authoritative demotion writes a tombstone at that key.

Every write carries `ContainerImageIdentityWrite.EvidenceAsOf`, captured before
the handler's first fact load and persisted as `fact_records.fencing_token`.
The conflict update is guarded:

```sql
WHERE fact_records.fencing_token <= EXCLUDED.fencing_token
```

The guard rejects a stale pass whole, content included. Raising only the token
while assigning stale content would advertise freshness the payload does not
have. Equal tokens remain accepted so retry, redelivery, and later chunks of
the same pass are idempotent. A missing evidence watermark is a hard error.

## Authoritative retirement

The planner derives only the legacy outcome-keyed ID the old writer could have
published for each evaluated reference. Publication, tombstone, and exact
legacy cleanup share one transaction. It never uses a generation-wide DELETE.

Collector incompleteness blocks destructive absence:

- `tag_list_truncated` holds affected tag references.
- `config_blob_unavailable` holds mapped manifest digests.
- `missing_manifest_digest` holds the named repository conservatively.
- malformed, unreadable, or unavailable warning-loader state stops the
  destructive pass before the writer runs.

An all-canonical pass skips the warning read because it cannot demote a
canonical publication.

## Rolling-upgrade compatibility

Migration 088 prevents an old binary from recreating an outcome-keyed row after
the new writer cleans it:

- v2 rows declare `payload.identity_format=image_ref_v2`;
- the first v2 writer atomically creates a durable scope-generation cutover
  marker, publishes v2 rows, and removes exact eligible legacy rows; an empty
  cleanup list never bypasses this fence;
- the marker trigger locks the exact reducer work item and sets
  `container_image_identity_v2_required`;
- the queue claim trigger increments
  `container_image_identity_claim_epoch` only for this domain;
- ACK, retry, failure, replay, and recovery bind the epoch and maintain
  `container_image_identity_v2_authorized_status`;
- the row constraint requires a marked row's status to equal its authorized
  status, so old terminal SQL cannot certify a post-cutover transition;
- the statement-level legacy fact guard suppresses old-format writes after the
  marker while leaving unrelated scope generations concurrent.

A partial legacy-row index backs a bounded `ORDER BY fact_id LIMIT 1` cleanup
probe. Marker plus zero-legacy proof enables the publication-only steady-state
path. Held legacy rows keep exact cleanup enabled until the warning clears.

## Operational signals

`eshu_dp_container_image_identity_decisions_total` records decision outcomes.
`eshu_dp_container_image_identity_retirements_total` records bounded outcomes:
`retirement_attempted`, `legacy_deleted`, and `held_<reason>`. Existing reducer
run spans, execution status, and Postgres query-duration metrics expose
failures and latency without high-cardinality labels.

## Verification contract

Promotion requires:

- mixed eligible and held rows followed by warning-clear retirement;
- tombstone stale-resurrection and fresh-revival proof;
- old-writer INSERT/UPDATE behavior before, during, and after marker commit;
- exact-epoch rejection after reclaim and replay;
- first-cutover and later-chunk rollback atomicity;
- unrelated-key concurrency;
- migration retry, backfill, and idempotence;
- partial-index cleanup probe plans;
- writer-only, cache-warm, and uncached production-handler performance lanes;
- the live golden-corpus gate.
