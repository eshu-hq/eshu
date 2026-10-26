# Changelog

Eshu's release-by-release changelog. Per-version release notes with the full
set of merged pull requests, verification gates, and runtime evidence live
under `docs/public/releases/v*.md`. The index of stable releases is at
`docs/public/releases/index.md`. This file is the rolling change log for the
in-flight release train, the place where a maintainer can find the most
recent shipped work grouped by feature area.

## Unreleased

### Supply chain: sbt dependencies join their Maven package

- **An sbt `%%` dependency now reaches the registry package it publishes**
  ([#8961](https://github.com/eshu-hq/eshu/issues/8961)). A Scala dependency
  declared in `build.sbt` used to carry no registry identity, so it never
  matched a package version or a security alert. A `%` row now joins its exact
  Maven coordinate, and a `%%` row joins only `organization:name_<binary>`
  (`_2.13`, `_3`) when one Scala binary version is known. The unsuffixed name
  never matches, and `%%%`, a custom `cross`, a pre-release, and a row with no
  known binary version join nothing. The active-dependency read for a Maven
  ecosystem now also reaches Gradle and sbt rows. Stored sbt rows gain keys
  through the reducer's sbt repair within minutes of the upgrade; rows a
  pre-upgrade reducer or ingester rewrites during the rollout are repaired at
  the next change of that repository. The repair only adds keys, so readiness
  does not flip. The git collector content version does not change.
### Git collector: content epoch replaces the hand-bumped version

- **The git content version is now derived from a per-file pin ledger**
  ([#9097](https://github.com/eshu-hq/eshu/issues/9097)). The hand-typed
  `gitGenerationContentVersion` (`git-collector.v15`) is gone. The freshness
  hint now folds `git-collector.<epoch>`, where the epoch comes from the
  committed ledger `go/internal/collector/repo/git/epoch/ledger.tsv`: one line
  per content-version fixture file (input digest, fact count, output digest)
  plus a `_repository` line. The re-pin (`-update-content-version-pin`)
  advances the epoch only when a file's output moved on unchanged input, and
  chains it from the previous epoch, so two branches that move output
  differently cannot land on the same value. A fixture-only change keeps the
  epoch. This change is itself one advance (from `git-collector.v15` to
  `git-collector.7609794ecb42af4d`), so each git repository stores one new
  generation on its next sync, as after any bump.

### Declared runtime versions reach the graph

- **`runtime_declaration.version` facts now project to the graph as
  `Repository-[:DECLARES_RUNTIME]->RuntimeDeclaration`**
  ([#8816](https://github.com/eshu-hq/eshu/issues/8816), part of
  [#8807](https://github.com/eshu-hq/eshu/issues/8807)). A new reducer domain,
  `runtime_declaration_projection`, writes one `RuntimeDeclaration` node per
  declared version, keyed on (repository, file, line, runtime, token
  position), with the spec, its kind and the parsed version parts. Version
  parts stay null for aliases, ranges and unresolved specs. A full generation
  replaces a repository's declarations; a delta generation replaces only the
  declarations of the files it changed or deleted, so deleting a pin file
  removes its declarations. The graph schema gains the
  `runtime_declaration_identity` constraint and a `(runtime, version_major)`
  index for the coming version-threshold query (#8817). The extra follow-up
  fact moves `gitGenerationContentVersion` to `git-collector.v15`, so each
  repository re-collects once. Evidence:
  `docs/internal/evidence/8816-runtime-declaration-graph.md`.

### CI: CHANGELOG.md rebases without conflicts

- **`CHANGELOG.md` is union-merged, and a new gate checks the result**
  ([#9096](https://github.com/eshu-hq/eshu/issues/9096)). Every PR appends under
  `## Unreleased`, so parallel PRs conflicted on the same lines and each
  conflict was a keep-both merge. `.gitattributes` now marks the root file
  `/CHANGELOG.md` `merge=union`, so a local rebase or merge keeps both sides.
  The two SDK changelogs are release documents and keep conflicts. Union keeps
  what it cannot judge, so `scripts/verify-changelog-unreleased.sh` (gate
  `changelog-unreleased`) fails a second `## Unreleased` heading, a conflict
  marker, a duplicate `### ` heading, and a duplicate entry. An entry is keyed
  on its bold title joined across wrapped lines, because a first-line key
  reports two different titles that share an opening line. Two branches that
  edit the same existing entry still merge to a file that passes the gate, so
  read such a merge. Whether GitHub's server-side merge honors the attribute is
  unverified, so rebase locally before merging.

### Parser reads: one per-file read cap

- **A parser file over 64 MiB is skipped instead of read whole**
  ([#8986](https://github.com/eshu-hq/eshu/issues/8986)). `shared.ReadSource`,
  the read every language parser uses, now refuses a file over
  `shared.MaxSourceBytes` with `ErrSourceTooLarge`. The git collector skips such
  a parser file at discovery from its recorded size, before the import
  pre-scan, so the rest of the repository keeps indexing; before this change
  the file was read whole by every parse worker that touched it, and with only
  the shared cap a single such file would have failed the whole snapshot at
  pre-scan. The skip counts as `skip_reason=content:oversize` on
  `eshu_dp_discovery_files_skipped_total` and logs WARN
  `git_snapshot_file_skipped_oversize` per file with the repository, scope, and
  generation ids, the path, and the size. The parse and content-stream reads
  are capped too, for a file that grows after discovery, and a file that grows
  past the cap before the import pre-scan is skipped like an oversize file
  instead of failing the snapshot (it also marks the snapshot degraded). The parser
  side reads of headers, sibling modules, tsconfig and package.json, composer
  files, Python package and Lambda files, and `go.mod`. A delta whose target
  grew past the cap retracts the file instead of leaving its earlier facts
  served as current, and a workflow over the cap no longer marks every delta
  sync `workflow_read_failed`. The largest lock file
  measured in the repository and golden corpus is 274,035 bytes, so no lock
  file below the cap is affected. `gitGenerationContentVersion` moves from
  `git-collector.v13` to `git-collector.v14`, so each repository re-collects
  once when its reconciliation sweep next comes due.

### Security alerts: a newly allowlisted repository no longer reads as covered

- **Per-generation target set**
  ([#8838](https://github.com/eshu-hq/eshu/issues/8838)). Each security-alert
  generation records the repositories it collected in
  `scope_generations.payload` (`security_alert_target_set` and
  `security_alert_target_set_hash`). An empty answer for a repository added to
  an allowlist, or for a reused repository `scope_id`, now reads
  `target_incomplete` with `no_committed_generation` until a generation that
  collected it projects. Before, it read `complete`, exact and fresh. The set's
  hash is part of the freshness hint, so an allowlist change commits a new
  generation even when the provider's alerts did not change. Each target
  commits one extra generation after the upgrade; an unchanged allowlist still
  skips every cycle. No DDL. An unanchored empty answer costs what it did
  (19.88 to 19.75 ms for 1,024 targets with 5,000-repository sets). An anchored
  one adds about 0.2 ms and 14 buffers per covering target at 5,000
  incompressible repositories (0.09 to 0.34 ms for one target).
  Evidence: `docs/internal/evidence/8838-security-alert-target-set.md`.

### Security alerts: org targets over 100 repositories are collected as shards

- **Upgrade order: the collector and the API before the coordinator.** All
  three ship in one image set. A new coordinator plans shard scopes an old
  collector does not know (those claims fail retryable until it upgrades). A
  new collector that an old coordinator still hands a sharded target's base
  scope (or that a coordinator on another configuration does, mid rolling
  restart) fails the claim retryable and logs
  `security_alert.target_sharded_coordinator_skew` instead of collecting part
  of the allowlist.
- **An org target of more than 100 same-org repositories is planned as shards
  of at most 100** ([#9019](https://github.com/eshu-hq/eshu/issues/9019)).
  One claim reads at most 100 repositories after a truncated org read, so a
  larger target used to leave the rest unread on every claim. The coordinator
  now plans one work item per shard, computed from the configuration alone
  (`go/internal/scope/org/shard`, shared by the planner, the collector, and
  the query layer). A target of at most 100 keeps its `scope_id`; a larger one
  is collected as `<scope_id>#shard-<bits>` scopes, and every repository is
  read within one scan interval (250 repositories in 4 shards: 254 provider
  requests at `max_pages` 1, measured). A configured `scope_id` containing
  `#shard-` is now refused.
- **Scopes the partition no longer plans are retired.** After a split (the
  base scope) or a merge (both leaves), the coordinator plans a retirement
  work item and the collector commits an empty generation attested with
  `TrustedDeletion`, so list, count, and inventory stop returning that
  scope's alerts and a repository removed from the allowlist leaves every
  read. Until a re-created scope's first collection lands, its empty answer
  reads as still collecting, not complete. A removed or re-scoped target is
  not retired yet
  ([#9071](https://github.com/eshu-hq/eshu/issues/9071)); an ordinary shrink
  of a scope can still be refused by the empty-successor guard
  ([#9072](https://github.com/eshu-hq/eshu/issues/9072)).
- **Fix: org targets never covered an anchored empty answer.** The query
  layer compared an org target's scope with `organization`, a value the
  collector refuses; it accepts only `org`. Repositories of an org target now
  read the target's real collection state instead of
  `repository_outside_target_set`.
- **Tenant-bounded deployments** authorize a shard item, and the retirement
  of one, through the base scope's grant; an ungranted base still denies
  them. A scoped API token granted the base scope id keeps reading every
  shard's alerts.

### Status: blockage rows name the domain that holds the fence

- **`queue_blockages[]` carries `holder_domain`**
  ([#8954](https://github.com/eshu-hq/eshu/issues/8954)). A blocked reducer
  row used to show its conflict key but not which domain held the fence, so an
  operator could not see that a blocked `security_alert_reconciliation` was
  waiting for `supply_chain_impact`. Each row now reports the domain of the one
  live claimed or running row that holds the fence, read in the same statement.
  The field is an empty string for a readiness-gated row. The status JSON,
  text report, and `docs/openapi/runtime-admin-v1.yaml` move together. A stored
  status summary written before this change still decodes, with an empty holder.

### Collector status: extension-defined failure classes on component-activated first-party kinds

- **A component-activated instance that carries a first-party kind such as
  `jira` reads `extension_defined` for the classes its extension defines**
  ([#8951](https://github.com/eshu-hq/eshu/issues/8951)). The status leaf
  bounded failure classes per collector kind and had no instance-level signal,
  so these classes read `unknown`. The status reader now marks an instance
  extension-hosted when the generic component planner claims its stored
  configuration, the check the coordinator already uses, and the class bound
  collapses pattern-valid extension classes only for such instances. An in-tree
  instance of the same kind still reads `unknown` for an unregistered class.
  The marker is a boolean read from existing state: no payload field and no new
  `failure_class` label value. The admin failed-scope list, the acknowledge
  response, and the stored acknowledgement records bound the class by the same
  instance, so they agree with collector status. A record whose instance is
  gone reads the kind-only form.

### Security alerts: a reconciliation give-up reads apart from a genuine no-match

- **An `unmatched` reconciliation row now says when the reducer gave up
  waiting for impact evidence**
  ([#8956](https://github.com/eshu-hq/eshu/issues/8956)). A row written after
  the handler stopped waiting carries `impact_wait_outcome`
  (`gave_up_unmatched` or `gave_up_bound_exceeded`) on the reducer payload, the
  `/api/v0/supply-chain/security-alerts/reconciliations` row, and the
  `list_security_alert_reconciliations` MCP row. A genuine no-match, where the
  same generation's `supply_chain_impact` work all succeeded and matched
  nothing, omits it and keeps its payload byte for byte.
  `eshu_dp_security_alert_reconciliation_impact_wait_total` gains the
  `producer_settled` outcome for that case, and `gave_up_unmatched` no longer
  counts it. No migration; rows written before the change carry no field.

### Parser: Gradle version selectors are no longer exact versions

- **One fail-closed exact-version rule for Gradle readers**
  ([#8988](https://github.com/eshu-hq/eshu/issues/8988)). A `build.gradle` or
  `build.gradle.kts` dependency declared as `1.+`, `latest.release`,
  `[1.0,2.0)`, `1.0!!` or `1.0@jar` used to be marked `resolved`, and the
  reducer took the selector text as the observed version. The new
  `go/internal/parser/gradle/selector` package holds the rule and a shared
  table. A selector row is now `unresolved` and keeps the text as written.
  The reducer backstop `payloadcore.ExactManifestDependencyVersion` also
  rejects a trailing `+`, a `latest.` prefix, `!!`, `@`, and `:` (a colon
  means leaked notation text). Security-alert
  reconciliation now uses that same function and reports a selector as a
  missing version, not a malformed one. Semver build metadata such as
  `1.0.0+build.1` stays exact in the reducer for npm and Cargo. A string
  notation classifier no longer leaks into the version: `g:a:1.0:jdk8` now
  resolves to `1.0`, not `1.0:jdk8`, and text past the classifier
  (`g:a:1.0:jdk8:extra`, which Gradle rejects) emits no row. The git
  collector content version moves to `git-collector.v12`, so each repository
  re-collects once and stale `resolved` selector rows are replaced.

- **An unresolved manifest row never becomes an observed version in the
  persisted-fact readers** ([#8988](https://github.com/eshu-hq/eshu/issues/8988)).
  Package correlation already gave an `unresolved` row no observed version
  (#9101). The persisted package-consumption correlation fact now carries an
  optional `dependency_resolution_state` field (a minor, additive contract
  change that old readers ignore), and security-alert reconciliation and
  supply-chain impact skip an `unresolved` row, so a `1.0!`, a `1.0+build.1` or
  an sbt version identifier the parser could not resolve no longer becomes the
  installed version. A `partial` row and an
  ecosystem that emits no state keep today's answer; a fact written before the
  field exists keeps today's answer until its repository is re-materialized.

### Security alerts: disabled Dependabot alerts no longer read as a credential failure

- **A 403 saying Dependabot alerts are disabled classifies as `not_found`**
  ([#8947](https://github.com/eshu-hq/eshu/issues/8947)). A repository-scope
  target whose repository has Dependabot alerts disabled used to fail
  terminally as `auth_denied`, so readiness told the operator to fix a working
  credential. It now fails as terminal `not_found`, the class the client
  already documented for a disabled alert surface, and
  `eshu_dp_security_alert_provider_requests_total` records
  `status_class=not_found` for it. A 403 with a rate-limit signal stays
  `rate_limited`, and every other 403 stays `auth_denied`. The org fan-out
  still reports the repository as `alerts_disabled` and continues.

### CI: cold-cache cgo builds on GCC 15 and glibc 2.43

- **Go builds compile cgo as `gnu17`**
  ([#9066](https://github.com/eshu-hq/eshu/issues/9066)). A Go build cache miss
  on the `go-sitter-forest/perl` grammar failed with
  `bsearch.h:72:7: error: expected identifier or '(' before '_Generic'` on
  GCC 15 with glibc 2.43 (the Ubuntu 26.04 `ubuntu-latest` image before the runners were pinned to `ubuntu-24.04`), because C23's
  `bsearch` macro breaks the grammar's own `bsearch` definition. Every workflow
  that builds Go, the Makefile, and the developer entrypoints (`pre-push`,
  `pre-pr`, `prove`, `run-selected-gates`, `install-local-binaries`) and the
  golden-corpus and Ifa build helpers now set `CGO_CFLAGS="-O2 -g -std=gnu17"`,
  keeping Go's default `-O2 -g`. A script run by hand outside those needs the
  flag in the environment; on an affected host run
  `go env -w CGO_CFLAGS="-O2 -g -std=gnu17"` once. The new `cgo-cflags-guard`
  gate fails a workflow or a `CGO_ENABLED=1 go` script that lacks the flag.
  Remove the flag once the grammar builds under C23; see
  [Cgo C Dialect Flag](docs/public/reference/local-testing/cgo-cflags.md).

### Parser: `.npmrc` registry URLs

- **`.npmrc` is admitted under the `metadata` content-store policy and reports
  its registry URLs**
  ([#8978](https://github.com/eshu-hq/eshu/issues/8978)). Eshu could not say
  which package registry a repository installs from, because `.npmrc` sat next
  to its credentials and was withheld. It now has its own exact-name `npmrc`
  definition bound to `metadata`: the content row keeps the path and the source
  digest with an empty body, and a new parser in `go/internal/parser/npmrc` emits
  one `config_kind=registry_url` row per `registry=` and `@scope:registry=` line
  with the scheme, host, port, and path. Userinfo, query, and fragment are
  removed. A line is skipped whole if it holds an environment expansion, a
  non-http scheme, a shape the shared secret detector flags in the scheme or
  host, or an `@` that the URL parser did not read as the userinfo delimiter
  (`https://user:/pw@host/` would otherwise report the user name as the host).
  The path is kept only on a documented token-free vendor layout (Artifactory,
  Nexus, Azure Artifacts, GitLab, Gitea, Cloudsmith, Artifact Registry,
  CodeArtifact, including Azure feed views such as `my-feed@Release`); any other path, and every Gemfury path, is dropped and the row
  keeps the scheme and host with `registry_path_withheld`. No other key is read,
  so `_authToken`, `_auth`, `_password`, `//host/:username`, and `email` values
  reach no fact. Parser output is not governed by the content-store policy, so
  the proof is a seeded full-stream scan of every emitted fact
  (`TestNpmrcSeededCredentialReachesNoFactUnderTheProductionBinding`) and a fuzz
  target with a seeded credential tail. The parser registry fingerprint moves
  once, so every repository re-collects once. The default registry is now
  governed, so every repository pays one policy lookup per file: the snapshot
  stage for 5,000 files went from 8.8 ns to 1.09 ms
  (`BenchmarkContentStoreGovernedDefault`). Filesystem source mode now copies
  `.npmrc` into the managed workspace. A token a vendor puts in a documented
  path slot would still be emitted if it is a short word, digits only, or a
  PascalCase word of up to four parts and 24 bytes.

### Docs: a dropped hand-written section now fails the docs gates

- **Required hand-written sections are checked**
  ([#9028](https://github.com/eshu-hq/eshu/issues/9028)). The parser
  admission check covers only the generated table in
  `docs/public/languages/admitted-files.md`, so a squash dropped the
  hand-written `## Content store policy` section and nothing failed. The new
  `required-doc-sections` gate (`scripts/verify-required-doc-sections.sh`)
  reads the heading list in `scripts/required-doc-sections.txt` and fails,
  naming the heading and the page, when one is missing. It runs in the
  `docs-helm-hygiene` CI job and in the path-selected docs gates for any
  change under `docs/public/`. To protect another section, add a line to the
  list.

### Security alerts: a failing repository no longer pins the org fan-out

- **The org fan-out start moves forward past a failing repository**
  ([#8946](https://github.com/eshu-hq/eshu/issues/8946)). An org-scope claim
  stopped at the first repository whose read failed for a reason that is not
  repository-specific, and every later claim restarted at the top and stopped
  there again, so the repositories behind it were never read. For an allowlist
  of at most 100 same-org repositories the next claim now starts after the
  failing repository, reads the rest, wraps around, and retries it last. A
  failed read at the last position closes a lap: the start returns to the top,
  where it was before, and stays there until a claim reads every repository or
  that last repository leaves the allowlist; a rate limit leaves the start. With
  several failing repositories the start settles after at most one stored
  generation per failing repository plus one, the committer then skips
  unchanged generations, and the settled read set is never smaller than the
  one before. A repository behind a failing one can stay unread, labeled
  `org_repository_fanout_stopped`, until the failing repository is fixed or
  removed. The position commits with the generation in the org read state row
  (migration 232: `fanout_start_repository`, `fanout_next_repository`,
  `fanout_lap_repository`), so it survives a restart. A target with more than
  100 same-org repositories still reads only its first 100 on every claim; the
  collector now logs one `security_alert.org_fanout_over_cap` warning for it at
  startup, and splitting it into targets of at most 100 repositories is the fix
  (#9019 tracks doing it automatically). Spans gain
  `eshu.security_alert.org_fanout.start_offset`, `.next_offset`, and `.lapped`.

### Collector readiness: acknowledge an expected terminal failure

- **An operator can acknowledge one terminal collector failure**
  ([#8830](https://github.com/eshu-hq/eshu/issues/8830)). A failure on a target
  nobody will fix (a deleted repository, a credential revoked on purpose) used
  to keep the instance `degraded` and `failed` for as long as the target kept
  failing. `POST /api/v0/admin/collector-failures/acknowledge` and
  `eshu admin collector-failure acknowledge` record the acknowledgement for one
  instance and scope with a required reason and idempotency key, behind an admin
  token, with one governance audit event per decision (migration 231).
  `POST /api/v0/admin/collector-failures/query` and `eshu admin collector-failure
  list` list the failed scopes of one instance, because no status surface carries
  a scope id. The
  instance then reads `partial` with the blocker `terminal fetch failure
  acknowledged: <class> (<operation>)`, never healthy or implemented, and the
  failure stays visible as an `acknowledged_failure` object on collector status
  and readiness. The acknowledgement covers that failure and later failures on the
  scope with the same raw class; a completed work item after it, or a failure of
  another class, reports the failure again with no further write. It has no
  expiry. An acknowledgement of a scope with no unresolved failure inside the
  window is refused with 422. The operator's reason is returned only by
  `POST /api/v0/admin/collector-failures/acknowledgements/query`. The
  terminal-failure gauge counts only unacknowledged failures; the new
  `eshu_runtime_coordinator_collector_terminal_failure_acknowledged_instances`
  gauge and `eshu_dp_collector_terminal_failure_acknowledgements_total` counter
  cover the rest. The 3 AM runbook says when to acknowledge and when not to.
### Operations: credential-file cleanup runbook

- **Operator runbook for rows stored before #8833**
  ([#8833](https://github.com/eshu-hq/eshu/issues/8833)).
  `docs/public/deployment/credential-file-cleanup.md` holds the run order,
  the read-only census, the watermark-guarded purge SQL for `.yarnrc.yml` and
  `*.tfvars` copies (file bodies, entity caches, references, and the active
  generation's search documents and fact payloads), the exit criterion, and
  what the cleanup cannot reach (superseded generations until retention, dead
  tuples, WAL, and backups). Live tests run the page's SQL verbatim, including
  against the real content writer, and prove the default retention policy
  prunes every superseded copy kind.

### Collector: .yarnrc.yml and *.tfvars are stored redacted

- **Credential-bearing files bound to the content-store policy**
  ([#8833](https://github.com/eshu-hq/eshu/issues/8833)). `*.tfvars` and
  `*.tfvars.json` now bind through a dedicated `tfvars` definition (hcl engine)
  stored as an allowlist skeleton, and `.yarnrc.yml` through a dedicated
  `yarnrc` definition (yaml engine) stored with `npmAuthToken` and
  `npmAuthIdent` values replaced at any nesting and only the userinfo of its
  registry and proxy URLs replaced. Parser facts, path search, the tree, and the
  counts are unchanged. The registry fingerprint moves, so each repository
  re-collects once at its next forced snapshot.
- **Migrations 234 and 235: marker arm and detector extras.**
  `eshu_secret_line_findings` now keeps a `REDACTED_<kind>` line's raw kind
  (a case-sensitive marker arm), and detects `npmAuthIdent`, JSON-quoted keys,
  `user:pass@` URL userinfo, and HCL blocks that name a secret. 234 adds
  `policy_version` to the side table's state row. 235 replaces the function
  and, in the same transaction, moves the state to `not_built`; the reducer's
  repair then rebuilds the side table, and until it publishes, investigation
  reads use the legacy scan, which applies the same policy. Neither migration
  locks `content_files`. On a 150k-file corpus the shipped function adds 7,956
  findings to 16,403 (none lost) and costs 24 to 33% more per derived file;
  see `docs/internal/evidence/8833-detector-migration.md`.
- **`eshu_dp_content_policy_raw_rows`** counts raw credential lines still
  stored on governed paths; it is the exit signal of the #8813 cleanup runbook.


### Collector: skeleton redactor and URL userinfo rule for the content-store policy

- **Allowlist-skeleton redactor and URL-userinfo keys, not yet bound**
  ([#8833](https://github.com/eshu-hq/eshu/issues/8833)). The `redact` policy
  gains a `skeleton` mode for open-schema files such as `*.tfvars`: it keeps
  structure, keys, and values whose shape cannot carry a credential (booleans,
  numbers, `var.`/`local.`/`module.`/`data.` references, quoted versions), writes
  `REDACTED_<finding_kind>` for a value or line the hardcoded-secret detector
  reports, and `REDACTED_value` for everything else. Line numbers are kept. The
  `marker` mode can now list URL-valued keys (`npmRegistryServer`, `httpsProxy`)
  whose URL loses only its userinfo, so the host stays readable; any value the
  rule cannot read as exactly one URL is redacted whole.
  `eshu_dp_collector_content_store_files_total` gains a `redactor` label
  (`marker`, `skeleton`, `none`). No definition binds either mode yet, so the
  registry fingerprint and every stored body are unchanged; the bindings for
  `.yarnrc.yml` and `*.tfvars` land with the detector migration in a later
  #8833 change.

### Storage: security-alert empty-answer read no longer scans target history

- **Latest-work-item index for the collection-state read**
  ([#8840](https://github.com/eshu-hq/eshu/issues/8840)). Migration 233 adds
  `workflow_work_items_security_alert_scope_updated_idx` on `(scope_id,
  updated_at DESC, work_item_id ASC) WHERE collector_kind =
  'security_alert'`, built `CONCURRENTLY`. The security-alert empty-answer
  read used to scan and sort every work item a target ever had, so a long
  history could push it past its 1 s probe deadline. It is now a one-entry
  index-only probe per target. On 561,120 work items, a 120,000-item target
  went from 22 to 36 ms to about 0.1 ms (13 or 14 buffers), and 50 targets
  from 46 to 64 ms to about 1 ms. The index is partial so the work-item
  retention prune keeps its plan; a full key made that prune 8x to 13x slower
  on targets that recovered from a run of terminal failures.
  Worst case, every write a `security_alert` row: about 9% more WAL per
  insert, claim, and completion at 4 and 8 clients.
  Evidence: `docs/internal/evidence/8840-security-alert-latest-work-item-index.md`.

### Parser: hostile Cargo TOML no longer crashes the ingester

- **Nesting guard before every Cargo TOML decode**
  ([#8985](https://github.com/eshu-hq/eshu/issues/8985)). A `Cargo.toml` or
  `Cargo.lock` of about 0.9 MB with nested `[`, inline tables, or a long
  dotted key used to overflow the goroutine stack inside go-toml v2.3.1, a
  fatal error that killed the whole ingester. The new shared guard
  `go/internal/parser/shared/toml/nesting` measures nesting in one
  allocation-free pass that skips strings and comments, and the Rust parser
  rejects a file deeper than 64 levels as unparseable. The collector skips
  that file and keeps indexing the repository; a too-deep ancestor workspace
  manifest is skipped and the member still parses. A collector-wide read cap
  is tracked separately in #8986.

### CI: readiness shard weights refreshed from the first weighted wave

- **Live PostgreSQL readiness shard weights refreshed**
  ([#8930](https://github.com/eshu-hq/eshu/issues/8930)).
  `specs/live-postgres-readiness-weights.v1.json` now also fits run
  40071537963, the first wave on the weighted cut (shard jobs 1242, 958, 1244,
  and 1418 s). The two packages the first cut priced at the default are now
  measured, leaving one unweighted test. The refreshed cut predicts 1176.3,
  1204.3, 1291.2, and 1318.1 s (worst 1318.1 s, largest deviation from the mean
  5.7%) against the 1440 s budget. Replayed on the wave's own cut, shard 2
  predicted +25.2% over its measured 958 s. A second wave on the refreshed cut
  is not yet checked. Evidence:
  `docs/internal/evidence/8930-readiness-weights-refresh.md`.

### Parser: pip constraints files

- **`constraints.txt` and `constraints-*.txt` now give Python version-policy rows**
  ([#8821](https://github.com/eshu-hq/eshu/issues/8821)). The pip requirements
  parser reads `constraints.txt`, `constraints-*.txt`, and `constraints_*.txt`.
  A constraint limits the version a package gets if something else installs
  it, so each row carries `central_version_declaration` (the flag a NuGet
  central `PackageVersion` uses), so no consumption, alert, or `DEPENDS_ON`
  evidence or edge results from it. Registry target planning still picks the
  rows as collection targets, as it does for NuGet pins, and no match or edge
  follows. A row never stores the raw line, a line with a direct source (URL,
  VCS, path) keeps only its kind, and a pin whose marker holds URL-shaped text reads as a
  malformed row. The entity `source_cache` is withheld for these rows. The file body
  keeps the whole file, credentials included, until the
  [#8812](https://github.com/eshu-hq/eshu/issues/8812) content-store policy
  covers this parser. The family needs the `.txt` extension, so a
  `constraints-*.yaml` policy file keeps its YAML parser. These files were
  documents before; they are no longer emitted as documents. The git
  collector content version moves to `git-collector.v11`, so each repository
  stores one new generation on its next sync.

### Parser: uv.lock dependency rows

- **`uv.lock` now gives exact-version Python dependency rows**
  ([#8821](https://github.com/eshu-hq/eshu/issues/8821)). A fail-closed
  version-1 reader in `go/internal/parser/pythondep` resolves a `[[package]]`
  entry only when its name, version, and source match a small documented
  grammar. Only the public PyPI index yields `config_kind=dependency`; git,
  path, editable, url, and other-index entries get sibling kinds, and anything
  else is a counted `malformed_dependency` row, with totals in the payload's
  `uv_lock_state`. The project itself and its `[manifest]` workspace members
  are counted there, not emitted, so a uv-locked repository with only PyPI
  pins can read `ready_zero_findings` instead of `unsupported`. Source URLs
  lose userinfo, query, and fragment, and a source with any other `@` is
  refused. Quoted-key and root-array package forms read partial, and so does
  an array left open outside an entry: any `[package` header line ends it, so
  the entries after it are still read. A members list is trusted only when it
  follows the array grammar, holds at most 4,096 names, and each member path
  stays inside the workspace. Dev or
  runtime scope is not captured.
  The raw lock body, including direct-URL credentials `uv` persists, is stored
  verbatim in the content store until the
  [#8812](https://github.com/eshu-hq/eshu/issues/8812) policy gates land.

### NuGet central package files parsed

- **`Directory.Packages.props` and `Directory.Build.props` are now parsed**
  ([#8824](https://github.com/eshu-hq/eshu/issues/8824)). The registry admits
  the two exact names (case-insensitive, no generic `.props` rule) and the
  NuGet project parser reads them with a fail-closed XML scanner. `PackageVersion`,
  `GlobalPackageReference`, and `PackageReference` items become dependency rows
  in their own section, and literal `PropertyGroup` values become
  `msbuild_property` rows. A `PackageVersion` is a version pin: it is not
  joined to the project `PackageReference` rows that use it. The reducer never
  counts it as package consumption, a `DEPENDS_ON` edge, or security-alert
  evidence, the Postgres consumption sidecar never stores it, and supply-chain
  readiness does not count it as `package.consumption`, so a repository with
  only pins reads `evidence_incomplete` with `owned_packages` missing. Conditional, expression, and ambiguous values stay
  partial, and constructs outside the documented grammar are skipped and
  counted in `msbuild_props_limits`. A row that MSBuild can change after its
  `Include` (a later `Update` or `Remove`, a duplicate `Include`, a conditioned
  metadata child, `Exclude`, an `Import` or other unread element, or a
  `ManagePackageVersionsCentrally` that is not a literal `true`) is partial
  with an `item_ambiguity` reason. MSBuild `%XX` escapes are not unescaped:
  an escaped version is unresolved and an escaped item spec is treated like a
  wildcard. The `.csproj` path now applies the same rule (#8994): a
  `PackageReference` whose `Include` or `Update` holds a `%XX` escape emits
  no row, and an escaped version (declared, or reached through a property)
  stays `unresolved_msbuild_property`. A `GlobalPackageReference` is `development` scope (`test` for a test-named package), because
  NuGet sets `PrivateAssets="All"` on it; other rows leave `dependency_scope`
  absent (`unresolved_asset_metadata`) when a conditioned or unresolved asset
  value could change it. Input size, nesting depth, row counts,
  tracked item names, and field lengths are bounded. The change
  adds two registry names, so every repository re-collects once; the content
  version constant stays at `git-collector.v10` because the parser registry
  fingerprint already moves the generation version. The ecosystem-gate
  allowlist drops its `Directory.Packages.props` entry.

### Gradle version catalog becomes dependency facts

- **`gradle/libs.versions.toml` is parsed**
  ([#8822](https://github.com/eshu-hq/eshu/issues/8822)). The registry admits
  the exact name `libs.versions.toml` on the existing `gradle` definition, and
  a new `go/internal/parser/gradle/catalog` package reads `[libraries]` and
  `[plugins]` entries into the existing Gradle dependency row shape. A version
  is `resolved` only when the file writes out one exact version or points at a
  plain `[versions]` string with `version.ref`; a rich version, a missing
  reference, an interpolation, or a selector (`1.+`, `latest.release`, a range,
  `1.0!!`, `1.0@jar`) stays `unresolved` with no value, and a construct outside
  the documented grammar yields no row and is counted in
  `gradle_catalog_state`. Aliases match the way Gradle normalizes them (`-`,
  `_` and `.` are one separator); Gradle keeps only one of two colliding
  aliases, chosen by registration order the file does not show, so every
  colliding row stays `unresolved` and is counted in `alias_collision_count`.
  An alias written as a dotted entry or a `[libraries.alias]` sub-table emits
  no row but still collides, and two aliases with one Gradle accessor name
  (`fooBar`, `foo-bar`) collide too. A key with more than 64 dotted segments,
  or a file with more than 262,144 key segments in all, makes the file
  `too_deep`.
  A library string needs exactly three parts and a plugin string two, and a
  file TOML rejects yields no rows: a key tree checks every key and header
  with go-toml's rules, so `x = ...` with `x.module = ...` or a
  `[libraries.x]` sub-table, a `[versions]` alias that is also a rich version,
  or a key repeated in any table or inline table is `malformed`, and so is a
  number or date value go-toml's decoder rejects (`1979-02-30`, an int64
  overflow, `1e400`). Plugin rows use `config_kind=gradle_plugin`, so the
  package reducer ignores them. The parser refuses a file over 2 MiB, nesting
  over 16 levels, and rows past 5000 per table, and counts each. The TOML
  library overflows the stack on a one-million-level array, so the nesting
  check runs before it. No fact kind, schema, or content version changes: the
  exact name moves the registry fingerprint. The wrapper pin,
  `gradle/wrapper/gradle-wrapper.properties`, is admitted separately, below.

- **`gradle/wrapper/gradle-wrapper.properties` is parsed for the Gradle
  version** ([#8822](https://github.com/eshu-hq/eshu/issues/8822)). A
  `distributionUrl` can carry `user:password@` userinfo, so the name is its own
  `gradle_wrapper` definition bound to the `metadata` content-store policy:
  the row keeps the path and the source digest with an empty body, and the file
  is never stored. The parser emits one `gradle_distribution` row with the
  version from the archive name (`gradle-8.5-bin.zip`) and the `bin` or `all`
  type, and nothing else: no URL, host, userinfo, query, or other property,
  even when the caller asks for source indexing. A value outside the archive
  grammar (a repeated `distributionUrl`, a version-less `gradle-bin.zip`, a
  custom archive name) yields no row and is counted in
  `gradle_wrapper_state.unsupported_count`. A seeded scan of every emitted fact
  finds none of the planted userinfo, query, fragment, or second-property
  values. The package reducer ignores the row (`config_kind` is not
  `dependency`). No fact kind or schema changes. The parser registry
  fingerprint moves once, so every repository re-collects once, and the
  `content_version` fixture pin moves with the new wrapper fixture.

### Content store: per-definition policy

- **Parser definitions can now withhold or redact a file body in the content
  store** ([#8812](https://github.com/eshu-hq/eshu/issues/8812)). A definition
  names one of three policies. `store` keeps the body (every definition today).
  `metadata` keeps an empty-body row, so the path stays in the tree, counts,
  language inventory, and coverage. `redact` (marker mode) replaces credential
  values with `REDACTED_<finding_kind>`. The policy applies at the collector
  write boundary: the `content` fact, `content_files`, the derived secret-line
  and reference rows, and `content_entities.source_cache` all follow it. A
  redactor error or non-UTF-8 input stores a metadata row and counts
  `eshu_dp_collector_content_store_files_total{outcome="redactor_failed"}`; it
  never stores the raw body. `content_hash` stays the digest of the source file.
  `Registry.Fingerprint` includes a non-store policy, so binding one makes every
  repository re-collect once; this change binds none, so the fingerprint and
  freshness hints do not move. The marker redactor shares the migration 138
  detector through the new `go/internal/secret/detect` package. See
  [Admitted File Names](docs/public/languages/admitted-files.md#content-store-policy).

### Security alerts: unread org repositories no longer read as complete

- **An empty security-alert answer for a repository the collector did not read
  in full now reports `target_incomplete` with the reason**
  ([#8943](https://github.com/eshu-hq/eshu/issues/8943)). Every org-scope
  generation records, in the same transaction, whether the org stream
  stopped at `max_pages` and which allowlisted repositories the fan-out left
  unread (past the 100-repository cap, rate limited, stopped, failed, or alerts
  disabled). The read path reports such a repository with the stored
  `org_repository_*` or `provider_open_alert_page_limit_reached` reason, and a
  repository read in full still reads `complete`. The record is two tables
  (migration 230) that generation retention removes with the generation. No
  repository name appears in a metric label or span attribute; the observe span
  gains `eshu.security_alert.org_read_truncated`.
  Each org target commits one extra generation after the upgrade so that its
  first record is written; repository-scope targets are unchanged.

### Gate: supported languages declare manifest and pin files

- **Ledger-driven manifest and pin gate**
  ([#8825](https://github.com/eshu-hq/eshu/issues/8825)). Each row in
  `specs/language-feature-parity-ledger.v1.yaml` now declares `ecosystem`
  (`none` plus a reason when the language has no package ecosystem),
  `manifest_files`, and `pin_files`. A test in `go/internal/parser` fails when
  the registry does not admit a listed manifest at L2 or pin file at L1. Six
  names the registry lacks today are allow-listed with an issue each.

### Content-version fixtures now reach discovery

- **The six dependency-manifest fixtures of the content-version pin test are
  now indexed by the pin digest** ([#8905](https://github.com/eshu-hq/eshu/issues/8905)).
  They sat under a directory named `deps`, which discovery prunes, so the gomod,
  NuGet, Node and Python dependency parsers had no fixture the digest could see
  and a change to their output moved no digest. The fixtures moved to
  `manifests/`, a new guard resolves the fixture with the collector's own
  discovery and fails on any parsed file it skips, and the pinned stream grew
  from 293 to 311 facts. `gitGenerationContentVersion` moves from
  `git-collector.v9` to `git-collector.v10`, so each repository re-collects once
  when its reconciliation sweep next comes due. Collector output for real
  repositories does not change.

### Declared runtime versions become typed facts

- **`package.json` and `go.mod` now emit `runtime_declaration.version` facts**
  ([#8815](https://github.com/eshu-hq/eshu/issues/8815), part of
  [#8807](https://github.com/eshu-hq/eshu/issues/8807)). The git collector
  reads `engines.node`, `.npm`, `.yarn` and `.pnpm`, `volta.node`, `.npm`,
  `.yarn` and `.pnpm`, and `packageManager` from every `package.json`, and the
  `go` and `toolchain` directives from every `go.mod`, at any depth. A monorepo
  gets one set of facts per package directory. The `source_kind` names the
  section (`package_json_engines`, `package_json_volta`,
  `package_json_package_manager`, `go_mod_go`, `go_mod_toolchain`). A range,
  `lts/*`, `workspace:*`, `catalog:`, `toolchain default`, `toolchain local`
  and `${X}` are kept as written and never resolved. `packageManager`
  `pnpm@9.1.0+sha512.…` is runtime `pnpm`, `raw_spec` `9.1.0`: the integrity
  suffix is cut. `require`, `replace` and every other `go.mod` line never emit,
  and a copy under `node_modules` or `vendor` emits nothing.
- **A value outside the version grammar never reaches a fact.** The two files are
  stored with the `store` policy, so a manifest value must be at most 128 bytes
  of version grammar with no long opaque token and no URL; anything else is
  dropped and counted, never truncated, and the file's other declarations still
  emit. The collector logs WARN `git_runtime_declaration_values_rejected` with
  `rejected_count`. A `package.json` that is not readable JSON, or is over 1 MiB,
  emits nothing and logs `git_runtime_declaration_file_skipped` with reason
  `manifest_unparseable` or `byte_cap_exceeded`. `gitGenerationContentVersion`
  moves from `git-collector.v12` to `git-collector.v13`, so each repository
  re-collects once when its reconciliation sweep next comes due. The fact kind
  and schema are unchanged.
- **Version-pin files now emit `runtime_declaration.version` facts**
  ([#8815](https://github.com/eshu-hq/eshu/issues/8815), part of
  [#8807](https://github.com/eshu-hq/eshu/issues/8807)). The git collector reads
  `.nvmrc`, `.node-version`, `.python-version`, `.ruby-version`,
  `.java-version`, `.go-version`, `.terraform-version`, `.tool-versions`,
  `.sdkmanrc`, `.swift-version`, `rust-toolchain` and `rust-toolchain.toml` at
  any depth and emits one fact per (line, token), each with its `scope_dir`, a
  `spec_kind` of `exact`, `partial`, `range`, `alias` or `unresolved`, and the
  parsed major, minor and patch for exact and partial specs. An alias such as `lts/iron` or
  `system`, or a placeholder such as `${X}`, is reported as written and is never
  resolved or guessed. `.npmrc` and the other credential carriers emit nothing.
  A `.swift-version` gets version parts only for a plain numeric release; a
  release tag or snapshot name is an alias.
- **A declared expression is never cut into fragments.** A comparator written
  apart from its version (`>= 18`, `= 20`) is one declaration with the spec of
  the unspaced form. A value with a hyphen range, a `||` or a dangling
  comparator (`18 - 20`, `^14 || >=16`) is one `alias` for the whole value,
  because hyphen ranges and disjunctions are not read in a pin file. The
  `.tool-versions` fallback list `nodejs 20.11.1 18.19.0` still emits two
  exact facts.
- **Oversized pin files are skipped, not parsed.** A pin file over 64 KiB, or
  with more than 1,024 tokens, emits no fact and logs WARN
  `git_runtime_declaration_file_skipped` with the reason, sizes and caps. The
  byte cap is checked before any parsing, so a 10 MB hostile file costs no
  allocation (1.87 GB before).
- **Contract only, nothing reads it yet.** The kind is registered additively as
  schema 1.0.0 and disclosed unconsumed; the graph projection (#8816), the
  query tool (#8817) and the Dockerfile and CI sources (#8818) are separate
  changes. Existing repositories pick the facts up at their
  next reconciliation sweep, because adding a fact kind moves the fact-schema
  digest in `GenerationContentVersion`.


### Collector readiness names each kind's own terminal failure class

- **Collector readiness reports a bounded failure class for every collector
  kind** ([#8896](https://github.com/eshu-hq/eshu/issues/8896)). A terminal
  failure on Jira, AWS, the OCI and package registries, SBOM attestation,
  vulnerability intelligence, the scanner worker, or the Terraform state
  collector used to read `last_failure_class: unknown` when the class was one
  only that kind writes. Each kind now reports the nine classes every kind
  shares, the four the extension host writes (a component extension can carry a
  first-party kind such as `jira`), and its own (for example `permission_hidden`,
  `permission_denied`, `stale_fence`, `registry_auth_denied`, `timeout`). A
  class an extension defines for a kind that is not first-party reads the
  single token `extension_defined`. Anything else still reads `unknown`,
  including an extension-defined class on a first-party kind and, on a
  first-party kind, the one-time `legacy_missing_generation_identity` upgrade
  backfill (on any other kind it reads `extension_defined`), so no persisted text reaches the field or the
  `failure_class` label of `eshu_runtime_coordinator_collector_terminal_failure_instances`,
  which takes at most 41 values. The two OpenAPI `last_failure_class` enums and
  the class table in the terminal-failure reference grow together. Existing
  values keep their meaning; a client that treats the enum as closed must
  accept the new members.


### MCP `initialize` negotiates the protocol version

- **`initialize` echoes a supported requested protocol version**
  ([#8800](https://github.com/eshu-hq/eshu/issues/8800)). The MCP lifecycle
  rule says a server that supports the requested version answers with that
  version, and otherwise answers with another version it supports. Eshu now
  negotiates in one place for `POST /mcp/message`, SSE sessions, and stdio.
  Eshu supports only `2024-11-05`, so every reply is still `2024-11-05`: it
  does not implement Streamable HTTP or batching (`2025-03-26`) or the
  `MCP-Protocol-Version` header (`2025-06-18`), and it does not claim them. A
  request for another version, or none, is answered, not rejected, and is
  logged once at info level. See the MCP reference, "Protocol version".
- **The opt-in `structured` result shape is documented as shipped.** Earlier
  notes said the second MCP copy stays "until #8800". The `structured` shape
  from #8800 sends the result once, and it is opt-in: the default stays `dual`.

### Content store skips unchanged file rewrites

- **The `content_files` upsert skips a row that did not change**
  ([#8915](https://github.com/eshu-hq/eshu/issues/8915)). Re-upserting an
  identical file used to write a new row version, re-insert both large trigram
  index entries, and log the whole file. For a same-generation replay of a
  findings-heavy batch (50 files of 469 KB, 491 findings each) the upsert
  statement took a median 4.9 to 11.0 s and wrote 65.8 to 71.4 MiB of WAL;
  with the change it takes 135 ms and writes no row WAL. Both file upserts now
  share one conflict clause that skips the update when every written column
  except `indexed_at` already matches (NULL-safe). The whole `Write` call for
  that batch falls from 7.3 to 13.4 s to 2.5 s: the rest is hostname and
  service-name extraction over the bodies (about 2.4 s), which this change does
  not touch. A batch whose rows all change is unchanged within noise (statement
  median 6.3 s before, 6.5 s after). A skipped row keeps its old `indexed_at`,
  so `content_last_indexed_at`, the language inventory `last_indexed_at`, and
  the `updated_at` of a `repository_file` semantic-search result report the
  last write that changed the file, not the last replay. A replay under a new
  `generation_id` or `commit_sha` still rewrites the row (#8950). One duplicate
  `(repo_id, path)` key inside a single batch is rejected before any statement
  runs, as the database already rejected it. Evidence:
  `docs/internal/evidence/8915-content-file-upsert-guard.md`.

### Unchanged-content rewrites are now counted

- **The content writer counts each `content_files` upsert row by outcome**
  ([#8950](https://github.com/eshu-hq/eshu/issues/8950)).
  `eshu_dp_content_file_upserts_total{outcome,snapshot,batch_class}` splits rows into
  `inserted`, `unchanged_skipped`, `metadata_only` (same content, new commit or
  generation) and `content_changed`, with `snapshot` `full` or `delta`;
  `eshu_dp_content_file_upsert_body_bytes_total{outcome,snapshot,batch_class}` counts their body
  bytes, and the `upsert_files` stage log carries the same counts. The outcome
  comes from one lock-free pre-read per batch that compares the content digest
  and body size, so it is a proxy for body equality. Batches under 100 rows are
  classified about one in eight; the skipped rows and bytes are counted in
  `eshu_dp_content_file_upsert_unclassified_rows_total{reason,snapshot,batch_class}` and
  `eshu_dp_content_file_upsert_unclassified_body_bytes_total{reason,snapshot,batch_class}`,
  and every counter carries `batch_class` (`small` or `large`) so a share can
  weight them back. The reference page gives scaled-estimate, lower-bound and
  coverage queries that stay defined when a series was never created. Nothing
  about the write changes. See [Content File Upsert Outcomes](docs/public/reference/telemetry/content-file-upserts.md).

### Security alert reconciliation waits for its own impact producer without spending attempts

- **A reconciliation no longer burns attempts while `supply_chain_impact` is in
  flight, and its give-up is visible**
  ([#8827](https://github.com/eshu-hq/eshu/issues/8827)). The projector enqueues
  both domains from one scope generation, but the shared conflict key only
  fences a producer that is claimed or running. While the producer was pending
  or retrying, the reconciliation ran, spent a counting attempt, and wrote
  `unmatched` decisions at attempt 3. It now defers with the non-counting class
  `security_alert_reconciliation_impact_not_ready` while the producer is in
  flight, bounded by 30 minutes of producer inactivity. The give-up at the
  handler's cap now records
  `eshu_dp_security_alert_reconciliation_impact_wait_total{outcome}` and a WARN
  log. See the [3 AM runbook](docs/public/operate/3am-operator-runbook.md) and
  [Security Alert Reconciliation Impact Wait](docs/public/reference/telemetry/security-alert-reconciliation-impact-wait.md).

### Scala build.sbt is parsed for dependencies

- **`build.sbt` is admitted and parsed**
  ([#8938](https://github.com/eshu-hq/eshu/issues/8938)). The Scala manifest
  named by the language ledger was not admitted, so Scala dependency
  declarations never became facts. `build.sbt` now binds to a bounded sbt
  scanner that reads literal `libraryDependencies` coordinates (`+=`, `++=`,
  `:=`; `%`, `%%`, `%%%`; optional configuration such as `Test`) and the literal
  `scalaVersion`, and emits them as the same dependency rows Gradle and Maven
  use, with `package_manager` `sbt`. Non-literal forms are reported as
  `unresolved` or counted in `sbt_state.unresolved_declaration_count`, and that
  count includes a row whose revision stays unresolved; sbt is
  never evaluated. A row is `resolved` only when its whole statement matches a
  closed grammar: a bare key at the file top level or in a project's
  `.settings(...)`, `+=` with one element or `++=`/`:=` with a `Seq`, `List` or
  `Vector`, at most one `% config` (`Test`, `"it,test"`, `Provided`), and
  allowlisted infix ModuleID calls (`exclude`, `classifier`, `intransitive`,
  `withSources`, `cross`, and the like). Every other statement falls back:
  its literal coordinates are emitted `partial` and counted, never a resolved
  `compile`. That covers any axis (`Test /`, `ThisBuild /`), `inConfig(...)`, a
  brace block, `.map(_ % Test)`, `++`, a parenthesised or dotted call,
  `withConfigurations`, `withRevision`, a repeated `% config` (a load error in
  sbt, not a replacement) and an empty one. Because sbt 2.x reads `build.sbt`
  as Scala 3, an indented line at depth zero (a braceless `if`, `def`, `val`,
  `match`, `for` or `while` body) is partial too, as is the rest of a line
  after a closer that ends a bracket opened on an indented line, and only a
  column-0 `val` resolves a version. Configuration strings match case exactly (`"Test"` is
  not `test`), and a string holding a backslash is opaque. `:=`, `~=`, `-=`,
  `--=`, sbt 0.13's `<<=`, and the members sbt's SettingKey API defines to
  rewrite the key (`.transform`, `.set`, `.remove1`, `.removeN`, and
  `.withRank` before `:=`, `~=`, `-=`, `--=` or `<<=`) make the rows they
  supersede partial, wherever the member appears (a `val` body, after an
  operator, an `if` branch). Both keys are default-deny: any other occurrence
  of `libraryDependencies` (a parenthesized key at any depth, chained `in`
  axes, an infix `set` or `withRank`, a member that returns no Setting such as
  `.map`, a bare key) supersedes every row and is counted, and any other
  `scalaVersion` occurrence poisons the envelope; only reads (`.value`,
  `.in(Test).value`, `(k in Test).value`, and the same reads with axes or
  closers inside a dependency, such as `% (ThisBuild / scalaVersion).value`)
  are left alone, and one statement is counted once. A key named as a plain
  element of `buildInfoKeys`, `excludeLintKeys` or `includeLintKeys` is a
  read when the list ends the statement
  (`buildInfoKeys := Seq[BuildInfoKey](name, version, scalaVersion, sbtVersion)`,
  `Global / excludeLintKeys += libraryDependencies`); a key named as a value
  anywhere else, or in a key list followed by `.map(...)` or an assignment,
  is denied. This also covers
  [#9007](https://github.com/eshu-hq/eshu/issues/9007). `<+=`, `<++=`,
  `.append1`, `.appendN`, `.withRank(...) +=` and any other assignment
  operator fall back with partial rows. `scalaVersion` is read only where
  `libraryDependencies` would resolve; an unreadable `scalaVersion` statement
  there (`sys.env(...)`, `~=`, `.set`, `.withRank(...) :=`, `.:=(...)`), any
  `scalaVersion` assignment outside one (a settings `val`, `inThisBuild(...)`,
  `in Test`), or two distinct literals leave `scala_version` empty and drop
  the hint. `project/build.sbt` (sbt's
  meta-build, admitted by its base name) is parsed with
  `sbt_state.meta_build` true and every row partial; the check runs on the
  path relative to the repository root, so a clone inside a directory named
  `project` is not flagged (this also covers
  [#8997](https://github.com/eshu-hq/eshu/issues/8997)).
  A runtime-plus-test list is `runtime`,
  compiler-tool configurations are undetermined, Ivy ranges are partial, and a
  configuration string over 256 bytes is never split or stored. Package-registry correlation for sbt rows
  ([#8961](https://github.com/eshu-hq/eshu/issues/8961)) and the line-keyed
  entity id ([#8962](https://github.com/eshu-hq/eshu/issues/8962)) stay open.
  The registry fingerprint moves with the new exact name, so every repository
  re-collects once; the `git-collector` content version constant is unchanged.

### Reducer: hardcoded-secret side table repair

- **The reducer rebuilds the hardcoded-secret side table whenever it is not
  ready** ([#8833](https://github.com/eshu-hq/eshu/issues/8833)). Until now only
  `bootstrap-index` finalized `content_file_secret_lines`, so a failed finalize,
  or a later migration that changes the derivation, left
  `investigate_hardcoded_secrets` on the slow legacy scan until an operator
  reran it. The reducer now polls the state every 30 s
  (`ESHU_SECRET_LINES_REPAIR_POLL_INTERVAL`). It tries the bulk-load lock
  `(5318,1)` and a new deferred-writer lock `(5318,2)` without waiting, on its
  own one-connection pool, rebuilds for the current epoch, and publishes
  `ready`. Every `bootstrap-index` connection holds `(5318,2)` shared, so the
  reducer never publishes over a load that is still writing, even if the
  bootstrap's pinned lock connection died. `bootstrap-index` also refuses to
  open a connection once its lock connection is gone, keeps that connection
  alive against `idle_session_timeout`, and fails loudly if its own finalize
  claims nothing for its epoch. A `bootstrap-index` that starts during a rebuild
  waits for it, up to `ESHU_SCHEMA_BOOTSTRAP_OWNERSHIP_WAIT`, and fails naming
  the holder if it is still running (the worst case before work starts is now
  three of those waits). The reducer opens up to
  `ESHU_POSTGRES_MAX_OPEN_CONNS` + 1 connections: the extra one is open for the
  startup ping, each poll that finds the table not ready, and a rebuild. Finalizer batches (the reducer's
  and `bootstrap-index`'s) are now also bounded by 4 MiB of content, so a live
  write waits about half a second behind a batch instead of up to 11 s. It is
  on by default (`ESHU_SECRET_LINES_REPAIR_ENABLED`). Watch
  `eshu_dp_secret_lines_repair_runs_total{outcome}` and the
  `secret_lines.repair_*` logs.

### Service story defaults to a bounded handles view over MCP

- **`get_service_story` defaults to a bounded `handles` view**
  ([#8798](https://github.com/eshu-hq/eshu/issues/8798)). A large service's
  story measured 266 KB and failed `mcp_response_over_budget`. The route now
  takes `evidence_detail` (`full` or `handles`); the MCP tool defaults to
  `handles` and the HTTP route stays `full`. A large service's story is answered
  instead of refused: `handles` projects the bulky lists to identity rows that
  keep their row truth labels and is 128,547 bytes on the every-list-at-cap
  fixture (35.7 percent of full). That single copy fits the 262,144-byte budget;
  the MCP result still carries the payload twice (290,970 bytes), so dispatch
  answers it as the resource copy alone and records
  `eshu_dp_mcp_response_resource_fallback_total` for a connection on the
  default `dual` shape. The opt-in `structured` shape
  ([#8800](https://github.com/eshu-hq/eshu/issues/8800)) sends the result once;
  the default stays `dual`. Deployment artifact rows keep `id`, `relationship_type`, and
  `resolved_id`; `get_relationship_evidence` by `resolved_id` returns the rest,
  including confidence. `handles` lists each reduced family in `truth.omissions`
  with its total. Each total is the story's own pre-cap count and may be a
  lower bound, because the story carries a flag for only some of the bounds its
  reads apply; treat a total as exact only for the families the Context
  evidence budget reference lists as exact. A trace preview also carries
  `truncated`, `evidence_shipped`, and `evidence_omitted`, the evidence graph
  carries `nodes_truncated` and `nodes_omitted`, and a dropped list is named by
  a `*_shipped_at` pointer or only in `truth.omissions`.
  `investigation.repositories_with_evidence` reports the rows it shipped (at
  most 50), so it is also a lower bound when `repositories_considered` ends with
  the `__truncated__` marker. The size fixture holds 60 rows per family and the
  story ships 50 (the cap); a fixture with 60 uncorrelated cloud-resource
  candidates (284,854 bytes dual) and one with 60 `entrypoint_candidates` and
  `network_paths` rows (330,200 bytes dual) take the same path, and the measured
  bytes are the 50-row ship size, not a bound above it. `handles` projects those
  two lists to identity rows too. In both modes the endpoint rows now ship once,
  at `api_surface.endpoints`; `deployment_overview.api_surface` keeps its counts
  and points there with `endpoints_shipped_at`. Pass `evidence_detail: full` for
  every row.

### Swift version pin file indexed as raw text

- **`.swift-version` is now admitted as `raw_text`**
  ([#8939](https://github.com/eshu-hq/eshu/issues/8939)), so a repository's
  declared Swift toolchain is readable. The file holds a toolchain version or a
  snapshot name and no credentials. It follows the other runtime-version pins:
  an explicit exact name (never a `*-version` rule), kept in the filesystem-mode
  workspace copy, and picked up by an unchanged repository at its next
  reconciliation sweep.

### Version pin files indexed as raw text

- **Runtime-version pin files are now admitted as `raw_text`**
  ([#8809](https://github.com/eshu-hq/eshu/issues/8809)): `.nvmrc`,
  `.node-version`, `.tool-versions`, `.python-version`, `.ruby-version`,
  `.java-version`, `.go-version`, `.sdkmanrc`, `.terraform-version`,
  `rust-toolchain`, and `rust-toolchain.toml`. Credential carriers such as
  `.npmrc`, `.env*`, and `mise.toml` stay excluded.
- **Existing repositories pick the pins up at their next reconciliation, not
  at release.** Admitting a registry name changes the registry fingerprint and
  therefore `GenerationContentVersion`, but nothing forces a re-snapshot on
  that change. An unchanged repository is re-snapshotted when its sweep comes
  due (`interval_elapsed`, `ESHU_REPO_RECONCILE_INTERVAL_HOURS`, default 24),
  and the sweep forces at most `ESHU_REPO_RECONCILE_MAX_PER_CYCLE` scopes per
  cycle (default 10), so a large fleet drains over several cycles. A delta sync
  parses only the files a commit changed, so a pin in an untouched file stays
  unindexed until that sweep. With the sweep disabled
  (`ESHU_REPO_RECONCILE_INTERVAL_HOURS=0`), request a reindex to pick the pins
  up. Per-repository cost is in the
  [reconciliation sweep](docs/public/reference/reconciliation-sweep.md); watch
  the ingester queue and the reducer backlog while the sweep drains.
- **Hardening for every admitted name:** full and delta (`--file-targets`)
  discovery now drop a symlink unless its resolved target is a regular file
  inside the repository in the same admission category (same parser
  definition, or both terraform state, documentation, `.gitmodules`, or
  CODEOWNERS), at a path discovery would admit as a regular file (no ignored or
  hidden directory, hidden name, ignored extension, or ignored path glob). So
  `.nvmrc -> .env` or a link into `.git/config` no longer stores the target's
  text. `.gitignore` and `.eshuignore` are not re-applied to the target.
  Dangling links, cycles, and directory links are dropped too. In a delta
  sync, an in-repository link whose target leaves the repository is now
  dropped and counted instead of failing the whole delta with "outside
  repository root"; a target path that is itself outside the repository still
  fails. Each dropped link is counted as `skip_reason=content:symlink_target` on
  `eshu_dp_discovery_files_skipped_total` and logged in the `discovery stats`
  line as `files_skipped.content.symlink_target`; a repository that relied on
  such a link, for example `sub/app.conf -> ../vendor/x/app.conf`, stops
  indexing it.
- **A failed full discovery is no longer hidden in a delta sync.** When
  `--file-targets` was set, the delta file-set call overwrote the error from
  the full walk, so a failed walk (for example a missing repository root)
  surfaced as a later, unrelated error, or none. The snapshot now returns the
  discovery error itself.

### Dockerfile base images read ARG defaults

- **The Dockerfile parser now expands `ARG` defaults declared before the first
  `FROM` into the base image and tag** ([#8810](https://github.com/eshu-hq/eshu/issues/8810)).
  `ARG NODE_VERSION=24.1.0` with `FROM node:${NODE_VERSION}-slim` now reports
  tag `24.1.0-slim` instead of the literal placeholder. A stage-local `ARG`,
  an `ARG` with no default, and expressions such as `${V%%.*}` or
  `${V:?msg}` stay unresolved: the stage keeps the placeholder, lists the
  variable name in `base_ref_unresolved_args`, and the repository runtime
  artifacts read model marks it with `base_ref_unresolved` and carries the
  declared default as `base_ref_arg_defaults`. A construct with no variable
  name (`$(cmd)`, a bare `$`, an unterminated `${`, or a reference over the
  bounds) lists a fixed marker instead of the expression text. An unaliased stage that used a variable is
  now named after its expanded image. The container image base-ref helper now
  also skips a base whose tag still holds a placeholder. A resolved value is
  the Dockerfile's own default: a CI `--build-arg` can override it at build
  time, and the parser cannot see that. A reference over 4096 bytes, nested deeper
  than 64 `${` levels, or expanding to more than 4096 bytes stays unresolved.
  The default evidence is bounded as well: each `base_ref_arg_defaults` value
  is cut to 256 bytes and a stage keeps at most 16, with
  `base_ref_arg_defaults_truncated` and `base_ref_arg_defaults_omitted` marking
  what was left out, so a Dockerfile with many long `ARG` defaults and many
  `FROM` lines cannot multiply into a huge payload.

### Build and tool configuration files indexed as text

- **Discovery now admits small build and tool configuration files as plain
  text** ([#8814](https://github.com/eshu-hq/eshu/issues/8814)): `Makefile`,
  `GNUmakefile`, `justfile`, `.editorconfig`, `.gitattributes`, `.dockerignore`,
  `.gitignore`, `.eslintrc`, `.prettierrc`, `.babelrc`, `.npmignore`, `tox.ini`,
  `mypy.ini`, `pytest.ini`, `.flake8`, `rustfmt.toml`, `clippy.toml`,
  `renovate.json5` and `.mvn/extensions.xml`. They reach the content store as
  searchable text: each file emits a file fact and a content fact, and no
  entities. Credential-bearing names (`.env`, `.npmrc`, `settings.xml`,
  `phpunit.xml`, `.mvn/jvm.config`, `.mvn/maven.config`) stay unadmitted.
  Filesystem source mode now keeps these dot-prefixed names, matched
  case-insensitively, and the whole `.mvn` directory in its managed copy, so it
  treats them like clone mode, except that a name under an unallowlisted
  dot-directory, such as `tools/.config/.eslintrc` (unlike `.github`,
  `.github/workflows`, `.mvn` and a dot-directory whose name is itself an
  allowlisted file name), is still pruned there with its parent. The copy's
  existing `.gitlab-ci.yml`, `.gitlab-ci.yaml`, `.gitmodules`, `.github` and
  `.github/workflows` entries now match case-insensitively there as well, so
  `.GITHUB/workflows/*.yml` is indexed; `.gitignore` is still not copied there.
  The runtime-version pin names added by #8809 (`.nvmrc`, `.node-version`,
  `.tool-versions`, `.python-version`, `.ruby-version`, `.java-version`,
  `.go-version`, `.sdkmanrc`, `.terraform-version`) are kept the same way. Because
  the copy keeps `.mvn` whole, filesystem mode also now indexes other files the
  registry admits under it, such as `.mvn/wrapper/MavenWrapperDownloader.java`,
  as clone mode already did. The registry fingerprint and `git-collector`
  content version move, so each repository re-collects once when its
  reconciliation sweep next comes due (see the #8809 entry above).

### Shared-snapshot export refused on unqualified PostgreSQL readers (breaking)

- **Code-topic shared-snapshot reads now refuse an unqualified PostgreSQL
  reader build with a permanent `503`** ([#7957](https://github.com/eshu-hq/eshu/issues/7957)).
  Two PostgreSQL 18 defects (standby `pg_export_snapshot` missing
  subtransaction XIDs, and an import xmin race with VACUUM) are fixed only
  after 18.6. The API and MCP now export a shared snapshot only on PostgreSQL
  18.7 or later, or on a build whose exact `server_version` is declared in
  `ESHU_POSTGRES_READ_SNAPSHOT_EXPORT_BUILDS` with evidence. **Breaking:** on
  any undeclared reader below 18.7, including the CI standby mirror and local
  Compose while its floating `postgres:18-alpine` tag resolves below 18.7, a
  code-topic or change-surface request whose topic expands to 16 terms answers
  `503` `backend_unavailable` with `details.reason=snapshot_export_unqualified`
  and no `Retry-After` until the reader is upgraded or declared. There is no
  single-statement fallback. Watch
  `eshu_dp_postgres_reader_snapshot_export_qualified` and
  `eshu_dp_postgres_reader_snapshot_export_denials_total`.

### Unscoped file search rechecks the stored lowercase copy

- **Unscoped file-content search tests `content_lower` with `LIKE` instead of
  a per-row `ILIKE`, and every answer carries `documents_not_searched`**
  ([#7975](https://github.com/eshu-hq/eshu/issues/7975)). The per-row `ILIKE`
  paid a whole-value lowercase copy per candidate inside an uninterruptible
  stretch, so one 53 MB document held a common term's tail past the budget;
  the stored copy rechecks about 6x faster with identical rows (live
  differential over upper, mixed, wildcard, single-character, and non-ASCII
  tokens). Migrations 176-177 add the generated column and its trigram index;
  the search refuses to run before the index exists instead of falling back
  to an unindexed scan. A partial page names the rows no completed statement
  read in `documents_not_searched` (zero on an exact page); a page with a
  nonzero count is never exact.

### OpenCode role files pin manifest tier models

- **Tracked OpenCode role files now pin their tier model from `.agents/roles.json` `models.opencode`, reversing the earlier
  deliberately-unpinned posture** ([#7984](https://github.com/eshu-hq/eshu/issues/7984)). PR
  [#7973](https://github.com/eshu-hq/eshu/pull/7973) migrated the ten `.opencode/agent/*.md` role files to native V2 shapes
  (`mode: subagent` with ordered `permissions:` rules) and bound each tier: fast `deepseek/deepseek-flash#low`, workhorse
  `deepseek/deepseek-v4-pro#high`, deep `deepseek/deepseek-v4-pro#max`, arbiter `minimax/minimax-m3`. The `#variant` suffix
  carries reasoning effort; MiniMax-M3 exposes no variants, so its pin is the bare reference. A personal model choice goes in
  the local `agents` map (`OPENCODE_CONFIG_CONTENT`) or `opencode run --model`, not the tracked role file: the config layer beats
  the frontmatter pin. Cursor keeps `model: inherit`, so its shims run the session model, and pi/future harnesses carry no role
  bindings at all: the non-pinning harnesses are policy, not drift.

### Cross-repo dead-code stays unknown when a consumer's snapshot is incomplete

- **`find_cross_repo_dead_code` / `POST /api/v0/code/dead-code/cross-repo` no
  longer report a symbol dead when a consumer repository's reachability snapshot
  is incomplete** ([#7547](https://github.com/eshu-hq/eshu/issues/7547)). A symbol
  with no consumer row was `dead` even when the consumer's active snapshot was
  missing or truncated, so every symbol that consumer calls read dead. The route
  now checks `code_reachability_repository_watermarks` once per request and
  returns `unknown_needs_evidence` with the new reason `consumer_coverage_incomplete`
  when a consumer repository with `code_calls` or `inheritance_edges` work for its
  active generation has no watermark for that generation, a truncated one, or one
  stamped with an older verdict schema epoch.
  Strong live consumer evidence still wins. The check cannot see a stale or partly
  drained snapshot whose watermark says `truncated = false`, and a repository with
  zero roots counts only once the writer stamps its watermark truncated.
  The response gains `consumer_coverage` (`complete`, `retryable`, `incomplete`,
  `incomplete_repo_ids`, `incomplete_truncated`). Each `incomplete` entry names
  the repository, a `state` (`no_snapshot_yet`, `older_epoch`, `truncated` or
  `no_active_scope`), the `generation_id` and a `retryable` hint: for a missing or older-epoch
  snapshot one is expected to appear or refresh without action, for a truncated
  one it is not. The hint is not a promise: it can stay true for a long time when
  the active generation is a delta generation or a full generation whose reducer
  work did not complete. `retryable` at the top is true only when every gap is
  retryable. An unscoped request with no named consumers checks every
  repository, so name `consumer_repo_ids` to narrow it.
  Each `incomplete` entry also carries a plain-language `reason` and `next_step`,
  and `consumer_coverage` a `coverage_summary` sentence
  ([#7594](https://github.com/eshu-hq/eshu/issues/7594)). All three are fixed text
  derived from `state` and whether the request named `consumer_repo_ids` (a
  request that named them is not told to name them again): no new data and no
  new query. The wait advice is a hint, so it says "should clear", not "will".

### Cross-repo dead-code returns repository-boundary evidence once

- **`find_cross_repo_dead_code` / `POST /api/v0/code/dead-code/cross-repo` no
  longer repeat the repository-boundary evidence on every row**
  ([#7129](https://github.com/eshu-hq/eshu/issues/7129)). A candidate with no
  entity-level consumer evidence carried the same incoming-relationship items,
  about 12 KB per row on one ops-qa repository, so default arguments returned
  `mcp_response_over_budget`. The items now come once in
  `data.boundary_consumer_evidence` (with `boundary_consumer_evidence_count`,
  always present, empty and 0 when no row used the fallback). Such rows keep `consumer_evidence: []` and gain
  `consumer_evidence_source: "repository_boundary"`; other rows get `"entity"`;
  every row gets `consumer_evidence_count`. Classification and
  `hidden_consumer_evidence_count` are unchanged. Clients that read a fallback
  row's `consumer_evidence` should read `boundary_consumer_evidence` instead.
  Measured on the 400-candidate, 20-relationship fixture through the MCP
  dispatcher (both wire copies): 824,864 bytes before, 187,776 after.

### Cross-repo dead-code `evidence_detail` handles

- **`find_cross_repo_dead_code` defaults to `evidence_detail: handles`; the HTTP
  route keeps `full`** ([#7129](https://github.com/eshu-hq/eshu/issues/7129)).
  Hoisting the boundary list left per-entity evidence shipping in full on every
  row: one SQL page can hold up to 1,000 items at about 616 bytes each. Under
  `handles` each row's `consumer_evidence` is at most 5 groups
  (`consumer_repo_id`, `relationship_type`, `evidence_family`,
  `confidence_label`, `item_count`), strongest first so the group that decided
  `live_by_consumer` always leads, and the hoisted boundary list is capped at 25.
  `consumer_evidence_count`, `consumer_evidence_group_count`,
  `boundary_consumer_evidence_count`, the `*_truncated` markers and
  `truth.omissions` carry the totals. Buckets, reasons, hidden counts and
  `analysis` are identical in both modes; pass `evidence_detail: "full"` for every
  item. An unknown value is HTTP 400. Clients that read a live row's
  `consumer_evidence[].citation` on MCP should repeat the call with `full`.
  Evidence under `handles` is bounded to at most 17.5% of the response budget;
  the row base is not reduced by it. Each docstring is clipped to 512 bytes but
  echoed about six times per row, so on a repository with long docstrings the
  MCP reply can still be delivered as the full resource only (no
  `structuredContent`) or exceed the budget until that echo is deduplicated.
  Measured through the MCP dispatcher on the calibrated #7168 base (60-byte
  docstrings, full suppressed bucket, limit 25) with 24 rows of 40 items in
  distinct consumer repositories, one fallback row and a 60-item boundary list:
  1,482,262 bytes (`mcp_response_over_budget`) under `full`, 209,824 under
  `handles` (both wire copies, budget 262,144), against 164,116 with no
  evidence.

### Cross-repo dead-code says when only tests call a symbol

- **`find_cross_repo_dead_code` / `POST /api/v0/code/dead-code/cross-repo` rows
  gain `test_only_consumers`** ([#7603](https://github.com/eshu-hq/eshu/issues/7603)).
  A `live_by_consumer` row carries `test_only_consumers: true` when every consumer's
  root entity is in a test file (the existing `DeadCodeIsTestFile` rule), so an
  admin sees that removing the symbol also means removing or rewriting its tests.
  The key is omitted otherwise. Liveness, buckets and every other field are
  unchanged: a test that calls a function is still a caller. A test method is a
  reachability root only for C#, Java, Kotlin, Scala, Rust and Swift, so there a
  test-only consumer is flagged; in Go, Python, JavaScript and TypeScript a test
  function is never a root, so the flag appears only when another root, such as
  a main or script entry point, sits in a test path. The same-repository
  dead-code routes do not carry it. It costs one batched primary-key read of
  `content_entities` per request, skipped when no row has a consumer root.

### Dead-code investigation coverage skips the entity scan

- **`investigate_dead_code` / `POST /api/v0/code/dead-code/investigate` no
  longer report `entity_count`, and `content_last_indexed_at` now comes from
  files only**
  ([#7525](https://github.com/eshu-hq/eshu/issues/7525), part of
  [#7249](https://github.com/eshu-hq/eshu/issues/7249)). The coverage block
  counted every content entity of the repository on each call, about 135k heap
  blocks on the largest repository sampled (measured on the QA replica before
  this change; the after-latency is not measured). It now uses the same narrow file
  summary repository context uses plus one files `max(indexed_at)` read.
  `file_count` and `languages` are unchanged. The repository stats and coverage
  routes still report `entity_count`.

### Workload and service context stay inside the MCP response budget

- **`get_workload_context` and `get_service_context` bound their evidence**
  ([#7129](https://github.com/eshu-hq/eshu/issues/7129)). On services with
  70-80 endpoint edges these two tools still went over the 256 KiB response
  budget after the hostname cap, because `deployment_overview.api_surface`
  repeated the endpoint rows and `deployment_evidence` carried full artifact
  rows. `api_surface.endpoints` and the `deployment_evidence` lists
  `artifacts`, `delivery_paths`, `delivery_workflows`, and
  `shared_config_paths` are now cut to 50 rows, each cut is named in
  `partial_reasons` (which also now reports a list whose read had already
  stopped at its own bound), and `deployment_overview.api_surface` keeps its
  counts but no longer repeats the endpoint rows (`endpoints_shipped_at` points
  at the top-level list). Both routes take a new `evidence_detail` of `full`
  or `handles`. HTTP defaults to `full`, so the console and existing callers
  see the same rows; the MCP tools default to `handles`, which returns
  artifact and endpoint rows as identity rows, drops the content-derived
  evidence lists that have no row cap of their own, keeps every count, and lists
  each reduced family with its total in `truth.omissions`. Pass `evidence_detail: full` for
  the rows, or call `get_relationship_evidence` with a `resolved_id` for one
  artifact.

- **`infrastructure` is cut to 50 rows on the context routes**
  ([#7129](https://github.com/eshu-hq/eshu/issues/7129)). A re-measure on ops-qa
  after the evidence caps deployed found 25 services still over the MCP budget,
  all dominated by the `infrastructure` list: the read bound is 5,000 rows and
  the 50-row context caps did not cover it. The list is now cut to 50 rows, the
  total is on `result_limits.infrastructure_count`, and the cut is named
  `infrastructure_rows_truncated` in `partial_reasons`. The existing
  `infrastructure_truncated` still means the read itself hit its 5,000-row bound.

- **`entrypoint_candidates` is cut to 50 rows on the context routes**
  ([#7129](https://github.com/eshu-hq/eshu/issues/7129)). The list had no bound
  and grows with repository size: 357 rows, about 63 KB, on the largest service
  measured on ops-qa. It is now cut to 50 rows, the total is on
  `result_limits.entrypoint_candidate_count`, and the cut is named
  `entrypoint_candidates_truncated` in `partial_reasons`.

### Code and content search responses drop the `matches` alias

- **`matches` is removed; read `results`**
  ([#7170](https://github.com/eshu-hq/eshu/issues/7170)). `find_symbol`,
  `search_entity_content`, `search_file_content`, `find_code`,
  `inspect_code_inventory`, and their HTTP routes (`POST /api/v0/code/search`,
  `/code/symbols/search`, `/code/structure/inventory`, and the content search
  routes) returned the same rows twice, once as `results` and once as the
  compatibility alias `matches`. That doubled every response and pushed three
  MCP tools over the response budget (#7129). The alias is gone from HTTP and
  MCP alike; `results` is unchanged. `resolve_entity` (`POST
  /api/v0/entities/resolve`) dropped its `matches` alias the same way
  ([#7173](https://github.com/eshu-hq/eshu/issues/7173)) once the console read
  `entities` only: read `entities`, which is unchanged. A full 100-row resolve
  response measured 68,468 bytes with the alias and 34,256 without.

### Symbol, inventory, and content search rows clip long docstrings

- **A row's `docstring` is clipped to 512 bytes at read time**
  ([#7234](https://github.com/eshu-hq/eshu/issues/7234)). `find_symbol`,
  `inspect_code_inventory`, `find_code`, and `search_entity_content` (and their
  HTTP routes) echoed a docstring five or six times per row (`metadata`,
  `semantic_summary`, `semantic_profile`, the language blocks, `story`), so a
  repository with long docstrings pushed a default page over the MCP response
  budget even after the `source_cache` clip (#7171). The docstring is cut before
  the echoes are derived, so every echo is derived from the 512-byte string. A clipped row carries
  `docstring_clipped`, `docstring_clip_bytes`, and `docstring_total_bytes`; every
  response carries `docstring_clip_bytes` and `docstring_clipped_rows`. The
  change is additive: rows that fit are unchanged. `get_entity_content` returns
  the stored docstring whole. In the test fixture a default `find_symbol` page
  over 16 KiB docstrings went from `mcp_response_over_budget` (3.5 MB) to
  157 KB, delivered as the single resource copy the dispatcher keeps when two
  full copies would exceed the budget.
- **The clip now covers `find_dead_code`, `investigate_dead_code`,
  `find_cross_repo_dead_code`, and `find_most_complex_functions`**
  ([#7234](https://github.com/eshu-hq/eshu/issues/7234)). Over rows with a
  16 KiB docstring these four returned `mcp_response_over_budget` at their
  default limits, because each row echoes the docstring five times. They now
  clip to 512 bytes and carry the same `docstring_clipped` markers, and the
  investigation route clips its `suppressed` bucket too. Measured through the
  MCP dispatcher: `find_dead_code` 203,410 bytes (25 rows), `investigate_dead_code`
  249,562 bytes (25 active rows, 95.2% of the 262,144-byte budget), and
  `find_most_complex_functions` 68,876 bytes (10 rows). `calculate_cyclomatic_complexity`
  with an entity id returns one row, measured 166,420 bytes, and is unchanged.
  `find_cross_repo_dead_code` builds rows the same way and is clipped too: with
  a 16 KiB docstring on every row its default reply went from
  `mcp_response_over_budget` to 235,095 bytes (89.7% of the budget, delivered as
  the single resource copy).
  The change is additive: rows that fit are unchanged.

### Compact-by-default catalog and playbook/workflow list responses

- **`get_capability_catalog`, `list_query_playbooks`, and
  `list_investigation_workflows` default to small, paginated responses**
  ([#6795](https://github.com/eshu-hq/eshu/issues/6795)). All three read
  tools could return payloads large enough to exceed an MCP client's
  per-result token cap. `list_query_playbooks` and
  `list_investigation_workflows` had no `limit`/`offset` at all and always
  serialized every playbook/workflow's full steps, required inputs, failure
  modes, and tool groups; `get_capability_catalog` paged but repeated the
  full ~10KB role/grant/data-class authorization catalog on every page and
  shipped per-capability `profiles` and `proof_signals` unconditionally.
  All three now default to a **compact** view (id/name/version/description,
  plus `prompt_family` for playbooks and `domain` for workflows) with
  `limit`/`offset` paging and `truncated`/`next_offset` in the response;
  `get_capability_catalog`'s default `limit` dropped from 200 to 12 and its
  top-level `authorization` field defaults to empty. `view=full` (and, for
  capabilities, `include_authorization=true`) restores the complete
  per-entry shape for any caller that needs it — the console capability
  matrix and the console/CLI playbook list both opt in this way. Paging
  still applies under `view=full` for all three, and for
  `list_query_playbooks`/`list_investigation_workflows`, `count` is the
  number of items in the returned page, not the catalog total; `total`
  carries the catalog size. Measured default-page reduction: `get_capability_catalog`
  217KB->6.7KB, `list_query_playbooks` 46KB->5.5KB,
  `list_investigation_workflows` 27KB->1.1KB (live A/B against the deployed
  binary).

### Graph read failures no longer echo the graph's statement text

- **A failed graph read answers `graph query failed`, not the driver's message**
  ([#7253](https://github.com/eshu-hq/eshu/issues/7253)). A Neo4j statement
  error quotes the statement it rejected, inline literals included, and any
  handler that fell through the deadline/unavailable mapping wrote that text
  into an HTTP 500 body. `Neo4jReader` now returns an error whose text is the
  fixed `graph query failed`; the redacted detail moved to the `neo4j.query`
  span and a new `query.graph_read.error` log (`graph_read.error`, with the
  statement fingerprint and head). The two routes that run a caller-authored
  statement, `POST /api/v0/code/cypher` and `/api/v0/code/visualize`, answer
  `400 invalid_argument` with the redacted graph message when the graph rejects
  the statement as malformed, instead of a 500. Postgres and internal error text
  in other 5xx bodies is listed in
  `docs/internal/evidence/7253-graph-read-error-public-text.md`; the writer-pool
  half is fixed in the next entry.

### Postgres writer-pool failures no longer echo the connection target or server message

- **A failed call on the API and MCP server writer pool answers a fixed text, not
  the driver's** ([#7253](https://github.com/eshu-hq/eshu/issues/7253)). pgx
  formats a connection failure as ``failed to connect to `user=<user>
  database=<db>`: <host:port>: ...`` and a server error with the relation,
  column, or constraint it rejected, and handlers wrote that text into 5xx
  bodies. The business-read (guarded reader) pool already returned fixed texts
  (#7482); the writer pool, which serves authorization, audit, mutation, and
  sign-in, did not. It is now built through `boundederr.NewConnector`, which
  wraps the `database/sql` driver: the error text is one of `postgres store
  unavailable`, `postgres store timed out`, `postgres store request canceled`,
  or `postgres store statement failed`, and `errors.Is` and `errors.As` still
  reach the pgx error (`PgError`, `ConnectError`, context deadlines,
  `driver.ErrBadConn`). The detail moved to a new `postgres.store.error` log
  (`postgres_store.operation`, `.sqlstate`, `.statement_head`, `.error`) on the
  process logger. The ingester, reducer, projector, collectors, and
  `admin-status` pools are not bounded by this change.
- **A missing `content_file_references` table falls back to the content scan
  again.** The guarded reader's fixed error text hid the table name from
  `contentReferenceIndexUnavailable`, so the cross-repo reference search failed
  instead of falling back; it now reads the SQLSTATE (`42P01`) through the
  error's cause.

### EC2 AMI node class resolves the instance->AMI relationship

- **Materialize the AMI as a CloudResource node so the instance->AMI edge
  finally resolves** ([#5717](https://github.com/eshu-hq/eshu/issues/5717)).
  Issue #5448 shipped an `ec2_instance_uses_ami` `aws_relationship` fact, but
  no `aws_resource` fact existed for the AMI itself, so the generic AWS
  relationship edge join (`buildCloudResourceJoinIndex`,
  `go/internal/reducer/aws_relationship_join.go`) could never resolve the
  target — the edge was always counted `unresolved` and dropped, never
  written. The EC2 collector now also emits one `aws_resource` fact per
  distinct AMI id per scan (`resource_type=aws_ec2_ami`,
  `go/internal/collector/awscloud/services/ec2/ami_identity.go`),
  deduplicated across every instance in the scan that shares the same AMI —
  many instances commonly launch from one AMI, so this avoids emitting a
  redundant fact per instance. The AMI materializes under the EXISTING
  `CloudResource` label through the SAME generic AWS resource node
  materialization every other resource type uses: no new node label, no
  dedicated node/edge writer, and no reducer join-code change was needed. The
  fact carries only identity (account/region/resource_id, with Name set to the
  bare resource id like every other EC2 resource type) — no rich state,
  owner, or creation-date metadata, since that requires a separate
  `DescribeImages` API call this increment deliberately does not make (a
  distinct, separately costed enrichment follow-up). `ResourceTypeEC2AMI` now
  aliases the new `sdk/go/factschema/aws/v1.ResourceTypeEC2AMI` constant,
  matching every sibling AWS resource-type constant.

### `trace_deployment_chain` stays inside the MCP response budget

- `POST /api/v0/impact/trace-deployment-chain` accepts two optional fields,
  `evidence_detail` (`full` or `handles`) and `sections` (a list of family
  names), and returns `evidence_detail` plus a `section_detail` entry per
  family. The HTTP default is unchanged (`full`), so existing HTTP responses
  keep every field. (#7174)
- The MCP `trace_deployment_chain` tool now defaults to `evidence_detail:
  handles` when `sections` is absent: primary families ship as identity rows
  and the derived families (`delivery_paths`, `deployment_facts`,
  `controller_driven_paths`, `k8s_relationships`, `topology_edges`,
  `artifact_lineage`, `network_paths`, `entrypoints`) are omitted. With every
  family at its cap the MCP result measured 198,560 counted bytes (75.7% of
  the 262,144-byte budget), down from 1,435,236 bytes (547.5%). Callers that
  read the omitted keys over MCP should pass `sections` with
  `evidence_detail: full`, as each `section_detail` drilldown shows.
- The truth envelope gains an additive `omissions` list
  (`{section, detail, total}`), absent when a response is complete, so a cut
  is never silent on either surface.

### Bare `backend "local" {}` drift ownership resolution

- **Apply Terraform's own default local-backend path when resolving
  config-vs-state drift ownership** ([#5594](https://github.com/eshu-hq/eshu/issues/5594)).
  A `backend "local" {}` block written with no `path` attribute — the
  ordinary way to write a local backend, since Terraform itself defaults
  `path` to `"terraform.tfstate"` relative to the root module directory
  (https://developer.hashicorp.com/terraform/language/backend/local) —
  produced no config-side `DiscoveryCandidate` at all:
  `EvaluateBackendConfig`/`backendConfigCandidate`
  (`go/internal/collector/terraformstate/backend_config.go`) only derived a
  candidate for `s3` backends, so every drift candidate for a local backend
  was rejected with `failure_class: no_config_repo_owns_backend`, with or
  without an explicit `path`. `backendConfigCandidate` now also derives a
  `BackendLocal` candidate, applying Terraform's default when the attribute
  is absent. The HCL parser (`go/internal/parser/hcl/terraform_backend.go`)
  now captures the local backend's `path` attribute under
  `row["state_path"]` — a new key, since `row["path"]` already held the
  source `.tf` file's own path for every backend row (and `"local_path"`
  was rejected as a candidate name: the durable `repository` fact already
  uses that key for a differently-scoped value, the repo checkout root).
  A `BackendLocal` candidate's locator is an absolute path matching the
  repository checkout root (`BackendConfigContext.RepoLocalPath`, the
  durable `repository` fact's `local_path`), joined in
  `go/internal/storage/postgres/tfstate/backend_canonical.go` by a
  `repo_local_paths` CTE keyed on `(scope_id, generation_id, repo_id)` with
  `DISTINCT ON` + latest-`observed_at`-wins, mirroring
  `listTerraformBackendFactsQuery`'s existing `active_repositories`
  convention — an earlier unfiltered, unordered version of this join could
  cross-contaminate `local_path` across repos sharing one
  `(scope_id, generation_id)` and could fan out duplicate rows (including
  for the pre-existing `s3` path) when more than one `repository` fact
  existed per repo; both are covered by a new
  `ESHU_POSTGRES_DSN`-gated integration test proven against a real
  Postgres 16 instance. Without a resolvable `repoLocalPath`, no candidate
  is produced rather than a guessed locator. Backend kinds Eshu does not
  model (`gcs`, `azurerm`, `remote`, `http`, ...) were audited and confirmed
  to still produce neither a candidate nor a warning, unchanged. Terragrunt's
  own `remote_state { backend = "local" }` config was investigated
  separately and is NOT defaulted by this change — see the open question
  below.
  - No-Regression Evidence: `go test ./internal/parser/ ./internal/collector/
    ./internal/collector/terraformstate/... ./internal/storage/postgres/...
    ./internal/relationships/tfstatebackend/... ./internal/reducer/ -count=1`
    is green. `TestPostgresTerraformBackendQueryResolvesBareLocalBackendDefaultPath`,
    `TestEvaluateBackendConfigDefaultsBareLocalBackendPath`, and the parser-level
    `TestDefaultEngineParsePathHCLLocalBackendBareBlockOmitsPathAttribute` fail
    before this change (zero candidates / no `state_path` field) and pass after.
    `TestPostgresTerraformBackendQueryRepoLocalPathJoinIsRepoScopedAndLatest`
    (`go/internal/storage/postgres/tfstate/backend_canonical_repo_local_path_integration_test.go`)
    was run against a real, freshly schema-migrated Postgres 16 container
    (isolated, not the shared Compose stack): fails against the pre-fix join
    (0 rows where 1 was expected), passes against the fix. The full
    `TestPostgresTerraformBackendQuery*` and
    `TestPostgresDriftEvidenceLoaderSurvivesNull*` set (22 tests) also passed
    against that same live instance. `EXPLAIN (ANALYZE, BUFFERS)` on a
    200-repo seeded corpus showed no measurable regression: 2.890 ms
    (pre-fix) vs. 2.873 ms (fixed), both returning the correct 200 rows with
    no duplication.
  - No-Observability-Change (rejection path): the reducer's existing `"drift
    candidate rejected"` structured log
    (`go/internal/reducer/terraform_config_state_drift.go:logRejection`,
    `failure_class`/`rejection.reason` fields) already covers this path; the
    fix reduces how often `failure_class=no_config_repo_owns_backend` fires
    for the ordinary bare-local-backend spelling, it does not add a stage,
    query, worker, or metric.
  - Observability Evidence (success path): added
    `DiscoveryCandidate.LocatorDefaulted`, threaded through
    `TerraformBackendRow`/`CommitAnchor` unchanged, and a new info-level
    `"drift candidate resolved via defaulted locator"` log
    (`locator_defaulted=true`) in the reducer handler, gated so the ordinary
    explicit-path and `s3` flows gain no new log volume — an operator could
    not otherwise tell a defaulted resolution apart from an explicit one.
    `TestDriftHandlerLogsDefaultedLocatorResolution` and
    `TestDriftHandlerDoesNotLogDefaultedLocatorForExplicitResolution`
    (`go/internal/reducer/terraform_config_state_drift_defaulted_locator_test.go`)
    cover both cases.
  - Adjacent fix found while proving the above against a live database:
    `TestPostgresTerraformBackendQuerySurvivesNullTerraformBackendsPath`
    computed its expected hash with `terraformstate.LocatorHash` instead of
    the canonical adapter's `ScopeLocatorHash` — issue #203's exact
    hash-mismatch class, in a test this time, not production code — so it
    silently returned 0 rows every time it actually ran against Postgres,
    masked locally because it is DSN-gated and skips without
    `ESHU_POSTGRES_DSN` set. Fixed inline; verified green against the same
    live instance.

  **Terragrunt finding (informational, not fixed):** Terragrunt's own
  `remote_state { backend = "local" }` was investigated per hostile-review
  follow-up. Terragrunt's docs (`docs.terragrunt.com/features/units/state-backend`)
  state that `local` is not one of its specially-bootstrapped backend kinds
  (only `s3`, `gcs`, `azurerm` are); for any other backend the `remote_state`
  block "operates in the same manner as `generate`" — Terragrunt renders the
  `config` map verbatim into a generated Terraform `backend "local" { ... }`
  block and lets Terraform interpret it, rather than documenting any default
  of its own. Terraform's own default would technically apply once that
  block is generated — but the working directory it is generated into is
  Terragrunt's per-unit `.terragrunt-cache` path, not the `terragrunt.hcl`
  file's own directory, so Eshu's static parser (which never runs Terragrunt)
  has no way to reproduce or observe that path. Applying "the same" default
  here would mean guessing a working directory Eshu cannot see, which the
  correlation-truth rules forbid without stronger evidence. Left unchanged:
  `go/internal/collector/terraformstate/source_terragrunt.go`'s
  `terragruntRemoteStateLocalCandidate` continues to require an explicit,
  literal, absolute `path` for its (separately security-gated, operator-approval-required)
  local-state-reading candidate; the ownership-join path never modeled
  Terragrunt as its own backend kind and still does not. The repo owner
  resolved the open question this raised (below): rather than guessing a
  path Eshu cannot observe, make the resulting rejection visible at the read
  surface instead.

- **Surface rejected/ambiguous config-vs-state drift candidates at the read
  edge, closing the general `no_config_repo_owns_backend` visibility gap**
  ([#5594](https://github.com/eshu-hq/eshu/issues/5594) follow-up; owner
  decision, not a reviewer suggestion). The Terragrunt finding above is one
  instance of a broader, pre-existing gap: a state-snapshot scope whose
  backend never resolved to any config repo
  (`tfstatebackend.ErrNoConfigRepoOwnsBackend`) was log-only since issue
  #5442's durability work, so `POST /api/v0/terraform/config-state-drift/findings`
  returned an identical empty page for "evaluated, no drift" and "ownership
  never resolved at all." The repo owner put the disposition to a
  hostile-review pass and chose: fix the general visibility gap inline,
  covering both pre-existing rejection classes
  (`no_config_repo_owns_backend` and the already-durable
  `ambiguous_backend_owner`), not just the Terragrunt symptom.
  `TerraformConfigStateDriftWrite` gains a third write mode,
  `UnresolvedOwner`, persisting exactly one durable `"unresolved"` finding
  per state-snapshot scope (`Address`/`DriftKind` empty,
  `AmbiguousOwnerCandidates` also empty — no competing evidence to record,
  only the absence of any owner), mirroring the existing `"ambiguous"` write
  in every respect: same upsert-by-stable-fact-id idempotency, same
  non-fatal write-failure handling, same
  `eshu_dp_drift_ambiguous_owner_write_failed_total`-style counter pattern.
  The read surface (`go/internal/query/terraform_config_state_drift.go`),
  its OpenAPI contract
  (`go/internal/query/openapi_paths_terraform_config_state_drift.go`), and
  its MCP tool (`list_terraform_config_state_drift_findings`,
  `go/internal/mcp/tools_iac.go`) all accept and document `"unresolved"`
  alongside `exact`/`ambiguous` in the same change, per the repo's
  OpenAPI/MCP lockstep rule. Matched to existing prior art rather than
  inventing new vocabulary: the `relationships_complete`/
  `k8s_relationships_complete` partial-truth disclosure convention
  (`go/internal/query/entity_context_content.go`,
  `impact_trace_deployment_k8s_select.go`) and, more directly, the CI/CD run
  correlation aggregate handler's existing
  exact/derived/ambiguous/unresolved/rejected outcome enum
  (`go/internal/query/cicd/run_correlation_aggregates_handler.go`),
  which already uses `"unresolved"` for this exact semantic per the
  six-value vocabulary in
  `docs/internal/design/391-observability-coverage-correlation.md`.
  `go/internal/correlation/drift/tfconfigstate/doc.go` records this as a
  decision superseding the #5442-era "unresolved stays log-only" call, with
  the reasoning for why the reopened "unbounded write volume" concern is
  addressed (same per-scope-per-generation cadence the already-accepted
  `"ambiguous"` write has always had, not a new unbounded category), a
  post-deployment volume watch note (the *cadence* per key is proven
  identical, but a partially-onboarded org could still see materially more
  distinct scopes land in `"unresolved"` than ever go `"ambiguous"`, since an
  unonboarded repo's backend is the default case for
  `no_config_repo_owns_backend` — worth watching after rollout, not a defect
  today). This write path, and the `backend "local" {}` default-path fix
  itself, are now both proven end-to-end by the golden corpus (see the
  golden-corpus fixture entry below).
  - Observability Evidence: `eshu_dp_drift_unresolved_owner_write_failed_total`
    (`go/internal/telemetry/instruments.go`, registered alongside
    `DriftAmbiguousOwnerWriteFailed`), with its X1 contract row in
    `docs/public/observability/telemetry-coverage.md` (two rows: the
    `terraform_config_state_drift.go` call-site row and a dedicated row for
    the new `terraform_config_state_drift_unresolved_owner.go` file). Also
    closed a stale X1 gap surfaced while adding this row: an earlier commit
    on this branch (`go/internal/collector/terraformstate/backend_config_local.go`)
    had landed without its own X1 coverage row; added a
    `No-Observability-Change:` row for it in the same change. Verified with
    `bash scripts/test-verify-telemetry-coverage.sh` (15/15) and
    `ESHU_TELEMETRY_COVERAGE_BASE=origin/main bash scripts/verify-telemetry-coverage.sh`
    (clean, no drift), plus `bash scripts/test-generate-operator-dashboard.sh`
    (9/9) and a re-run of `scripts/generate-operator-dashboard.sh` confirming
    no diff (idempotent, no dashboard update needed).
  - No-Regression Evidence: `go test ./internal/reducer/... ./internal/query/...
    ./internal/storage/postgres/... ./internal/mcp/... ./internal/telemetry/...
    ./internal/correlation/... -count=1` is green (all packages `ok`). TDD:
    `TestPostgresTerraformConfigStateDriftWriterPersistsUnresolvedOwnerFinding`,
    `TestDriftHandlerNoOwnerWritesDurableUnresolvedFinding`, and
    `TestHandleTerraformConfigStateDriftFindingsDistinguishesUnresolvedFromNoDrift`
    each fail before the corresponding production change (missing field /
    zero writes / identical empty page for both cases) and pass after.
    `TestDriftHandlerAmbiguousOwnerStillWritesAmbiguousNotUnresolved` and
    `TestTerraformConfigStateDriftFindingStoreFiltersByOutcome` (pre-existing)
    both stayed green throughout, proving the pre-existing
    `"ambiguous"` path is untouched. Golden-corpus fixtures for both the
    `"unresolved"` write path and the `backend "local" {}` default-path fix
    landed in a follow-up commit on this branch (see below); at the time of
    this commit the corpus's one Terraform-state cassette/fixture pair
    (`testdata/cassettes/terraformstate/supply-chain-demo.json`,
    `tests/fixtures/ecosystems/terraform_comprehensive/main.tf`) was still
    deliberately backend-aligned by #5442's own design so ownership
    resolution always succeeds — confirmed by direct corpus scan (exactly
    one `backend` block across all 20 fixture repos, exactly one
    `terraformstate` cassette, both in that same aligned pair) — so the new
    `"unresolved"` write path could not yet fire against the existing corpus.

- **Golden-corpus fixtures proving both the `backend "local" {}`
  default-path fix and the `"unresolved"` outcome fire for real**
  ([#5594](https://github.com/eshu-hq/eshu/issues/5594)). Neither change
  above had live golden-corpus coverage: the corpus's only Terraform backend
  fixture (`terraform_comprehensive`) is deliberately S3-aligned by #5442's
  design, so it can prove neither a bare local backend's default-path
  resolution nor a genuinely-unowned backend's `"unresolved"` write. Added
  two new scopes to `testdata/cassettes/terraformstate/supply-chain-demo.json`
  and one new fixture repo, `tests/fixtures/ecosystems/terraform_local_backend_demo`
  (a bare `backend "local" {}` block, no `path`, at repo root), alongside —
  not replacing — the existing S3-aligned pair: (1) a resolvable local-backend
  scope whose state resources deliberately do not overlap the new fixture's
  declared resources, proving `tfstatebackend.ResolveConfigCommitForBackend`
  resolves the default-path locator and materializes a real
  `outcome: "exact"` finding; (2) a deliberately orphaned S3 bucket/key no
  fixture's config declares, proving `outcome: "unresolved"` fires. A
  `BackendLocal` locator is an absolute path rooted at the fixture's real,
  run-time git-checkout location (`scripts/verify-golden-corpus-gate.sh`
  stages every fixture inside a fresh `mktemp -d` per run), so neither the
  cassette nor the snapshot can pin its resolved `scope_id` as a literal;
  both instead carry a `$LOCAL_BACKEND_SCOPE_ID$` sentinel. Added
  `go/cmd/golden-corpus-gate/local_backend_scope_id.go` (a
  `-print-local-backend-scope-id` compute-and-print mode reproducing
  production's join-key formula, pinned against the real
  `terraformstate.ScopeLocatorHash` by
  `TestComputeLocalBackendScopeIDMatchesScopeLocatorHash`, plus a
  `-local-backend-scope-id` flag that substitutes the sentinel into the
  loaded snapshot's query shapes before the query phase runs) and
  `scripts/lib/golden-corpus-local-backend.sh` (`stage_local_backend_cassette`:
  resolves the fixture's `local_path` from Postgres after bootstrap-index,
  computes the real `scope_id`, and writes a sentinel-substituted runtime
  copy of the cassette that only the `terraformstate` collector replays —
  every other collector keeps its original, unmodified, committed cassette).
  Added two new HTTP query shapes to `testdata/golden/e2e-20repo-snapshot.json`
  (`POST /api/v0/terraform/config-state-drift/findings?variant=local-backend-resolved`
  and `...?variant=unresolved`; the query-string suffix exists only to give
  each entry a distinct map key — `net/http.ServeMux` ignores it when
  routing, and `parseHTTPShapeKey` passes it straight through — the real
  selector is `scope_id` in `request_body`, matching the precedent already
  in this file at `POST /api/v0/impact/trace-deployment-chain?anchor=declared-object`),
  each filtering by `outcome` and asserting `required_json_values` on
  `outcome_groups[].outcome`/`drift_findings[].outcome`, so the two new
  scopes are provably distinguishable from each other and from the
  pre-existing S3 scope's shape at the gate, not just in unit tests. No new
  MCP query shape: `list_terraform_config_state_drift_findings` is the one
  registered MCP tool name for this capability, and the snapshot's MCP shape
  map key is dispatched as the literal `tools/call` tool name, so only one
  MCP entry can exist for it; the existing S3-scope MCP entry already proves
  the MCP tool layer itself dispatches this capability correctly, and it is
  left unchanged. Updated `go/internal/correlation/drift/tfconfigstate/doc.go`
  and this file's earlier "known gap" entries above to describe what these
  fixtures now prove instead of recording the gap as deferred.
  - Local verification: `cd go && go test ./internal/replay/schema/...
    ./cmd/golden-corpus-gate/... ./internal/goldengate/... -count=1` is
    green; `bash scripts/test-verify-golden-corpus-gate.sh` passes;
    `testdata/cassettes/terraformstate/supply-chain-demo.json` and
    `testdata/golden/e2e-20repo-snapshot.json` are both valid JSON and the
    former passes `TestCommittedCassettesValid`'s schema/loader validation.
    The live `bash scripts/verify-golden-corpus-gate.sh` run (Docker,
    real Postgres/graph backend) that proves the two new query shapes and
    the corpus-wide count ranges is not runnable in this environment; see
    the PR description for that evidence.

- **Live golden-corpus-gate round trip on the new fixtures (superseded by the
  correction below).** A live run found two failures the local-only
  verification above could not catch: `?variant=local-backend-resolved`
  returned zero findings instead of the expected `outcome: "exact"` finding,
  and `GET /api/v0/iac/resources` count drifted from the new fixture's own
  resources. `?variant=unresolved` passed at the time. **Correction:** this
  run predates the branch being rebased onto a base carrying #5862, which made
  `results_field` mandatory for any query shape setting `minimum_results`
  (`EvaluateQueryShape` now hard-errors before evaluating the array instead of
  inferring the first array-valued field). Neither of these two entries ever
  set `results_field` (confirmed by `git log -p` across every commit that
  touched them), so on the rebased base both `?variant=local-backend-resolved`
  and `?variant=unresolved` hard-fail with `"results_field is required when
  minimum_results ... is set"` before the `outcome_groups[].outcome` assertion
  ever runs -- the "`?variant=unresolved` passed" result above no longer holds
  against the currently committed snapshot and must not be read as current
  proof. Fixed by adding `"results_field": "drift_findings"` to both entries,
  matching the pre-existing S3-scope sibling shape immediately above them.
  `cd go && go test ./cmd/golden-corpus-gate/... -count=1` is green after the
  fix (was `--- FAIL: TestQueryClientChecksHTTPShapes` naming both entries
  before it). Confirmed the assertion is not vacuous once the missing-field
  error is gone: evaluating each shape against a synthetic response with an
  empty `drift_findings` array (all other required fields present) fails with
  `"drift_findings" has 0 results, want >= 1`, not the results_field error --
  proving `minimum_results` genuinely binds. This local proof only establishes
  that the shape assertion itself is sound; it does not re-establish that
  `?variant=unresolved` produces a real, distinguishable `"unresolved"` finding
  against the live corpus. That requires a fresh `bash
  scripts/verify-golden-corpus-gate.sh` run against the fixed entries, which
  is the repo owner's to run, not restated as proven here.
  For the first failure: built an offline reproduction
  (`TestEvaluateBackendConfigLocalBackendThroughRealParser`,
  `go/internal/collector/terraformstate/backend_config_local_parser_integration_test.go`)
  that parses the real fixture `.tf` content with the real
  `go/internal/parser/hcl` package and feeds the result straight into
  `EvaluateBackendConfig` -- no hand-built row, unlike every pre-existing
  `backendConfigLocalCandidate` test -- and it passes, proving the
  candidate-derivation algorithm, the parser's absolute-path row shape, and
  the locator/hash formula are all correct for this exact fixture shape.
  That ruled out a production-code defect in the default-path fix itself.
  The remaining, most likely explanation: `scripts/lib/golden-corpus-local-backend.sh`'s
  `stage_local_backend_cassette` computed the fixture's `local_path` from
  `ingestion_scopes.payload`, but the resolver's actual join
  (`go/internal/storage/postgres/tfstate/backend_canonical.go`'s
  `repo_local_paths` CTE) reads it from `fact_records WHERE fact_kind =
  'repository'` instead -- a different table. Tracing the write path shows
  both should carry the identical value (same `repositoryidentity.Metadata`
  struct field, written into both places by the same collector call), so
  this was not provably the defect, but it was a real, unverified gap
  between what the orchestrator precomputed and what the resolver actually
  reads. Fixed by reading the identical table/predicate/tiebreak the
  resolver's own CTE uses, removing the gap rather than relying on the two
  staying in sync; also added a diagnostic print of the repository fact's
  and the active backend-bearing file fact's generation_id side by side,
  since the canonical join requires them to match exactly. For the second
  failure: `GET /api/v0/iac/resources`'s `kind=resource` list and
  `summary.by_kind.resource` both scan the `TerraformResource` graph label
  sourced from `fact_kind='content_entity'` config-side parsing
  (`go/internal/query/iac_resources.go`), unrelated to drift-ownership
  resolution, so the fixture's two resource blocks
  (`aws_instance.local_backend_demo`, `aws_s3_bucket.local_backend_demo`)
  landed regardless of the first failure; `count`/`summary.by_kind.resource`
  raised 11 -> 13 and `summary.total` (the sum of all three kind counts,
  `go/internal/query/iac_inventory_postgres.go`) raised 19 -> 21, with the
  module/data-source counts (19 - 11 = 8) held constant.
  `node_count_Repository: 30` passed against range `[15,30]` on this live
  run -- exactly at the ceiling. Left unchanged (correct today), but the
  **next** fixture repo added to this corpus will break that assertion;
  whoever adds it must raise the ceiling deliberately, not be surprised by
  a red gate.
  - Local verification: `cd go && go test ./internal/collector/terraformstate/...
    -run TestEvaluateBackendConfigLocalBackendThroughRealParser -v -count=1`
    passes; `bash scripts/test-verify-golden-corpus-gate.sh` passes;
    `testdata/golden/e2e-20repo-snapshot.json` is valid JSON. The live gate
    re-run that confirms this fix is not runnable in this environment.

- **Third live golden-corpus-gate round trip: the actual root cause of the
  zero-finding failure, found once the `results_field` fix above let the
  assertion evaluate at all.** With `results_field` fixed,
  `?variant=local-backend-resolved` still failed live: `"drift_findings" has
  0 results, want >= 1`. Direct diagnostic queries against that run
  (`print_local_backend_drift_diagnostics`'s `[1a]`) showed **zero**
  `ingestion_scopes` rows for the computed scope_id
  `state_snapshot:local:10ae50af...` -- no scope, no state-snapshot fact, no
  work item, nothing for the resolver or the evidence loader to ever see.
  The fixture repo itself landed and processed correctly (confirmed
  separately: `deployable_unit_correlation` ran against
  `repo:terraform_local_backend_demo`), so this was not a repo-ingestion
  problem -- the *state-snapshot scope specifically* never landed under the
  identifier the orchestrator computed.
  The root cause: `stage_local_backend_cassette`'s `sed` substitution
  (`scripts/lib/golden-corpus-local-backend.sh`) has never actually
  substituted anything, since the commit that introduced it
  (`1e4d37089`). The `-e` arguments were double-quoted --
  `"s|\$LOCAL_BACKEND_SCOPE_ID\$|${local_backend_scope_id}|g"` -- so bash's
  own double-quote parsing consumes the backslash before each `\$`,
  and `sed` receives a bare, unescaped `$LOCAL_BACKEND_SCOPE_ID$` pattern.
  In a BRE, an unescaped trailing `$` is an end-of-line anchor, so the
  pattern only ever matches "...SCOPE_ID$" immediately followed by end of
  line -- never true in the cassette, where the sentinel is always followed
  by a closing quote and more JSON. `sed` exits 0 having substituted
  nothing, on every occurrence, every run. Verified directly: re-running the
  exact pre-fix `-e` arguments against the real committed cassette left all
  four sentinel occurrences (including the one inside the scope's own
  descriptive `"note"` field) byte-identical in the output. The runtime
  cassette copy the terraformstate collector actually replayed therefore
  still carried the literal, un-substituted string `$LOCAL_BACKEND_SCOPE_ID$`
  as its `scope_id` -- a value that can never match the hash-based scope_id
  the diagnostic query (correctly) checked for, hence zero rows. This also
  explains why the sibling `?variant=unresolved` scope landed and passed
  live: its `scope_id` is a plain literal hash, never routed through this
  broken sentinel substitution at all.
  This was not the `3c7b1f844` local_path-source alignment fix's territory
  after all -- that fix targeted which Postgres table/predicate
  `stage_local_backend_cassette` reads `repo_local_path` from, and was
  itself never confirmed live (see above); the actual defect was in a wholly
  different step (the `sed` escaping), present since the cassette-staging
  mechanism was first introduced, and never previously observable because no
  live run had ever gotten past the `results_field` hard-error to reach the
  count assertion that would have revealed it.
  Fixed by single-quoting the `\$`-escaped pattern segments and concatenating
  the variable-bearing replacement as a separate double-quoted segment
  (`'s|\$LOCAL_BACKEND_SCOPE_ID\$|'"${local_backend_scope_id}"'|g'`), so the
  backslashes reach `sed` intact. Verified by sourcing the actual patched
  `stage_local_backend_cassette` function (stubbing `pg`/`bin_dir`, not
  reimplementing the sed call by hand) against the real committed cassette:
  the output carries zero remaining sentinel occurrences and the correct
  substituted `scope_id`/`partition_key`/`payload.locator_hash` values.
  **What the corpus has and has not proven, plainly stated:** across all
  three live rounds to date, the `backend "local" {}` default-path fix and
  the `"unresolved"` outcome have never both been proven end to end by a
  live golden-corpus-gate run with a green `?variant=local-backend-resolved`
  assertion -- the state-snapshot scope this proof depends on has never
  actually reached Postgres under its intended identifier until this fix.
  Every prior claim of corpus proof for the `"exact"`-outcome local-backend
  path in this file was, in hindsight, unearned; only the `"unresolved"`
  path (which never depended on this substitution) has live proof today. A
  fresh `bash scripts/verify-golden-corpus-gate.sh` run against this fix is
  required before the `"exact"`-outcome path can be called proven, and that
  run is the repo owner's to make, not restated as proven here.
  - Local verification: `bash scripts/test-verify-golden-corpus-gate.sh`
    passes. `bash -n scripts/lib/golden-corpus-local-backend.sh` is clean.
    The live gate re-run that confirms this fix is not runnable in this
    environment.

- **Fourth live golden-corpus-gate round trip: both fixes confirmed end to
  end, closing out this section's earlier caveats.** [PR
  #5882](https://github.com/eshu-hq/eshu/pull/5882)'s live
  `bash scripts/verify-golden-corpus-gate.sh` run against the `results_field`
  fix and the `sed`-escaping fix above proved the whole chain for the first
  time on this branch: `[1a]`'s `ingestion_scopes` check shows the
  local-backend state-snapshot scope landing under its computed identifier,
  `POST /api/v0/terraform/config-state-drift/findings?variant=local-backend-resolved`
  returns 4 `drift_findings` with `outcome: "exact"`, and
  `...?variant=unresolved` returns 1 finding with `outcome: "unresolved"` --
  distinguishable from the resolved case in the same run. This supersedes
  every earlier "requires a fresh live-gate run, not yet re-proven, deferred
  to the repo owner" caveat recorded above in this section (the "Live
  golden-corpus-gate round trip" and "Third live golden-corpus-gate round
  trip" entries, and the `?variant=local-backend-resolved`/`?variant=unresolved`
  descriptions in `testdata/golden/e2e-20repo-snapshot.json`, updated in the
  same commit as this entry): both are now proven, not merely locally
  plausible.

### Config-vs-state drift: "derived" outcome for unresolved module-prefix addresses

- **Split "exact" into "exact" vs "derived" by per-address module-resolution
  confidence** ([#5572](https://github.com/eshu-hq/eshu/issues/5572)).
  `go/internal/correlation/drift/tfconfigstate/doc.go`'s own recorded
  limitation named the risk: a module-nested resource's config-side address
  is computed by resolving `module {}` call chains
  (`go/internal/storage/postgres/tfstate/drift_evidence_module_prefix.go`),
  and two documented failure shapes can silently produce a wrong address --
  `classifyModuleSource`'s Terraform-Registry-shorthand heuristic
  misclassifying a genuinely local module source as external (the
  "terraform-aws-modules" false-positive the ADR already names), or a
  resolved local module chain abandoned mid-walk at `maxModulePrefixDepth`.
  Both were previously invisible at the finding level: every per-address
  outcome was stamped `"exact"` regardless, because the comparison step
  genuinely was an exact string match even when its input address was not
  trustworthy.

  A new `moduleResolutionConfidenceMap`
  (`go/internal/storage/postgres/tfstate/drift_evidence_module_confidence.go`)
  tracks both failure shapes as a directory-keyed low-confidence signal,
  populated by `BuildModulePrefixMap` alongside the real `modulePrefixMap`.
  `ResourceRow.ModuleResolutionReason` carries it onto the config-side row;
  `BuildCandidates` attaches a new `terraform_module_resolution_confidence`
  evidence atom when present; the reducer writer's `moduleResolutionOutcome`
  downgrades `Outcome` from `"exact"` to `"derived"` whenever that atom is
  present. One `"derived"` value covers both causes deliberately -- the
  atom's `Value` carries the specific reason (`"external_registry"` or
  `"depth_exceeded"`), preserved in the finding's `Evidence` array, so an
  operator can still tell the two causes apart without the outcome
  vocabulary growing per cause.

  **A masking bug the naive design would have missed, found by TDD before
  landing:** `modulePrefixMap.modulePrefixForPath`'s own longest-ancestor-
  match walk means an unresolved module call is not reliably visible as
  "zero prefixes returned." Both failure shapes are reached only after one
  or more ancestor levels already resolved successfully (a depth-exceeded
  callee requires depths 1..N-1 to have already succeeded; a nested
  registry-misclassified call sits inside whatever module resolved its own
  call site), so the immediately shallower ancestor directory almost always
  has a real, populated prefix -- and the walk-up silently returns THAT
  prefix instead of nothing, misattributing the resource to the wrong,
  too-shallow parent module rather than falling back to a plainly
  root-shaped address. Consulting the confidence map only when
  `modulePrefixForPath` returned zero prefixes would have missed this,
  demonstrably: a first version of the depth-exceeded integration test
  failed with the resource silently getting a real (wrong) 10-level-deep
  prefix instead of a root fallback. The fix compares MATCH SPECIFICITY —
  `moduleResolutionReasonForEntry` prefers the confidence signal whenever
  its matched directory is at least as deep as (as close to the file as)
  whatever directory supplied the real prefix — so the masked case is
  caught too, not just the plain "no prefix" case.

  No schema or contract bump: `outcome` is `"type": "string"` with no
  `enum`/`pattern`/`oneOf` in the generated JSON Schema
  (`sdk/go/factschema/schema/reducer_terraform_config_state_drift_finding.v1.schema.json`),
  and no field was added, removed, renamed, or retyped on
  `TerraformConfigStateDriftFinding` -- `Evidence []map[string]any` already
  carried arbitrary atoms before this change. `specs/fact-kind-registry.v1.yaml`
  is unaffected for the same reason.

  - No-Regression Evidence: see
    `docs/internal/evidence/5572-drift-derived-outcome-module-resolution-confidence.md`
    for the full complexity argument (zero new Postgres queries; the added
    per-entry work is the same O(directory depth) walk-up shape
    `modulePrefixForPath` already performs, doubled) and the green focused
    test run across `internal/correlation/drift/tfconfigstate`,
    `internal/storage/postgres`, `internal/reducer`, `internal/query`, and
    `internal/mcp`.
  - No-Observability-Change: reuses the existing
    `eshu_dp_drift_unresolved_module_calls_total{reason}` counter path
    (issue #169) unchanged; the new evidence atom flows through the
    finding's existing `Evidence` field and the existing
    `POST /api/v0/terraform/config-state-drift/findings` response shape, not
    a new field, log, span, or metric.
  - OpenAPI (`go/internal/query/openapi_paths_terraform_config_state_drift.go`),
    the MCP tool description (`go/internal/mcp/tools_iac.go`), and both
    outcome-filter validators
    (`go/internal/query/terraform_config_state_drift.go`,
    `go/internal/storage/postgres/terraform_config_state_drift_findings.go`)
    now accept and document `"derived"` alongside `"exact"`, `"ambiguous"`,
    and `"unresolved"`.

- **Review follow-up: golden-corpus proof for the `external_registry` cause,
  plus two stale doc-comment corrections.** Independent review confirmed the
  masking-bug fix and traced the cause end to end to the read surface, but
  flagged that a new, permanent, operator-visible outcome value threaded
  into reducer-materialized truth, the OpenAPI enum, and the MCP contract
  calls for cassette/golden replay proof, per issue #5594's precedent in
  this same writer (#5594 added two fixture scopes specifically to prove its
  new `"unresolved"` outcome fires end to end, rather than trusting unit
  tests alone).
  `tests/fixtures/ecosystems/terraform_comprehensive/terraform-aws-modules/vpc/aws/main.tf`
  now gives modules.tf's pre-existing (previously dead) `module "vpc" {
  source = "terraform-aws-modules/vpc/aws" }` reference a real target
  directory containing a real resource
  (`aws_security_group.vpc_endpoints`); the matching cassette-side
  `module.vpc.aws_security_group.vpc_endpoints` resource
  (`testdata/cassettes/terraformstate/supply-chain-demo.json`) makes both
  sides genuinely drift, so a real `added_in_config`/`added_in_state` pair
  materializes exactly as `tfconfigstate/doc.go` describes it. The new
  `POST /api/v0/terraform/config-state-drift/findings?variant=derived`
  snapshot entry (`testdata/golden/e2e-20repo-snapshot.json`) asserts
  `outcome="derived"` AND, via `required_json_object_matches` (not two
  independent wildcard checks that could accept unrelated fields), that the
  same finding's evidence array carries a
  `terraform_module_resolution_confidence` atom with
  `value="external_registry"` on one correlated object -- proving the
  specific cause survives to the read surface, the whole justification for
  one `"derived"` outcome value instead of splitting per cause.
  `depth_exceeded` deliberately keeps unit/integration-only coverage: an
  11-level module chain is a heavy fixture for a rare shape the existing
  focused tests already prove precisely; see the evidence doc's "Golden-
  corpus coverage per cause" section for that explicit decision.
  `cd go && go test ./cmd/golden-corpus-gate/... -count=1` passed after the
  snapshot edit.

  Review also flagged two doc comments this session's own tests disprove:
  `tfstate_drift_evidence_config_row.go`'s `configRowFromParserEntry` doc
  claimed the caller only ever passes a non-empty
  `moduleResolutionReason` alongside an empty `modulePrefix` -- false,
  proven false by `TestLoadDriftEvidenceMarksLowConfidenceForDepthExceededModuleChain`
  itself, which passes a non-empty reason alongside a non-empty masked
  prefix; and `classify.go`'s `ResourceRow.ModuleResolutionReason` doc
  claimed both causes "fall back to a root-module address" -- true only for
  `external_registry`, not `depth_exceeded` (which almost always produces
  the masked wrong-ancestor address instead, per the masking-bug fix
  above). Both corrected to describe the actual, tested behavior.

- **Review follow-up: downgrade BOTH halves of a spurious mismatch pair, not
  just the config-side half.** An unresolved module-prefix chain does not
  make one address uncertain -- it makes the config/state join key wrong, so
  the loader always produced TWO candidates for the same real resource: a
  config-only `added_in_config` at the fallback address (correctly flagged
  and downgraded, per the work above) and a state-only `added_in_state` at
  the real, prefixed address, which stayed `"exact"` because
  `ResourceRow.ModuleResolutionReason` only ever carried the reason on the
  config-side row. A caller filtering `outcome=exact` still got back half of
  a pair this feature exists to flag as uncertain. A related gap: the
  prior-config walk that powers `removed_from_config`
  (`PostgresDriftEvidenceLoader.loadPriorConfigAddresses`) built its own
  `moduleResolutionConfidenceMap` per prior generation and discarded it
  (`priorPrefixMap, _, err := l.BuildModulePrefixMap(...)`), so a
  `removed_from_config` finding promoted from a low-confidence prior-config
  address also stayed `"exact"`.

  A new `pairSpuriousModuleMismatches`
  (`go/internal/storage/postgres/tfstate/drift_evidence_pairing.go`) runs
  inside `mergeDriftRows` and mirrors `ModuleResolutionReason` onto the
  paired state-only row, but only when the pairing is unambiguous: exactly
  one low-confidence config-only row and exactly one state-only row share
  the trailing `<type>.<name>[index]` resource key (`resourceAddressKey`,
  derived purely from the address string -- no module-prefix-map lookup
  needed). Ambiguous collisions (2+ candidates sharing a key on either side)
  are left untouched deliberately: Terraform's own idiomatic "singleton
  resource" naming convention (`aws_s3_bucket.this`, `aws_iam_role.this`,
  and similar -- the exact convention `terraform-aws-modules` itself uses)
  means the same `<type>.<name>` key legitimately recurs across unrelated,
  independently resolved modules, so a blind match risks mirroring the
  reason onto a genuinely unrelated resource. `collectPriorConfigAddresses`
  now threads the prior generation's own confidence map through the same
  `moduleResolutionReasonForEntry` comparison the current-generation path
  already uses, and `BuildCandidates` gained a `row.State.ModuleResolutionReason`
  branch symmetric with the pre-existing `row.Config` branch -- the reducer
  writer's `moduleResolutionOutcome` needed no change, since it already only
  checked atom presence, not which side attached it.
  - No-Regression Evidence / Observability Evidence: see the "Follow-up: both
    halves of a spurious mismatch pair now downgrade" section of
    `docs/internal/evidence/5572-drift-derived-outcome-module-resolution-confidence.md`
    for the complexity argument (zero new Postgres queries) and the green
    focused test run across `internal/correlation/drift/tfconfigstate`,
    `internal/storage/postgres`, `internal/reducer`, `internal/query`, and
    `internal/mcp`, including new regression coverage for the pairing
    (`TestPairSpuriousModuleMismatchesMirrorsReasonOntoUnambiguousStateOnlyRow`,
    `TestPairSpuriousModuleMismatchesSkipsAmbiguousResourceKeyCollision`,
    `TestLoadDriftEvidencePairsSpuriousMismatchAcrossModuleResolutionFailure`)
    and the prior-config threading
    (`TestPostgresDriftEvidenceLoaderPriorConfigConfidenceThreadedOntoRemovedFromConfigRow`).
  - The `POST /api/v0/terraform/config-state-drift/findings?variant=derived`
    golden-corpus entry's `minimum_results` is raised from `1` to `2` --
    review correctly flagged that a floor of `1` plus existential
    `drift_findings[].*` path matching could not distinguish "both halves
    downgraded" from the exact bug this PR fixes ("only the config-side half
    downgraded"), so the gate could not tell fixed from broken. The `2` was
    derived by tracing the actual fixture and cassette content, not assumed:
    this repo's single ingested generation flags exactly one directory
    (`module.s3_bucket`'s local path and `module.eks`'s `git::` source are
    both never flagged), that directory declares exactly one resource, and
    the cassette carries exactly one state address sharing that resource's
    `<type>.<name>` key -- an unambiguous 1:1 pairing, with no prior
    generation in the corpus to promote a third `derived` finding through
    `removed_from_config`. Two `required_json_object_matches` entries under
    `drift_findings[]` now pin `outcome="derived"` to EACH specific address
    independently (`aws_security_group.vpc_endpoints` for the config-only
    half, `module.vpc.aws_security_group.vpc_endpoints` for the state-only
    half) -- since one finding object cannot carry two different `address`
    values at once, this proves two distinct derived findings exist, which a
    bare count of 2 (satisfiable by two unrelated or duplicated config-side
    findings) could not. `cd go && go test ./cmd/golden-corpus-gate/...
    -count=1` is green against the updated snapshot.

- **Review follow-up: fix a real false-pairing defect in `resourceAddressKey`
  itself.** The pairing key computed by taking an address's last two
  dot-separated segments (`strings.Split(address, ".")`) broke on any
  address whose `for_each` index literally contains a dot -- proved
  empirically: `aws_route53_record.this["api.example.com"]` and the
  UNRELATED `aws_acm_certificate.cert["www.example.com"]` both collapsed to
  the identical wrong key `example.com"]`, and `data.aws_ami.ubuntu`
  collapsed onto the unrelated managed resource `aws_ami.ubuntu`. Either
  collision, if it were the only one in a join, would satisfy
  `pairSpuriousModuleMismatches`'s "exactly one candidate on each side"
  ambiguity guard and mirror `ModuleResolutionReason` onto a genuinely
  unrelated, real finding -- a false `derived` downgrade of true drift, the
  precise failure the guard exists to prevent. `for_each` over domain names
  or similar dotted strings is a common Terraform pattern, not an edge case.

  `resourceAddressKey` now FRONT-strips leading `module.<name>[<index>]`
  segments instead of taking the last two segments from the end, tracking
  bracket depth and double-quote state (`skipModuleNameSegment`,
  `hasResourceTypeNameShape`) so a `.` or `]` inside a quoted index --
  whether on a `for_each` instance's own key or on an indexed MODULE NAME's
  index (`module.vpc["a.b"].aws_x.y`) -- is never mistaken for a segment
  boundary. Whatever remains after stripping every leading `module.` segment
  is returned verbatim, byte-identical to what follows the address's own
  last module prefix. A `data.` prefix is deliberately preserved rather than
  stripped (Terraform itself treats `data.TYPE.NAME` and `TYPE.NAME` as
  different resources): checked, not assumed, that this collision can only
  threaten the STATE side of a pairing -- the HCL parser routes `data`
  blocks into a separate `terraform_data_sources` bucket the drift loader's
  config-side query never reads, but the collector's `resourceAddress`
  (`internal/collector/terraformstate/identity.go`) does prefix `"data."`
  onto a state-side data source's address with no mode filter on the
  state-side query. Separately confirmed this collector's actual
  `for_each`/`count` addressing never emits the literal-dot-in-index shape
  at all (it appends a `facts.StableID` hash digest, `[key:<hash>]`, or a
  plain integer `[index:<N>]`), so the collision could not occur through
  today's data end to end -- the fix is still correct-by-construction
  regardless, since a different or future ingestion path could plausibly
  carry the literal Terraform-CLI-display shape.
  - No-Regression Evidence: `cd go && go test
    ./internal/storage/postgres/... -count=1` is green.
    `TestResourceAddressKeyStripsModulePrefixes` was rewritten with the
    reviewer's exact collision table plus an indexed-module-name case and
    two explicit non-collision assertions; it reproduces every reported
    collision as a genuine RED against the prior implementation and is
    GREEN against the front-stripping one. The pre-existing
    `TestPairSpuriousModuleMismatches*` tests are unaffected (their
    addresses carry no brackets, so old and new implementations agree on
    them).
  - See the evidence doc's "Follow-up: front-stripping fix for
    resourceAddressKey" section for the full RED output and the
    data-source-reachability trace.

- **Review follow-up: fix a P1 pairing no-op for every count/for_each
  resource, and a P2 nondeterministic-ordering bug in the prior-config
  confidence signal.** A second, independent review pass found two more
  defects the first follow-up missed.

  **P1 (verified against the committed code, not assumed):**
  `resourceAddressKey`'s front-stripping only ever removed leading
  `module.<name>` segments; it never touched a trailing per-instance index.
  Config-side rows never carry one (the parser has no per-instance
  information, only a static resource block), while state-side rows for a
  `count`/`for_each` resource ALWAYS do
  (`internal/collector/terraformstate/identity.go` appends `[index:<N>]` or
  `[key:<hash>]`). So a config-only `aws_instance.web` could never equal a
  state-only `aws_instance.web[index:0]` -- `pairSpuriousModuleMismatches`
  silently never paired ANY indexed resource, regardless of module
  resolution confidence. Fixed by stripping a trailing `[INDEX]` suffix too
  (new `stripTrailingIndexSuffix`), using the same bracket/quote-depth
  tracking rather than a naive first-`[` search. This correctly produces
  TWO different outcomes depending on instance count: `count = 1` or a
  single-key `for_each` now pairs (exactly one state instance shares the
  stripped key); `count > 1` or a multi-key `for_each` correctly REFUSES to
  pair (every instance shares one key, so the state side has 2+ candidates
  and a spurious mismatch cannot be attributed to one specific sibling) --
  a documented, intentional scope limitation, not a residual gap.

  **P2 (proven via `EXPLAIN (ANALYZE, VERBOSE)` against a real Postgres 18
  instance, per `eshu-postgres-rigor`):** `loadPriorConfigAddresses`'s
  first-write-wins confidence threading assumed
  `listPriorConfigAddressesQuery`'s row order was most-recent-generation-
  first, but the query's OUTER `ORDER BY` was `pg.generation_id ASC,
  fact.fact_id ASC` -- lexicographic on an opaque `TEXT PRIMARY KEY` with no
  chronological relationship. The CTE's own `ORDER BY ingested_at DESC
  LIMIT $3` bounds which generations are INCLUDED, never the outer row
  order. Proven empirically: three prior generations seeded with
  `generation_id` order (gen-alpha, gen-charlie, gen-omega) deliberately
  scrambled relative to their true `ingested_at` order (gen-charlie,
  gen-alpha, gen-omega) -- the unmodified query returned rows in
  `generation_id` order, not recency order, and `EXPLAIN` showed the CTE
  fully inlined into a Nested Loop on Postgres 18 (no separate CTE Scan
  node). Fixed by exposing `ingested_at` from the CTE and ordering the
  outer `SELECT` by it directly; the fixed query reuses the SAME
  `scope_generations_scope_latest_lookup_idx` index at identical cost, no
  new index needed.
  - No-Regression Evidence: `cd go && go test ./internal/storage/postgres/...
    -count=1` is green, including new coverage for both defects --
    `TestPairSpuriousModuleMismatchesPairsSingleIndexedStateInstance` /
    `TestPairSpuriousModuleMismatchesRefusesWhenMultipleIndexedStateInstancesShareStrippedKey`
    for P1, and
    `TestListPriorConfigAddressesQueryOrdersByIngestedAtDescending` (a SQL
    constant text assertion -- fakeExecQueryer bypasses real SQL execution,
    so only a real Postgres planner run, captured in the evidence doc, can
    prove or disprove a row-ordering claim) plus
    `TestPostgresDriftEvidenceLoaderPrefersMostRecentPriorGenerationConfidenceOnConflict`
    (the first regression exercising two prior generations with conflicting
    confidence for the same address) for P2.
  - Doc comments on `resourceAddressKey` and `pairSpuriousModuleMismatches`
    previously overclaimed "zero false pairings" unconditionally and "any
    index suffix" handling; both now state the narrower, real guarantee and
    name the known gaps (the count/for_each multi-instance miss;
    unescaped-quote-inside-a-quoted-index edge case) explicitly.
  - See the evidence doc's "Follow-up: two independent review findings the
    first follow-up missed" section for the full EXPLAIN output from both
    the broken and fixed query.

- **Review follow-up: convert the P2 SQL-ordering proof from a throwaway
  session into a committed, re-runnable integration test.** The prior
  ordering proof (real Postgres 18, scrambled generations, EXPLAIN
  confirming CTE inlining) lived only as prose and pasted plan output in
  the evidence doc; the one committed test was a substring assertion on the
  SQL constant, which cannot catch a syntactically different `ORDER BY`
  that still contains the substring but produces the wrong order, and
  cannot catch a planner-level regression at all.
  `TestLoadPriorConfigAddressesPrefersMostRecentGenerationAgainstRealPostgres`
  (new, `tfstate_drift_evidence_prior_config_ordering_live_test.go`) is the
  committed evidence of record now: `ESHU_POSTGRES_DSN`-gated, follows this
  package's established live-Postgres pattern (isolated `CREATE
  SCHEMA`/`SET search_path`, `MigrationSQL`), seeds the same
  three-scrambled-generation shape the throwaway proof used, calls
  `loadPriorConfigAddresses` directly against real Postgres, and asserts
  the most recently ingested generation's confidence wins. The connection
  handle is capped at one (`SetMaxOpenConns(1)`/`SetMaxIdleConns(1)`),
  matching `TestLatestGenerationCTETruthEquivalenceAndPlan`'s own guard
  against `SET search_path` being connection-local while `*sql.DB` is a
  pool -- the exact failure mode issue #4451 hit. Also asserts (mirroring
  that same test's `SubPlan` check, the only existing fixture-plan pattern
  in this package) that the `EXPLAIN` output contains no `CTE Scan` node.
  - No-Regression Evidence: verified RED by temporarily restoring the
    pre-fix outer `ORDER BY pg.generation_id ASC, fact.fact_id ASC` and
    running the new test against a real Postgres 18 instance --
    `out["aws_instance.web"] = "", want "external_registry"`, FAIL. GREEN
    after restoring the fix, same seeded data, same instance. Confirmed
    `ESHU_POSTGRES_DSN` unset skips cleanly (credential-free CI /
    `make pre-pr` unaffected) and `git diff` on the SQL file was clean
    after the temporary revert-and-restore cycle. The substring test
    (`TestListPriorConfigAddressesQueryOrdersByIngestedAtDescending`) is
    kept as a cheap, credential-free complement, not replaced.
  - See the evidence doc's "Committed re-runnable proof" section for the
    full RED and GREEN output.

### Route-fact-based Rails controller liveness

- **Join the Rails controller dead-code-root verdict against real route facts**
  ([#5494](https://github.com/eshu-hq/eshu/issues/5494), follow-up to
  [#5376](https://github.com/eshu-hq/eshu/issues/5376)). The #5376 repo-wide
  ancestry walk can confirm/downgrade a `ruby.rails_controller_action` root
  only on whether its class extends a Rails controller base -- a routable
  controller was always kept, even when no route in `config/routes.rb` ever
  reached that specific action. The reducer's `BuildCodeRootVerdicts` now
  additionally joins an ancestry-confirmed action against a repo-wide Rails
  route-fact snapshot (`RubyRailsRouteFacts`) and downgrades it (reason
  `route_unreachable`) only when the repo's route surface is exactly modeled
  (every call the parser saw inside every `Rails.application.routes.draw`
  block resolved into an exact route entry) and proven observed, and no
  `route_entries` handler matches. Any other outcome -- no route data
  observed, or an unmodeled/dynamic route present anywhere in the repo --
  keeps, preserving the #5376 false-negative-safer bias. The Ruby parser
  (`internal/parser/ruby/framework_routes_ambiguity.go`) uses a fail-safe,
  default-to-ambiguous scan (`rubyScanRailsDrawBlockForAmbiguity`): any call
  inside a routes.draw block that does not resolve into an exact route
  (`root`, `match`, gem route macros such as `devise_for`,
  `controller:`/`action:` keyword pairs, bare or interpolated paths, non-string
  `to:` targets, `resources`/`resource` macros, and any other unmodeled
  construct) stamps `framework_semantics.rails.has_unmodeled_routes`, rather
  than enumerating a fixed list of known-ambiguous shapes.
  `CodeReachabilityVerdictSchemaEpoch` is bumped to 3 (layered on #5500's
  epoch 2) to force a one-time re-projection of already-indexed repos (same
  #5376 P1 upgrade-backfill mechanism), since an ancestry-confirmed verdict
  does not otherwise change shape and would stay silently stale without the
  bump.
  - Performance Evidence / No-Regression Evidence / Observability Evidence:
    see `go/internal/reducer/evidence-5494-route-liveness.md` for the
    EXPLAIN (ANALYZE, BUFFERS) proof of the new route-fact load (index-backed
    via the existing `fact_records_framework_routes_repo_path_idx`), the
    schema-epoch assessment, and the real-Postgres correctness proof across
    the routed/unrouted/ambiguous/no-data cases.

### Contract System v1 — reducer accuracy fixes

- **Reject blank `ci.run` `run_id` before indexing an image anchor**
  ([#5234](https://github.com/eshu-hq/eshu/issues/5234), follow-up to
  [#4685](https://github.com/eshu-hq/eshu/issues/4685)). The typed
  container-image-identity decode accepted a present-but-blank `run_id`, and
  `cicdRunKeyFromParts` still returned a non-empty key like `github_actions::1`
  from the provider/attempt alone, so a malformed `ci.run` was indexed and let a
  matching malformed `ci.artifact` inherit its repository anchor. A blank
  provider/run_id is now guarded explicitly, restoring the pre-typing raw
  `cicdRunKey`'s refusal of blank join identity.
  - No-Regression Evidence: the guard is one `strings.TrimSpace` per `ci.run`
    envelope on the container-image-identity map-build path (not a Cypher or
    graph-write hot path). Valid facts carry a non-blank `run_id` and take the
    identical index path as before, so the #4685 golden-corpus result (417 pass,
    0 required-fail, no B-12 drift, NornicDB backend) is unchanged — the guard
    only diverts malformed blank-identity facts, which the golden corpus does not
    contain. Regression test `TestContainerImageCIRunsSkipsBlankRunID` fails
    without the guard (indexes `github_actions::1`) and passes with it.
  - No-Observability-Change: the blank run is skipped through the same `continue`
    path as the pre-existing `key/repositoryID == ""` guard; no metric, span, or
    structured log is added or changed. Existing
    `eshu_dp_reducer_input_invalid_facts_total` still covers true decode-time
    quarantines.

### Telemetry Coverage Discipline (Epic X)

Epic X closes the **telemetry inventory drift** failure class — the recurring
pattern of metrics defined in code but not documented (or vice versa) that
silently blinds an operator at 3 AM. The discipline is four artifacts:

- **X1 — contract doc** ([#3689](https://github.com/eshu-hq/eshu/issues/3689), [PR #3715](https://github.com/eshu-hq/eshu/pull/3715)). `docs/public/observability/telemetry-coverage.md` maps every reducer / projector / collector / parser stage to a metric, span, log key, or `No-Observability-Change:` marker. The X1 doc is the single source of truth.
- **X2 — static-analysis verifier** ([#3690](https://github.com/eshu-hq/eshu/issues/3690), [PR #3718](https://github.com/eshu-hq/eshu/pull/3718)). `scripts/verify-telemetry-coverage.sh` and its test mirror `scripts/test-verify-telemetry-coverage.sh` (8 / 8 cases pass) diff the X1 doc against `go/internal/telemetry/instruments.go` and against new files added since the base ref. Fails on any drift in either direction.
- **X3 — CI gate** ([#3691](https://github.com/eshu-hq/eshu/issues/3691), [PR #3720](https://github.com/eshu-hq/eshu/pull/3720)). `.github/workflows/verify-telemetry-coverage.yml` runs the verifier on every pull request and push to `main`. Hermetic; no Postgres, NornicDB, or Go build required.
- **X4 — operator dashboard** ([#3692](https://github.com/eshu-hq/eshu/issues/3692), [PR #3722](https://github.com/eshu-hq/eshu/pull/3722)). `docs/public/observability/dashboards/eshu-operator-overview.json` — 20 panels, generated by `scripts/generate-operator-dashboard.sh` and its test mirror (7 / 7 cases pass). The "Is Eshu Healthy?" row surfaces `eshu_dp_active_generations{age_bucket="stuck"}` and `eshu_dp_generation_liveness_failures_total` as the 3 AM alarm signal.

The X5 precedent doc ([#3693](https://github.com/eshu-hq/eshu/issues/3693))
ties the artifacts to the historical incidents they prevent:

- [#3633](https://github.com/eshu-hq/eshu/issues/3633) (closed 2026-06-23) — generation-liveness counters missing from the telemetry README and docs index.
- `docs/public/reference/telemetry/index.md:140-156` (historical note) — `eshu_dp_shared_acceptance_rows` and `eshu_dp_worker_pool_active` were defined-but-never-registered for an extended period.
- [#3680](https://github.com/eshu-hq/eshu/issues/3680) (open, 2026-06-24) — per-collector telemetry, the first major in-flight adoption of the discipline.

See `docs/internal/telemetry-discipline-precedent.md` for the full narrative
and a contributor runbook for adding a new metric.

### Merged Pull Requests

- [#3715](https://github.com/eshu-hq/eshu/pull/3715) — docs(telemetry): X1 telemetry-coverage contract
- [#3716](https://github.com/eshu-hq/eshu/pull/3716) — fix(capability-inventory): regenerate stale surface inventory
- [#3718](https://github.com/eshu-hq/eshu/pull/3718) — feat(scripts): X2 telemetry-coverage verifier + test mirror
- [#3720](https://github.com/eshu-hq/eshu/pull/3720) — ci(telemetry): add verify-telemetry-coverage workflow (X3)
- [#3722](https://github.com/eshu-hq/eshu/pull/3722) — feat(dashboards): Eshu operator overview dashboard (X4)

### Verification

- `bash scripts/test-verify-telemetry-coverage.sh` — 8 / 8 cases pass on `main`
- `bash scripts/verify-telemetry-coverage.sh` (with `ESHU_TELEMETRY_COVERAGE_BASE=origin/main`) — clean on `main`
- `bash scripts/test-generate-operator-dashboard.sh` — 7 / 7 cases pass on `main`
- `.github/workflows/verify-telemetry-coverage.yml` — both jobs green on every Epic X PR
- `.github/workflows/generate-operator-dashboard.yml` — both jobs green on the X4 PR
- `mkdocs build --strict --clean --config-file docs/mkdocs.yml` — clean on every Epic X PR
- `git diff --check` — clean on every Epic X PR
