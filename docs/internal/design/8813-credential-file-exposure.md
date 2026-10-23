# Redact `.yarnrc.yml` and `*.tfvars` in the content store instead of keeping them verbatim (#8813)

Status: Proposed. Research only. This record approves no code, schema change,
or deployment action. Part of #8807. The implementation depends on the
per-definition `ContentStore` policy in #8812. Citations are at commit
`238b4015a9` and cite the function name, so they survive small line drift.

**Decision (recommended).** Neither file stays "store the body verbatim". Give
both the `redact` policy from #8812: `.yarnrc.yml` and `*.tfvars` (plus
`*.tfvars.json`, which the registry already routes to the same parser) are
stored with credential values replaced at the collector write boundary, and
bodies already stored are rewritten by a bounded, idempotent backfill. Choose
metadata-only only if the arbiter rules that a best-effort denylist is not
acceptable for `*.tfvars` (Decision 1).

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
  sizing, PagerDuty declarations) that is the reason to index it.

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
   therefore leaves the old body in place. #8812's "zero rows" test, written
   against a fresh database, would pass while an upgraded database keeps the
   literal.
2. **The parser fingerprint does not see a policy.** `Registry.Fingerprint`
   hashes key, language, extensions, exact names, and prefix names only
   (`registry.go`). The freshness hint in `snapshot_freshness.go` folds that
   fingerprint into the generation identity. Adding a policy field leaves the
   hint unchanged, so unchanged repositories are not re-collected and keep
   their stored bodies until the file next changes.
3. **Delta generations never reap.** `FullSnapshot` is set only for a
   repository-scoped, non-delta generation (`content/writer.go`).

A `redact` policy avoids finding 1 for the rewritten path, because the content
record is still emitted and the upsert overwrites the body. Findings 2 and 3
still need the backfill below.

## Options

| Option | Cost | Verdict |
| --- | --- | --- |
| A. Keep both verbatim (status quo) | Zero work. Credentials stay searchable and returnable. | Rejected as the end state. Acceptable only until #8812 lands. |
| B. Metadata-only for both | Safest. Loses the readable registry URLs, scopes, and environment shape that are the reason to index them. Needs the finding-1 fix. | Fallback if Decision 1 goes against redaction. |
| C. Redact both, backfill stored rows | Keeps the useful content. Redaction is a denylist, so some values can escape (see below). | Recommended. |
| D. Operator `.eshuignore` for both files | Works today (`docs/public/reference/eshuignore.md` already shows `*.tfvars`). Per repository, opt-in, and drops the file's parser facts too. | Document as the interim mitigation. Not a policy. |

## Decision

### Rule per file

| File | Policy | Notes |
| --- | --- | --- |
| `.yarnrc.yml` | `redact` | Base patterns plus extras for `npmAuthIdent` and URL userinfo. |
| `*.tfvars`, `*.tfvars.json` | `redact` | Base patterns plus block-aware redaction and a JSON-quoted-key pattern. |

Parser facts are unaffected: the parser runs on the original file before the
store policy applies, so PagerDuty declarations and config artifacts built from
`tfvars` (`hcl/pagerduty_declarations.go`, `content/artifacts/hcl.go`) keep
working. The policy governs what is persisted, not what is parsed.

### Redaction contract these two files need from #8812

1. **Marker.** Replace the value with `REDACTED_<finding_kind>`, for example
   `db_password = "REDACTED_password_literal"`. Measured against the migration
   138 expressions: this marker still matches the detector and is not
   suppressed, so `investigate_hardcoded_secrets` keeps reporting the finding.
   `[redacted]` and `***` do not match, so they would silently blind it. The
   marker is also a fixed point: redacting an already redacted line gives the
   same line, which makes the backfill idempotent.
2. **Pattern source.** The base set is the migration 138 `finding_kind`
   expressions, bound by a test as #8812 already requires. Definitions may add
   collector-side extras. The sync test asserts the 138 set is a subset of the
   collector set. Extras for these files are listed under "Gaps" below.
3. **Fail closed.** If the redactor errors, or the file is not valid UTF-8 text,
   store nothing for that path and count it. Never fall back to the raw body.
4. **Block-aware.** When a redacted line opens a `{`, `[`, or heredoc (`<<`),
   redact every line through the matching close.
5. **Scope.** The policy must cover `content_entities.source_cache` and the
   body copies in fact payloads for the path, not only `content_files.content`.
   Otherwise the bytes survive in the derived copies listed above.
6. **Store-nothing needs a tombstone.** For #8812's third policy, emit a content
   tombstone (or drop the path from `RetainedPaths`) so the reap removes an
   existing row. This is a defect in #8812's current plan, found here.
7. **Fold the policy into the generation identity.** Include each definition's
   `ContentStore` value in `Registry.Fingerprint`, or bump
   `gitGenerationContentVersion`, so a policy change is visible to freshness.
   Account for the side effect: a fingerprint change mints a new generation for
   every repository (the documented behavior of the fingerprint). Prefer the
   targeted backfill below for these two files to avoid a fleet re-collect.

