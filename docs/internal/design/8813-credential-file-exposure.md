# Redact `.yarnrc.yml` and `*.tfvars`, never store them verbatim (#8813)

Status: Accepted (arbiter rulings relayed by the coordinator, 2026-10-23, two
rounds). This record approves no code, schema change, or deployment action.
Part of #8807. The work splits across #8812 (policy enum, marker-mode
redactor, digest rule, fingerprint) and #8833 (skeleton mode, the tfvars
binding, the detector migration, the differential). Citations were taken at
`238b4015a9`, re-checked at `816a039136` (`origin/main`, 2026-10-24), and name
the function, so they survive small line drift.

**Decision.** Both files use the `redact` policy from #8812, in two modes.
`.yarnrc.yml` is closed-schema and uses **marker mode**: credential values are
replaced in place. `*.tfvars` and `*.tfvars.json` (which the registry already
routes to the `hcl` parser) are open-schema, so a key denylist cannot make them
safe: whether a variable is secret is declared in `variables.tf`
(`sensitive = true`), not in its name. They use **allowlist-skeleton mode**: a
value survives only if its shape is provably safe, and every other value is
replaced. Rows already stored are fixed by the one-time re-collect that the
policy-aware registry fingerprint triggers, not by a rewrite loop. The `metadata`
policy (empty body, row kept) exists in the enum and is the floor for the
operator SQL, but neither file uses it, because it would silently drop the
committed-tfvars-secret finding and the raw-Terraform evidence class.

## Context

### How the two files reach `content_files.content`

Discovery applies no name-based sensitivity filter. Its path filters are the
registry, `.gitignore`, `.eshuignore`, default ignored directories and
extensions, operator globs, and the #8809 symlink check. A search of the
discovery package and `snapshot_discovery.go` for credential, secret, `.env`,
`npmrc`, or `netrc` finds one symlink comment.

