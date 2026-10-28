# Ingester Extraction Prep Census (`#8879`)

Status: **PREP ONLY**. No code moves, no repo seeding beyond the existing
placeholder, no deploy, no cutover, no in-tree removal. This manifest records
the current in-tree truth so the cutover gate can compare in-tree and external
outputs on identical inputs.

Scope: `go/cmd/ingester/` (intake orchestration outside the default Git/parser
implementation) plus the six assigned document-import capabilities under
`go/internal/collector/document/{export,media,ocr}/` and
`go/internal/collector/preflight/{manifest,media,picture}/`.
Out of scope: `collector-git` (`core_by_design`), discovery/parsing,
reducer/resolution, canonical admission/projection/storage, API/MCP/CLI,
deployment, and the sibling `eshu-webhook` lane (`#8876`).

Refs `#8879`. Part of `#6707`.

## 1. Current packages

### Binary: `go/cmd/ingester/`

| File | Role | Key symbol |
| --- | --- | --- |
| `main.go:27,42` | entry point, `--version` flag, `run` | `main`, `run` |
| `main.go:44` | OTEL telemetry bootstrap | `telemetry.NewBootstrap("ingester")` |
| `main.go:88` | Postgres open (process-owned pool) | `runtimecfg.OpenPostgres` |
| `main.go:97` | writer-session fence check | `inventory.VerifyWriterSession` |
| `main.go:98` | schema compat gate | `graphschemacompat.RequireCompatibleForRuntime` |
| `main.go:105` | queue observer store | `postgres.NewQueueObserverStore` |
| `main.go:111` | canonical graph writer open | `openIngesterCanonicalWriter` |
| `main.go:126` | intake service build | `buildIngesterService` |
| `main.go:140` | retry policy load | `loadIngesterRetryPolicy` |
| `main.go:166` | hosted status server assembly | `app.NewHostedWithStatusServer` |
| `main.go:181` | Postgres gauge refresher lifecycle | `startPostgresQueueGauges` |
| `wiring.go:119` | collector+projector assembly | `buildIngesterService` |
| `wiring.go:142` | Git intake collector wiring | `buildIngesterCollectorService` |
| `wiring.go:159` | durable fact committer | `postgres.NewIngestionStore` |
| `wiring.go:165` | corpus-wide backfill stays batched | `SkipRelationshipBackfill = true` (`#4451`) |
| `wiring.go:182` | repo snapshotter | `git.NativeRepositorySnapshotter` |
| `wiring.go:201` | repo selector | `git.NativeRepositorySelector` |
| `wiring.go:234` | webhook trigger selector | `git.WebhookTriggerRepositorySelector` |
| `wiring.go:260` | Git source | `git.GitSource` |
| `wiring.go:275` | deferred-relationship barrier hook | `ingesterDeferredRelationshipMaintenance` |
| `wiring.go:290` | single-shard collector-off escape | `AfterEmptyBatchDrained` (`#7665`) |
| `wiring.go:297` | projector wiring | `buildIngesterProjectorService` |
| `wiring.go:305` | projector queue | `postgres.NewProjectorQueue(database, "ingester", 5m)` |
| `composite_runner.go:61,87` | collector+projector supervision | `newCompositeRunner`, `Run` |
| `composite_runner.go:25` | bounded sibling drain ceiling | `defaultCompositeDrainGrace = 30s` |
| `config.go:15,28` | retry injector/policy env load | `loadIngesterRetryInjector`, `loadIngesterRetryPolicy` |
| `local_lightweight.go` | local-only no-op writer profile | `ingesterLocalLightweight`, `maybeLocalLightweightCanonicalWriter` |
| `reducer_admission.go` | reducer intent writer | `ingesterReducerIntentWriter` |
| `wiring_canonical_writer_open.go:57` | backend writer open (Neo4j/NornicDB) | `openIngesterCanonicalWriter` |
| `wiring_canonical_writer.go` | canonical gate (`#4729`/`#4456`) | `newIngesterCanonicalGate` |
| `wiring_deferred_relationship.go` | fleet maintenance barrier | `ingesterDeferredRelationshipMaintenance` |
| `queue_gauge_wiring.go` | queue gauges (`#7064`) | `registerPostgresQueueGauges` |
| `wiring_neo4j_executor.go` | Neo4j executor | canonical Neo4j write path |
| `wiring_nornicdb_config.go`, `wiring_nornicdb_env.go` | NornicDB tuning knobs | phase-group/batch/concurrency env |
| `reindex_watermark.go` | fleet reindex watermark (`#7620`) | `reindexWatermarkReader` |
| `kustomize_overlay_resolver.go` | overlay resolution | kustomize target mapping |
| `terraform_state_ownership.go`, `terraform_state_config_match.go` | tfstate ownership | terraform state scope guards |
| `files_group_probe.go`, `neo4j_write_timeout.go` | write-path probes | group/timeout instrumentation |
| `doc.go` | package contract | pipeline, routes, StatefulSet ownership |

