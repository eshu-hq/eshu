# Evidence and measurements for the credential-file record (#8813)

This page holds the measurements behind
[the #8813 record](8813-credential-file-exposure.md). It moved out of that
record's `<details>` block so both files stay under the 500-line cap. If this
page and an issue body differ, the issue body wins.

## Local sample

Counts only; no value or path recorded; two local checkout roots, 1 and 20
repositories. Script: `git ls-files` per repository (what Eshu would index,
since it honors `.gitignore`), then `rg -q` per file. Exit 0.

| Checkout root | Repositories | `.yarnrc.yml` | with `npmAuthToken` key | literal `npmAuthToken` | with `npmAuthIdent` key | `*.tfvars` | `*.tfvars.json` | `*.tfvars` matching the 138 detector | after excluding `var.`/`data.`/`local.`/`module.` references |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| First local root (worktree directories excluded) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| Second local root | 20 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |

Cross-check, run from each repository root (both print `0`; on this branch's
checkout too):
`git ls-files | rg -i '(\.tfvars(\.json)?|\.yarnrc\.yml)$' | wc -l` and
`rg --files --hidden --no-ignore -g '*.tfvars' -g '*.tfvars.json' -g '.yarnrc.yml' | wc -l`.
Seeded on a scratch directory with `a/.yarnrc.yml`, `.yarnrc.yml`, `x/y.tfvars`,
`z.tfvars.json`, `b/yarnrc.yml`, and a decoy, `rg --files` piped to the regex
and the `-g` form both print 4; the regex with `\\.` (double backslash) prints
nothing. In a `git init` directory with only `.yarnrc.yml` and `x.tfvars`, the
`git ls-files` form prints 2 and its double-backslash twin prints 0.
Developer machine, not a deployment: deployment count NOT_CHECKED.

## Detector probe (the gap corpus)

Pure `SELECT` against Postgres 18 (`psql -d postgres`, no DDL), using the
migration 138 `CASE` and the `eshu_secret_line_findings` detector expression,
with fabricated values.

| Line shape | Detected by 138 |
| --- | --- |
| `npmAuthToken: <literal>` | yes (`api_token`) |
| `npmAuthToken: "${NPM_TOKEN}"` | no (correct: an env reference) |
| `npmAuthIdent: "<base64>"` | no |
| `npmRegistryServer` / `httpsProxy` with `user:pass@` | no |
| `db_password = "<literal>"` | yes (`password_literal`) |
| `"db_password": "<literal>"` (JSON-quoted key) | no |
| `access_key = ...`, `connection_string = ...` | no |
| `app_secrets = {`, then `stripe = "<literal>"` | no, no |
| `db_password = var.db_password` | yes (false positive) |
| `api_token = "abc"` (under 6 characters) | no |

## Marker probe, all six finding kinds

The same pure `SELECT` (migration 138 `CASE` and its line gate copied from
`138_content_file_secret_lines.sql`, run on Postgres 18.6, exit 0, 28 probe
lines). Each kind has a key line and a free-text line. The raw side used
fabricated values of the detector's shape; they are shown as `<shape>`. Every
marked line below is literal. Suppression was `f` on every row.

| Kind | Line | Raw line | Marked line (`REDACTED_<kind>`) |
| --- | --- | --- | --- |
| `aws_access_key` | key | `access_key_id = "AKIA<16 chars>"`: `aws_access_key` | `access_key_id = "REDACTED_aws_access_key"`: **not detected** |
| `aws_access_key` | free text | `# rotate AKIA<16 chars> soon`: `aws_access_key` | `# rotate REDACTED_aws_access_key soon`: **not detected** |
| `slack_token` | key | `slack_webhook = "xoxb-<10+ chars>"`: `slack_token` | `slack_webhook = "REDACTED_slack_token"`: **not detected** |
| `slack_token` | free text | `# post with xoxb-<10+ chars> here`: `slack_token` | `# post with REDACTED_slack_token here`: **not detected** |
| `private_key` | key | `key = "-----BEGIN RSA PRIVATE KEY-----"`: `private_key` | `key = "REDACTED_private_key"`: **not detected** |
| `private_key` | free text | `# -----BEGIN RSA PRIVATE KEY-----`: `private_key` | `# REDACTED_private_key`: **not detected** |
| `api_token` | key | `npmAuthToken: <12 chars>`: `api_token` | `npmAuthToken: REDACTED_api_token`: `api_token` |
| `api_token` | free text | `# the api_key=<12 chars> was pasted`: `api_token` | `# the api_key=REDACTED_api_token was pasted`: `api_token` |
| `password_literal` | key | `db_password = "<12 chars>"`: `password_literal` | `db_password = "REDACTED_password_literal"`: `password_literal` |
| `password_literal` | free text | `# password: <12 chars> per ops`: `password_literal` | `# password: REDACTED_password_literal per ops`: `password_literal` |
| `secret_literal` | key | `client_secret = "<12 chars>"`: `secret_literal` | `client_secret = "REDACTED_secret_literal"`: `secret_literal` |
| `secret_literal` | free text | `# the client_secret = <12 chars> shared`: `secret_literal` | `# the client_secret = REDACTED_secret_literal shared`: `secret_literal` |

Kind changes and the whole-line case (marked lines only):

| Marked line | Result under 138 |
| --- | --- |
| `npmAuthToken: REDACTED_aws_access_key` (an `AKIA` value misplaced under a listed key) | detected as `api_token`, raw kind was `aws_access_key` |
| `slack_token = "REDACTED_slack_token"` | detected as `api_token`, raw kind was `slack_token` |
| `REDACTED_aws_access_key` alone (a skeleton whole-line marker) | not detected |
| `db_password = "REDACTED_value"` | detected as `password_literal` |

The CASE tests the value shapes (`AKIA`, PEM header, `xox`) before the key
names, and the three value-shape kinds match nothing but that shape. A marker
that replaces the shape therefore removes the finding. The last row shows that
`REDACTED_value` on a line whose key the detector already lists would still be
detected; skeleton mode avoids that because a detector-flagged key gets
`REDACTED_<finding_kind>`, and the marker arm excludes `REDACTED_value`.
`db_password = "[redacted]"` and `db_password = "***"` are detected `f`
(measured in the first review round).

## Census query

Run on a deployment, counts only; it never selects `content` or `line_text`.
The first query was validated on temporary tables with the shapes from
`schema_content_store.go` and migration 138, seeded as described below; the
second was not run. Findings are meaningful only while
`content_file_secret_lines_state.state = 'ready'`, and the table misses the
shapes in the probe above until the new migration lands.

```sql
SELECT
  count(*) FILTER (WHERE f.relative_path ~* '(^|/)\.yarnrc\.yml$') AS yarnrc_yml_files,
  count(*) FILTER (WHERE f.relative_path ~* '\.tfvars(\.json)?$')  AS tfvars_files,
  count(*) FILTER (WHERE f.relative_path ~* '(^|/)\.yarnrc\.yml$' AND EXISTS (
    SELECT 1 FROM content_file_secret_lines s
    WHERE s.repo_id = f.repo_id AND s.relative_path = f.relative_path
      AND NOT s.suppressed)) AS yarnrc_with_unsuppressed_finding,
  count(*) FILTER (WHERE f.relative_path ~* '\.tfvars(\.json)?$' AND EXISTS (
    SELECT 1 FROM content_file_secret_lines s
    WHERE s.repo_id = f.repo_id AND s.relative_path = f.relative_path
      AND NOT s.suppressed)) AS tfvars_with_unsuppressed_finding
FROM content_files f;

-- Sizing input: entity rows for these paths.
SELECT count(*) FROM content_entities
WHERE relative_path ~* '\.tfvars(\.json)?$' OR relative_path ~* '(^|/)\.yarnrc\.yml$';
```

Seeded semantic check (temporary tables, `standard_conforming_strings = on`).
`content_files` rows: `a/.yarnrc.yml`, `.yarnrc.yml`, `x/y.tfvars`,
`z.tfvars.json`, `b/yarnrc.yml`, `decoy.txt`. `content_file_secret_lines` rows:
`.yarnrc.yml` unsuppressed, `x/y.tfvars` suppressed, `z.tfvars.json`
unsuppressed. Expected and observed, in column order:

| Pattern form | `yarnrc_yml_files` | `tfvars_files` | `yarnrc_with_unsuppressed_finding` | `tfvars_with_unsuppressed_finding` |
| --- | --- | --- | --- | --- |
| Single backslash (as above) | 2 | 2 | 1 | 1 |
| Double backslash (broken) | 0 | 0 | 0 | 0 |

The broken form reads a literal backslash plus any character, so it matches no
path and reports a false zero. Keep single backslashes in psql, `rg`, and shell.

## Claim status

- Proven (code read at `238b4015a9`, re-read at `4d78b83273`): admission path,
  the filesystem-mode dotfile allowlist, and no name-based sensitivity filter;
  verbatim body flow to `content_files.content`; `get_file_content` returns it;
  `get_file_lines` splits `fc.Content` and never reads `line_count`
  (`ContentReader.GetFileLines`); unscoped `search_file_content` matches
  `content_lower` and scoped search matches `content ILIKE`, both returning
  empty content on hits; `RetainedPaths` keeps a body when the content fact is
  skipped; `Registry.Fingerprint` omits any policy field; `FullSnapshot` is
  non-delta only; tree, counts, language inventory, and coverage read
  `content_files`; relationship evidence reads `content_body` from the fact and
  accepts tfvars; references derive from the stored body; `fileContentHash`
  returns the collector's source digest when set; the upsert overwrites
  `content`, `content_hash`, and `line_count`; the search-document projection
  reads `content_files.content` and serves a bounded `context_text`.
- Proven (run): gap table, marker behavior for all six kinds, first census
  query syntax, local 0.
- Inferred: `content` facts persist `content_body` in `fact_records` for every
  retained generation; real tfvars files yield evidence the skeleton would lose;
  other scanners flag credential-shaped markers (the reason that form was
  rejected).
- NOT_CHECKED: deployment file, credential, tfvars evidence, and finding counts;
  whether `content_entities` rows exist for these paths; the migration's
  finding-count delta and lock window; retention of the `content` and
  content-entity fact kinds; search-document retention (the
  `reducer_eshu_search_document` fact and the two index tables) under the
  default policy; whether invalidating a scope's projection state re-enqueues
  the sweeper given the work-item conflict key
  (`eshu_search_document_sweeper.go`); whether vector embedding sends
  `context_text` to a remote provider; the marker arm's false-positive count on a
  real corpus; Yarn's `enableStrictSettings` default; the delta file-set
  selection code; the empty-body response of `get_file_lines` (the code path is
  read, no request was run).
