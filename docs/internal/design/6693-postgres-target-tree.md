# storage/postgres target tree (#6693)

**Status: approved on #6693 (2026-09-22).** The owner answered every open
decision; the answers are under [Owner decisions](#owner-decisions-answered-2026-09-22).
Each destination directory lands in its own PR, in the order under
[Move order](#move-order-and-checklist), and ticks its box there.

Baseline: `origin/main` `958e833e9` (2026-09-22). The root package
`go/internal/storage/postgres` holds **368 non-test files** and **713 test
files** (27 of them behind the `integration`, `perf5854_ack`,
`perf5740_completion` or `perf6785_wait` build tags). The dirgate ledger pins
the row at 368 (`scripts/lib/dirgate-grandfather.tsv`).

Both counts come from `git ls-tree --name-only <commit> go/internal/storage/postgres/`.

The binding shape is the #6692 epic's
[storage-collector-tree.md](storage-collector-tree.md#target-tree-internalstoragepostgres)
and the naming rules in [naming.md](../naming.md). This page is the
file-by-file version of that tree. Every directory it adds that the epic did
not list has its reason in
[directories the epic tree does not list](6693-postgres-target-tree/new-directories.md).

## What the result looks like

| | today | after this plan |
| --- | ---: | ---: |
| root non-test files | 368 | 4 |
| largest directory (non-test) | 368 (root) | 36 (`facts/`) |
| directories over the 40 cap | 1 | 0 |
| files deleted | | 9 (six empty; three after their notes move, see D5) |
| files with no decided home | | 0 |

Root keeps only the composition core that cannot move: `adapters.go`
(`SQLDB`, `SQLTx`), `schema.go` (bootstrap apply), `schema_bootstrap_lock.go`
and `doc.go`. They stay together because of the lock trap the epic records:
`SQLDB.withSchemaBootstrapLock` satisfies the package-private
`schemaBootstrapLocker` interface that `applyBootstrapDefinitions` checks with
a type assertion (`schema.go:277`). An unexported interface method can only be
satisfied inside its own package. If `SQLDB` moved alone, the assertion would
quietly return false and bootstrap would run DDL without the advisory lock,
with no compile or runtime error.

## How the mapping was derived

Membership is by symbol, never by file-name prefix (epic ground rule). The
steps:

1. **A `go/types` census** (`golang.org/x/tools/go/packages`, `Tests: true`,
   all four build tags) of the root package plus its 59 importers. For each
   file it recorded the declarations, every cross-file reference inside root,
   and every outside package that uses its symbols.
2. **Method-set closure.** Go keeps a type's methods in the type's package, so
   every file that declares a method on a type moves with that type. This
   decides more than any name does: `FactStore` has methods in 30 files,
   `IdentitySubjectStore` in 24, `IngestionStore` in 17, and `StatusStore` in 6.
   Most of the "prefix lied" corrections below come from this rule. A
   separate check confirms it over the final mapping: of the 236 files that
   declare methods on a root type (269 file/type pairs), every one lands with its type except the 23
   `IdentitySubjectStore` files that wait for D1. That check caught one mapping
   the package-graph check could not: `eshu_search_vector_documents.go`
   declares methods on `EshuSearchDocumentStore`, so it lives in
   `search/document/`, not `search/vector/`.
3. **Package-graph check.** For a candidate mapping, every cross-file
   reference became a package edge and a cycle search ran over the result. A
   directory can leave root only if nothing it uses is still in root, so the
   final graph has **zero edges back into root**. The only cycle left is inside
   `identity/` and needs decision D1.
4. **Hoists** remove the cycles caused by small shared helpers (table below).
   The check models where a hoisted symbol lands, not what that symbol needs in
   its new home, so every move PR still proves itself with a whole-module build.
5. **Tests** follow the production code they exercise; see
   [Test placement](#test-placement).

Every structural claim here (receiver counts, the empty files, the lock
assertion, the `BootstrapDefinitions` calls) was re-derived from source, not
from a file name.

## Naming rules applied

- The directory carries the family, so file names drop it:
  `eshu_search_vector_metadata.go` -> `search/vector/metadata.go`,
  `identity_saml_sql.go` -> `identity/saml/sql.go`.
- No file name repeats a word of its path. A file named exactly for its own
  directory is allowed (`queue/reducer/reducer.go`, `identity/local/local.go`).
  The stutter check in the census reports zero hits across all 1,081 names.
- A `_store` suffix goes when it adds nothing
  (`function_source_store.go` -> `code/flow/function_source.go`) and stays
  where the directory also holds non-store files.
- Existing words are kept, not swapped: a file that said `_sql` still says
  `_sql`.
- Compound names nest: `terraform/state/drift/`, `scope/completion/`,
  `facts/schema/`, never `tfstatedrift`.
- Package clauses follow the convention the merged leaves set (`semantic/` is
  `package semanticstore`): the plain directory word plus `store`, so a caller
  importing both `internal/reducer/semantic` and this package does not need an
  alias. Where two new leaves share a last word and meet in one caller, the
  clause adds the parent: `cloud/aws/drift` is `awsdriftstore` and
  `terraform/state/drift` is `statedriftstore`. The three `freshness/`
  children do the same (`awsfreshnessstore`, `gcpfreshnessstore`,
  `incidentfreshnessstore`), so they do not collide with `cloud/aws`
  (`awsstore`) or `incident/` (`incidentstore`).
  `cloud/inventory` is `inventorystore`, because `infra/inventory` already uses
  `package inventory`. Deriving every clause this way gives no collision with
  an existing package name under `go/`.
- Exported identifiers will stutter after some moves
  (`reducerstore.ReducerQueue`, `imagestore.ContainerImageIdentityBeginner`).
  This plan records those and does not rename identifiers. A move PR changes
  paths and imports only, so reviewers can check it mechanically; identifier
  renames are separate work.

## Membership corrections (the prefix lied)

| file | name suggests | actual home | evidence |
| --- | --- | --- | --- |
| `generation_freshness.go`, `generation_projected_commit.go` | `generation/` | `ingestion/` | methods on `IngestionStore` |
| `drift_catchup_lister.go`, `drift_enqueue.go` | terraform drift | `ingestion/` | methods on `IngestionStore` |
| `deferred_maintenance_barrier*.go` | `maintenance/` | `ingestion/` | methods on `IngestionStore` |
| `iac_reachability_materializer.go` | `iac/` | `ingestion/` | methods on `IngestionStore` |
| `fact_records_scope_generation_enum.go` | `generation/` | `facts/` | method on `FactStore` |
| `code_{function_source,function_summary,interproc_evidence,taint_evidence}_loader.go` | `code/` | `facts/` | methods on `FactStore` |
| `incident_routing_evidence_loader.go`, `installed_advisory_targets*.go`, `owned_package_targets.go`, `secrets_iam_trust_chain_evidence_loader.go`, `service_vulnerability_advisory_loader.go`, `container_image_identity_support_fact_loader.go` | their domains | `facts/` | methods on `FactStore` (decision D3) |
| `generation_lifecycle*.go`, `changed_since*.go`, `service_changed_since*.go` | various | `status/` | methods on `StatusStore` |
| `accepted_generation.go` | `generation/` | `relationship/` | methods on `RelationshipStore` |
| `cloud_runtime_drift_aggregate_findings.go` | `cloud/inventory/` | `cloud/multi/` | methods on `MultiCloudRuntimeDriftFindingStore` |
| `identity_epoch_cache.go` | `identity/` | `facts/` | container-image identity epochs; only `FactStore` reads it |
| `identity_api_tokens*.go` | API token lifecycle | `identity/api/scoped_resolution*.go` | methods on `ScopedAPITokenStore`, not `IdentitySubjectStore` |
| `status_requests.go` | `status/` | `maintenance/requests.go` | `StatusRequestStore` is an operator scan-request queue with no `StatusStore` coupling |
| `terraform_config_state_drift_findings.go` | `terraform/` | `terraform/state/drift/` | findings layer for the same config-vs-state drift evidence |
| `vulnerability_source_state.go` | freshness | `vulnerability/` | collector source state with none of the freshness trigger methods (N4) |
| `factschema_decode_cloud_tag_evidence.go` | `facts/` | `cloud/inventory/` | decodes payloads for `cloud_tag_evidence.go`, its only reader |

## Prerequisite hoists

Each hoist is a small PR (or the first commit of the move that needs it) that
moves one shared helper into a leaf both sides can import. Symbols that other
packages call become exported; the table lists only symbols that change
package.

| symbol | from | to | why |
| --- | --- | --- | --- |
| `searchIndexTermCopyUnsupportedError` (with its `driver` field) | `adapters.go` | `db/` | generic driver-capability error used by InstrumentedDB |
| `querySummaryFromContext` | `status_read_telemetry.go` | `db/` | query-label tracing plumbing used by InstrumentedDB |
| `durationFromSeconds` | `status.go` | `scalars/` | seconds -> time.Duration conversion |
| `nullableTimeUTC` | `status_aws_cloud.go` | `scalars/` | null-time scan helper |
| `nullableTime` | `workflow_control_helpers.go` | `scalars/` | null-time scan helper |
| `stringMapToAny` | `ingestion.go` | `scalars/` | map conversion helper |
| `terraformStateLastSerialQuery` | `status_queries.go` | `terraform/state/` | Terraform-only SQL moves with its reader |
| `terraformStateRecentWarningsQuery` | `status_queries.go` | `terraform/state/` | Terraform-only SQL moves with its reader |
| `sanitizeFailureText` | `projector_queue.go` | `queue/` | generic dead-letter text sanitizer, moves beside failure metadata |
| `claimedAtValue` | `reducer_queue_helpers.go` | `queue/` | claim timestamp helper shared by reducer queue and value-flow ack |
| `deferredScopedFactOwnRepoIDFromScope` | `ingestion_backfill_deferred_regex.go` | `scope/` | scope-id parsing shared by ingestion and relationship |
| `cleanStringSet` | `aws_cloud_runtime_drift_findings.go` | `scalars/` | string-set normalizer shared by aws/multi/terraform drift |
| `scopeSourceKey` | `ingestion_queries.go` | `scope/` | scope source-key helper shared by ingestion and the deferred-maintenance lock |
| `BootstrapDefinitions`, `Definition` (fields `Name`, `Path`, `SQL`) | `schema.go` | `migrations/` | the migrations leaf owns the `//go:embed` (decision D2) |
| `migrationChecksum` | `schema_bootstrap_lock.go` | `migrations/` | migration content checksum, shared by the bootstrap tracker and status |
| `eshuSearchVectorPendingMaxLimit` | `eshu_search_vector_pending.go` | `search/document/` | pending-read limit shared by the vector sidecar and the pending-document read |

## Owner decisions (answered 2026-09-22)

The owner accepted these answers on #6693. Each one names the evidence it
rests on; the move that depends on it re-checks that evidence first.

**D1. Identity splits after `IdentitySubjectStore` is decomposed.** 24 files
declare methods on this one type (`identity_subjects.go:364`), and Go keeps a
type's methods in its own package. Each child declares its own store struct
(`localstore.Store`, `samlstore.Store` and so on) holding only the handle and
keyring it uses. `IdentitySubjectStore` keeps its name in `identity/` and
embeds them, so every `store.Method(...)` call site keeps compiling through
promoted methods. The hoists:

- `beginLocalIdentityTx` -> a `db.BeginTx` helper. Its body is a nil check,
  a `db.Beginner` assertion and `Begin`; the helper must keep returning the
  exported `ErrLocalIdentityTransactionRequired` (`identity_local.go:366`).
- `resolvePermissionGrantsForRoles`, `resolveLocalIdentityRolesQuery` and the
  OIDC role-target queries -> a new `identity/permission/` leaf.
- `cleanBrowserSessionStrings`, `timeFromNull` -> `scalars/`.
- `ConsumeBootstrapCredential` and `consumeBootstrapCredentialQuery` move into
  `identity/local`. Local calls it once (`identity_local_rotate.go:186`), and
  its body needs only the handle and that query. A method move has no unwired
  path; an injected interface would, and a forgotten wiring would leave the
  bootstrap credential retrievable after rotation.

With those, the package graph has no cycle. The cost is 12 private `identity/local`
symbols that bootstrap uses becoming exported: `consumeBootstrapCredentialQuery`
(setup completion runs it directly, `identity_setup_completion.go:105`),
`countExistingLocalIdentityUsers`, `insertBootstrapLocalIdentity`, `insertLocalIdentityMFA`,
`localIdentityBootstrapLockQuery`, `lockLocalIdentityMFAReset`, `normalizeBootstrapRecord`,
`normalizeMFAReset`, `revokeLocalIdentityMFAFactorsQuery`, `revokeLocalIdentityRecoveryCodesQuery`,
`validateBootstrapRecord` and `validateMFAReset`. The rejected alternative was folding
`bootstrap/` into `local/`. A same-named promoted method in two children would be a
compile error, so the D1 PR's build catches it.

**D2. `migrations/embed.go` owns the embed.** Only three non-test files read
`BootstrapDefinitions()`: `schema.go`, `status_queries.go` and
`graph_node_owner_store.go:110`. The new `package migrations` exports
`Definition`, `BootstrapDefinitions` and `Checksum`, and carries its own
`doc.go`, `README.md` and `AGENTS.md`. `Definition` also has two unexported
fields, `variant` and `fullChecksum` (`schema.go:27-28`), which root's
`BootstrapDefinitionsWithoutContentSearchIndexes` sets. That function stays in
root, because it needs the content store's deferred DDL, so the two fields
become exported. No `.sql` file moves or changes, so names and checksums stay
the same; the embed pattern `*.sql` cannot pick up the new Go file.

**D3. All 30 `FactStore` method files go to `facts/`.** The domain loaders call
`FactStore`'s private paging helpers, so turning them into free functions in
their domain directories would export more, not less. `facts/` ends at 36
files with U1, leaving four files of room; dirgate catches it if feature work
fills that.

**D4. The status read surface moves to `status/`.** `StatusStore` is a read
dispatcher over a `db.Queryer` (`status.go:39-42`). Moving its 17 files leaves
root with the 4-file core.

**D5. Delete 9 files, after saving the notes three of them carry.** Six hold
only the license header and `package postgres`, and go in one PR:
`admin_replay_request_schema.go`, `collector_evidence_summary_schema.go`,
`collector_generation_dead_letter_schema.go`, `graph_schema_applications.go`,
`schema_fact_records_sbom.go` and
`schema_fact_records_service_catalog_indexes.go`. The other three hold design
notes (#1943, #3389) that exist nowhere else. They go in the move that creates
their owner's package, with the notes folded into that package's `README.md`:
`service_materialization_schema.go` in the `service/` move, and
`supply_chain_impact_canonical_winners_schema.go` and
`supply_chain_impact_winners_materialization_schema.go` in the
`supply/chain/impact/` move. The notes do not go into the migration files: the
tracker checksums each migration's full text, comments included
(`migrationChecksum`, `schema_bootstrap_lock.go:143`), and refuses to start
when an applied migration's checksum changes (`schema_bootstrap_lock.go:302`),
so a comment edit would stop bootstrap on every existing database.

**D6. Shared test fakes become a real package, `fake/`, and that is a code
change.** Test files cannot be imported across packages. In total, 71 helper
test files are used by tests that land in two or more destinations, and
`work_queue_lifecycle_test.go` alone serves 38. Its fake database decides what to
return by matching root's private query constants (`activeScopeGenerationQuery`
and `listDeferredScopedRelationshipFactRecordsQuery`,
`work_queue_lifecycle_test.go:205,239`), which another package cannot see. So
the routing becomes injectable (query prefix to rows), the ingestion-specific
cases stay in the ingestion tests, and the change gets its own tests before the
first domain move. The name avoids `testing/`, which would shadow the standard
library.

**U1. `reducer_input_invalid_facts.go` goes to `facts/`** as
`reducer_input_invalid.go`. It is a per-fact ledger keyed by scope,
generation, fact id, missing field and domain, with no claim or lease. The
reducer writes it (`cmd/reducer`) and `internal/query/admin/store` reads it;
`queue/reducer/` holds only work-queue state. Its Go copy of migration 060's
DDL and its `EnsureSchema` are a drift risk to note, not part of the move.

### Naming answers

- **N1. `identity/signin/`** (package `signinstore`). "Policy" already names
  permission-policy revisions (`PolicyRevisionHash`) and would sit beside
  `identity/permission/`; bare "sign" reads as signatures. "Sign-in" is one
  noun and matches the exported `SignInPolicy`.
- **N2. `iamcantargets/` becomes `cloud/aws/iam/target/`.** Its package doc
  describes resolving AWS ARNs across sibling scopes of one AWS account, and
  the projector already spells this family `cloud/aws/iam/...`. The "can" is
  the reducer's shorthand; the types (`CrossScopeTarget`) do not carry it.
- **N3. One-file directories are accepted, except `service/`.** The three
  `service/*` leaves share one owner word, so they merge into one `service/`
  of 4 files and save six package-doc files. `cicd/`, `decisions/`, `iac/`,
  `incident/`, `maintenance/`, `recovery/`, `search/index/`,
  `terraform/state/` and `vulnerability/` stay: each has its own table or
  runtime owner, or is a parent the epic lists.
- **N4. `freshness/{aws,gcp,incident}/` stay together; the vulnerability store
  moves to `vulnerability/source_state.go`.** The three freshness stores share
  one trigger shape (`StoreTrigger`, `ClaimQueuedTriggers`,
  `ReapExpiredTriggerClaims`, `MarkTriggersHandedOff`, `MarkTriggersFailed`),
  and the coordinator and webhook listener build them, not the collectors
  (`cmd/workflow-coordinator/main.go:148,158,168`,
  `cmd/webhook-listener/main.go:98,111,125`). `vulnerability_source_state.go`
  has none of those methods; it upserts and reads collector source state, and
  `cmd/collector-vulnerability-intelligence` builds it.

The epic tree lists `cloud/gcp/`, but this mapping sends no file there: the
only GCP files are the freshness triggers, which follow their runtime owner.

## Existing subdirectories

| today | after | note |
| --- | --- | --- |
| `db/` | `db/` | gains the hoists above |
| `scalars/` | `scalars/` | gains the hoists above |
| `migrations/` | `migrations/` | no `.sql` file moves or changes; D2 adds `embed.go` |
| `pgarray/` | `array/` | `pg` repeats its parent (issue) |
| `rebuildreset/` | `rebuild/reset/` | glued compound (issue) |
| `iamcantargets/` | `cloud/aws/iam/target/` | glued compound; AWS-only ARN resolution (N2) |
| `semantic/`, `tenant/`, `webhook/` | unchanged | landed in #6856, #6847, #6844 |
| `coordination/` | unchanged | added by #6970 after the baseline; migrator wait/retry loops that leave root's locker contract in root |
| `readiness/wait/`, `infra/inventory/` | unchanged | already nested |

## Test placement

A test goes to the destination of the production file whose name it extends
(`reducer_queue_batch_test.go` follows `reducer_queue_batch.go`). If none
matches, it goes where most of its production references point. A test that
reads private symbols of exactly one package goes to that package, whatever
its name says. Then Go's package rules decide the form:

| form | tests | when |
| --- | ---: | --- |
| in-package test | 373 | it only needs its own package and packages below it |
| external test package (`package x_test`) | 139 | it also needs a package that imports its subject (root's `ApplyBootstrap` for live tests, for example); uses exported symbols only |
| external test package plus `export_test.go` shim | 88 | as above, and it also reads its subject's private symbols |
| stays in root, split at move time (`SPLIT`) | 40 | it reads private symbols of two or more future packages |
| stays in root | 73 | it exercises the 4 root files or root's private bootstrap symbols, or has no production references at all (14, such as migration-file checks) |

Test names drop leading words the destination path already says. The census
reports no stutter and no duplicate name in any destination. Test files do not
count toward the 40-file cap.

## Move order and checklist

The issue sets the order: `db` hoist first, then domains smallest first, then
`identity` last. One more constraint applies. A directory can leave root only
after everything it imports has left, or root and the child would import each
other. So the order below is dependency-first, with the smaller directory
first at each step. Every move PR follows the epic's
[mandatory proof requirements](storage-collector-tree.md#mandatory-proof-requirements-every-pr-under-this-epic)
and re-pins the dirgate row down.

Landed:

- [x] `db/` contracts hoist: #6754, #6757
- [x] `webhook/` #6844, `tenant/` #6847, `semantic/` #6856

Prerequisites:

- [x] This target-tree doc: #6973; owner answers and corrections: this PR
- [x] D5: delete the six empty files (368 -> 362); the other three (notes-carrying) go in the `service/` and `supply/chain/impact/` moves
- [x] D6: `fake/` package added (injectable routes); root's test-only fake retires as its tests move
- [x] Hoists into `db/` and `scalars/` (the `db/` and `scalars/` rows of the table above)
- [x] D2: `migrations/embed.go` leaf
- [x] `pgarray/` -> `array/`; `rebuildreset/` -> `rebuild/reset/`

Domains, dependency-first, smaller first at each step (non-test files moved):

1. [x] `scope/` (new leaf; receives hoisted helpers only)
2. [x] `cicd/` (1 file)
3. [x] `decisions/` (1 file)
4. [x] `facts/payload/` (1 file)
5. [x] `iac/` (1 file)
6. [x] `incident/` (1 file)
7. [x] `maintenance/` (1 file)
8. [x] `search/index/` (1 file)
9. [x] `terraform/state/` (1 file)
10. [ ] `vulnerability/` (1 file)
11. [ ] `cloud/aws/` (2 files)
12. [ ] `code/taint/` (2 files)
13. [ ] `governance/audit/` (2 files)
14. [ ] `graph/owner/` (2 files)
15. [ ] `queue/` (2 files)
16. [ ] `admission/` (3 files)
17. [ ] `code/reachability/` (3 files)
18. [ ] `freshness/aws/` (3 files)
19. [ ] `freshness/gcp/` (3 files)
20. [ ] `freshness/incident/` (3 files; removes the three `incident_freshness_*` dirgate markers)
21. [ ] `lock/` (3 files; after `scope/`)
22. [ ] `scope/completion/` (4 files; removes the `scope_quiescence.go` dirgate marker)
23. [ ] `crossplane/` (5 files)
24. [ ] `search/document/` (6 files)
25. [ ] `container/image/` (7 files)
26. [ ] `facts/schema/` (7 files; after `container/image/`)
27. [ ] `queue/projector/` (7 files; after `crossplane/`, `facts/payload/`, `queue/`)
28. [ ] `code/flow/` (8 files; after `queue/`)
29. [ ] `terraform/state/drift/` (10 files)
30. [ ] `cloud/aws/drift/` (9 files; after `terraform/state/drift/`)
31. [ ] `cloud/multi/` (5 files; after `cloud/aws/drift/`, `terraform/state/drift/`)
32. [ ] `workflow/` (10 files)
33. [ ] `generation/` (11 files)
34. [ ] `freshness/` (2 files; after `generation/`)
35. [ ] `intent/` (11 files; after `lock/`)
36. [ ] `relationship/` (6 files; after `facts/payload/`, `intent/`, `scope/`)
37. [ ] `content/` (13 files)
38. [ ] `queue/reducer/` (13 files; after `code/flow/`, `facts/payload/`, `queue/`)
39. [ ] `facts/` (36 files; after `facts/payload/`, `generation/`, `queue/projector/`; removes the `incident_routing_evidence_loader.go` dirgate marker)
40. [ ] `graph/` (1 file; after `facts/`)
41. [ ] `supply/chain/impact/` (2 files; after `facts/`, `queue/projector/`)
42. [ ] `code/divergence/` (3 files; after `facts/`)
43. [ ] `service/` (4 files; after `facts/`)
44. [ ] `terraform/state/backend/` (5 files; after `facts/`)
45. [ ] `search/vector/` (10 files; after `facts/`, `search/document/`)
46. [ ] `cloud/inventory/` (12 files; after `facts/`, `terraform/state/drift/`)
47. [ ] `ingestion/` (31 files; after `facts/`, `facts/payload/`, `generation/`, `iac/`, `lock/`, `queue/projector/`, `queue/reducer/`, `relationship/`, `scope/`, `workflow/`; removes the `iac_reachability_materializer.go` dirgate marker)
48. [ ] `collector/` (4 files; after `facts/payload/`, `ingestion/`)
49. [ ] `recovery/` (1 file; after `collector/`)
50. [ ] `status/` (17 files; after `collector/`, `generation/`, `queue/reducer/`, `terraform/state/`, `vulnerability/`, `workflow/`)

Identity, last:

51. [ ] D1: decompose `IdentitySubjectStore` into embedded per-child stores; add `identity/permission/`; move `ConsumeBootstrapCredential` into `identity/local`
52. [ ] `identity/github/` (2 files)
53. [ ] `identity/oidc/` (3 files)
54. [ ] `identity/admin/` (4 files)
55. [ ] `identity/session/` (4 files)
56. [ ] `identity/signin/` (3 files; after `identity/session/`)
57. [ ] `identity/provider/` (8 files)
58. [ ] `identity/saml/` (5 files; after `identity/provider/`)
59. [ ] `identity/local/` (12 files; after `identity/signin/`)
60. [ ] `identity/bootstrap/` (6 files; after `identity/local/`)
61. [ ] `identity/api/` (7 files; after `identity/local/`, `identity/oidc/`)
62. [ ] `identity/` (1 file: `subjects.go`, the store that embeds the children; after every `identity/*` child)

Close-out:

- [ ] U1 placed; N2 `iamcantargets/` renamed
- [ ] dirgate row for `internal/storage/postgres` re-pinned at 4 or removed

## Done

`storage/postgres` root holds the 4-file core; every directory under it has at
most 40 non-test files; no file repeats its directory name; the dirgate row is
removed or re-pinned at 4.

## Per-directory counts

Non-test count is the dirgate number; every row must read 40 or under.

| destination | non-test | test | cap |
| --- | ---: | ---: | --- |
| `storage/postgres` (root) | 4 | 113 | ok |
| `admission/` | 3 | 4 | ok |
| `cicd/` | 1 | 1 | ok |
| `cloud/aws/` | 2 | 2 | ok |
| `cloud/aws/drift/` | 9 | 14 | ok |
| `cloud/inventory/` | 12 | 12 | ok |
| `cloud/multi/` | 5 | 5 | ok |
| `code/divergence/` | 3 | 3 | ok |
| `code/flow/` | 8 | 11 | ok |
| `code/reachability/` | 3 | 4 | ok |
| `code/taint/` | 2 | 2 | ok |
| `collector/` | 4 | 2 | ok |
| `container/image/` | 7 | 9 | ok |
| `content/` | 13 | 15 | ok |
| `crossplane/` | 5 | 5 | ok |
| `db/` | 3 | 2 | ok |
| `decisions/` | 1 | 1 | ok |
| `facts/` | 36 | 69 | ok |
| `facts/payload/` | 1 | 1 | ok |
| `facts/schema/` | 7 | 8 | ok |
| `freshness/` | 2 | 4 | ok |
| `freshness/aws/` | 3 | 2 | ok |
| `freshness/gcp/` | 3 | 2 | ok |
| `freshness/incident/` | 3 | 1 | ok |
| `generation/` | 11 | 21 | ok |
| `governance/audit/` | 2 | 5 | ok |
| `graph/` | 1 | 1 | ok |
| `graph/owner/` | 2 | 3 | ok |
| `iac/` | 1 | 1 | ok |
| `identity/` | 1 | 1 | ok |
| `identity/admin/` | 4 | 3 | ok |
| `identity/api/` | 7 | 3 | ok |
| `identity/bootstrap/` | 6 | 8 | ok |
| `identity/github/` | 2 | 1 | ok |
| `identity/local/` | 12 | 13 | ok |
| `identity/oidc/` | 3 | 2 | ok |
| `identity/provider/` | 8 | 6 | ok |
| `identity/saml/` | 5 | 3 | ok |
| `identity/session/` | 4 | 5 | ok |
| `identity/signin/` | 3 | 4 | ok |
| `incident/` | 1 | 1 | ok |
| `ingestion/` | 31 | 77 | ok |
| `intent/` | 11 | 20 | ok |
| `lock/` | 3 | 3 | ok |
| `maintenance/` | 1 | 1 | ok |
| `queue/` | 2 | 1 | ok |
| `queue/projector/` | 7 | 19 | ok |
| `queue/reducer/` | 13 | 99 | ok |
| `recovery/` | 1 | 7 | ok |
| `relationship/` | 6 | 9 | ok |
| `scope/` | 2 | 2 | ok |
| `scope/completion/` | 4 | 6 | ok |
| `search/document/` | 6 | 7 | ok |
| `search/index/` | 1 | 2 | ok |
| `search/vector/` | 10 | 19 | ok |
| `service/` | 4 | 3 | ok |
| `status/` | 17 | 20 | ok |
| `supply/chain/impact/` | 2 | 4 | ok |
| `terraform/state/` | 1 | 1 | ok |
| `terraform/state/backend/` | 5 | 7 | ok |
| `terraform/state/drift/` | 10 | 13 | ok |
| `vulnerability/` | 1 | 1 | ok |
| `workflow/` | 10 | 20 | ok |
| DELETE | 9 | 0 | n/a |
| **total** | **368** | **713** | |

## File-by-file mapping

Every root file, test or not, appears exactly once, grouped by destination. Each line reads `current -> new`; a trailing `#` note gives the reason for anything unusual.

- [Directories the epic tree does not list](6693-postgres-target-tree/new-directories.md)
- [Root, deletions and the undecided file](6693-postgres-target-tree/root.md)
- [Work queues](6693-postgres-target-tree/queue.md)
- [Facts](6693-postgres-target-tree/facts.md)
- [Ingestion, relationships, intents, locks and scope](6693-postgres-target-tree/ingestion.md)
- [Identity and governance audit](6693-postgres-target-tree/identity.md)
- [Cloud, Terraform state, freshness and supply chain](6693-postgres-target-tree/cloud.md)
- [Code analysis, search and content](6693-postgres-target-tree/code.md)
- [Status, generations, workflow and recovery](6693-postgres-target-tree/control-plane.md)