### Supporting packages (imported by `wiring.go:15-33`, `main.go:16-24`)

`internal/collector`, `collector/repo/git` (+`membership`, `preparse`,
`rest`), `internal/content`, `internal/cpubudget`, `internal/projector`
(+`failure`, `runtime`), `internal/runtime`, `internal/storage/nornicdb`,
`internal/storage/postgres` (+`db`, `freshness/links`, `maintenance`,
`membership`, `webhook`), `internal/telemetry`, `internal/app`,
`internal/buildinfo`, `internal/graphschemacompat`, `internal/recovery`,
`internal/status`.

## 2. Retained Git/parser boundary

Per `docs/internal/design/eshu-ecosystem-repository-migration.md:16-22`
(eshu retains default Git collector, discovery/parsing, reducer/resolution,
canonical admission/projection/storage) and the repository map
(`:56-60`, `eshu-ingester` owns intake orchestration outside the default
Git/parser implementation). `collector-git` is absent from the collector
table because it is `core_by_design` (`:105-108`).

Stays in eshu: `go/cmd/collector-git`, `go/internal/collector/repo/git`
(selector, `GitSource`, snapshotter, preparse, webhook-handoff,
membership observer), `go/cmd/reducer`, Postgres ingestion/projector-queue/
status/recovery stores, canonical writer (Neo4j executor + NornicDB phases).

Move candidate (intake shell only): the `buildIngesterService` builders
(`wiring.go:119-140`), the collector/projector service constructors
(`wiring.go:142-295`, `wiring.go:297+`), the composite supervision
(`composite_runner.go`), the hosted status/recovery assembly
(`main.go:126-187`), the retry env injector (`config.go`), the queue-gauge
wiring, and the `local_lightweight` profile switch.

## 3. Fact kinds

`go/internal/facts/fact_kind_registry.generated.go` holds 185 distinct
`{Kind: "..."}` entries (count verified live). The ingester emits the
corpus-wide Git fact families through `NewIngestionStore`; no per-kind
version table is duplicated here — the registry is the source of truth.
`documentation_*` kinds are present for the document-import capabilities below.

## 4. Assigned document-import capabilities (all dormant)

| Package | Contract (`doc.go`) | Production caller |
| --- | --- | --- |
| `document/export` | offline export parse only; no discovery, archives, providers, or persistence; fail-closed manifest preflight | none |
| `preflight/manifest` | bounded JSON manifest validation; metadata-only counts and warning classes; emits no facts | none |
| `document/media` | reviewed local transcripts via injected engine; provenance-only mentions; redaction before persistence | none; hosted enablement caller-owned |
| `preflight/media` | container-metadata classification; metadata-only; no facts, persistence, or transcription | none |
| `document/ocr` | reviewed local OCR via injected engine; document+section facts only | none; hosted enablement caller-owned |
| `preflight/picture` | image-container classification; metadata-only; no OCR or graph writes | none |

Dormancy proof: `rg` for `collector/document|collector/preflight` across
`go/cmd/ingester/*.go` and `go/internal/app/*.go` (excluding tests) returns
no matches. Hosted activation for media/ocr stays **DISABLED** until sandbox
and runtime proof pass. Retained parser core (not assigned here):
`preflight/archive`, `preflight/diagram`, `preflight/ooxml` (Git document
parsing uses them) and `preflight/pdf` (dormant format-safety classifier).

## 5. Durable-state owner

Postgres owns queue, content, status, recovery, and generation state; Neo4j
owns projected graph truth. The ingester holds no durable state of its own:
the workspace PVC is clone scratch only, and it is the only long-running
runtime that mounts it (`doc.go:39-40`, StatefulSet, SIGINT/SIGTERM-clean).
The canonical writer opens once at startup (`main.go:111`) behind the schema
compat gate (`main.go:98`) and the writer-session fence (`main.go:97`); the
corpus-wide deferred-relationship backfill runs as a separate batch phase
behind the drain barrier (`wiring.go:275`, `wiring_deferred_relationship.go`).

## 6. Intake-to-core contracts and protocols

- Collector: `collector.Service{Source: git.GitSource, Committer:
  postgres.NewIngestionStore (SkipRelationshipBackfill), DeadLetters:
  CollectorGenerationDeadLetterStore, PollInterval: 1s, AfterBatchDrained:
  deferred-maintenance barrier, AfterEmptyBatchDrained escape}`.
- Projector: `projector.Service{queue "ingester" (5m), reducer intent
  writer, runtime.CanonicalWriter}` with retry injector/policy from
  `ESHU_PROJECTOR_*` env (`config.go:28`).
