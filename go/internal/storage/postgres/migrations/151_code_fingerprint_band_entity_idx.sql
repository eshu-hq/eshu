-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7254: code_fingerprint_band carries entity_id only as the fourth column of
-- its primary key (repo_id, band_no, band_hash, entity_id), so a statement that
-- filters on (repo_id, entity_id) cannot seek. The steady-state upsert delete
-- (deleteFingerprintBandsForEntitiesSQL, one 500-id chunk per Write for every
-- re-upserted fingerprinted entity) and the second reap read (the stale
-- band-entity EXCEPT) both paid for that: under a generic plan the delete read
-- the whole repository range and filtered it (one 500-id chunk on a local 5x
-- repository, 799,370 band rows: 4.4-4.7 s warm against 0.13-0.24 s under a
-- custom plan; auto mode picks the slow generic plan on the sixth execution).
-- With this index the same chunk is an index scan of about 4 ms under either
-- plan mode. The stale-id read still reads the whole repository range, now as
-- an index-only scan of the smaller index: 3.1-3.4 s falls to 0.53-0.91 s. See
-- docs/internal/evidence/7254-code-fingerprint-band-entity-idx.md.
--
-- The index costs write time: inserting a 1x repository's bands (about 160k
-- rows) rose from about 1.2 s to about 1.7 s in the same fixture, and the
-- index is 81 MB beside a 430 MB primary key. That is the price of the seek.
--
-- Built on cold bootstrap too: nothing rebuilds a deferred index later, and an
-- empty table builds instantly. Keep this the only statement in the file.
-- PostgreSQL rejects a concurrent build inside a transaction block, the
-- migration coordinator runs a sole concurrent definition in autocommit mode
-- without the bootstrap lock_timeout (#7004), and it drops an invalid
-- same-name index before retrying a failed build.
CREATE INDEX CONCURRENTLY IF NOT EXISTS code_fingerprint_band_entity_idx
    ON code_fingerprint_band (repo_id, entity_id);
