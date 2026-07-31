# Reducer Gotchas — Container Image Identity

Split from `gotchas-supply-chain-and-vulnerabilities.md` to keep reducer
documentation bounded. The format-cutover evidence is recorded in
`docs/internal/evidence/5854-container-image-identity-cutover.md`; the
digest-v3 canonicalization proof is in
`docs/internal/evidence/5740-container-image-identity-canonicalization.md`.

## Digest-first admission

`ContainerImageIdentityHandler` admits only explicit digest references or a
tag resolved to one digest. Ambiguous, unresolved, and stale tag outcomes
remain diagnostic until stronger evidence proves safe identity.

Git parser facts can expose image references through
`entity_metadata.container_images`; the reducer accepts the older
`metadata.container_images` fixture shape for compatibility. CI/CD
`container_image` artifacts can seed identity when they carry a digest. A
matching CI run contributes its repository anchor, and immutable digests
outrank mutable tags. Digest-only artifacts observed under multiple registry
repositories remain ambiguous.

## Digest-v3 authority

The durable logical identity is the image digest. Each independent evidence
path becomes one normalized row in `container_image_identity_supports`; rows
with the same digest deliberately coexist when their image reference,
repository anchor, provenance, or evidence differs. The stable compatibility
`fact_id` and `canonical_id` are derived from the digest only.

Each reducer pass constructs a complete, immutable support set. The set ID is a
content hash of its normalized rows plus the scope, so an unchanged replay is
idempotent across generations. Publication inserts the set and its supports,
then moves `container_image_identity_scope_state.active_set_id` atomically.
Readers never union historical sets.

Before a scope's first digest-v3 publication, the compatibility view exposes
active-generation legacy `fact_records`. Once `active_set_id` is non-null,
only the active typed set is authoritative; the same fenced statement removes
the exact scope generation's legacy rows. A trigger rejects later legacy
writes for that scope.

## Lifecycle and claim fences

The handler snapshots `activation_epoch` before loading evidence. Generation
activation increments that epoch and clears `active_set_id`, so stale truth
becomes invisible immediately, including an activation ABA that returns to the
same generation ID.

The final publication locks and verifies all of these in one statement:

- exact scope and active generation;
- exact activation epoch;
- exact reducer work-item ID and claim epoch;
- both v2 and v3 queue authorization latches.

A stale or reclaimed worker therefore cannot move the pointer. Empty output is
published as an explicit empty set rather than leaving old authority visible.

## Authoritative retirement and holds

Collector incompleteness blocks destructive absence:

- `tag_list_truncated` holds affected tag references;
- `config_blob_unavailable` holds mapped manifest digests, widening only to
  that warning's repository when the active manifest cannot map its config;
- `missing_manifest_digest` holds the named repository conservatively;
- malformed, unreadable, or unavailable warning-loader state stops the
  destructive pass before the writer runs.

Only a pass with held decisions loads prior support. That read is bounded by
the exact scope, generation, activation epoch, and normalized held image
references. It reads the current typed set, or active-generation legacy rows
only while `active_set_id` is null; it never falls back to `last_set_id` from a
superseded generation. Current and retained rows are normalized, assigned new
semantic support IDs, deduplicated, sorted, and hashed together before the
atomic publication fence. A hold with no prior support invents nothing. When
the warning clears, omission from the next complete set retires the support.

An all-canonical pass performs no prior-support read because it cannot demote a
canonical publication.

## Bounded reads and indexes

Public query surfaces call `container_image_identity_current_facts_for` with at
least one selector and a keyset cursor plus result limit. It selects digests,
then folds their supports into one presentation row per digest.

Reducer consumers call
`container_image_identity_current_support_facts_for` instead. It returns one
bounded envelope per immutable support so image, repository, source repository,
build provenance, and runtime fields stay correlated until the owning reducer
applies its established selection rules. Its cursor encodes the ordered
scope/digest/support tuple; foreign cursors from unioned fact kinds are handled
by namespace ordering rather than decoded as support cursors. Pre-pointer
legacy rows use the same adapter only while a scope has no active typed set.

Typed support indexes are scoped by `set_id`: the primary key covers
`(set_id, digest, support_id)`, B-trees cover image reference, repository, and
outcome selectors, and GIN covers `source_repository_ids`. Held-support loads
reuse the image-reference B-tree after resolving the exact active set.

## Operational signals

`eshu_dp_container_image_identity_decisions_total` records decision outcomes.
`eshu_dp_container_image_identity_retirements_total` records bounded outcomes:
`retirement_attempted`, `legacy_deleted`, and `held_<reason>`. Existing reducer
execution/run duration and Postgres query-duration telemetry cover the support
load and atomic publication without new high-cardinality labels.

## Verification contract

Promotion requires:

- typed-set and pre-v3 legacy support carry while a warning holds;
- warning-clear retirement and a hold with no prior support;
- exact-claim rejection and activation-ABA rejection after prior support loads;
- current-plus-retained semantic deduplication and replay idempotence;
- explicit empty-set publication and absence of v3 `fact_records` shadows;
- bounded read plans on a production-shaped support set;
- one-row and representative worst-case held-support loader plans;
- concurrent shared, disjoint, and partially overlapping publication proof;
- the live golden-corpus gate.
