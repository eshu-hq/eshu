-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7279: generation retention asks, per candidate content_entity key, whether
-- any generation outside the batch still holds a live fact for it. Without a
-- key index that question is a grouped pass over every live content_entity
-- fact, run while the batch holds FOR UPDATE on its ingestion_scopes rows, so
-- an FK insert into one of those scopes waited 17.8 s and 32.5 s on an 11 GB
-- fixture. With this index each candidate key is one bounded probe: the lock
-- hold fell to 1.0-1.2 s and its buffers stayed flat when the table tripled.
--
-- The key columns only, not generation_id: generation_id <> ALL($1) is not
-- btree-indexable, and leaving it out lets deduplication fold a key's repeats
-- across generations (91 MB against 407 MB on the fixture). The predicate
-- repeats the retention statements' literal filters so a generic plan can prove
-- it. The retention store refuses a cycle until this index is valid.
--
-- Built on cold bootstrap too: a btree over two kinds costs no measurable
-- insert time on the fixture, and nothing rebuilds a deferred fact_records
-- index later. Keep this the only statement in the file. PostgreSQL rejects a
-- concurrent build inside a transaction block, the migration coordinator runs a
-- sole concurrent definition in autocommit mode, and it drops an invalid
-- same-name index before retrying a failed build.
CREATE INDEX CONCURRENTLY IF NOT EXISTS fact_records_content_entity_key_idx
    ON fact_records ((payload->>'repo_id'), (payload->>'entity_id'))
    WHERE fact_kind = 'content_entity'
      AND is_tombstone = FALSE;
