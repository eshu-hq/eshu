# storage/postgres target tree (#6693)

**Status: proposed, waiting for owner approval on #6693.** Nothing below has
moved yet. After approval, each destination directory lands in its own PR, in
the order under [Move order](#move-order-and-checklist), and ticks its box
there.

Baseline: `origin/main` `97a9cbcf1` (2026-09-22). The root package
`go/internal/storage/postgres` holds **368 non-test files** and **711 test
files** (26 of them behind the `integration`, `perf5854_ack`,
`perf5740_completion` or `perf6785_wait` build tags). The dirgate ledger pins
the row at 368 (`scripts/lib/dirgate-grandfather.tsv`).

```bash
git ls-tree --name-only origin/main go/internal/storage/postgres/ | rg '\.go$' | rg -vc '_test\.go$'   # 368
git ls-tree --name-only origin/main go/internal/storage/postgres/ | rg -c '_test\.go$'                  # 711
```

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
| largest directory (non-test) | 368 (root) | 35 (`facts/`) |
| directories over the 40 cap | 1 | 0 |
| files deleted (empty) | | 9 |
| files with no decided home | | 1 |

Root keeps only the composition core that cannot move: `adapters.go`
(`SQLDB`, `SQLTx`), `schema.go` (bootstrap apply), `schema_bootstrap_lock.go`
and `doc.go`. They stay together because of the lock trap the epic records:
`SQLDB.withSchemaBootstrapLock` satisfies the package-private
`schemaBootstrapLocker` interface that `applyBootstrapDefinitions` checks with
a type assertion (`schema.go:218`). An unexported interface method can only be
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
   declare methods on a root type (267 file/type pairs), every one lands with its type except the 23
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
  The stutter check in the census reports zero hits across all 1,079 names.
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
  `terraform/state/drift` is `statedriftstore`. The four `freshness/`
  children do the same (`awsfreshnessstore`, `gcpfreshnessstore`,
  `incidentfreshnessstore`, `vulnerabilityfreshnessstore`), so they do not
  collide with `cloud/aws` (`awsstore`) or `incident/` (`incidentstore`).
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
| `vulnerability_source_state.go` | vulnerability | `freshness/vulnerability/` | same schema/claim shape as the AWS, GCP and incident freshness stores |
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

Identity needs its own set, listed under D1.

## Owner decisions

The owner answers these before the move that depends on them. Each has a
recommendation, and none blocks an earlier step.

**D1. Decompose `IdentitySubjectStore` before the identity split.** 24 files
declare methods on this one type (`identity_subjects.go:364`). Go cannot spread
one method set across the eight `identity/` children, so the split needs a code
change first, not only moves. Recommended: each child declares its own store
struct (`localstore.Store`, `samlstore.Store` and so on) holding only the
handle and keyring it uses, and `IdentitySubjectStore`, which stays in
`identity/` under its current name, embeds them. Every existing
`store.Method(...)` call site keeps compiling through promoted methods. With
that change plus these hoists, the census reduces identity to a single cycle:

- `beginLocalIdentityTx` -> a `db.BeginTx` helper. Its body is a nil check,
  a `db.Beginner` assertion and `Begin`; the helper must keep returning the
  exported `ErrLocalIdentityTransactionRequired` when the handle cannot begin
  a transaction (`identity_local.go:366`).
- `resolvePermissionGrantsForRoles`, `resolveLocalIdentityRolesQuery` and the
  OIDC role-target queries -> a new `identity/permission/` leaf. API tokens,
  OIDC, SAML and local login all resolve roles to grants the same way.
- `cleanBrowserSessionStrings`, `timeFromNull` -> `scalars/`.

The remaining cycle is real domain coupling: `identity/local` calls
`ConsumeBootstrapCredential` during password rotation, while `identity/bootstrap`
uses 22 local-identity symbols to create the first admin. Recommended: pass a
small `BootstrapCredentialConsumer` interface into the rotate path instead of
importing `identity/bootstrap`.

**D2. Give `migrations/` a Go file that owns the embed.** Today `schema.go`
holds `//go:embed migrations/*.sql`, and two stores read migration DDL back
through root's `BootstrapDefinitions()`: `graph_node_owner_store.go:110`
(`graphNodeOwnerSchemaSQL`) and `status_queries.go`. A child cannot import
root, so as things stand both are pinned there. Recommended: add
`migrations/embed.go` (`package migrations`, `//go:embed *.sql`) exporting
`Definition`, `BootstrapDefinitions` and `Checksum`. No `.sql` file moves or
changes, so migration names and checksums stay identical. `BootstrapDefinitions`
already skips names without the `NNN_` prefix, and the embed pattern only
matches `*.sql`.

**D3. All `FactStore` methods live in `facts/`.** 30 files declare methods on
`FactStore`, including 7 domain loaders (incident routing, advisory targets,
IAM trust chains). Recommended: they move into `facts/` as they are, which
gives 35 files (under the cap) and needs no code change. The alternative is
to turn each loader into a free function in its domain directory, taking a
`db.Queryer`, with a one-line `FactStore` wrapper left in `facts/`. That
alternative keeps domain code together, but it is a behavior-preserving rewrite
of seven readers rather than a move.

**D4. The status read surface leaves root.** `status.go` dispatches to readers
in about 11 families. Once those families live in children, `status/` (17
files) imports them the same way root does today, and root drops to the 4-file
core. The alternative is to keep the 17 status files in root, which misses the
issue's "root holds only what cannot move" bar by 17 files.

**D5. Delete 9 empty files.** Each holds only the license header and
`package postgres`. They are leftovers from DDL that moved into
`migrations/*.sql`: `admin_replay_request_schema.go`,
`collector_evidence_summary_schema.go`,
`collector_generation_dead_letter_schema.go`, `graph_schema_applications.go`,
`schema_fact_records_sbom.go`,
`schema_fact_records_service_catalog_indexes.go`,
`service_materialization_schema.go`,
`supply_chain_impact_canonical_winners_schema.go` and
`supply_chain_impact_winners_materialization_schema.go`.

**D6. Shared test fakes become a real package, `fake/`.** Test files cannot be
imported across packages. `work_queue_lifecycle_test.go` holds the fake
database that tests headed for 40 different destinations use. In total, 71
helper test files are used by tests that land in two or more destinations.
Recommended: move the shared fakes (`fakeExecQueryer`, `fakeRows`,
`fakeTransaction` and their kin) into a non-test package `fake/` before the
first domain move. The name avoids `testing/`, which would shadow the standard
library.

**U1. `reducer_input_invalid_facts.go` (UNDECIDED).**
`ReducerInputInvalidFactStore` quarantines fact payloads the reducer rejects.
Nothing in the package uses it, and its only caller is `cmd/reducer`. The
subject says `facts/` and the caller says `queue/reducer/`, and the census
cannot choose between them.

### Naming questions

- **N1. `identity/sign`.** The issue lists `sign`, but it holds the tenant
  sign-in policy (require SSO, require MFA, session timeouts), and a reader
  could take `sign` to mean signatures. Candidates: keep `sign`, use
  `identity/signin` (a one-word compound), or `identity/policy`.
- **N2. `iamcantargets/`** (existing, glued) resolves exact ARNs for
  CAN_PERFORM catalog targets across sibling scopes. Candidate:
  `iam/target/`. It is outside the 368 but inside this lane.
- **N3. One-file directories.** `cicd/`, `decisions/`, `iac/`, `incident/`,
  `maintenance/`, `recovery/`, `search/index/`, `service/catalog/`,
  `service/materialization/`, `terraform/state/` and `freshness/vulnerability/`
  each start with one file. Each is its own table or store with its own
  caller, so none fits in a sibling without a naming lie. Confirm that one-file
  packages are acceptable, or name a sibling to merge into.
- **N4. `freshness/{aws,gcp,incident,vulnerability}`.** The four collector
  freshness-trigger stores share one schema/claim/reap shape, so they sit
  together rather than under `cloud/aws/freshness` and `cloud/gcp/freshness`.
  `freshness/` itself holds repository freshness (`repository.go`).

## Existing subdirectories

| today | after | note |
| --- | --- | --- |
| `db/` | `db/` | gains the hoists above |
| `scalars/` | `scalars/` | gains the hoists above |
| `migrations/` | `migrations/` | no `.sql` file moves or changes; D2 adds `embed.go` |
| `pgarray/` | `array/` | `pg` repeats its parent (issue) |
| `rebuildreset/` | `rebuild/reset/` | glued compound (issue) |
| `iamcantargets/` | see N2 | glued compound |
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
| in-package test | 384 | it only needs its own package and packages below it (2 of these follow the UNDECIDED file) |
| external test package (`package x_test`) | 138 | it also needs a package that imports its subject (root's `ApplyBootstrap` for live tests, for example); uses exported symbols only |
| external test package plus `export_test.go` shim | 85 | as above, and it also reads its subject's private symbols |
| stays in root, split at move time (`SPLIT`) | 35 | it reads private symbols of two or more future packages |
| stays in root | 69 | it exercises the 4 root files or root's private bootstrap symbols, or has no production references at all (14, such as migration-file checks) |

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

- [ ] This target-tree doc (docs only)
- [ ] D5: delete the 9 empty files (368 -> 359)
- [ ] D6: shared test fakes into `fake/`
- [ ] Hoists into `db/` and `scalars/` (table above)
- [ ] D2: `migrations/embed.go` leaf
- [ ] `pgarray/` -> `array/`; `rebuildreset/` -> `rebuild/reset/`

Domains, dependency-first, smaller first at each step (non-test files moved):

1. [ ] `scope/` (new leaf; receives hoisted helpers only)
2. [ ] `cicd/` (1 file)
3. [ ] `decisions/` (1 file)
4. [ ] `facts/payload/` (1 file)
5. [ ] `freshness/vulnerability/` (1 file)
6. [ ] `iac/` (1 file)
7. [ ] `incident/` (1 file)
8. [ ] `maintenance/` (1 file)
9. [ ] `search/index/` (1 file)
10. [ ] `service/catalog/` (1 file)
11. [ ] `service/materialization/` (1 file)
12. [ ] `terraform/state/` (1 file)
13. [ ] `cloud/aws/` (2 files)
14. [ ] `code/taint/` (2 files)
15. [ ] `governance/audit/` (2 files)
16. [ ] `graph/owner/` (2 files)
17. [ ] `queue/` (2 files)
18. [ ] `admission/` (3 files)
19. [ ] `code/reachability/` (3 files)
20. [ ] `freshness/aws/` (3 files)
21. [ ] `freshness/gcp/` (3 files)
22. [ ] `freshness/incident/` (3 files)
23. [ ] `lock/` (3 files; after `scope/`)
24. [ ] `scope/completion/` (4 files)
25. [ ] `crossplane/` (5 files)
26. [ ] `search/document/` (6 files)
27. [ ] `container/image/` (7 files)
28. [ ] `facts/schema/` (7 files; after `container/image/`)
29. [ ] `queue/projector/` (7 files; after `crossplane/`, `facts/payload/`, `queue/`)
30. [ ] `code/flow/` (8 files; after `queue/`)
31. [ ] `terraform/state/drift/` (10 files)
32. [ ] `cloud/aws/drift/` (9 files; after `terraform/state/drift/`)
33. [ ] `cloud/multi/` (5 files; after `cloud/aws/drift/`, `terraform/state/drift/`)
34. [ ] `workflow/` (10 files)
35. [ ] `generation/` (11 files)
36. [ ] `freshness/` (2 files; after `generation/`)
37. [ ] `intent/` (11 files; after `lock/`)
38. [ ] `relationship/` (6 files; after `facts/payload/`, `intent/`, `scope/`)
39. [ ] `content/` (13 files)
40. [ ] `queue/reducer/` (13 files; after `code/flow/`, `facts/payload/`, `queue/`)
41. [ ] `facts/` (35 files; after `facts/payload/`, `generation/`, `queue/projector/`)
42. [ ] `graph/` (1 file; after `facts/`)
43. [ ] `service/evidence/` (2 files; after `facts/`)
44. [ ] `supply/chain/impact/` (2 files; after `facts/`, `queue/projector/`)
45. [ ] `code/divergence/` (3 files; after `facts/`)
46. [ ] `terraform/state/backend/` (5 files; after `facts/`)
47. [ ] `search/vector/` (10 files; after `facts/`, `search/document/`)
48. [ ] `cloud/inventory/` (12 files; after `facts/`, `terraform/state/drift/`)
49. [ ] `ingestion/` (31 files; after `facts/`, `facts/payload/`, `generation/`, `iac/`, `lock/`, `queue/projector/`, `queue/reducer/`, `relationship/`, `scope/`, `workflow/`)
50. [ ] `collector/` (4 files; after `facts/payload/`, `ingestion/`)
51. [ ] `recovery/` (1 file; after `collector/`)
52. [ ] `status/` (17 files; after `collector/`, `freshness/vulnerability/`, `generation/`, `queue/reducer/`, `terraform/state/`, `workflow/`)

Identity, last:

53. [ ] D1: decompose `IdentitySubjectStore`; add `identity/permission/`; invert the rotate -> bootstrap call
54. [ ] `identity/github/` (2 files)
55. [ ] `identity/oidc/` (3 files)
56. [ ] `identity/admin/` (4 files)
57. [ ] `identity/session/` (4 files)
58. [ ] `identity/sign/` (3 files; after `identity/session/`)
59. [ ] `identity/provider/` (8 files)
60. [ ] `identity/saml/` (5 files; after `identity/provider/`)
61. [ ] `identity/local/` (12 files; after `identity/sign/`)
62. [ ] `identity/bootstrap/` (6 files; after `identity/local/`)
63. [ ] `identity/api/` (7 files; after `identity/local/`, `identity/oidc/`)
64. [ ] `identity/` (1 file: `subjects.go`, the store that embeds the children; after every `identity/*` child)

Close-out:

- [ ] U1 placed; N2 `iamcantargets/` renamed
- [ ] dirgate row for `internal/storage/postgres` re-pinned at 4 or removed

## Done

`storage/postgres` root holds the 4-file core; every directory under it has at
most 40 non-test files; no file repeats its directory name; the dirgate row is
removed or re-pinned at 4; D1-D6, U1 and N1-N4 are answered on #6693.

## Per-directory counts

Non-test count is the dirgate number; every row must read 40 or under.

| destination | non-test | test | cap |
| --- | ---: | ---: | --- |
| `storage/postgres` (root) | 4 | 104 | ok |
| `admission/` | 3 | 4 | ok |
| `cicd/` | 1 | 2 | ok |
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
| `facts/` | 35 | 67 | ok |
| `facts/payload/` | 1 | 2 | ok |
| `facts/schema/` | 7 | 8 | ok |
| `freshness/` | 2 | 4 | ok |
| `freshness/aws/` | 3 | 2 | ok |
| `freshness/gcp/` | 3 | 2 | ok |
| `freshness/incident/` | 3 | 1 | ok |
| `freshness/vulnerability/` | 1 | 1 | ok |
| `generation/` | 11 | 21 | ok |
| `governance/audit/` | 2 | 5 | ok |
| `graph/` | 1 | 1 | ok |
| `graph/owner/` | 2 | 3 | ok |
| `iac/` | 1 | 1 | ok |
| `identity/` | 1 | 1 | ok |
| `identity/admin/` | 4 | 4 | ok |
| `identity/api/` | 7 | 4 | ok |
| `identity/bootstrap/` | 6 | 8 | ok |
| `identity/github/` | 2 | 1 | ok |
| `identity/local/` | 12 | 14 | ok |
| `identity/oidc/` | 3 | 2 | ok |
| `identity/provider/` | 8 | 6 | ok |
| `identity/saml/` | 5 | 3 | ok |
| `identity/session/` | 4 | 5 | ok |
| `identity/sign/` | 3 | 4 | ok |
| `incident/` | 1 | 1 | ok |
| `ingestion/` | 31 | 78 | ok |
| `intent/` | 11 | 20 | ok |
| `lock/` | 3 | 3 | ok |
| `maintenance/` | 1 | 1 | ok |
| `queue/` | 2 | 1 | ok |
| `queue/projector/` | 7 | 19 | ok |
| `queue/reducer/` | 13 | 99 | ok |
| `recovery/` | 1 | 7 | ok |
| `relationship/` | 6 | 9 | ok |
| `scope/completion/` | 4 | 6 | ok |
| `search/document/` | 6 | 7 | ok |
| `search/index/` | 1 | 4 | ok |
| `search/vector/` | 10 | 19 | ok |
| `service/catalog/` | 1 | 1 | ok |
| `service/evidence/` | 2 | 2 | ok |
| `service/materialization/` | 1 | 0 | ok |
| `status/` | 17 | 20 | ok |
| `supply/chain/impact/` | 2 | 4 | ok |
| `terraform/state/` | 1 | 1 | ok |
| `terraform/state/backend/` | 5 | 7 | ok |
| `terraform/state/drift/` | 10 | 13 | ok |
| `workflow/` | 10 | 20 | ok |
| UNDECIDED | 1 | 2 | n/a |
| DELETE | 9 | 0 | n/a |
| **total** | **368** | **711** | |

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
