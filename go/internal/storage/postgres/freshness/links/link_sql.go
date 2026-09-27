// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

// SlotLockClass is the first key of the two-integer advisory lock that holds
// one full-link slot: pg_try_advisory_xact_lock(SlotLockClass, slot).
// TestSlotLockClassDiffersFromTreeKeys derives every other two-integer lock
// class in the module from the code (SQL literals, hashtext() arguments and
// the Go constants bound to them) and fails if one equals this class or
// cannot be resolved.
const SlotLockClass = 7127

// ensureCursorQuery creates the scope's cursor row outside the link
// transaction. It is a separate autocommit statement so the link transaction
// never waits on another writer's uncommitted insert of the same row.
//
// $1 scope_id, $2 digest_version, $3 updated_at.
const ensureCursorQuery = `
INSERT INTO changed_since_scope_cursor (scope_id, state_generation_id, state_activation_seq, digest_version, updated_at)
VALUES ($1, NULL, 0, $2, $3)
ON CONFLICT (scope_id) DO NOTHING
`

// lockCursorQuery is the per-scope writer fence. SKIP LOCKED returns no row
// when another writer holds the scope, which the caller turns into a
// cursor_locked retry; it never waits.
const lockCursorQuery = `
SELECT COALESCE(state_generation_id, ''), state_activation_seq, digest_version,
       COALESCE(attempt_activation_seq, 0), attempt_count, next_attempt_at
FROM changed_since_scope_cursor
WHERE scope_id = $1
FOR UPDATE SKIP LOCKED
`

// nextActivationQuery returns the scope's oldest activation above the cursor.
// The (scope_id, activation_seq) index serves it.
const nextActivationQuery = `
SELECT activation_seq, generation_id, COALESCE(prior_generation_id, ''), prior_generation_id IS NULL
FROM changed_since_activations
WHERE scope_id = $1 AND activation_seq > $2
ORDER BY activation_seq
LIMIT 1
`

// generationExistsQuery is the plain existence read before the generation
// lock: only an absent row is a pruned_before_link break.
const generationExistsQuery = `
SELECT is_delta
FROM scope_generations
WHERE generation_id = $1 AND scope_id = $2
`

// lockGenerationQuery keeps generation retention off the activating
// generation for the link's lifetime. Retention locks candidates FOR UPDATE
// SKIP LOCKED and so skips it; KEY SHARE does not conflict with Ack's status
// updates. A row held by retention returns no row: generation_locked, retry.
const lockGenerationQuery = `
SELECT 1
FROM scope_generations
WHERE generation_id = $1 AND scope_id = $2
FOR KEY SHARE SKIP LOCKED
`

// trySlotQuery takes one full-link slot for the transaction's lifetime.
const trySlotQuery = `SELECT pg_try_advisory_xact_lock($1::integer, $2::integer)`

// Transaction-local settings for a full link (#7127 ruling 8.3). A link runs
// once per activation and takes seconds; a cached generic plan cannot know
// the scope's size and planning is milliseconds.
const (
	setWorkMemStatement       = `SET LOCAL work_mem = '256MB'`
	setPlanCacheModeStatement = `SET LOCAL plan_cache_mode = force_custom_plan`
)

// setStatementTimeoutStatement is built from the configured timeout in
// milliseconds; SET does not take bind parameters.
const setStatementTimeoutStatementPrefix = `SET LOCAL statement_timeout = `

// clearStateQuery removes a scope's state rows before a root. Rows exist here
// only after a digest_version change; a first root deletes nothing.
const clearStateQuery = `DELETE FROM changed_since_key_state WHERE scope_id = $1`

// keyAggregateCTE aggregates the activating generation once: one row per
// (category, key) with the multiset state digest of its active rows, whether
// the generation tombstones it, its kinds and its owner path. It is the only
// fact_records scan of a link. The OFFSET 0 fence keeps the per-row sha256
// below the sort, so the sort carries 32-byte digests, not payloads.
//
// $1 scope_id, $2 activating generation.
const keyAggregateCTE = `
cur AS MATERIALIZED (
    SELECT cat, stable_fact_key,
           MIN(fact_kind) FILTER (WHERE NOT is_tombstone) AS kind,
           MIN(fact_kind) AS any_kind,
           sha256(string_agg(h, ''::bytea ORDER BY h) FILTER (WHERE NOT is_tombstone)) AS state,
           bool_or(is_tombstone) AS tombstoned,
           MIN(source_uri) FILTER (WHERE NOT is_tombstone) AS owner_uri
    FROM (
        SELECT CASE WHEN fact_kind = 'file' THEN 'files'
                    WHEN fact_kind = 'content_entity' THEN 'content_entities'
                    ELSE 'facts' END AS cat,
               stable_fact_key, fact_kind, is_tombstone, source_uri,
               CASE WHEN is_tombstone THEN NULL
                    ELSE sha256(convert_to((` + PayloadDigestInput + `)::text, 'UTF8')) END AS h
        FROM fact_records
        WHERE scope_id = $1 AND generation_id = $2 AND ` + ExcludeReducerDerivedKinds + `
        OFFSET 0
    ) AS r
    GROUP BY cat, stable_fact_key
)`