| Step | Where | What happens |
| --- | --- | --- |
| Copy (filesystem-source mode only) | `shouldSkipFilesystemEntry` in `go/internal/collector/repo/git/selection_filesystem.go` | Prunes dot-prefixed names except `.gitlab-ci.y*ml`, `.gitmodules`, and `.github/workflows`, so that mode does not store `.yarnrc.yml` today but does store `*.tfvars`. The git-sourced mode keeps hidden names (`IgnoreHidden` is never set), so it stores `.yarnrc.yml`. |
| Admit | `resolveNativeSnapshotFileSet` in `go/internal/collector/repo/git/snapshot_discovery.go` (the closure passed to `ResolveRepositoryFileSetsWithStats`) | A path is kept when `symlinkAdmission.category` names it: a registry definition via `registry.LookupByPath(path)`, or terraform state, documentation, `.gitmodules`, or CODEOWNERS. A symlink also needs a regular-file target in the same category (#8809, #8882). |
| Match `.yarnrc.yml` | `Registry.LookupByPath` in `go/internal/parser/registry.go` | No exact name or prefix matches. `filepath.Ext(".yarnrc.yml")` is `.yml`, so the `yaml` definition (`registry_definitions.go`, `Extensions: .yaml, .yml`, admission L2) wins; `TestDefaultRegistryKeepsCredentialBearingDotfilesOut` leaves it out of the exclusion list on purpose. |
| Match `*.tfvars` | same | Extension `.tfvars` belongs to the `hcl` definition (`Extensions: .hcl, .tf, .tfvars`, admission L2). `LookupByPath` also hard-codes `*.tfvars.json` to `hcl` (listed in `PathDispatches`). |
| Read body | `streamFacts` in `go/internal/collector/repo/git/fact_builder.go` (`streamContentBodyReadFile`, then `ContentFactEnvelope`) | The whole file is read into a string for every admitted file. |
| Emit | `ContentFactEnvelope` in `go/internal/collector/git/content/envelopes.go` | The body goes into the fact payload as `content_body`. |
| Project | `BuildContentRecord` in `go/internal/projector/runtime/content_record.go` | `content_body` becomes `content.Record.Body` untouched. |
| Store | `ContentWriter.Write` (`content_writer.go`, `body: record.Body`) and `upsertContentFileBatch` (`content_writer_upserts.go`, `row.body`) | The body is upserted into `content_files.content` (`schema_content_store.go`). |

No step transforms the body; #8812 adds the per-definition store policy.

### Where the body is then readable

- `get_file_content` and `get_file_lines`: `ContentReader.GetFileContent`
  (`go/internal/query/content_reader.go`) returns `content` verbatim through
  `POST /api/v0/content/files/read` (`contentread/content_handler.go`,
  `readFile`).
- `search_file_content`: hit rows come back with an empty `content` column, but
  the match runs against the body (`content_lower`, a stored lowercase copy,
  migration 176) in `go/internal/query/search/unscoped/files.go` (every
  `content_lower LIKE` statement). The
  tool is a substring oracle over the credential, even though it does not print it.
- Derived copies of the same bytes:
  - `content_files.content_lower` (generated, `lower(content)`).
  - `content_file_secret_lines.line_text` (migration 138, the full text of every
    flagged line).
  - `content_entities.source_cache` for any entity the parser cuts from the file.
  - Curated search documents, read from `content_files.content` by
    `eshu_search_document_source_loader.go`.
  - `content_body` in the `content` fact payload of every retained generation.
- `investigate_hardcoded_secrets` redacts only its own response; that protects
  none of the paths above.

### The exposure

Eshu becomes a second copy of the credential with a different access model:
aggregated, searchable across repositories, returned to model-provider
transcripts through MCP, and carried into Postgres backups and WAL. The
`/api/v0/content/files/*` routes still check the repository grant
(`auth_scoped_routes_completeness.go`), so this is credential duplication, not
an unauthenticated leak.

`.yarnrc.yml` can hold `npmAuthToken`, `npmAuthIdent` (base64 `user:password`),
and `user:password@` userinfo in `npmRegistryServer`, `httpProxy`, and
`httpsProxy`. `*.tfvars` can hold database passwords, API keys, and private
keys beside the non-secret environment shape (versions, sizing, PagerDuty
declarations).

### Evidence on how common it is

| Sample | `.yarnrc.yml` | literal `npmAuthToken` | `*.tfvars` | literal credential in `*.tfvars` | Source |
| --- | --- | --- | --- | --- | --- |
| One hosted deployment | 2 | 0 | not recorded | not recorded | Issue #8813 body |
| Local checkouts (21 repositories) | 0 | 0 | 0 (and 0 `*.tfvars.json`) | 0 | `<details>` below |
| Deployment census (this research's acceptance item) | NOT_CHECKED | NOT_CHECKED | NOT_CHECKED | NOT_CHECKED | No deployment access |

No exposure was observed where it was measured, and the local sample is empty,
so it neither supports nor weakens the recommendation. The risk rests on the
registry admitting a credential-capable file with no policy. A counts-only
census query for an operator is in the `<details>` block.

### Why existing rows do not heal themselves

Three findings. Each defeats a "just stop storing it" fix.

1. **A skipped content fact keeps the old body.** For a non-delta generation
   the projector sets `FullSnapshot` and passes every file fact's path as
   `RetainedPaths` (`projection.go`, `snapshotFilePaths` in
   `repository_refs.go`). The reap (`staleContentFilePaths` in
   `content_writer_reap.go`) treats a retained path as present and keeps its
   stored content. A policy that only omits the content fact leaves the old
   body in place, and a "zero rows" test run against a fresh database would
   still pass. The fix is to keep emitting the record, so the upsert
   overwrites the body (see "Rows already stored").
2. **The parser fingerprint does not see a policy.** `Registry.Fingerprint`
   hashes key, language, extensions, exact names, and prefix names only
   (`registry.go`); the #8849 `Ecosystem` and `Admission` labels are not
   hashed. The freshness hint in `snapshot_freshness.go` folds that
   fingerprint into the generation identity. A policy field leaves the hint
   unchanged, so unchanged repositories are not re-collected and keep their
   stored bodies until the file next changes.
3. **Delta generations never reap.** `contentFullSnapshot` in
   `go/internal/projector/runtime/repository_refs.go` sets `FullSnapshot` only
   when the generation carries the repository fact and no `delta_generation`
   marker (field doc on `Materialization.FullSnapshot` in `content/writer.go`). Editing
   `RetainedPaths` therefore cannot be the fix: a delta never consults it. The
   upsert overwrites on delta and full generations alike.

### What depends on the stored body

These decide what each policy costs for `*.tfvars`.

- **Repository tree, file counts, language inventory, and coverage** are read
  from `content_files` rows (`go/internal/query/repository/tree.go`,
  `stats.go`, `language_inventory.go`, `coverage.go`). A deleted row removes the
  path from all four, so the design never deletes a row.
- **Relationship evidence** is discovered by the reducer from the `content`
  fact bodies (`relationships.DiscoverEvidence`, `envelopeContentIdentity` in
  `evidence_content_index.go`). `isTerraformArtifact` in
  `terraform_evidence.go` accepts `.tfvars` and `.tfvars.json`. Its five raw
  patterns (`terraformPatterns`) read the body: `app_repo = "..."`, `app_name = "..."`,
  a `github.com` URL, `repo:org/name:`, and a `/configd/<name>/` or
  `/api/<name>/` path. Structured module evidence comes from the file fact's
  `parsed_file_data`, which any policy leaves intact.
- **The committed-secret finding** for tfvars comes from
  `content_file_secret_lines`, derived from the stored body.
- **`content_file_references`** rows are derived from the stored body
  (`contentReferencesForFiles` in `content_writer_references.go`).
- **Parser facts and config-artifact rows** come from the original file before
  any store policy applies (`content/artifacts/hcl.go`: the tfvars row is built
  from the path alone; `hcl/pagerduty_declarations.go`).

## Options

| Option | Cost | Verdict |
| --- | --- | --- |
| A. Keep both verbatim (status quo) | Credentials stay searchable and returnable. | Rejected. |
| B. Marker-redact both with a key denylist | Complete only for a closed schema. A tfvars value under an unlisted name is stored. | Chosen for `.yarnrc.yml` only. |
| C. `metadata` for tfvars (empty body) | Cannot leak. Silently drops the committed-secret finding and the raw-Terraform evidence class. | Rejected for the two files. Kept as the operator-SQL floor. |
| D. Allowlist-skeleton `redact` for tfvars | Keeps structure, keys, and provably safe values. Stored body is not parseable. Needs a differential before the binding ships. | Chosen. |
| E. Operator `.eshuignore` for either file | Works today (`docs/public/reference/eshuignore.md` shows `*.tfvars`). Per repository, opt-in, and drops the file's parser facts too. | Stays the override. Not a policy. |

## Decision

### Rule per file

| File | Policy | Mode | Ships in |
| --- | --- | --- | --- |
| `.yarnrc.yml` | `redact` | marker | #8812 redactor, detector extras from #8833 |
| `*.tfvars`, `*.tfvars.json` | `redact` | allowlist skeleton | #8833 |

`.eshuignore` stays the override. A future change to a looser tfvars rule needs
a new record.

### Policy enum and row shape (#8812)

The enum is `store`, `metadata`, `redact`. `metadata` writes a `content_files`
row with these values and never a tombstone:

| Column | Value |
| --- | --- |
| `content` | `''` (`content_lower` follows, it is generated) |
| `line_count` | `0`; it describes the stored body, and `get_file_lines` range checks read it |
| `content_hash` | digest of the **source** bytes, computed before the policy applies |
| `repo_id`, `relative_path`, `commit_sha`, `language`, `artifact_type`, `template_dialect`, `iac_relevant`, `generation_id`, `indexed_at` | unchanged |
| `content_entities.source_cache` for the path | `metadata`: empty. `redact`: slice of the redacted body (marker or skeleton), never the source. Operator SQL: blanked |

`content_hash` is the digest of the source file, not of the stored body; the
docs must say so. `fileContentHash` already returns `record.Digest` when the
collector set it (`content_writer_upserts.go`), and the collector passes
`meta.Digest` today. #8812 must keep computing it from the source bytes before
the policy applies, so an unchanged file keeps the same freshness hint and the
fingerprint, not the digest, invalidates on a policy change.

Why this shape is safe on every generation: the upsert
(`ON CONFLICT ... DO UPDATE` in `content_writer_sql.go`) overwrites an old body
on full and delta generations; references are deleted per batch before they are
re-derived from the stored body (`deleteContentReferenceBatch`, defined in
`content_writer_references.go` and called in `upsertContentFileBatch` before the
INSERT); and the migration 138 trigger derives no secret line from an empty
body. No `RetainedPaths` edit and no entity-ordering dependency is needed.

### Marker mode, `.yarnrc.yml` (#8812)

1. **Marker.** Replace the value with `REDACTED_<finding_kind>`, for example
   `npmAuthToken: REDACTED_api_token`. Measured against the migration 138
   expressions, this marker still matches the detector and is not suppressed, so
   `investigate_hardcoded_secrets` keeps reporting the line. `[redacted]` and
   `***` do not match and would blind it. The marker is a fixed point:
   redacting a redacted line returns the same line.
2. **One pattern constant.** The collector redactor binds to the same Go
   constant as the SQL detector (`hardcodedSecretSQLPattern` in
   `go/internal/query/content_reader_security_secrets.go`), not to a copy.
3. **Fail closed.** If the redactor errors, or the file is not valid UTF-8 text,
   store the `metadata` row for that path and count it. Never fall back to the
   raw body.
4. **Scope.** `content_entities.source_cache` is the second copy: the entity
   builder slices `file.Body`. Under `redact` the slice is taken from the
   redacted body, never the source. `content_lower` and `content_file_secret_lines` derive from the stored
   body by generated column and trigger, so they follow with no code; the
   follow-up adds a negative test only.

### Allowlist-skeleton mode, `*.tfvars` and `*.tfvars.json` (#8833)

The redactor is line-wise. Line count and line positions are preserved.

| Line kind | Stored as |
| --- | --- |
| Structural: `{`, `}`, `[`, `]`, `key = {`, `key = [`, heredoc opener and closer | kept |
| `key = value`, key flagged by the detector (checked first) | value becomes `REDACTED_<finding_kind>`, even for a reference |
| `key = value`, value is a boolean, a number, a `var.`, `local.`, `module.`, or `data.` reference, or a version string | kept |
| `key = value`, any other value | value becomes `REDACTED_value` |
| Comment, bare list element, heredoc body line, any line the classifier cannot place | whole line becomes `REDACTED_value` |

A detector-flagged key wins over the reference-kept rule, so
`db_password = var.db_password` is stored as a marker. Comment, heredoc, and
bare-list lines lose their findings, because the redacted line no longer
matches the detector; the differential shows how many.

`.tfvars.json` follows the same rule on `"key": value`. The stored body is
documented as not parseable HCL or JSON. Evidence shapes may join the allowlist
only if the differential shows they are needed and the shape cannot carry a
credential: the `app_repo` and `app_name` values, the path of a code-host URL
with userinfo and query stripped, and `repo:org/name:`. The fifth raw pattern,
`/configd/<name>/` or `/api/<name>/`, matches inside arbitrary strings, so it is
the likeliest to differ. List it in the differential explicitly.

**The differential gates #8833's tfvars binding**, before the fingerprint flip,
and does not gate #8812. On a representative corpus:

1. `relationships.DiscoverEvidence` over tfvars content facts returns the same
   evidence set from the skeleton body as from the raw body.
2. `eshu_secret_line_findings` returns the same `(line_number, finding_kind)`
   set from the skeleton body as from the raw body.

Every difference is listed, then closed (by an allowlist addition that passes
the credential test above) or accepted in writing on #8833.

### One new detector migration (#8833)

Migration 138 never gets edited. One new migration replaces
`eshu_secret_line_findings` with an extended `CASE`, re-derives
`content_file_secret_lines`, and carries these extras, all ASCII keys:

`npmAuthIdent`, `user:pass@` URL userinfo, JSON-quoted keys, and secret-named
block openers. The gap table in the `<details>` block shows each is undetected
today.

Values under six characters stay out. The new migration number bumps
`hardcodedSecretSQLPattern`, `hardcoded_secret_migration_binding_test.go`, and
the collector redactor in one change. The sync test is an engine-parity test:
the Go RE2 patterns and the Postgres ARE patterns classify the same corpus
(the gap table in the `<details>` block) identically. A subset check is not
enough, because the two engines case-fold and match differently.

Prove the theory before the migration ships: measure the finding-count delta on
a representative corpus and the re-derive lock window. Migration 138 recorded
about 0.27 ms per file under a lock that blocks content writes (38 s at 145k
files); this one needs its own number.

### Rows already stored

No rewrite loop, no tombstone. The policy-aware `Registry.Fingerprint` (#8812;
the fleet-wide re-collect is the accepted effect) makes each repository mint a
new generation. The collector re-emits the content record under the new policy,
and the upsert overwrites the stored body: marker-redacted for `.yarnrc.yml`,
skeleton for tfvars. The heal depends on that generation re-emitting content
records for every admitted path; the #8812 upgrade test proves it, and the delta
file-set selection code is NOT_CHECKED.

Gaps the follow-up closes:

1. **Repositories not scheduled for collection** keep their bodies. #8833 ships
   a one-shot operator SQL that applies the `metadata` floor: set `content = ''`
   on matched rows, delete their `content_file_references`, and blank
   `content_entities.source_cache`. It is repo-scoped, 500 rows per statement,
   and idempotent. An operator runs it; no loop does.
2. **Residue.** Dead tuples, WAL, and backups keep the old bytes until vacuum
   and backup expiry. The operator note says so.
3. **Fact payloads.** Superseded `content` facts keep `content_body`, and
   content-entity facts keep their `source_cache` bytes, until generation
   retention prunes them. Defaults are in `generation_retention.go`
   (`defaultGenerationRetentionMinSuperseded = 24`, `MaxAge = 7 days`,
   `HardMaxAge = 90 days`). Ruling: do not scrub; accept the window and write an
   operator note. Condition: #8833 executes the retention path against a `content`
   fact and a content-entity fact and records whether both kinds are pruned by
   default policy. If they are exempt, this decision reopens.

Operator signals (3 AM): a gauge of policy-path rows that still hold a raw body;
counters for `redacted`, `metadata_fallback`, and `redactor_failed` by policy; a
structured log per collection with the same counts.

## Consequences

Good: a tfvars value cannot be stored or searched unless its shape is
allowlisted; `.yarnrc.yml` keeps its registry URLs readable;
`investigate_hardcoded_secrets` keeps reporting `key = value` findings on both
files; the substring oracle closes because `content_lower` derives from what is
stored; tree, counts, language inventory, and coverage are unchanged because no
row is deleted; no new background job. The work also surfaces defects in
#8812's plan (RetainedPaths, fingerprint, derived copies).

Bad: one fleet-wide re-collect when the fingerprint changes; a stored tfvars
body is not parseable and reads poorly in `get_file_content`; old bytes persist
in superseded fact payloads, dead tuples, and backups for a bounded time; the
skeleton may lose some evidence and the findings on comment, heredoc, and
bare-list lines, which the differential measures before the binding ships.

## Implementation plan

Paths (verify before editing): `go/internal/parser/registry*.go` (enum,
fingerprint; #8812), `go/internal/collector/git/content/` (redactors, emit),
`go/internal/collector/repo/git/fact_builder.go` (call site),
`go/internal/storage/postgres/migrations/` (detector migration),
`go/internal/query/content_reader_security_secrets.go` and
`hardcoded_secret_migration_binding_test.go` (constant and binding). Prove first:
the differential, the migration's finding-count delta and lock window, the
retention check, and `get_file_lines` at `line_count = 0`. Avoid query-layer
redaction (search and exports bypass it), `[redacted]` or `***` markers, any
raw-body fallback, tombstones for these paths, editing `RetainedPaths`, and a key
denylist for tfvars.

## Acceptance criteria

The issue bodies are the binding acceptance lists. If this record and an issue
body ever differ, the issue body wins. The split mirrors them.

**#8812 owns** the enum, marker redactor, digest rule, fingerprint,
`source_cache` rule, upgrade test, and policy docs. It does not own the detector
migration, the engine-parity test, the gap-table corpus, or the `user:pass@`
case, because migration 138 does not detect it.

- [ ] A `metadata` definition never writes a body, by test. A seeded `.npmrc`
  with a literal `_authToken` yields a row with empty `content` and
  `content_lower`, `line_count = 0`, `content_hash` the source digest, and no
  `content_file_secret_lines` or `content_file_references` row.
- [ ] Upgrade test: a seeded verbatim `content_files`, reference, and
  `content_entities` row (non-empty `source_cache`) for a path that becomes
  `metadata` is empty after the next projected generation, on a full-snapshot
  and a delta generation. No tombstone, no `RetainedPaths` change.
- [ ] `Registry.Fingerprint` includes each definition's policy, with a test; the
  PR body states the one-time re-collect.
- [ ] `source_cache` is empty under `metadata` and sliced from the redacted body
  under `redact`; a seeded credential never reaches `content`, `content_lower`,
  `line_text`, or `source_cache`.
- [ ] The `content` fact carries the empty or redacted body and the source
  digest; an unchanged file keeps the same freshness hint across two cycles.
- [ ] Marker redaction uses `REDACTED_<finding_kind>`, still matches the
  detector and is not suppressed; `[redacted]` and `***` are rejected by test.
- [ ] A redactor error or non-UTF-8 input stores a `metadata` row and increments
  `redactor_failed`; no raw-body fallback exists, by test.
- [ ] The marker redactor binds to `hardcodedSecretSQLPattern`, pinned by test.
- [ ] Docs carry the policy table and say `content_hash` is the digest of the
  source file, not of the stored body.
- [ ] Independent deep review of the write path and the redaction patterns.

**#8833 owns** the tfvars and `.yarnrc.yml` bindings, the detector migration, the
engine-parity test, the gap-table corpus, the differential, the operator SQL,
the retention check, and the census.

- [ ] Seeded `.yarnrc.yml` (literal `npmAuthToken`, `user:pass@` registry URL)
  stores neither value, keeps the host, and the token line is still reported.
- [ ] Seeded tfvars and `.tfvars.json` (password, secret-named block, heredoc,
  token in a comment, `user:pass@` URL) store none of the values; the password
  line is reported at its original line; parser facts and config-artifact rows
  survive; `source_cache` holds only skeleton text.
- [ ] One migration adds the detector extras and re-derives
  `content_file_secret_lines`; finding-count delta and lock window are recorded
  first; an engine-parity test compares Go RE2 and Postgres ARE over the
  gap-table corpus.
- [ ] After one collection cycle on a database holding verbatim rows, the
  exposure gauge reads zero (the test seeds the old rows first).
- [ ] The operator SQL sets `content = ''`, deletes the reference rows, and
  blanks `source_cache`; 500 rows per statement; idempotent; run order documented.
- [ ] A retention check runs the pruning path against a `content` fact and a
  content-entity fact; if either is exempt, the payload decision reopens. The
  docs carry the operator note (7 to 90 days, dead tuples, WAL, backups).
- [ ] A deployment census is recorded as counts, before and after.
- [ ] Property test over skeleton bodies, with a credential-shape corpus that
  never survives and a fail-closed unterminated-string line.
- [ ] Differential before the fingerprint flip: `DiscoverEvidence` and
  `eshu_secret_line_findings` agree between skeleton and raw body, or each
  difference is closed or accepted on #8833.
- [ ] Independent deep review of the write path, redactor, and migration.

## Decisions for the arbiter

All decided; none open. (1) `.yarnrc.yml` is marker-mode `redact`; tfvars and
tfvars.json are skeleton-mode `redact`, not `metadata`. (2) Superseded content
and content-entity fact payloads are not scrubbed; the retention window is
accepted, conditional on #8833's retention check. (3) The `content_entities`
census is not a gate; `source_cache` handling is mandatory in #8812. (4) Detector
extras ship in one migration with an engine-parity test; short values stay out.
(5) `store nothing` is `metadata`: an empty-content row with the source digest,
never a tombstone. (6) The skeleton differential decides which evidence shapes
join the allowlist.

<details>
<summary>Evidence and measurements</summary>

**Local sample (counts only; no value or path recorded; two local checkout roots, 1 and 20 repositories).** Script:
`git ls-files` per repository (what Eshu would index, since it honors
`.gitignore`), then `rg -q` per file. Exit 0.

| Checkout root | Repositories | `.yarnrc.yml` | with `npmAuthToken` key | literal `npmAuthToken` | with `npmAuthIdent` key | `*.tfvars` | `*.tfvars.json` | `*.tfvars` matching the 138 detector | after excluding `var.`/`data.`/`local.`/`module.` references |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| First local root (worktree directories excluded) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| Second local root | 20 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |

Cross-check: `git ls-files | rg -c -i '(\.tfvars(\.json)?|\.yarnrc\.yml)$'` and
`rg --files --hidden --no-ignore` for the three globs both returned 0 across the
same roots. This is a developer machine, not a deployment; the deployment count
is NOT_CHECKED.

**Detector probe (the gap corpus).** Pure `SELECT` against Postgres 18
(`psql -d postgres`, no DDL), using the migration 138 `CASE` and the
`eshu_secret_line_findings` detector expression, with fabricated values.

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

Marker probe: `db_password = "REDACTED_password_literal"` detected `t`,
suppressed `f`; `npmAuthToken: REDACTED_api_token` detected `t`, suppressed `f`;
`db_password = "[redacted]"` and `db_password = "***"` detected `f`.

**Census query (run on a deployment, counts only; it never selects `content` or
`line_text`).** The first query was validated on temporary tables with the
shapes from `schema_content_store.go` and migration 138; the second was not
run. Findings are meaningful only while
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

**Claim status.**

- Proven (code read at `238b4015a9`, re-read at `816a039136`): admission path,
  the filesystem-mode dotfile prune, and no name-based sensitivity filter;
  verbatim body flow to `content_files.content`; `get_file_content` returns it; `search_file_content` matches `content_lower`
  and returns empty content on hits; `RetainedPaths` keeps a body when the
  content fact is skipped; `Registry.Fingerprint` omits any policy field;
  `FullSnapshot` is non-delta only; tree, counts, language inventory, and
  coverage read `content_files`; relationship evidence reads `content_body` from
  the fact and accepts tfvars; references derive from the stored body;
  `fileContentHash` returns the collector's source digest when set; the upsert
  overwrites `content`, `content_hash`, and `line_count`.
- Proven (run): detector gap table, marker behavior, syntax of the first census
  query, local count of 0.
- Inferred: `content` facts persist `content_body` in `fact_records` for every
  retained generation; real tfvars files yield evidence the skeleton would lose.
- NOT_CHECKED: deployment file, credential, tfvars evidence, and finding counts;
  whether `content_entities` rows exist for these paths; the migration's
  finding-count delta and lock window; retention of the `content` and
  content-entity fact kinds; the delta file-set selection code; `get_file_lines`
  at `line_count = 0`.

</details>
