# Redact `.yarnrc.yml` and store no body for `*.tfvars` (#8813)

Status: Accepted for the file rules (arbiter ruling relayed by the coordinator,
2026-10-23). Two items still need a ruling (see "Decisions for the arbiter").
This record approves no code, schema change, or deployment action. Part of
#8807. The implementation depends on the per-definition `ContentStore` policy
in #8812. Citations are at commit `238b4015a9` and name the function, so they
survive small line drift.

**Decision.** `.yarnrc.yml` gets the `redact` policy from #8812: credential
values are replaced at the collector write boundary. `*.tfvars` and
`*.tfvars.json` (which the registry already routes to the `hcl` parser) store
no body. A key denylist is complete only against a closed schema, and tfvars is
an open schema: whether a variable is secret is declared in `variables.tf`
(`sensitive = true`), not in the name, so a redacted tfvars body can still hold
a credential. Rows already stored are fixed by the one-time re-collect that the
policy-aware registry fingerprint triggers, not by a rewrite loop.

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
   stored content. A `store nothing` policy that only omits the content fact
   leaves the old body in place, and #8812's "zero rows" test, run against a
   fresh database, would still pass.
2. **The parser fingerprint does not see a policy.** `Registry.Fingerprint`
   hashes key, language, extensions, exact names, and prefix names only
   (`registry.go`). The freshness hint in `snapshot_freshness.go` folds that
   fingerprint into the generation identity. A policy field leaves the hint
   unchanged, so unchanged repositories are not re-collected and keep their
   stored bodies until the file next changes.
3. **Delta generations never reap.** `FullSnapshot` is set only for a
   repository-scoped, non-delta generation (`content/writer.go`). Editing
   `RetainedPaths` therefore cannot be the fix: a delta never consults it.

### What reads `content_files` or the content fact besides the file tools

These decide what "store no body" costs for `*.tfvars`.

- **Repository tree, file counts, language inventory, and coverage** are read
  from `content_files` rows (`go/internal/query/repository/tree.go`,
  `stats.go`, `language_inventory.go`, `coverage.go`). A deleted row removes the
  path from all four.
- **Relationship evidence** is discovered by the reducer from the `content`
  fact bodies (`relationships.DiscoverEvidence`,
  `envelopeContentIdentity` in `evidence_content_index.go`).
  `isTerraformArtifact` in `terraform_evidence.go` and the catalog matcher in
  `catalog_matcher.go` explicitly accept `.tfvars` and `.tfvars.json`. If the
  collector stops putting the body in the fact, this evidence disappears for
  tfvars. Whether real tfvars files yield any is unmeasured.
- **`content_file_references`** rows are derived from the file body
  (`contentReferencesForFiles` in `content_writer_references.go`).
- **Parser facts and config-artifact rows** come from the original file before
  any store policy applies (`content/artifacts/hcl.go`: the tfvars row is built
  from the path alone; `hcl/pagerduty_declarations.go`).

## Options

| Option | Cost | Verdict |
| --- | --- | --- |
| A. Keep both verbatim (status quo) | Zero work. Credentials stay searchable and returnable. | Rejected as the end state. |
| B. Redact both | Keeps tfvars shape. A denylist cannot cover an open schema, so a credential can still be stored. | Rejected for tfvars. Chosen for `.yarnrc.yml`, whose key set is closed and small. |
| C. `.yarnrc.yml` redacted, tfvars no body | Cannot leak a tfvars value. Loses readable tfvars shape and, unless handled, three dependents (see above). | Chosen. |
| D. Operator `.eshuignore` for either file | Works today (`docs/public/reference/eshuignore.md` shows `*.tfvars`). Per repository, opt-in, and drops the file's parser facts too. | Stays the override. Not a policy. |

## Decision

### Rule per file

| File | Policy | Notes |
| --- | --- | --- |
| `.yarnrc.yml` | `redact` | Detector extras below, then collector redactor bound to the same constant. |
| `*.tfvars`, `*.tfvars.json` | no body (`store nothing`) | Parser facts and config-artifact rows keep coming from the original file. `.eshuignore` stays the override. |

A future `redact` for tfvars is allowed only with an **allowlist-shaped value
redactor**: keep booleans, numbers, `var.`, `local.`, and `module.` references,
and version strings; redact every other string literal. A denylist, including a
longer one, never qualifies.

### `.yarnrc.yml` redaction contract (for #8812 and the follow-up)