// RootLinkSQL builds a scope's state from one full generation and records
// a root link (an empty prior). It writes no link deltas: a root has no prior.
//
// $1 scope_id, $2 activating generation, $3 digest_version, $4 computed_at.
const RootLinkSQL = `
WITH ` + keyAggregateCTE + `,
ups AS (
    INSERT INTO changed_since_key_state (scope_id, fact_category, stable_fact_key, fact_kind, state, owner_uri)
    SELECT $1, cat, stable_fact_key, kind, state, owner_uri FROM cur WHERE state IS NOT NULL
    RETURNING fact_category
),
lnk AS (
    INSERT INTO changed_since_links
        (scope_id, generation_id, prior_generation_id, link_kind, digest_version, delta_rows,
         files_keys, content_entities_keys, facts_keys, computed_at)
    SELECT $1, $2, '', 'root', $3, 0,
           count(*) FILTER (WHERE fact_category = 'files'),
           count(*) FILTER (WHERE fact_category = 'content_entities'),
           count(*) FILTER (WHERE fact_category = 'facts'),
           $4
    FROM ups
    RETURNING delta_rows, files_keys, content_entities_keys, facts_keys
)
SELECT delta_rows, files_keys, content_entities_keys, facts_keys FROM lnk
`

// IncrementalLinkSQL links the scope's state at $3 to the full generation
// $2 in one statement (#7127 ruling 8.3, shape L1b): aggregate $2, full-join
// it with the scope's state rows, record every differing or tombstoned key as
// a link delta with its classification, count the buckets, and move the state
// to $2. A full generation can drop any key, so every state row of the scope
// is visited; the state side is read by the primary key's scope_id prefix.
// The result row ends with the deleted, upserted and bucket counts, the
// expected deletes and upserts, and the deleted rows of another scope; the
// writer fails the link unless they agree (the row-count invariant).
//
// Classification per key, matching changedSinceDeltaQuery for two full
// generations: no prior and no current active row is dropped (a tombstone of
// a key that was not live); no prior is added; no current and tombstoned is
// retired; no current is superseded; differing digests are updated; an equal
// digest with a changed kind is unchanged (the row exists to move the state's
// kind).
//
// $1 scope_id, $2 activating generation, $3 state generation (prior),
// $4 digest_version, $5 computed_at.
const IncrementalLinkSQL = `
WITH ` + keyAggregateCTE + `,
diff AS MATERIALIZED (
    SELECT COALESCE(s.fact_category, c.cat) AS cat,
           COALESCE(s.stable_fact_key, c.stable_fact_key) AS k,
           s.fact_kind AS prior_kind, COALESCE(c.kind, c.any_kind) AS current_kind,
           s.state AS prior_state, c.state AS current_state,
           COALESCE(c.tombstoned, FALSE) AS tombstoned, c.owner_uri,
           s.state_ctid
    FROM (SELECT ctid AS state_ctid, * FROM changed_since_key_state WHERE scope_id = $1) AS s
    FULL JOIN cur AS c ON c.cat = s.fact_category AND c.stable_fact_key = s.stable_fact_key
    WHERE s.state IS DISTINCT FROM c.state
       OR (c.state IS NOT NULL AND s.fact_kind IS DISTINCT FROM c.kind)
       OR COALESCE(c.tombstoned, FALSE)
),
ins AS (
    INSERT INTO changed_since_link_deltas
        (scope_id, generation_id, prior_generation_id, fact_category, classification, stable_fact_key,
         prior_fact_kind, current_fact_kind, prior_state, current_state, current_tombstoned)
    SELECT $1, $2, $3, d.cat,
           CASE WHEN d.prior_state IS NULL AND d.current_state IS NULL THEN 'dropped'
                WHEN d.prior_state IS NULL THEN 'added'
                WHEN d.current_state IS NULL AND d.tombstoned THEN 'retired'
                WHEN d.current_state IS NULL THEN 'superseded'
                WHEN d.prior_state <> d.current_state THEN 'updated'
                ELSE 'unchanged' END,
           d.k, d.prior_kind, d.current_kind, d.prior_state, d.current_state, d.tombstoned
    FROM diff AS d
    RETURNING fact_category, classification
),
del AS (
    -- Delete by the ctid the diff read, with NO indexable predicate on the
    -- target (#7127 arbiter ruling arb-7127-g8). A key join, or a scope_id
    -- predicate here, gives the planner an index path on the state table,
    -- and with the scope estimated at one row it runs that path per diff
    -- row or filters every state row of the scope: the G8 stall. A ctid
    -- array leaves only a Tid Scan whatever the statistics. The scope guard
    -- is RETURNING t.scope_id, checked in Go with the row counts below:
    -- under a broken fence EvalPlanQual skips a changed row, and the counts
    -- turn that into a failed link.
    DELETE FROM changed_since_key_state AS t
    WHERE t.ctid = ANY (ARRAY(
              SELECT d.state_ctid FROM diff AS d
              WHERE d.current_state IS NULL AND d.state_ctid IS NOT NULL))
    RETURNING t.scope_id
),
ups AS (
    INSERT INTO changed_since_key_state (scope_id, fact_category, stable_fact_key, fact_kind, state, owner_uri)
    SELECT $1, cat, k, current_kind, current_state, owner_uri FROM diff WHERE current_state IS NOT NULL
    ON CONFLICT (scope_id, fact_category, stable_fact_key)
    DO UPDATE SET fact_kind = EXCLUDED.fact_kind, state = EXCLUDED.state, owner_uri = EXCLUDED.owner_uri
    RETURNING 1
),
bk AS (
    INSERT INTO changed_since_link_bucket_counts
        (scope_id, generation_id, prior_generation_id, fact_category, classification, key_count)
    SELECT $1, $2, $3, fact_category, classification, count(*)
    FROM ins
    GROUP BY fact_category, classification
    RETURNING 1
),
lnk AS (
    INSERT INTO changed_since_links
        (scope_id, generation_id, prior_generation_id, link_kind, digest_version, delta_rows,
         files_keys, content_entities_keys, facts_keys, computed_at)
    SELECT $1, $2, $3, 'incremental', $4, (SELECT count(*) FROM ins),
           count(*) FILTER (WHERE state IS NOT NULL AND cat = 'files'),
           count(*) FILTER (WHERE state IS NOT NULL AND cat = 'content_entities'),
           count(*) FILTER (WHERE state IS NOT NULL AND cat = 'facts'),
           $5
    FROM cur
    RETURNING delta_rows, files_keys, content_entities_keys, facts_keys
)
SELECT delta_rows, files_keys, content_entities_keys, facts_keys,
       (SELECT count(*) FROM del),
       (SELECT count(*) FROM ups),
       (SELECT count(*) FROM bk),
       (SELECT count(*) FROM diff WHERE current_state IS NULL AND state_ctid IS NOT NULL),
       (SELECT count(*) FROM diff WHERE current_state IS NOT NULL),
       (SELECT count(*) FROM del WHERE scope_id IS DISTINCT FROM $1)
FROM lnk
`

