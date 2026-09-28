// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package linksfreshnessstore

// journalLockSlot is the second key of the advisory lock that makes one
// journal pass exclusive across reducer replicas. It is slot 0 of
// SlotLockClass; full-link slots start at 1, so the two never collide. A
// pass that misses the lock skips the cycle: it never waits.
const journalLockSlot = 0

// backfillChainsQuery lists, for at most $1 scopes that have an active
// generation and no activation row yet, the chain of retained generations
// from the newest activated full generation forward to the active one (#7127
// ruling 2.8). The next link of the chain is the generation whose activated_at
// equals the previous one's superseded_at, which one Ack writes from a single
// clock reading. A scope whose walk is ambiguous (two candidates at one
// depth) or does not reach the active generation returns no rows: the sweeper
// then journals only its active generation. Rows come back in chain order.
//
// ingestion_scopes is small (one row per scope) and each walk reads
// scope_generations by its scope_id-leading indexes. The recursion is capped at
// 64 steps, well above the retained generations of a scope.
const backfillChainsQuery = `
WITH RECURSIVE candidates AS (
    SELECT scope.scope_id, scope.active_generation_id
    FROM ingestion_scopes AS scope
    WHERE scope.active_generation_id IS NOT NULL
      AND NOT EXISTS (
          SELECT 1 FROM changed_since_activations AS journal
          WHERE journal.scope_id = scope.scope_id
      )
    ORDER BY scope.scope_id
    LIMIT $1
),
start AS (
    SELECT DISTINCT ON (candidate.scope_id)
           candidate.scope_id, candidate.active_generation_id,
           generation.generation_id, generation.activated_at, generation.superseded_at
    FROM candidates AS candidate
    JOIN scope_generations AS generation ON generation.scope_id = candidate.scope_id
    WHERE generation.is_delta = FALSE AND generation.activated_at IS NOT NULL
    ORDER BY candidate.scope_id, generation.activated_at DESC, generation.generation_id
),
chain AS (
    SELECT scope_id, active_generation_id, generation_id, ''::text AS prior_generation_id,
           activated_at, superseded_at, 1 AS depth
    FROM start
    UNION ALL
    SELECT link.scope_id, link.active_generation_id, next.generation_id, link.generation_id,
           next.activated_at, next.superseded_at, link.depth + 1
    FROM chain AS link
    JOIN scope_generations AS next
      ON next.scope_id = link.scope_id AND next.activated_at = link.superseded_at
    WHERE link.generation_id <> link.active_generation_id
      AND link.superseded_at IS NOT NULL
      AND link.depth < 64
),
valid AS (
    SELECT scope_id
    FROM chain
    GROUP BY scope_id
    HAVING bool_or(generation_id = active_generation_id)
       AND count(*) = count(DISTINCT depth)
)
SELECT chain.scope_id, chain.generation_id, chain.prior_generation_id, chain.activated_at
FROM chain
JOIN valid ON valid.scope_id = chain.scope_id
ORDER BY chain.scope_id, chain.depth
`

// insertActivationQuery journals one backfilled activation through its
// generation row (arbiter ruling arb-7127-3d, C3): the row is inserted only
// while the generation exists, and the pass then holds that generation FOR
// KEY SHARE until it commits, so generation retention (FOR UPDATE SKIP
// LOCKED) skips it and its ledger delete sees the activation. A generation
// that retention holds or has pruned returns no row and inserts nothing,
// without waiting. ON CONFLICT makes a replayed row a no-op. activation_seq
// is assigned in insert order.
//
// $1 scope_id, $2 generation_id, $3 prior (an empty string stores NULL), $4 source,
// $5 activated_at.
const insertActivationQuery = `
INSERT INTO changed_since_activations (scope_id, generation_id, prior_generation_id, source, activated_at)
SELECT generation.scope_id, generation.generation_id, NULLIF($3, ''), $4, $5
FROM scope_generations AS generation
WHERE generation.generation_id = $2 AND generation.scope_id = $1
FOR KEY SHARE OF generation SKIP LOCKED
ON CONFLICT (scope_id, generation_id) DO NOTHING
`

