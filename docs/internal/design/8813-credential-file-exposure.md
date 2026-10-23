# Redact `.yarnrc.yml` and `*.tfvars`, never store them verbatim (#8813)

Status: Accepted (arbiter rulings relayed by the coordinator, 2026-10-23, two
rounds). This record approves no code, schema change, or deployment action.
Part of #8807. The work splits across #8812 (policy enum, marker-mode
redactor, digest rule, fingerprint) and #8833 (skeleton mode, the tfvars
binding, the detector migration, the differential). Citations are at commit
`238b4015a9` and name the function, so they survive small line drift.

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

Nothing in discovery looks at a file's name for sensitivity. The only filters
on the path are the parser registry, `.gitignore`, and `.eshuignore`. A search
of the discovery code for credential, secret, `.env`, `npmrc`, or `netrc`
finds only comment lines in
`go/internal/collector/repo/discovery/discovery.go`.

| Step | Where | What happens |
| --- | --- | --- |
| Admit | `resolveNativeSnapshotFileSet` in `go/internal/collector/repo/git/snapshot_discovery.go` (the closure passed to `ResolveRepositoryFileSetsWithStats`) | A path is kept when `registry.LookupByPath(path)` returns a definition. |
| Match `.yarnrc.yml` | `Registry.LookupByPath` in `go/internal/parser/registry.go` | No exact name or prefix matches. `filepath.Ext(".yarnrc.yml")` is `.yml`, so the `yaml` definition (`registry_definitions.go`, `Extensions: .yaml, .yml`) wins. |
| Match `*.tfvars` | same | Extension `.tfvars` belongs to the `hcl` definition (`Extensions: .hcl, .tf, .tfvars`). `LookupByPath` also hard-codes `*.tfvars.json` to `hcl`. |
| Read body | `streamFacts` in `go/internal/collector/repo/git/fact_builder.go` (`streamContentBodyReadFile`, then `ContentFactEnvelope`) | The whole file is read into a string for every admitted file. |
| Emit | `ContentFactEnvelope` in `go/internal/collector/git/content/envelopes.go` | The body goes into the fact payload as `content_body`. |
| Project | `BuildContentRecord` in `go/internal/projector/runtime/content_record.go` | `content_body` becomes `content.Record.Body` untouched. |
| Store | `ContentWriter.Write` (`content_writer.go`, `body: record.Body`) and `upsertContentFileBatch` (`content_writer_upserts.go`, `row.body`) | The body is upserted into `content_files.content` (`schema_content_store.go`). |

No step transforms the body. The registry has no per-definition store policy
today, which is what #8812 adds.

### Where the body is then readable

- `get_file_content` and `get_file_lines`: `ContentReader.GetFileContent`
  (`go/internal/query/content_reader.go`) returns `content` verbatim through
  `POST /api/v0/content/files/read` (`contentread/content_handler.go`,
  `readFile`).
- `search_file_content`: hit rows come back with an empty `content` column, but
  the match runs against the body (`content_lower`, a stored lowercase copy,
  migration 176) in `go/internal/query/search/unscoped/statements.go`. The
  tool is a substring oracle over the credential, even though it does not print it.
- Derived copies of the same bytes:
  - `content_files.content_lower` (generated, `lower(content)`).
  - `content_file_secret_lines.line_text` (migration 138, the full text of every
    flagged line).
  - `content_entities.source_cache` for any entity the parser cuts from the file.
  - Curated search documents, read from `content_files.content` by
    `eshu_search_document_source_loader.go`.
  - `content_body` in the `content` fact payload of every retained generation.
- `investigate_hardcoded_secrets` redacts its own response from
  `content_file_secret_lines`. That does not protect any of the paths above.

### The exposure

Eshu becomes a second copy of the credential with a different access model
from the source repository: it is aggregated, searchable across repositories,
returned to model-provider transcripts through MCP, and carried into Postgres
backups and WAL. It is still gated by the repository grant check on the
`/api/v0/content/files/*` routes (`auth_scoped_routes_completeness.go`), so
this is credential duplication and aggregation, not an unauthenticated leak.

What the two files can hold:

- `.yarnrc.yml`: `npmAuthToken`, `npmAuthIdent` (base64 `user:password`), and
  `user:password@` userinfo in `npmRegistryServer`, `httpProxy`, `httpsProxy`.