### Gaps measured against the migration 138 patterns

Each line was classified with the migration 138 `CASE` and detector run in a
local Postgres 18 (`SELECT` only; fabricated values; see `<details>`).

| Line shape | Detected by 138 | Consequence |
| --- | --- | --- |
| `npmAuthToken: <literal>` | yes (`api_token`) | Covered. |
| `npmAuthToken: "${NPM_TOKEN}"` | no | Correct: an env reference is not a secret. |
| `npmAuthIdent: "<base64>"` | no | Needs a yarnrc extra. |
| `npmRegistryServer` / `httpsProxy` with `user:pass@` | no | Needs a userinfo extra. |
| `db_password = "<literal>"` | yes (`password_literal`) | Covered. |
| `"db_password": "<literal>"` (`.tfvars.json`) | no | Needs a quoted-key extra. |
| `access_key = ...`, `connection_string = ...` | no | Key names outside the denylist. Extras or accept the gap. |
| `app_secrets = {` then `stripe = "<literal>"` | no, no | Block-aware rule 4 closes the inner lines only if the opener is flagged, so add a secret-key opener extra. |
| `db_password = var.db_password` | yes (false positive) | Redacted needlessly. Acceptable: conservative. |
| `api_token = "abc"` (under 6 characters) | no | Below the detector's minimum length. |

The denylist is best effort. It does not make `*.tfvars` safe to store; it
lowers the exposure. That is the substance of Decision 1.

### Rows already stored

Rule: rewrite in place with the same redactor the collector uses. Do not
delete: the path stays searchable by name and its parser-derived facts stay
valid.

Mechanism, following the reducer-hosted loop in
`go/cmd/reducer/package_manifest_backfill.go` (advisory-lock election, so one
replica runs a pass) and the batching of `secret/lines/backfill.go`:

1. **Order.** Ship the redacting writer first. If the backfill runs first, the
   next collection rewrites the literal.
2. **Candidates.** Keyset-paginate `content_files` on `(repo_id, relative_path)`
   where the path matches `\.tfvars(\.json)?$` or `(^|/)\.yarnrc\.yml$`.
   Batch of 500 rows, the same bound migration 138's backfill uses.
3. **Rewrite.** For each row, compute the redacted body. Skip the row when it
   equals the stored body (idempotence). Otherwise
   `UPDATE content_files SET content = $new WHERE repo_id = $1 AND
   relative_path = $2 AND content_hash = $3`. The `content_hash` guard makes a
   concurrent collector write win; the loser retries on the next pass. Keep the
   original digest as the freshness key.
4. **Derived copies.** The statement-level triggers re-derive
   `content_file_secret_lines` on a content change (see
   `secret/lines/README.md`, "Operations"), and `content_lower` is a stored
   generated column, so both follow the UPDATE. The backfill must also rewrite
   `content_entities.source_cache` for the same paths (existence for these two
   files is unmeasured; see Decision 3).
5. **Fact payloads.** Old `content` facts keep `content_body` until generation
   retention removes them. Defaults are in `generation_retention.go`
   (`defaultGenerationRetentionMinSuperseded = 24`, `MaxAge = 7 days`,
   `HardMaxAge = 90 days`). Recommendation: accept the retention window and
   document it; do not scrub immutable fact payloads unless the census finds a
   literal credential (Decision 2).
6. **Residue.** Dead tuples, WAL, and backups keep the old bytes until vacuum
   and backup expiry. State this in the operator note. It is not a bug in the
   backfill.
7. **Telemetry.** Counter `eshu_dp_content_policy_backfill_rows_total{policy,outcome}`
   with outcomes `rewritten`, `unchanged`, `skipped_conflict`, `failed`; a
   histogram for batch duration; a structured log per pass with candidate,
   rewritten, and remaining counts; and a gauge of rows whose path matches the
   candidate predicate and whose body still matches a 138 detector. At 3 AM the
   gauge answers "is anything still exposed".

## Consequences

Good:

- Closes the stored-credential exposure for the two admitted files while
  keeping registry URLs, scopes, and tfvars shape readable.
- `investigate_hardcoded_secrets` keeps working on the redacted rows.
- Closes the substring oracle for these paths, because `content_lower` is
  derived from the redacted body.
- Surfaces three defects in #8812's plan (finding 1, finding 2, derived copies).

Bad:

- Redaction is a denylist; some values escape (table above).
- One more bounded backfill loop to run and observe.
- Old bytes persist in superseded fact payloads, dead tuples, and backups for
  a bounded time.
- `var.*` references and short values are treated imprecisely.

Follow-up issue: to be filed by the coordinator (draft in the handoff report),
depends on #8812, part of #8807.

## Implementation plan