// journalActiveGenerationsQuery journals every active generation that has no
// activation row, with a NULL prior: the sweeper cannot know which
// generation Ack superseded. An activation that bypassed Ack is caught here.
// It reads ingestion_scopes (one row per scope) and probes the journal's
// unique (scope_id, generation_id) index per scope.
//
// $1 activated_at fallback for a generation without activated_at.
const journalActiveGenerationsQuery = `
INSERT INTO changed_since_activations (scope_id, generation_id, prior_generation_id, source, activated_at)
SELECT scope.scope_id, scope.active_generation_id, NULL, 'sweeper', COALESCE(generation.activated_at, $1)
FROM ingestion_scopes AS scope
LEFT JOIN scope_generations AS generation ON generation.generation_id = scope.active_generation_id
WHERE scope.active_generation_id IS NOT NULL
  AND NOT EXISTS (
      SELECT 1 FROM changed_since_activations AS journal
      WHERE journal.scope_id = scope.scope_id
        AND journal.generation_id = scope.active_generation_id
  )
ORDER BY scope.scope_id
ON CONFLICT (scope_id, generation_id) DO NOTHING
`

// createCursorsQuery creates the cursor row of every journaled scope that has
// none, in the journal pass's short transaction, never in a link transaction.
//
// $1 digest_version, $2 updated_at.
const createCursorsQuery = `
INSERT INTO changed_since_scope_cursor (scope_id, state_generation_id, state_activation_seq, digest_version, updated_at)
SELECT DISTINCT journal.scope_id, NULL::text, 0::bigint, $1::smallint, $2::timestamptz
FROM changed_since_activations AS journal
WHERE NOT EXISTS (SELECT 1 FROM changed_since_scope_cursor AS cursor WHERE cursor.scope_id = journal.scope_id)
ON CONFLICT (scope_id) DO NOTHING
`

// backlogScopesQuery lists up to $1 scopes with an activation above their
// cursor, oldest pending activation first, skipping scopes whose head is
// backing off until after $2 (now). A scope with no cursor row has never been
// linked and counts from zero. The list is a hint: the link transaction
// re-reads the head under the cursor lock (#7127 ruling 8.10). The journal is bounded by
// generation retention (PR-3d) and holds a few thousand rows, so the scan of
// it is accepted.
const backlogScopesQuery = `
SELECT journal.scope_id
FROM changed_since_activations AS journal
LEFT JOIN changed_since_scope_cursor AS cursor ON cursor.scope_id = journal.scope_id
WHERE journal.activation_seq > COALESCE(cursor.state_activation_seq, 0)
  AND (cursor.next_attempt_at IS NULL OR cursor.next_attempt_at <= $2)
GROUP BY journal.scope_id
ORDER BY min(journal.activation_seq)
LIMIT $1
`

// backlogStatsQuery returns the activation rows above their scope cursor, the
// age of the oldest one in seconds (0 when there is none), and the fleet's
// retrying scopes (a counted failure pending) and poisoned scopes (marker
// set). It is computed in SQL so any replica reports the fleet.
//
// $1 now.
const backlogStatsQuery = `
SELECT count(*),
       COALESCE(EXTRACT(EPOCH FROM ($1::timestamptz - min(journal.activated_at))), 0)::float8,
       (SELECT count(*) FROM changed_since_scope_cursor WHERE attempt_count > 0),
       (SELECT count(*) FROM changed_since_scope_cursor WHERE poisoned_activation_seq IS NOT NULL)
FROM changed_since_activations AS journal
LEFT JOIN changed_since_scope_cursor AS cursor ON cursor.scope_id = journal.scope_id
WHERE journal.activation_seq > COALESCE(cursor.state_activation_seq, 0)
`

