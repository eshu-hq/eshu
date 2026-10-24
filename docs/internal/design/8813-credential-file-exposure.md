# Redact `.yarnrc.yml` and `*.tfvars`, never store them verbatim (#8813)

Status: Accepted (arbiter rulings relayed by the coordinator, 2026-10-23, two
rounds; a third ruling, 2026-10-24, set the marker form, the redaction trigger,
and the search-document gap after an independent review). This record approves
no code, schema change, or deployment action. Part of #8807. The work splits
across #8812 (policy enum, marker-mode redactor, digest rule, fingerprint) and
#8833 (skeleton mode, the tfvars binding, the detector migration with its marker
arm, the differential). Citations were taken at `238b4015a9`, re-checked at
`4d78b83273` (`origin/main` after #8922, 2026-10-24), and name the function, so
they survive small line drift.

**Decision.** Both files use the `redact` policy from #8812, in two modes.
`.yarnrc.yml` is closed-schema and uses **marker mode**: credential values are
replaced in place. `*.tfvars` and `*.tfvars.json` (which the registry already
routes to the `hcl` parser) are open-schema, so a key denylist cannot make them
safe: whether a variable is secret is declared in `variables.tf`
(`sensitive = true`), not in its name. They use **allowlist-skeleton mode**: a
value survives only if its shape is provably safe, and every other value is
replaced. Rows already stored are fixed by the re-collect the sweep or a reindex
request forces after the fingerprint changes, not by a rewrite loop. The `metadata`
policy (empty body, row kept) exists in the enum and is the floor for the
operator SQL, but neither file uses it, because it would silently drop the
committed-tfvars-secret finding and the raw-Terraform evidence class.

## Context

### How the two files reach `content_files.content`

Discovery applies no name-based sensitivity filter. Its path filters are the
registry, `.gitignore`, `.eshuignore`, default ignored directories and
extensions, operator globs, and the #8809 symlink check. A search of the
discovery package and `snapshot_discovery.go` for credential, secret, `.env`,
`npmrc`, or `netrc` finds one symlink comment outside tests.

| Step | Where | What happens |
| --- | --- | --- |
| Copy (filesystem-source mode only) | `shouldSkipFilesystemEntry` in `go/internal/collector/repo/git/selection_filesystem.go`, then `preserveFilesystemHiddenPath` in `selection_copy_workspace.go` | Prunes a dot-prefixed entry unless an allowlist keeps it: `preservedHiddenFilesystemFileNames` (GitLab CI names, `.gitmodules`, eight #8814 tool-config dotfiles, nine #8809 version pins; never `.gitignore`), matched case-insensitively, plus `.github`, `.github/workflows`, and `.mvn`. Any other dot-directory is pruned whole. `.yarnrc.yml` is not on the list, so that mode does not store it, but it stores `*.tfvars`. The git-sourced mode keeps hidden names (`IgnoreHidden` is never set outside tests), so it stores `.yarnrc.yml`. #8904 tracks deriving the list from the registry. |
| Admit | `resolveNativeSnapshotFileSet` in `go/internal/collector/repo/git/snapshot_discovery.go` (the closure passed to `ResolveRepositoryFileSetsWithStats`) | A path is kept when `symlinkAdmission.category` names it: a registry definition via `registry.LookupByPath(path)`, or terraform state, documentation, `.gitmodules`, or CODEOWNERS. A symlink also needs a regular-file target in the same category (#8809, #8882). |
| Match `.yarnrc.yml` | `Registry.LookupByPath` in `go/internal/parser/registry.go` | No exact name or prefix matches. `filepath.Ext(".yarnrc.yml")` is `.yml`, so the `yaml` definition (`registry_definitions.go`, `Extensions: .yaml, .yml`, admission L2) wins; `TestDefaultRegistryKeepsCredentialBearingDotfilesOut` leaves it out of the exclusion list on purpose. |
| Match `*.tfvars` | same | Extension `.tfvars` belongs to the `hcl` definition (`Extensions: .hcl, .tf, .tfvars`, admission L2). `LookupByPath` also hard-codes `*.tfvars.json` to `hcl` (listed in `PathDispatches`). |
| Read body | `streamFacts` in `go/internal/collector/repo/git/fact_builder.go` (`streamContentBodyReadFile`, then `ContentFactEnvelope`) | The whole file is read into a string for every admitted file. |
| Emit | `ContentFactEnvelope` in `go/internal/collector/git/content/envelopes.go` | The body goes into the fact payload as `content_body`. |
| Project | `BuildContentRecord` in `go/internal/projector/runtime/content_record.go` | `content_body` becomes `content.Record.Body` untouched. |
| Store | `ContentWriter.Write` (`content_writer.go`, `body: record.Body`) and `upsertContentFileBatch` (`content_writer_upserts.go`, `row.body`) | The body is upserted into `content_files.content` (`schema_content_store.go`). |

No step transforms the body; #8812 adds the per-definition store policy. The
registry still withholds the credential-bearing names (`phpunit.xml(.dist)`,
`.mvn/jvm.config`, `.mvn/maven.config`, `.mvn/settings.xml`, `settings.xml`,
`.pypirc`, `.netrc`, `.env*`, `mise.toml`) while the store has no write-time
redaction (`TestDefaultRegistryKeepsCredentialBearing*`). `.npmrc` is the one
name admitted since #8978, under `metadata` (see the rule table). Admitting any
other needs a new record.

### Where the body is then readable

- `get_file_content` and `get_file_lines`: `ContentReader.GetFileContent`
  (`go/internal/query/content_reader.go`) returns `content` verbatim through
  `POST /api/v0/content/files/read` (`contentread/content_handler.go`,
  `readFile`).
- `search_file_content`: hit rows come back with an empty `content` column, but
  the match runs against the body. Unscoped search matches `content_lower` (a
  stored lowercase copy, migration 176) in
  `go/internal/query/search/unscoped/files.go` (every `content_lower LIKE`
  statement). Repository-scoped search matches `content ILIKE`
  (`content_reader.go`, `content_reader_search_page.go`,
  `content_reader_names.go`). The tool is a substring oracle over the
  credential, even though it does not print it.
- Derived copies of the same bytes:
  - `content_files.content_lower` (generated, `lower(content)`).
  - `content_file_secret_lines.line_text` (migration 138, full text of each
    flagged line).
  - `content_entities.source_cache` for any entity the parser cuts from the file.
  - The curated search document, read from `content_files.content` by
    `eshu_search_document_source_loader.go`. Its `context_text` holds up to
    4096 bytes of the body (`ProjectContentFile` in
    `go/internal/searchdocs/project.go`). The only filter drops a body that
    contains a term such as `api_key`, `access_key`, `password`, `private_key`,
    `secret`, or `token`, so it passes a file whose only credential is
    `npmAuthIdent`, proxy userinfo, or a connection-string shape. Semantic
    search returns `context_text` (`semantic_search_response.go`). The document
    is persisted per generation in the `reducer_eshu_search_document` fact,
    `eshu_search_index_documents`, and `eshu_search_index_terms`; reads join the
    active generation only.
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
| Local checkouts (21 repositories) | 0 | 0 | 0 (and 0 `*.tfvars.json`) | 0 | evidence page |
| Deployment census (this research's acceptance item) | NOT_CHECKED | NOT_CHECKED | NOT_CHECKED | NOT_CHECKED | No deployment access |

No exposure was observed where it was measured; the empty local sample neither
supports nor weakens the recommendation. The risk rests on the registry
admitting a credential-capable file with no policy. A counts-only census query
for an operator is on the evidence page.

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
   (`registry.go`); the #8849 `Ecosystem` and `Admission` labels are not hashed.
   The freshness hint in `snapshot_freshness.go` folds that fingerprint into the
   generation identity. A policy field leaves the hint unchanged, so unchanged
   repositories are not re-collected and keep their stored bodies until the file
   next changes. A registry name addition (#8814, #8809) does move it;
   `gitGenerationContentVersion` (`git-collector.v9`) is no policy hash.
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
  `terraform_evidence.go` accepts `.tfvars` and `.tfvars.json`.
  `discoverTerraformEvidence` reads the body for five raw patterns
  (`terraformPatterns`): `app_repo = "..."`, `app_name = "..."`, a `github.com`
  URL, `repo:org/name:`, and a `/configd/<name>/` or `/api/<name>/` path. It also
  scans the body for module sources, `config_path`, SSM reads, and resource
  blocks, which tfvars rarely carry; the #8833 differential covers the whole
  output. Structured module evidence comes from the file fact's
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
| `.yarnrc.yml` | `redact` | marker | #8812 mode; bound in #8833 with or after the marker-arm migration |
| `*.tfvars`, `*.tfvars.json` | `redact` | allowlist skeleton | #8833 |
| `.npmrc` | `metadata` | none | #8978 |

`.eshuignore` stays the override. A looser tfvars rule needs a new record.

`.npmrc` (#8978) lifts the #8814 and #8813 withholding for that one name. It is
its own exact-name `npmrc` definition (never the `hcl`, `yaml`, or `raw_text`
definitions) bound to `metadata`: the row keeps the path and the source digest
with an empty body, and `metadata` needs no marker-arm migration. A registry-URL
fact answers "which registry does this repository install from". Parser output
is outside the policy, so the `npmrc` parser emits only the scheme, host, port,
and path of `registry=` and `@scope:registry=` lines. It removes the userinfo,
query, and fragment, drops a whole line it cannot prove clean (an environment
expansion, a non-http scheme, a detector match in the scheme or host, or an `@`
that `net/url` did not read as the userinfo delimiter), and never reads a
credential key. The path is an allowlist of documented, token-free vendor
layouts (Artifactory, Nexus, Azure Artifacts, GitLab, Gitea, Cloudsmith,
Artifact Registry, CodeArtifact); any other path, and every Gemfury path, is
dropped to a host-only row, because Gemfury documents the token as the first
path segment and no heuristic sees a bare path token reliably. The seeded
full-stream scan the A1 ruling requires is
`TestNpmrcSeededCredentialReachesNoFactUnderTheProductionBinding` in
`go/internal/collector/repo/git`. Residual: a token a vendor puts in a
documented slot, drawn as a short word or digits only, would pass; the
documented vendors keep the credential in `_authToken`. A private registry on a
custom layout reports its host only.

Ordering (arbiter, 2026-10-24): no production definition binds to marker mode
before #8833's marker-arm migration lands. That covers `.yarnrc.yml` and the
phpunit, `.mvn`, and renovate candidates raised on #8812. #8812 ships the mode
and its tests only. This is dependency order, not serialization.

### Policy enum and row shape (#8812)

The enum is `store`, `metadata`, `redact`. `metadata` writes a `content_files`
row with these values and never a tombstone:

| Column | Value |
| --- | --- |
| `content` | `''` (`content_lower` follows, it is generated) |
| `line_count` | `0`; it describes the stored body. `tree.go` omits `size` when it is `0`; `get_file_lines` counts lines from `content` and never reads it, so an empty body returns one empty line |
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
   `npmAuthToken: REDACTED_api_token`, where the kind is computed on the raw
   line in the detector's `CASE` order. Measured on all six kinds against the
   migration 138 expressions (evidence page, "Marker probe"):
   - `api_token`, `password_literal`, and `secret_literal` lines keep their
     finding with the same kind and are not suppressed.
   - `aws_access_key`, `slack_token`, and `private_key` lines lose it:
     `access_key_id = "REDACTED_aws_access_key"`,
     `slack_webhook = "REDACTED_slack_token"`, and
     `key = "REDACTED_private_key"` are all not detected. The `CASE` tests those
     value shapes (`AKIA`, PEM header, `xox`) before any key name, and the marker
     replaces the shape.
   - A shape marker under a listed key changes kind:
     `npmAuthToken: REDACTED_aws_access_key` and
     `slack_token = "REDACTED_slack_token"` are detected as `api_token`.

   Arbiter decision, 2026-10-24: the marker keeps the form
   `REDACTED_<finding_kind>`, and #8833's one migration adds a marker arm. It
   goes first in the `CASE` and is also added to the line gate and to
   `hardcodedSecretSQLPattern`:
   `REDACTED_(aws_access_key|private_key|slack_token|api_token|password_literal|secret_literal)`
   maps to its own kind, and `REDACTED_value` never matches. With the arm,
   every kind-marked line keeps its raw `(line_number, finding_kind)`. Rejected:
   dropping the three kinds (it knowingly loses committed-secret findings when a
   no-cost fix exists); credential-shaped markers such as `AKIA`-, `xoxb-`, or
   PEM-header-shaped strings (other scanners would probably flag them; inferred,
   not tested); a side fact for the finding (a second finding path, against
   #8812's "the two systems cannot disagree", plus a new fact contract and
   retention class). `[redacted]` and `***` do not match and would blind the
   detector. The marker is a fixed point: redacting a redacted line returns the
   same line.
2. **One pattern constant.** The collector redactor binds to the same Go
   constant as the SQL detector (`hardcodedSecretSQLPattern` in
   `go/internal/query/content_reader_security_secrets.go`), not to a copy.
3. **Fail closed.** If the redactor errors, or the file is not valid UTF-8 text,
   store the `metadata` row and count it. Never fall back to the raw body.
4. **Scope.** `content_entities.source_cache` is the second copy: the entity
   builder slices `file.Body`. Under `redact` the slice is taken from the
   redacted body, never the source. `content_lower` and
   `content_file_secret_lines` derive from the stored body by generated column
   and trigger, so they follow with no code; the follow-up adds a negative test.
5. **Trigger** (arbiter decision, 2026-10-24). The trigger is the definition's
   credential-key list. For `.yarnrc.yml` that is `npmAuthToken` and
   `npmAuthIdent` at top level and under `npmScopes.*` and `npmRegistries.*`,
   plus userinfo in `npmRegistryServer`, `npmPublishRegistry`, `httpProxy`, and
   `httpsProxy`. Those values are redacted at any length and charset. Only an
   exact `${NAME}` reference survives; a `${NAME:-literal}` default is redacted.
   A detector match on any other line redacts the value to the end of the line
   (arbiter, 2026-10-24, B1): a key match applies the listed-key rule at the
   detector's key, and a value-shape match is replaced from the shape to the end
   of the line, so a credential tail after a space, a comma, or a comment cannot
   survive. Inside a PEM `BEGIN`..`END`
   block every line is redacted. The detector's `{6,}` floor is unchanged: it
   limits detection, not redaction of a listed key, so a short value under a
   listed key gains a finding through the marker (accepted). The file is a
   closed schema: Yarn rejects unknown settings in strict mode
   (`Configuration.ts`; the `enableStrictSettings` default is NOT_CHECKED), so a
   value-shape credential can sit there only as a misplaced `npmAuthToken` or
   `npmAuthIdent` value, or in free text such as a comment.

### Allowlist-skeleton mode, `*.tfvars` and `*.tfvars.json` (#8833)

The redactor is line-wise. Line count and line positions are preserved.

| Line kind | Stored as |
| --- | --- |
| Structural: `{`, `}`, `[`, `]`, `key = {`, `key = [`, heredoc opener and closer | kept |
| `key = value`, key flagged by the detector (checked first) | value becomes `REDACTED_<finding_kind>`, even for a reference |
| `key = value`, value is a boolean, a number, a `var.`, `local.`, `module.`, or `data.` reference, or a version string | kept |
| `key = value`, any other value | value becomes `REDACTED_value` |
| Comment, bare list element, heredoc body line, or any line the classifier cannot place as `key = value`, flagged by the detector | whole line becomes `REDACTED_<finding_kind>`, so it keeps its finding |
| The same kinds of line, not flagged | whole line becomes `REDACTED_value` |

A detector-flagged key wins over the reference-kept rule, so
`db_password = var.db_password` is stored as a marker. A flagged line keeps its
raw `(line_number, finding_kind)` once the marker arm lands. Only unflagged
lines become `REDACTED_value`; the differential lists each one, because any
evidence on them is lost.

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
block openers, plus the marker arm from "Marker mode". The gap table on the
evidence page shows each extra is undetected today.

The `{6,}` detection floor is unchanged (arbiter, 2026-10-24); it limits
detection, not redaction of a listed key. The new migration number bumps
`hardcodedSecretSQLPattern`, `hardcoded_secret_migration_binding_test.go`, and
the collector redactor in one change. The sync test is an engine-parity test:
the Go RE2 patterns and the Postgres ARE patterns classify the same corpus
(the gap table on the evidence page) identically. A subset check is not
enough, because the two engines case-fold and match differently.

Prove the theory before the migration ships: measure the finding-count delta on
a representative corpus and the re-derive lock window. Migration 138 recorded
about 0.27 ms per file under a lock that blocks content writes (38 s at 145k
files); this one needs its own number.

### Rows already stored

No rewrite loop, no tombstone. The policy-aware `Registry.Fingerprint` (#8812;
the fleet-wide re-collect is the accepted effect) makes each repository mint a
new generation when the sweep or a reindex request next forces it; the flip
alone forces no visit. The collector re-emits the content record under the new policy,
and the upsert overwrites the stored body: marker-redacted for `.yarnrc.yml`,
skeleton for tfvars. The heal depends on that generation re-emitting content
records for every admitted path; the #8812 upgrade test proves it, and the delta
file-set selection code is NOT_CHECKED.

Gaps the follow-up closes:

1. **Repositories the re-collect has not reached** keep their bodies. #8833 ships
   a one-shot operator SQL, run fleet-wide and guarded to generations ingested
   before the reindex watermark, that applies the `metadata` floor: set
   `content = ''` on matched rows, delete their `content_file_references`, and blank
   `content_entities.source_cache`. It also deletes the active generation's search
   index documents and terms for the matched paths, and the matching active
   `reducer_eshu_search_document` fact rows by `fact_id`, because the active
   generation keeps serving raw `context_text` after `content = ''`; the reindex
   rebuilds them from redacted rows. It is repo-scoped, 500 rows per statement,
   and idempotent. An operator runs it; no loop does. Run order and exit
   criterion are in [the cleanup runbook](8813-credential-file-exposure-cleanup.md);
   the census and statements are on
   [the purge SQL page](8813-credential-file-exposure-purge-sql.md).
2. **Residue.** Dead tuples, WAL, and backups keep the old bytes until vacuum
   and backup expiry. The operator note says so.
3. **Fact payloads and search documents.** Superseded `content` facts keep
   `content_body`, content-entity facts keep their `source_cache` bytes, and
   superseded `reducer_eshu_search_document` facts plus superseded
   `eshu_search_index_documents` and `eshu_search_index_terms` rows keep the
   bounded `context_text`, until
   generation retention prunes them. Defaults are in `generation_retention.go`
   (`defaultGenerationRetentionMinSuperseded = 24`, `MaxAge = 7 days`,
   `HardMaxAge = 90 days`). Superseded search documents are not served, because
   reads join the active generation. Ruling: do not scrub; accept the window and
   write an operator note. Condition: #8833 executes the retention path against
   a `content` fact, a content-entity fact, and a search document and records
   whether each kind is pruned by default policy. If one is exempt, this
   decision reopens. The search-document projector keys on the generation, not
   on `content_hash`, so digest-before-policy does not affect it: the next
   forced full mints a new generation and re-projects it from the redacted
   `content_files.content`.

Operator signals (3 AM): the gauge `eshu_dp_content_policy_raw_rows`, defined
once by #8833 as the detector-bounded raw-copy test: unsuppressed
`content_file_secret_lines` rows on a policy path whose `line_text` carries no
`REDACTED_<finding_kind>` marker (marker lines keep a finding by design, so a
"body matches the detector" gauge would never read zero); counters for `redacted`, `metadata_fallback`, and `redactor_failed` by policy; a
structured log per collection with the same counts.

## Consequences

Good: a tfvars value cannot be stored or searched unless its shape is
allowlisted; `.yarnrc.yml` keeps its registry URLs readable;
`investigate_hardcoded_secrets` keeps reporting every kind-marked finding with
its raw kind once the marker arm lands (before it only the key-identified kinds
survive, so nothing binds earlier); the substring oracle closes because
`content_lower` derives from what is stored; tree, counts, language inventory,
and coverage are unchanged because no row is deleted; no new background job.

Bad: one fleet-wide re-collect after the fingerprint changes, at the next sweep
or reindex; a stored tfvars
body is not parseable and reads poorly in `get_file_content`; old bytes persist
in superseded fact payloads, dead tuples, and backups for a bounded time; the
skeleton may lose some evidence, which the differential measures before the
binding ships.

## Implementation plan

Paths (verify before editing): `go/internal/parser/registry*.go` (enum,
fingerprint; #8812), `go/internal/collector/git/content/` (redactors, emit),
`go/internal/collector/repo/git/fact_builder.go` (call site),
`go/internal/storage/postgres/migrations/` (detector migration),
`go/internal/query/content_reader_security_secrets.go` (constant and binding).
Prove first: the differential, the migration's finding-count delta and lock
window, the retention check (search documents included), and the empty-body
response of `get_file_lines`. Avoid
query-layer redaction (search and exports bypass it), `[redacted]` or `***`
markers, any raw-body fallback, tombstones, editing `RetainedPaths`, and a
tfvars key denylist.

## Acceptance criteria

The issue bodies are the binding acceptance lists. If this record and an issue
body ever differ, the issue body wins. The lists that follow them are on
[the acceptance page](8813-credential-file-exposure-acceptance.md): #8812 owns
the enum, marker redactor, digest rule, fingerprint, `source_cache` rule, upgrade
test, and policy docs; #8833 owns the bindings, the detector migration with its
marker arm, the engine-parity test, the gap-table corpus, the differential, the
operator SQL, the retention check, and the census.

## Decisions for the arbiter

All decided; none open. (1) `.yarnrc.yml` is marker-mode `redact`; tfvars and
tfvars.json are skeleton-mode `redact`, not `metadata`. (2) Superseded content
and content-entity fact payloads are not scrubbed; the retention window is
accepted, conditional on #8833's retention check. (3) The `content_entities`
census is not a gate; `source_cache` handling is mandatory in #8812. (4) Detector
extras ship in one migration with an engine-parity test. The `{6,}` detection
floor is unchanged and does not limit redaction of a listed key; the trigger is
the definition's credential-key list (arbiter, 2026-10-24). (5) `store nothing`
is `metadata`: an empty-content row with the source digest, never a tombstone.
(6) The skeleton differential decides the evidence shapes. (7) The marker keeps
`REDACTED_<finding_kind>`, and #8833's migration adds a marker arm first in the
`CASE`, in the line gate, and in the Go constant; no production definition binds
to marker mode before it lands (arbiter, 2026-10-24). (8) Search documents are
owned by #8833 with the same retention window and no scrub, and the operator SQL
covers the active generation's documents and terms and deletes the matching
active `reducer_eshu_search_document` fact rows by `fact_id` (arbiter,
2026-10-24). (9) A detector
match without a listed key redacts from the separator (a key match) or from the
shape (a value-shape match) to the end of the line, with one marker per line; this
supersedes "redacts the matched span" in the trigger item above (arbiter,
2026-10-24, after the #8812 review found `Authorization: Bearer <token>` and
`db_password=a,<tail>` left a tail). Parser-extracted values stay outside the
policy and a definition may bind `metadata` or `redact` only after a seeded
full-stream scan proves its parser output clean (arbiter A1, 2026-10-24).

## Evidence

The measurements (local sample, gap table, the six-kind marker probe, census
query, claim status, and NOT_CHECKED list) are in
[the evidence page](8813-credential-file-exposure-evidence.md). Hosted figures
quoted above come from the #8813 issue body; deployment counts are NOT_CHECKED.