1. **Marker.** Replace the value with `REDACTED_<finding_kind>`, for example
   `npmAuthToken: REDACTED_api_token`. Measured against the migration 138
   expressions, this marker still matches the detector and is not suppressed, so
   `investigate_hardcoded_secrets` keeps reporting the line. `[redacted]` and
   `***` do not match and would blind it. The marker is a fixed point:
   redacting a redacted line returns the same line.
2. **One pattern constant.** The collector redactor binds to the same Go
   constant as the SQL detector (`hardcodedSecretSQLPattern` in
   `go/internal/query`), not to a copy.
3. **Fail closed.** If the redactor errors, or the file is not valid UTF-8 text,
   store nothing for that path and count it. Never fall back to the raw body.
4. **Scope.** Blank `content_entities.source_cache` for a redact-policy path.
   The entity builder slices `file.Body`, so the entity rows are the second
   copy. This is mandatory in #8812 (ruling 3). `content_lower` and
   `content_file_secret_lines` derive from the stored body by trigger and
   generated column, so they follow with no code; the follow-up adds a negative
   test only.

### One new detector migration

Migration 138 never gets edited. One new migration replaces
`eshu_secret_line_findings` with an extended `CASE`, re-derives
`content_file_secret_lines`, and carries these extras, all ASCII keys:

| Shape | Today (measured) | Extra |
| --- | --- | --- |
| `npmAuthIdent: "<base64>"` | not detected | key `npmauthident` |
| `user:pass@` in `npmRegistryServer`, `httpProxy`, `httpsProxy` | not detected | URL userinfo |
| `"password": "<literal>"` (JSON-quoted key) | not detected | quoted key form |
| `app_secrets = {` (secret-named block opener) | not detected | opener form |

Values under six characters stay out. Moving to the new migration number
bumps `hardcodedSecretSQLPattern`,
`hardcoded_secret_migration_binding_test.go`, and the collector redactor in one
change. The sync test is an engine-parity test: the Go RE2 patterns and the
Postgres ARE patterns classify the same corpus (the gap table in the
`<details>` block) identically. A subset check is not enough, because the two
engines case-fold and match differently.

Prove the theory before the migration ships (repository rule): measure the
finding-count delta on a representative corpus, and measure the re-derive lock
window. Migration 138 recorded about 0.27 ms per file under a share-row-exclusive
lock that blocks content writes (38 s at 145k files); this one re-derives the
same table and needs its own number.

The extras lose their tfvars use once tfvars stores no body, but they still
apply to every other stored file type, which is why they belong in the detector
and not in a yarnrc-only list.

### Rows already stored

No rewrite loop. Two existing mechanisms do the work once #8812 adds the policy
to `Registry.Fingerprint` (ruling: the fleet-wide re-collect is the accepted
effect):

- **`.yarnrc.yml`:** the new generation re-emits the content record, redacted.
  The upsert overwrites the stored body (`ON CONFLICT DO UPDATE` in
  `content_writer_sql.go`).
- **`*.tfvars`:** the collector emits a content tombstone (a `Deleted` record)
  for the path, not an edit to `RetainedPaths`. The writer then deletes the
  `content_files`, `content_entities`, and `content_file_references` rows for
  it. `content_file_secret_lines` goes by foreign-key cascade. Entity deletes
  run before entity upserts in `ContentWriter.Write`, so parser-derived entities
  in the same write are written back (inferred; the follow-up proves it).

Gaps the follow-up closes:

1. **Repositories not scheduled for collection** keep their bodies. Ship a
   one-shot operator SQL: repo-scoped, bounded to 500 rows per statement,
   idempotent, deleting the path-matched rows and the entity and reference rows
   with them. It is run by an operator, not by a loop.
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
and still hold a body; counters for `redacted`, `tombstoned`, and
`redactor_failed` by policy; a structured log per collection with the same
counts. Before and after census numbers go in the follow-up's PR.

## Consequences

Good:

- A tfvars value cannot be stored or searched, whatever its name.
- `.yarnrc.yml` keeps its registry URLs and scopes readable, and
  `investigate_hardcoded_secrets` keeps working on the redacted rows.
- The substring oracle closes for both files, because `content_lower` derives
  from what is stored.
- Surfaces defects in #8812's plan (RetainedPaths, fingerprint, derived copies).
- No new background job.

Bad:

- One fleet-wide re-collect when the fingerprint changes.
- Old bytes persist in superseded fact payloads, dead tuples, and backups for a
  bounded time.