// ledgerSizeQuery reads the state table's and the link-delta table's total
// size and the planner's row estimate from the catalog. It reads no table
// rows.
const ledgerSizeQuery = `
SELECT pg_total_relation_size('changed_since_key_state'::regclass),
       GREATEST(c_state.reltuples, 0)::bigint,
       pg_total_relation_size('changed_since_link_deltas'::regclass),
       GREATEST(c_delta.reltuples, 0)::bigint
FROM pg_class AS c_state, pg_class AS c_delta
WHERE c_state.oid = 'changed_since_key_state'::regclass
  AND c_delta.oid = 'changed_since_link_deltas'::regclass
`

// ledgerOrphansQuery is the orphan probe of arbiter ruling arb-7127-3d: links
// naming a generation (or a non-empty prior) with no scope_generations row,
// activation rows of such a generation, and bucket-count groups with no link
// row. It scans the links, the activations and the bucket counts, never the
// link deltas, and anti-joins scope_generations by its primary key. It costs
// about 100 ms at 25,000 links and 192,000 bucket rows, so the runner samples
// it at most once a minute (#7127 PR-3e evidence).
const ledgerOrphansQuery = `
SELECT
  (SELECT count(*) FROM changed_since_links AS l
    WHERE NOT EXISTS (SELECT 1 FROM scope_generations AS g WHERE g.generation_id = l.generation_id)
       OR (l.prior_generation_id <> ''
           AND NOT EXISTS (SELECT 1 FROM scope_generations AS g WHERE g.generation_id = l.prior_generation_id))),
  (SELECT count(*) FROM changed_since_activations AS a
    WHERE NOT EXISTS (SELECT 1 FROM scope_generations AS g WHERE g.generation_id = a.generation_id)),
  (SELECT count(*) FROM (SELECT DISTINCT scope_id, generation_id, prior_generation_id
                         FROM changed_since_link_bucket_counts) AS b
    WHERE NOT EXISTS (SELECT 1 FROM changed_since_links AS l WHERE l.scope_id = b.scope_id
                        AND l.generation_id = b.generation_id AND l.prior_generation_id = b.prior_generation_id))
`

// orphanScopesQuery lists up to $1 scopes that have ledger rows (a cursor or
// an activation) but no ingestion_scopes row. Both tables hold at most a few
// rows per scope.
const orphanScopesQuery = `
SELECT scope_id FROM (
    SELECT scope_id FROM changed_since_scope_cursor
    UNION
    SELECT DISTINCT scope_id FROM changed_since_activations
) AS ledger
WHERE NOT EXISTS (SELECT 1 FROM ingestion_scopes AS scope WHERE scope.scope_id = ledger.scope_id)
ORDER BY scope_id
LIMIT $1
`

// lockOrphanCursorQuery fences an orphan scope against a running link: a
// cursor row held by a writer returns no row and the scope waits for the
// next pass. The existence read tells a held cursor from a missing one.
const (
	lockOrphanCursorQuery   = `SELECT 1 FROM changed_since_scope_cursor WHERE scope_id = $1 FOR UPDATE SKIP LOCKED`
	orphanCursorExistsQuery = `SELECT 1 FROM changed_since_scope_cursor WHERE scope_id = $1`
)

// deleteOrphanScopeStatements remove one deleted scope's ledger rows, each by
// the scope_id prefix of the table's primary key or index.
var deleteOrphanScopeStatements = []string{
	`DELETE FROM changed_since_link_deltas WHERE scope_id = $1`,
	`DELETE FROM changed_since_link_bucket_counts WHERE scope_id = $1`,
	`DELETE FROM changed_since_links WHERE scope_id = $1`,
	`DELETE FROM changed_since_key_state WHERE scope_id = $1`,
	`DELETE FROM changed_since_activations WHERE scope_id = $1`,
	`DELETE FROM changed_since_scope_cursor WHERE scope_id = $1`,
}