- `*.tfvars`: variable values, including database passwords, API keys, and
  private keys, plus the non-secret environment shape (module versions,
  sizing, PagerDuty declarations).

### Evidence on how common it is

| Sample | `.yarnrc.yml` | literal `npmAuthToken` | `*.tfvars` | literal credential in `*.tfvars` | Source |
| --- | --- | --- | --- | --- | --- |
| One hosted deployment | 2 | 0 | not recorded | not recorded | Issue #8813 body |
| Local checkouts (21 repositories) | 0 | 0 | 0 (and 0 `*.tfvars.json`) | 0 | `<details>` below |
| Deployment census (this research's acceptance item) | NOT_CHECKED | NOT_CHECKED | NOT_CHECKED | NOT_CHECKED | No deployment access |

No exposure has been observed anywhere it was measured, and the local sample
is empty, so it cannot support or weaken the recommendation. The risk rests on
the registry admitting a credential-capable file with no policy. A read-only,
counts-only census query is in the `<details>` block for an operator to run.

### Why existing rows do not heal themselves

Three findings from the code. Each one defeats a "just stop storing it" fix.

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
   (`registry.go`). The freshness hint in `snapshot_freshness.go` folds that
   fingerprint into the generation identity. A policy field leaves the hint
   unchanged, so unchanged repositories are not re-collected and keep their
   stored bodies until the file next changes.
3. **Delta generations never reap.** `FullSnapshot` is set only for a
   repository-scoped, non-delta generation (`content/writer.go`). Editing
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
  patterns (lines 24-55) read the body: `app_repo = "..."`, `app_name = "..."`,
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
| A. Keep both verbatim (status quo) | Zero work. Credentials stay searchable and returnable. | Rejected as the end state. |
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

`content_hash` is the digest of the source file, not of the stored body; the
docs must say so. The writer already works this way: `fileContentHash` returns
`record.Digest` when the collector set it (`content_writer_upserts.go`), and the
collector passes `meta.Digest` today. #8812 must keep computing that digest from
the source bytes before the policy applies. An unchanged file therefore keeps the same
freshness hint across cycles, and the fingerprint, not the digest, invalidates
on a policy change.

Why this shape is safe on every generation: the upsert
(`ON CONFLICT ... DO UPDATE` in `content_writer_sql.go`) overwrites an old body
on full and delta generations; references are deleted per batch before they are
re-derived from the stored body (`content_writer_upserts.go`,
`deleteContentReferenceBatch`); and the migration 138 trigger derives no secret
line from an empty body. There is no `RetainedPaths` edit and no dependency on
the order of entity deletes and upserts.

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
4. **Scope.** Blank `content_entities.source_cache` for a redact-policy path.
   The entity builder slices `file.Body`, so the entity rows are the second
   copy. `content_lower` and `content_file_secret_lines` derive from the stored
   body by generated column and trigger, so they follow with no code; the
   follow-up adds a negative test only.

### Allowlist-skeleton mode, `*.tfvars` and `*.tfvars.json` (#8833)

The redactor is line-wise. Line count and line positions are preserved.

| Line kind | Stored as |
| --- | --- |
| Structural: `{`, `}`, `[`, `]`, `key = {`, `key = [`, heredoc opener and closer | kept |
| `key = value`, value is a boolean, a number, a `var.`, `local.`, `module.`, or `data.` reference, or a version string | kept |
| `key = value`, key flagged by the detector | value becomes `REDACTED_<finding_kind>` |
| `key = value`, any other value | value becomes `REDACTED_value` |
| Comment, bare list element, heredoc body line, any line the classifier cannot place | whole line becomes `REDACTED_value` |

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

| Shape | Today (measured) | Extra |
| --- | --- | --- |
| `npmAuthIdent: "<base64>"` | not detected | key `npmauthident` |
| `user:pass@` in `npmRegistryServer`, `httpProxy`, `httpsProxy` | not detected | URL userinfo |
| `"password": "<literal>"` (JSON-quoted key) | not detected | quoted key form |
| `app_secrets = {` (secret-named block opener) | not detected | opener form |

Values under six characters stay out. The new migration number bumps
`hardcodedSecretSQLPattern`, `hardcoded_secret_migration_binding_test.go`, and
the collector redactor in one change. The sync test is an engine-parity test:
the Go RE2 patterns and the Postgres ARE patterns classify the same corpus
(the gap table in the `<details>` block) identically. A subset check is not
enough, because the two engines case-fold and match differently.

Prove the theory before the migration ships (repository rule): measure the
finding-count delta on a representative corpus, and measure the re-derive lock
window. Migration 138 recorded about 0.27 ms per file under a share-row-exclusive
lock that blocks content writes (38 s at 145k files); this one re-derives the
same table and needs its own number.

### Rows already stored

No rewrite loop, no tombstone. The policy-aware `Registry.Fingerprint` (#8812;
the fleet-wide re-collect is the accepted effect) makes each repository mint a
new generation. The collector re-emits the content record under the new policy,
and the upsert overwrites the stored body: marker-redacted for `.yarnrc.yml`,
skeleton for tfvars.

Gaps the follow-up closes:

1. **Repositories not scheduled for collection** keep their bodies. #8833 ships
   a one-shot operator SQL that applies the `metadata` floor: set `content = ''`
   on matched rows, delete their `content_file_references`, and blank
   `content_entities.source_cache`. It is repo-scoped, 500 rows per statement,
   and idempotent. An operator runs it; no loop does.
2. **Residue.** Dead tuples, WAL, and backups keep the old bytes until vacuum
   and backup expiry. The operator note says so.
3. **Fact payloads.** Superseded `content` facts keep `content_body` until
   generation retention prunes them. Defaults are in `generation_retention.go`
   (`defaultGenerationRetentionMinSuperseded = 24`, `MaxAge = 7 days`,
   `HardMaxAge = 90 days`). Ruling: do not scrub; accept the window and write an
   operator note. Condition: the follow-up executes the retention path against a
   `content` fact and records whether superseded content facts are pruned by
   default policy. If they are exempt, this decision reopens.

Operator signals (3 AM): a gauge of rows whose path matches the policy predicate
and still hold a raw body; counters for `redacted`, `metadata_fallback`, and
`redactor_failed` by policy; a structured log per collection with the same
counts. Before and after census numbers go in the follow-up's PR.

## Consequences

Good:

- A tfvars value cannot be stored or searched unless its shape is allowlisted.
- `.yarnrc.yml` keeps its registry URLs and scopes readable, and
  `investigate_hardcoded_secrets` keeps working on both files.
- The substring oracle closes for both files, because `content_lower` derives
  from what is stored.
- Tree, counts, language inventory, and coverage are unchanged: no row is
  deleted.
- Surfaces defects in #8812's plan (RetainedPaths, fingerprint, derived copies).
- No new background job.

Bad:

- One fleet-wide re-collect when the fingerprint changes.
- A stored tfvars body is not parseable and reads poorly in `get_file_content`.
- Old bytes persist in superseded fact payloads, dead tuples, and backups for a
  bounded time.
- The skeleton may lose some evidence; the differential measures it before the
  binding ships.

Follow-up issues: #8833 carries the tfvars and detector work; the coordinator
files any other item. Both depend on #8812 and are part of #8807.

## Implementation plan

Paths (verify before editing): `go/internal/parser/registry*.go` (enum, field,
fingerprint; #8812), `go/internal/collector/git/content/` (redactors, emit),
`go/internal/collector/repo/git/fact_builder.go` (call site),
`go/internal/storage/postgres/migrations/` (detector migration),
`go/internal/query/hardcoded_secret_migration_binding_test.go` and
`content_reader_security_secrets.go` (constant and binding), docs under
`docs/public/`.

Prove first: the differential (tfvars binding); the finding-count delta and lock
window (migration); the retention check on a `content` fact; `get_file_lines`
behaviour at `line_count = 0`.

Avoid: query-layer redaction (search and exports bypass it), `[redacted]` or
`***` markers, any raw-body fallback, tombstones for these paths, editing
`RetainedPaths`, and a key denylist for tfvars.

## Acceptance criteria

- [ ] #8812: seeded `.yarnrc.yml` with a literal `npmAuthToken` and a
  `user:pass@` registry URL: stored body has neither value and keeps the host;
  `investigate_hardcoded_secrets` still reports the token line.
- [ ] #8812: a `metadata` definition never writes a body, and a test fails if it
  does; its row has `content = ''`, `line_count = 0`, and the source digest.
- [ ] #8812: an unchanged file keeps the same `content_hash` across a policy
  change; the fingerprint changes.
- [ ] #8833: seeded `terraform.tfvars` and `.tfvars.json` with literal values:
  zero value bytes in `content_files`, `source_cache`, or `content_lower`.
- [ ] #8833: differential report lists every evidence and finding difference,
  each closed or accepted in writing, before the fingerprint flip.
- [ ] #8833: engine-parity test passes; a redactor error stores the `metadata`
  row and increments `redactor_failed`.
- [ ] Upgraded database: after one collection cycle the exposure gauge reads 0.
- [ ] Retention path executed against a `content` fact; result recorded.
- [ ] Operator SQL is bounded, idempotent, and tested on a seeded copy.
- [ ] Census before and after recorded as counts.

## Decisions for the arbiter

All decided. None open.

1. `.yarnrc.yml` is marker-mode `redact`. tfvars and tfvars.json are
   allowlist-skeleton `redact`, not `metadata` (ruling 2).
2. Superseded fact payloads are not scrubbed; the retention window is accepted,
   conditional on the retention check.
3. The `content_entities` census is not a gate; `source_cache` blanking is
   mandatory in #8812.
4. Detector extras ship in one new migration with an engine-parity test; short
   values stay out.
5. `store nothing` is renamed `metadata`: a row with `content = ''`,
   `line_count = 0`, and the source digest. Never a tombstone.
6. tfvars are not `metadata`. The skeleton differential, not an assumption,
   decides which evidence shapes join the allowlist.

<details>
<summary>Evidence and measurements</summary>

**Local sample (counts only; no value or path recorded).** Script:
`git ls-files` per repository (what Eshu would index, since it honors
`.gitignore`), then `rg -q` per file. Exit 0.

| Root | Repositories | `.yarnrc.yml` | with `npmAuthToken` key | literal `npmAuthToken` | with `npmAuthIdent` key | `*.tfvars` | `*.tfvars.json` | `*.tfvars` matching the 138 detector | after excluding `var.`/`data.`/`local.`/`module.` references |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `/home/ubuntu/personal-repos` (worktree directories excluded) | 1 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |
| `/home/ubuntu/repos` | 20 | 0 | 0 | 0 | 0 | 0 | 0 | 0 | 0 |

Cross-check: `git ls-files | rg -c -i '(\.tfvars(\.json)?|\.yarnrc\.yml)$'` and
`rg --files --hidden --no-ignore` for the three globs both returned 0 across
the same roots. This is a local sample on a developer machine, not a
deployment. The deployment count is NOT_CHECKED.

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

- Proven (code read at `238b4015a9`): admission path and absence of any
  name-based sensitivity filter; body flows verbatim to `content_files.content`;
  `get_file_content` returns it; `search_file_content` matches `content_lower`
  and returns empty content on hits; `RetainedPaths` keeps a body when the
  content fact is skipped; `Registry.Fingerprint` omits any policy field;
  `FullSnapshot` is non-delta only; tree, counts, language inventory, and
  coverage read `content_files`; relationship evidence reads `content_body` from
  the fact and accepts tfvars; references derive from the stored body;
  `fileContentHash` returns the collector's source digest when set; the upsert
  overwrites `content`, `content_hash`, and `line_count`.
- Proven (run): detector gap table, marker behavior, syntax of the first census
  query, local count of 0.
- Inferred: that `content` facts persist `content_body` in `fact_records` for
  every retained generation (the projector reads them back, but the retention
  statement was not executed); that real tfvars files yield relationship
  evidence the skeleton would lose.
- NOT_CHECKED: deployment file and credential counts; whether
  `content_entities` rows exist for these paths; the finding-count delta and
  lock window of the new migration; retention interaction with the `content`
  fact kind; deployment tfvars evidence and finding counts; the delta
  file-set selection code (which paths a delta re-emits, so whether every
  overwrite lands on a delta as designed); `get_file_lines` behavior at
  `line_count = 0`.

</details>