- Webhook handoff: `ESHU_WEBHOOK_TRIGGER_HANDOFF_ENABLED` selects queued
  GitHub/GitLab/Bitbucket triggers before scheduled polling through the same
  sync/snapshot path (`doc.go:30-33`); trigger state lives in the
  core-owned webhook trigger store (peer `#8876` lane owns that census).
- Hosted surface: shared `/healthz`, `/readyz`, `/metrics`, `/admin/status`
  plus `/admin/replay`, `/admin/refinalize`,
  `/admin/replay-collector-generations` (`doc.go:12-15`). Queue gauges never
  query Postgres on scrape: a background refresher serves snapshots
  (`main.go:102-109`, `#7064`).
- Telemetry: OTEL bootstrap/providers/instruments (`main.go:44-62`),
  orphan-reaper for git-helper zombies (`main.go:71`), opt-in pprof bound to
  loopback (`main.go:74`, `doc.go:43-45`), GOMEMLIMIT recording
  (`main.go:64`).

## 7. Artifacts and deployment surface

- Binary `eshu-ingester` (`--version` via `buildinfo.PrintVersionFlag`).
- Chart `deploy/helm/eshu/Chart.yaml`: `0.0.3-pre-release-26`,
  appVersion `v0.0.3-pre-release-18`.
- `go.mod` pins go `1.26.10`; worktree host toolchain observed as `go1.26.6`
  — record the mismatch here; do not treat the older host toolchain as the
  declared version.
- Destination `eshu-hq/eshu-ingester` at `01f5d808` (placeholder README only,
  no cutover authority). No image tag or digest is recorded here: the ingester
  container image pin is unresolved (values.yaml ingester image ref not read).
- QA overlay configuration source: the IaC repo's `argocd/eshu/overlays/` QA
  overlay over `../../base`; the ingester env block sets Neo4j profiling
  groups, `PARSE_WORKERS 4`, multiprocess spawn, `COMMIT_WORKERS 2`,
  `GOMEMLIMIT`, and the workspace storage/CPU envelope. Postgres credentials
  flow from ExternalSecret `eshu-postgresql-auth` (rendered DSN keys); the
  exact ingester reader-DSN consumption on the QA overlay is unresolved.

## 8. Migration stage and prerequisites

Stage: **PREPARATION ONLY**. No cutover has occurred; the worktree was clean
at `origin/main` (`dc65b4d597`) before this manifest.

- `#6708` (SDK publication), `#6709` (producer delegation), `#6710`
  (standalone template): CLOSED and reusable.
- `#4047` (cutover readiness gate): CLOSED. The live per-collector cutover
  gate successor for this leaf is unidentified — do not treat the closed gate
  as cutover authority.
- `kubectl` is missing on the authoring host (install permitted by `#8879`
  scope, not yet performed). Deployment target and credential wiring beyond
  the QA-overlay pointer above are NOT_CHECKED; no cluster access was
  attempted.
- Sibling PREP-ONLY pattern: `go/internal/collector/sbom/EXTRACTION_PREP.md`
  (`#8865`, gated by `#4047`) and the `webhook-listener` census lane
  (`#8876`, open PR). No peer claim on this leaf was found (dest repo holds a
  single initial commit; open-PR search shows no competing `#8879` branch).

## 9. Acceptance sweep vs `origin/main`

All 12 `#8879` criteria are OPEN (none met); this manifest advances the first
two and records the rest:

1. Boundary + contracts + durable-state ownership: boundary drawn in the
   approved plan and recorded here (`§2`, `§5`, `§6`). No command code moves
   in this change.
2. Doc-capability inventory + dormant activation: inventoried in `§4` with
   dormancy proof; hosted activation stays DISABLED until sandbox/runtime
   proof passes.
3. Owners/target/config source: implementation owner confirmed
   (`@linuxdynasty`); public-contract, release, deployment owners and the
   production target/config source stay unconfirmed in-tree.
4. Census: packages, kinds, consumers, durable state, protocols, artifacts,
   stage, prerequisites recorded here.
5. Behavior/contract parity on representative and hostile inputs: NOT_CHECKED.
6. Reducer/graph/API/MCP truth preservation: NOT_CHECKED; read-only or
   deployment-only unaffected surfaces unidentified.
7. Concurrency (retry, duplicates, ordering, cancellation, revoked authority,
   stale claims, partial failures): NOT_CHECKED.
8. Resource bounds + no-regression with health/readiness/freshness/metrics/
   traces/logs/status: NOT_CHECKED.
9. Credentials, provenance, vulnerability response, oldest/newest core and
   component version pairs: NOT_CHECKED.
10. Independent build/release/deploy on public contracts: NOT_CHECKED.
11. Rollback rehearsal with truth/availability/state compatibility, claim
   fencing, and safe replay: NOT_CHECKED.
12. Independent review, local proof, stable exact-head CI; in-tree code
   retained (correctly, pre-acceptance): review/CI pending for this manifest.