Paths (verify before editing): `go/internal/parser/registry*.go` (policy field
and fingerprint, owned by #8812), `go/internal/collector/git/content/` (redactor
and emit), `go/internal/collector/repo/git/fact_builder.go` (call site),
`go/internal/projector/runtime/repository_refs.go` and `projection.go`
(`RetainedPaths`), `go/cmd/reducer/` (backfill loop), `go/internal/storage/postgres/`
(guarded UPDATE), docs under `docs/public/`.

Prove the theory first (repository rule): before writing the candidate query,
run `EXPLAIN ANALYZE` of the keyset-plus-suffix predicate against a
representative `content_files` corpus. The path trigram index (migration 134)
may serve it, but that is unmeasured here, and the lock and concurrency proof
against the live writer is required before the backfill ships.

Avoid: query-layer redaction (search and exports bypass it), `[redacted]` or
`***` markers, any raw-body fallback, and a fleet-wide fingerprint bump used
only to refresh two file types.

## Acceptance criteria

- [ ] Seeded fixture `.yarnrc.yml` with a literal `npmAuthToken` and a
  `user:pass@` registry URL: stored body has neither value, still has the
  registry host, and `investigate_hardcoded_secrets` still reports the token line.
- [ ] Seeded `terraform.tfvars` with a literal password, a secret-named block,
  and a `.tfvars.json` quoted key: stored body has none of the values.
- [ ] Upgrade test: an existing row holding a literal is redacted by the next
  pass, a second pass reports `unchanged`, and a concurrent collector write is
  not overwritten.
- [ ] For `store nothing`, an upgraded database loses the old row (guards finding 1).
- [ ] A test fails if a redact-policy path reaches `content_files`,
  `content_entities.source_cache`, or `content_lower` with a seeded value.
- [ ] Redactor error produces no row and increments the failure counter.
- [ ] Gap table cases each have a test, including the cases that stay open.
- [ ] Census SQL run on a deployment and recorded as counts.

## Decisions for the arbiter

1. **Redact or metadata-only for `*.tfvars`?** Redact keeps value and loses
   some secrets to the denylist gaps; metadata-only loses the readable shape
   but cannot leak. Recommendation: redact, with the block-aware and
   quoted-key extras, because the hosted and local samples show no exposure and
   the shape is the point of indexing the file.
2. **Scrub superseded fact payloads?** Accept the retention window (7 to 90
   days) or run a bounded scrub of `content_body` on matching old facts.
   Recommendation: accept, and revisit only if the deployment census finds a
   literal credential.
3. **Do `content_entities` rows exist for these paths?** Unmeasured. Run the
   second census query before sizing the backfill. If they do, rule 5 is a
   hard requirement, not a nicety.
4. **Should the collector-side extras (`npmAuthIdent`, userinfo, quoted keys)
   also be added to the migration 138 detector by a new migration?** Without
   it, `investigate_hardcoded_secrets` and the store disagree on those shapes,
   which #8812 sets out to prevent. Recommendation: yes, in the same epic.

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

**Detector probe.** Pure `SELECT` against Postgres 18 (`psql -d postgres`, no
DDL), using the migration 138 `CASE` and the `eshu_secret_line_findings`
detector expression, with fabricated values. Results are the gap table. Marker
probe: `db_password = "REDACTED_password_literal"` detected `t`, suppressed `f`;
`npmAuthToken: REDACTED_api_token` detected `t`, suppressed `f`;
`db_password = "[redacted]"` and `db_password = "***"` detected `f`.

**Census query (run on a deployment, counts only; it never selects `content` or
`line_text`).** The first query was validated on temporary tables with the shapes from
`schema_content_store.go` and migration 138; the second was not run. Findings are meaningful only while
`content_file_secret_lines_state.state = 'ready'`, and the table misses
`npmAuthIdent` and userinfo shapes (gap table).

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

-- Decision 3: do entity rows exist for these paths?
SELECT count(*) FROM content_entities
WHERE relative_path ~* '\.tfvars(\.json)?$' OR relative_path ~* '(^|/)\.yarnrc\.yml$';
```

**Claim status.**

- Proven (code read at `238b4015a9`): admission path and absence of any
  name-based sensitivity filter; body flows verbatim to `content_files.content`;
  `get_file_content` returns it; `search_file_content` matches `content_lower`
  and returns empty content on hits; `RetainedPaths` keeps a body when the
  content fact is skipped; `Registry.Fingerprint` omits any policy field;
  `FullSnapshot` is non-delta only.
- Proven (run): detector gap table, marker behavior, syntax of the first census query,
  local count of 0.
- Inferred: that `content` facts persist `content_body` in `fact_records` for
  every retained generation (the projector reads them back, but the retention
  statement was not executed); that a newly ignored file is reaped on the next
  full snapshot (its file fact disappears, so it leaves `RetainedPaths`); that
  the keyset-plus-suffix query is cheap.
- NOT_CHECKED: deployment file counts and credential counts; whether
  `content_entities` rows exist for these paths; the lock and concurrency
  behavior of the backfill against a live writer; retention interaction with
  the `content` fact kind.

</details>
