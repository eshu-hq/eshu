-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Hardcoded-secret findings side table (#7125).
--
-- POST /api/v0/code/security/secrets/investigate scanned every content_files
-- row per call: a trigram-bitmap recheck of about 92% of the corpus, then a
-- per-line split and classification, then a top-N sort. That is 33-49 s on
-- ops-qa (145k files) and 3.9 s even repo-scoped. This table holds the answer
-- instead: one row per finding line, derived inside Postgres at write time so
-- the investigation read is an ordered primary-key scan that stops at LIMIT.
--
-- The derivation uses the same Postgres regex engine, patterns, and database
-- ctype the query used at read time. It is deliberately not done in Go: RE2
-- (?i) and ARE ~* case-fold non-ASCII differently, which would change results.
--
-- Policy now lives in storage. hardcodedSecretSQLPattern, the CASE below, and
-- the suppressed expression are bound to their Go sources of truth by
-- go/internal/query/hardcoded_secret_migration_binding_test.go; changing any
-- of them needs a new migration that replaces eshu_secret_line_findings,
-- re-derives this table, and (Postgres 17+) ALTER COLUMN suppressed SET
-- EXPRESSION. Never edit this file once it has shipped.
--
-- Lock window: CREATE TRIGGER and the foreign key take SHARE ROW EXCLUSIVE on
-- content_files until this file commits, which blocks content writes (not
-- reads) for the backfill duration, about 0.27 ms per file (38 s at 145k files
-- on ops-qa) and linear in corpus size. A fresh install backfills nothing.
-- The operator note is in docs/public/deployment/service-runtimes-bootstrap.md.
--
-- Bulk loads (bootstrap-index) do not pay the per-file derivation: their
-- sessions skip the triggers below, and a post-collection finalizer rebuilds
-- the table and publishes readiness (content_file_secret_lines_state). The
-- reason is measured: the trigger added about 0.6 ms per file on an idle
-- 16 vCPU host, which the bootstrap stage budget cannot absorb. Steady-state
-- writers keep the triggers.

CREATE TABLE IF NOT EXISTS content_file_secret_lines (
    repo_id TEXT NOT NULL,
    relative_path TEXT NOT NULL,
    line_number INTEGER NOT NULL,
    language TEXT NOT NULL,
    finding_kind TEXT NOT NULL,
    line_text TEXT NOT NULL,
    suppressed BOOLEAN GENERATED ALWAYS AS (
        strpos(lower(relative_path), '_test.') > 0 OR strpos(lower(relative_path), '/testdata/') > 0 OR strpos(lower(relative_path), '/fixtures/') > 0 OR strpos(lower(relative_path), '/examples/') > 0 OR strpos(lower(line_text), 'example') > 0 OR strpos(lower(line_text), 'dummy') > 0 OR strpos(lower(line_text), 'placeholder') > 0 OR strpos(lower(line_text), 'changeme') > 0
    ) STORED,
    PRIMARY KEY (repo_id, relative_path, line_number),
    FOREIGN KEY (repo_id, relative_path) REFERENCES content_files (repo_id, relative_path)
        ON DELETE CASCADE ON UPDATE CASCADE
);

-- No further index: the primary key is both the ORDER BY prefix and the
-- repo_id filter prefix. Never index line_text.

CREATE OR REPLACE FUNCTION eshu_secret_line_findings(file_content TEXT)
RETURNS TABLE (line_number INTEGER, line_text TEXT, finding_kind TEXT)
LANGUAGE sql
STABLE
PARALLEL SAFE
AS $fn$
    SELECT d.line_number, d.line_text, d.finding_kind
    FROM (
        SELECT
            lines.line_number::int AS line_number,
            lines.line_text,
            CASE
                WHEN lines.line_text ~* 'AKIA[0-9A-Z]{16}' THEN 'aws_access_key'
                WHEN lines.line_text ~* '-----BEGIN [A-Z ]*PRIVATE KEY-----' THEN 'private_key'
                WHEN lines.line_text ~* 'xox[baprs]-[A-Za-z0-9-]{10,}' THEN 'slack_token'
                WHEN lines.line_text ~* '(api[_-]?key|apikey|token)[[:space:]]*[:=]' THEN 'api_token'
                WHEN lines.line_text ~* '(password|passwd|pwd)[[:space:]]*[:=]' THEN 'password_literal'
                WHEN lines.line_text ~* '(secret|client[_-]?secret|private[_-]?key|authorization)[[:space:]]*[:=]' THEN 'secret_literal'
                ELSE ''
            END AS finding_kind
        FROM regexp_split_to_table(file_content, E'\n') WITH ORDINALITY AS lines(line_text, line_number)
        WHERE file_content ~* '(password|passwd|pwd|api[_-]?key|apikey|token|secret|client[_-]?secret|private[_-]?key|authorization)[[:space:]]*[:=][[:space:]]*[''"]?[A-Za-z0-9_./+=:@!#$%^-]{6,}|AKIA[0-9A-Z]{16}|sk_live_[A-Za-z0-9]{8,}|xox[baprs]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----'
          AND lines.line_text ~* '(password|passwd|pwd|api[_-]?key|apikey|token|secret|client[_-]?secret|private[_-]?key|authorization)[[:space:]]*[:=][[:space:]]*[''"]?[A-Za-z0-9_./+=:@!#$%^-]{6,}|AKIA[0-9A-Z]{16}|sk_live_[A-Za-z0-9]{8,}|xox[baprs]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----'
    ) AS d
    WHERE d.finding_kind <> ''
$fn$;

-- Trigger functions are plpgsql and VOLATILE (the default) on purpose: each
-- statement then takes a fresh READ COMMITTED snapshot, which the
-- concurrent-upsert interleaving relies on (the second writer blocks on the
-- content_files row lock, then derives from the content it committed over).
--
-- Both triggers are statement-level with transition tables, like migration
-- 109. INSERT ... ON CONFLICT DO UPDATE fires both: inserted rows land in the
-- INSERT trigger's new_rows and updated rows in the UPDATE trigger's. A
-- transition-table trigger cannot name UPDATE OF columns, so the UPDATE
-- trigger fires on every UPDATE and its body keeps only rows whose content or
-- language changed; an unchanged re-upsert derives nothing. A primary-key
-- moving UPDATE is cascaded by the foreign key first (RI triggers are
-- row-level and run before statement-level AFTER triggers); the body then
-- re-derives because no old row carries the new key. There is no DELETE
-- trigger: the foreign key cascade covers single, batch, and retention
-- deletes.
CREATE OR REPLACE FUNCTION content_file_secret_lines_derive_inserted()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO content_file_secret_lines (repo_id, relative_path, line_number, language, finding_kind, line_text)
    SELECT n.repo_id, n.relative_path, d.line_number, coalesce(n.language, ''), d.finding_kind, d.line_text
    FROM new_rows n CROSS JOIN LATERAL eshu_secret_line_findings(n.content) d;
    RETURN NULL;
END;
$$;

CREATE OR REPLACE FUNCTION content_file_secret_lines_derive_updated()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    DELETE FROM content_file_secret_lines s USING new_rows n
    WHERE s.repo_id = n.repo_id AND s.relative_path = n.relative_path
      AND NOT EXISTS (
          SELECT 1 FROM old_rows o
          WHERE o.repo_id = n.repo_id AND o.relative_path = n.relative_path
            AND o.content IS NOT DISTINCT FROM n.content
            AND o.language IS NOT DISTINCT FROM n.language);
    -- The changed set is materialized so the planner cannot push the derivation
    -- function's inlined file-level regex below the unchanged-row anti join:
    -- without the fence an unchanged re-upsert still paid the regex on every
    -- file (#7125 profile).
    WITH changed AS MATERIALIZED (
        SELECT n.repo_id, n.relative_path, n.language, n.content
        FROM new_rows n
        WHERE NOT EXISTS (
            SELECT 1 FROM old_rows o
            WHERE o.repo_id = n.repo_id AND o.relative_path = n.relative_path
              AND o.content IS NOT DISTINCT FROM n.content
              AND o.language IS NOT DISTINCT FROM n.language)
    )
    INSERT INTO content_file_secret_lines (repo_id, relative_path, line_number, language, finding_kind, line_text)
    SELECT c.repo_id, c.relative_path, d.line_number, coalesce(c.language, ''), d.finding_kind, d.line_text
    FROM changed c CROSS JOIN LATERAL eshu_secret_line_findings(c.content) d;
    RETURN NULL;
END;
$$;

-- Bulk-load gate. A session that ran SET eshu.secret_lines_derive = 'deferred'
-- (DeferredSessionSQL in storage/postgres/secret/lines, opened by bootstrap-index only) skips both
-- triggers. Its content writes leave content_file_secret_lines behind until the
-- post-collection finalizer rebuilds it and publishes 'ready' in
-- content_file_secret_lines_state; readers use the legacy scan while the state
-- is not 'ready'. The gate is a per-session setting, not ALTER TABLE ... DISABLE
-- TRIGGER: that DDL takes ACCESS EXCLUSIVE-class locks on content_files and
-- would silence every other writer (ingester, projector, manual SQL) too. A
-- session that loses the setting (for example behind a connection pooler that
-- drops or never forwards it) simply keeps deriving, which is correct and only
-- slower. The opposite direction is not safe: a transaction-mode pooler does
-- not reset session settings between clients, so the SET can stay on a server
-- connection later handed to another binary, whose writes then skip derivation
-- while the state is 'ready'. bootstrap-index must not run through such a
-- pooler (docs/internal/evidence/6793-infra-read-model-fence.md records the same
-- leak class for eshu.infra_inventory_writer). The same
-- transition-table trade-off as migration 109's fence applies: PostgreSQL
-- still captures the transition tuples before the WHEN runs.
DROP TRIGGER IF EXISTS content_files_secret_lines_insert ON content_files;
CREATE TRIGGER content_files_secret_lines_insert
    AFTER INSERT ON content_files
    REFERENCING NEW TABLE AS new_rows
    FOR EACH STATEMENT
    WHEN (current_setting('eshu.secret_lines_derive', true) IS DISTINCT FROM 'deferred')
    EXECUTE FUNCTION content_file_secret_lines_derive_inserted();

DROP TRIGGER IF EXISTS content_files_secret_lines_update ON content_files;
CREATE TRIGGER content_files_secret_lines_update
    AFTER UPDATE ON content_files
    REFERENCING OLD TABLE AS old_rows NEW TABLE AS new_rows
    FOR EACH STATEMENT
    WHEN (current_setting('eshu.secret_lines_derive', true) IS DISTINCT FROM 'deferred')
    EXECUTE FUNCTION content_file_secret_lines_derive_updated();

-- Readiness of the side table (the content_substring_index_state precedent from
-- migration 057). 'ready' means every content_files row's findings are in
-- content_file_secret_lines. bootstrap-index moves it to 'not_built' and bumps
-- epoch before its first deferred write, the finalizer publishes 'ready' only
-- for the epoch it claimed, so a later bulk load cannot be published by an
-- earlier finalizer. A fresh install and this migration's own backfill end
-- 'ready' (below).
CREATE TABLE IF NOT EXISTS content_file_secret_lines_state (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    state TEXT NOT NULL CHECK (state IN ('not_built', 'building', 'ready', 'failed')),
    epoch BIGINT NOT NULL DEFAULT 1,
    build_started_at TIMESTAMPTZ NULL,
    build_completed_at TIMESTAMPTZ NULL,
    failed_at TIMESTAMPTZ NULL,
    failure_class TEXT NOT NULL DEFAULT ''
        CHECK (failure_class IN ('', 'backfill_failed')),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT clock_timestamp()
);

-- Backfill every existing content_files row. ON CONFLICT DO NOTHING keeps a
-- re-run of this file idempotent. The migration tracker never re-applies an
-- applied checksum, so the table is empty on the one real apply.
INSERT INTO content_file_secret_lines (repo_id, relative_path, line_number, language, finding_kind, line_text)
SELECT f.repo_id, f.relative_path, d.line_number, coalesce(f.language, ''), d.finding_kind, d.line_text
FROM content_files f CROSS JOIN LATERAL eshu_secret_line_findings(f.content) d
ON CONFLICT DO NOTHING;

-- The backfill above made the table current for every existing row, so the
-- state starts 'ready'. DO NOTHING keeps a re-run from resetting a bulk load in
-- flight.
INSERT INTO content_file_secret_lines_state (singleton, state, build_completed_at)
VALUES (TRUE, 'ready', clock_timestamp())
ON CONFLICT (singleton) DO NOTHING;