// advanceCursorQuery moves the cursor past one activation and clears the
// attempt fields. $2 is the new state generation: the linked generation after
// a link, or the unchanged state generation after a chain break (#7127 ruling
// 8.5 keeps the state). $6 is true for a root or incremental link, which also
// clears the poison marker (#7127 ruling 8.10).
//
// $1 scope_id, $2 state_generation_id (an empty string stores NULL),
// $3 activation_seq, $4 digest_version, $5 updated_at, $6 linked.
const advanceCursorQuery = `
UPDATE changed_since_scope_cursor
SET state_generation_id = NULLIF($2, ''),
    state_activation_seq = $3,
    digest_version = $4,
    updated_at = $5,
    attempt_activation_seq = NULL,
    attempt_count = 0,
    next_attempt_at = NULL,
    last_failure_class = NULL,
    poisoned_activation_seq = CASE WHEN $6 THEN NULL ELSE poisoned_activation_seq END,
    poisoned_at = CASE WHEN $6 THEN NULL ELSE poisoned_at END
WHERE scope_id = $1
`

// recordAttemptQuery counts one counting failure of the head activation.
//
// $1 scope_id, $2 activation_seq, $3 attempt_count, $4 next_attempt_at,
// $5 failure class, $6 updated_at.
const recordAttemptQuery = `
UPDATE changed_since_scope_cursor
SET attempt_activation_seq = $2,
    attempt_count = $3,
    next_attempt_at = $4,
    last_failure_class = $5,
    updated_at = $6
WHERE scope_id = $1
`

// poisonActivationQuery turns the head activation into a link_poisoned chain
// break: the cursor advances past it, state_generation_id and the state rows
// stay, the attempt fields clear, and the poison marker is set. The last
// failure class stays as the cause.
//
// $1 scope_id, $2 activation_seq, $3 failure class, $4 poisoned_at.
const poisonActivationQuery = `
UPDATE changed_since_scope_cursor
SET state_activation_seq = $2,
    poisoned_activation_seq = $2,
    poisoned_at = $4,
    attempt_activation_seq = NULL,
    attempt_count = 0,
    next_attempt_at = NULL,
    last_failure_class = $3,
    updated_at = $4
WHERE scope_id = $1
`
