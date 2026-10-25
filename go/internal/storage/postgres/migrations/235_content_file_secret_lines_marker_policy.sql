-- SPDX-License-Identifier: MIT
-- Copyright (c) 2025-2026 eshu-hq

-- Hardcoded-secret detector: marker arm and the #8813 extras (#8833).
--
-- Replaces eshu_secret_line_findings from migration 138, which is never
-- edited, and takes the side table's readiness away in the same transaction so
-- the reducer's repair rebuilds it under the new policy (#8833 ruling-3). The
-- content-store redactors (#8812, #8833) store a credential line as
-- REDACTED_<finding_kind>; under 138 a marked aws_access_key, slack_token, or
-- private_key line lost its finding because the CASE read the value shape
-- first. The new function:
--
-- * puts a marker arm first in the CASE, one arm per kind, matched with the
--   case-sensitive ~, so a marked line keeps the kind its raw line had and a
--   user line that spells a marker in another case keeps its own kind.
--   REDACTED_value matches none of them;
-- * gates a line on a case-sensitive strpos(..., 'REDACTED_') OR the regex,
--   at file and line level. The substring test is the marker arm's place in
--   the gate: a case-insensitive REDACTED_ alternative inside the ARE cost 66%
--   more derivation time on a 150k-file corpus, the substring test 24 to 33%
--   (docs/internal/evidence/8833-detector-migration.md);
-- * keeps the six migration 138 arms unchanged after the marker arm, so every
--   line 138 classified keeps its kind;
-- * adds the extras the #8813 gap table lists: npmAuthIdent, JSON-quoted keys,
--   user:password URL userinfo (also protocol-relative //user:pass@host), and
--   HCL block openers that name a secret, with the brace ending the line
--   (an optional carriage return, then a newline or the end of the text, so
--   the file prefilter, which sees the whole body, matches the same openers
--   as the line filter). Openers after a colon are left out: JSON API schemas
--   are full of them.
--
-- The pattern and the CASE are bound to go/internal/secret/detect
-- (SQLPattern, MarkerPrefix, ArmSources) by
-- go/internal/query/hardcoded_secret_migration_binding_test.go; changing any
-- of them needs a new migration.
--
-- Neither statement locks content_files, so content writes keep running. The
-- triggers from 138 call the function by name, so every write after this
-- commits derives with it. The UPDATE moves the state row to not_built with a
-- new epoch only while policy_version (migration 234) is below 235, so the
-- local supervisor, which re-applies every migration at each start, changes
-- nothing on a second apply. Until the reducer's repair publishes ready for
-- the new epoch, investigate_hardcoded_secrets reads use the legacy content
-- scan, whose CASE says what this function says. Both statements run as one
-- transaction, so no reader sees the new function with the old readiness.
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
                WHEN lines.line_text ~ 'REDACTED_aws_access_key' THEN 'aws_access_key'
                WHEN lines.line_text ~ 'REDACTED_private_key' THEN 'private_key'
                WHEN lines.line_text ~ 'REDACTED_slack_token' THEN 'slack_token'
                WHEN lines.line_text ~ 'REDACTED_api_token' THEN 'api_token'
                WHEN lines.line_text ~ 'REDACTED_password_literal' THEN 'password_literal'
                WHEN lines.line_text ~ 'REDACTED_secret_literal' THEN 'secret_literal'
                WHEN lines.line_text ~* 'AKIA[0-9A-Z]{16}' THEN 'aws_access_key'
                WHEN lines.line_text ~* '-----BEGIN [A-Z ]*PRIVATE KEY-----' THEN 'private_key'
                WHEN lines.line_text ~* 'xox[baprs]-[A-Za-z0-9-]{10,}' THEN 'slack_token'
                WHEN lines.line_text ~* '(api[_-]?key|apikey|token)[[:space:]]*[:=]' THEN 'api_token'
                WHEN lines.line_text ~* '(password|passwd|pwd)[[:space:]]*[:=]' THEN 'password_literal'
                WHEN lines.line_text ~* '(secret|client[_-]?secret|private[_-]?key|authorization)[[:space:]]*[:=]' THEN 'secret_literal'
                WHEN lines.line_text ~* '(api[_-]?key|apikey|token)s?[''"]?[[:space:]]*[:=]' THEN 'api_token'
                WHEN lines.line_text ~* '(password|passwd|pwd)s?[''"]?[[:space:]]*[:=]' THEN 'password_literal'
                WHEN lines.line_text ~* '(secret|client[_-]?secret|private[_-]?key|authorization|npm[_-]?auth[_-]?ident|credential)s?[''"]?[[:space:]]*[:=]' THEN 'secret_literal'
                WHEN lines.line_text ~* '//[^/?#@[:space:]:]+:[^/?#@[:space:]]+@' THEN 'password_literal'
                ELSE ''
            END AS finding_kind
        FROM regexp_split_to_table(file_content, E'\n') WITH ORDINALITY AS lines(line_text, line_number)
        WHERE (strpos(file_content, 'REDACTED_') > 0 OR file_content ~* '(password|passwd|pwd|api[_-]?key|apikey|token|secret|client[_-]?secret|private[_-]?key|authorization|npm[_-]?auth[_-]?ident)[''"]?[[:space:]]*[:=][[:space:]]*[''"]?[A-Za-z0-9_./+=:@!#$%^-]{6,}|AKIA[0-9A-Z]{16}|sk_live_[A-Za-z0-9]{8,}|xox[baprs]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|//[^/?#@[:space:]:]+:[^/?#@[:space:]]+@|(password|passwd|pwd|api[_-]?key|apikey|token|secret|credential)s?[[:space:]]*=[[:space:]]*[{][ \t]*\r?(\n|$)')
          AND (strpos(lines.line_text, 'REDACTED_') > 0 OR lines.line_text ~* '(password|passwd|pwd|api[_-]?key|apikey|token|secret|client[_-]?secret|private[_-]?key|authorization|npm[_-]?auth[_-]?ident)[''"]?[[:space:]]*[:=][[:space:]]*[''"]?[A-Za-z0-9_./+=:@!#$%^-]{6,}|AKIA[0-9A-Z]{16}|sk_live_[A-Za-z0-9]{8,}|xox[baprs]-[A-Za-z0-9-]{10,}|-----BEGIN [A-Z ]*PRIVATE KEY-----|//[^/?#@[:space:]:]+:[^/?#@[:space:]]+@|(password|passwd|pwd|api[_-]?key|apikey|token|secret|credential)s?[[:space:]]*=[[:space:]]*[{][ \t]*\r?(\n|$)')
    ) AS d
    WHERE d.finding_kind <> ''
$fn$;

UPDATE content_file_secret_lines_state
SET state = 'not_built',
    epoch = epoch + 1,
    policy_version = 235,
    build_started_at = NULL,
    build_completed_at = NULL,
    failed_at = NULL,
    failure_class = '',
    updated_at = clock_timestamp()
WHERE singleton = TRUE
  AND policy_version < 235;