- tfvars loses its readable body. The three dependents above (tree and counts,
  relationship evidence, references) must be handled or accepted. See the open
  decisions.
- `investigate_hardcoded_secrets` can no longer report a committed tfvars secret,
  because the derived table has nothing to scan.

Follow-up issue: to be filed by the coordinator (draft in the handoff report),
depends on #8812, part of #8807.

## Implementation plan

Paths (verify before editing): `go/internal/parser/registry*.go` (policy field
and fingerprint, #8812), `go/internal/collector/git/content/` (redactor, emit,
tombstone), `go/internal/collector/repo/git/fact_builder.go` (call site),
`go/internal/storage/postgres/migrations/` (detector migration),
`go/internal/query/hardcoded_secret_migration_binding_test.go` and
`content_reader_security_secrets.go` (constant and binding), docs under
`docs/public/`.

Prove first: the finding-count delta and lock window for the migration; the
retention check on a `content` fact; the tombstone test showing parser-derived
entities survive the same write.

Avoid: query-layer redaction (search and exports bypass it), `[redacted]` or
`***` markers, any raw-body fallback, editing `RetainedPaths`, and a denylist
for tfvars.

## Acceptance criteria

- [ ] Seeded `.yarnrc.yml` with a literal `npmAuthToken`, an `npmAuthIdent`, and
  a `user:pass@` registry URL: stored body has none of the values and keeps the
  registry host; `investigate_hardcoded_secrets` still reports the token line.
- [ ] Seeded `terraform.tfvars` and `.tfvars.json` with literal values: zero
  `content_files`, `source_cache`, and `content_lower` bytes of any value.
- [ ] Upgraded database: after one collection cycle, zero rows hold a body for a
  policy path (the exposure gauge reads 0).
- [ ] Parser-derived entities and config-artifact rows for the tfvars path
  survive the tombstone write.
- [ ] Engine-parity test: Go RE2 and Postgres ARE classify the gap corpus alike.
- [ ] Redactor error produces no row and increments `redactor_failed`.
- [ ] Retention check recorded for a `content` fact.
- [ ] One-shot operator SQL is bounded, idempotent, and tested on a seeded copy.
- [ ] Census before and after recorded as counts.

## Decisions for the arbiter

Resolved by the 2026-10-23 ruling:

1. `.yarnrc.yml` redacts; tfvars and tfvars.json store no body; a future tfvars
   redact needs an allowlist-shaped value redactor.
2. Superseded fact payloads are not scrubbed; the retention window is accepted,
   conditional on the retention check.
3. The `content_entities` census is not a gate; `source_cache` blanking is
   mandatory in #8812.
4. Detector extras ship in one new migration with an engine-parity test; short
   values stay out.

Open, found while applying the ruling:

5. **What does "store no body" mean for the `content_files` row of a tfvars
   path?** Deleting the row (tombstone) removes the path from the repository
   tree, file counts, language inventory, and coverage, and drops the derived
   references. Keeping a row with empty `content` keeps all of those, and the
   upsert overwrites an old body with no tombstone and no `RetainedPaths`
   concern. Recommendation: the empty-content row. The cost is that #8812 must
   define `store nothing` as "metadata row, no body" and not "no row".
6. **tfvars-derived relationship evidence and the secret signal.** The reducer
   discovers relationship evidence from the `content` fact body, and tfvars is
   an accepted input. Dropping the body from the fact drops that evidence, and
   drops the committed-secret finding for tfvars. Options: (a) the collector
   emits the evidence and the per-line finding facts (kind and line number, no
   value) from the original body before the body is dropped; (b) accept the
   loss after measuring it on a representative corpus. Recommendation: measure
   first (the count is unknown), then choose (a) if it is non-zero. The side
   table cannot take (a) directly: Go code must not write
   `content_file_secret_lines`.

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
  the fact and accepts tfvars; references derive from the file body.
- Proven (run): detector gap table, marker behavior, syntax of the first census
  query, local count of 0.
- Inferred: that `content` facts persist `content_body` in `fact_records` for
  every retained generation (the projector reads them back, but the retention
  statement was not executed); that parser-derived entities survive a tombstone
  in the same write (delete runs before upsert in `ContentWriter.Write`); that
  real tfvars files yield relationship evidence.
- NOT_CHECKED: deployment file and credential counts; whether
  `content_entities` rows exist for these paths; the finding-count delta and
  lock window of the new migration; retention interaction with the `content`
  fact kind; the measured volume of tfvars-derived relationship evidence.

</details>
