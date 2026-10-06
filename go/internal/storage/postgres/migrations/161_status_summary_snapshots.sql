-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- #7009: status_summary_snapshots is the reducer-owned read model behind the
-- status routes' active-work summary. Each model_key holds ONE row whose `rows`
-- jsonb carries the whole statement result as
-- [[section, ordinal, section_json_text], ...] in live order, so a reader
-- decodes it with the same decoder that reads the live statement. The row is
-- written by one guarded single-row upsert
-- (INSERT ... ON CONFLICT (model_key) DO UPDATE ... WHERE existing.as_of <
-- EXCLUDED.as_of), so a reader sees either the whole previous answer or the
-- whole new one and an older pass can never overwrite a newer one. A
-- multi-row model was rejected: a partial write exposes a torn answer.
--
-- Column meanings:
--   model_key        which summary the row holds (active_work_summary today).
--   schema_version   version of the `rows` encoding; bump only when it changes.
--   source_sha256    sha256 of the exact statement text the writer ran. A
--                    reader compares it with the sha of its own compiled
--                    statement and never decodes rows it did not produce the
--                    statement for (rolling upgrade fence).
--   as_of            the clock value the writer bound as the statement's $1.
--   computed_at      writer database clock when the row was written.
--   pass_duration_ms writer compute time for the pass, in milliseconds.
--   row_count        number of tuples in `rows`; a reader treats a mismatch
--                    with the decoded length as a corrupt row.
--
-- The table has one tiny hot row that is updated every few seconds, so it is
-- tuned to stay small: fillfactor 50 leaves room for HOT updates (no indexed
-- column changes on update), and the zero scale factors make autovacuum and
-- autoanalyze run after 50 row changes instead of waiting on a percentage of a
-- one-row table. No index exists beyond the primary key and nothing is
-- backfilled: the table starts empty and the first writer pass fills it. The
-- storage parameters can be changed later by a new ALTER TABLE ... SET
-- migration, so they are not a one-way door.
--
-- One-way door: the migration ledger pins this file's checksum. Never edit it
-- after it has applied; make any later change a new migration.
CREATE TABLE IF NOT EXISTS status_summary_snapshots (
    model_key        text PRIMARY KEY,
    schema_version   integer NOT NULL,
    source_sha256    text NOT NULL,
    as_of            timestamptz NOT NULL,
    computed_at      timestamptz NOT NULL,
    pass_duration_ms double precision NOT NULL,
    row_count        integer NOT NULL,
    rows             jsonb NOT NULL
) WITH (
    fillfactor = 50,
    autovacuum_vacuum_scale_factor = 0,
    autovacuum_vacuum_threshold = 50,
    autovacuum_analyze_scale_factor = 0,
    autovacuum_analyze_threshold = 50
);
