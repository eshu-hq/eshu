# Package restructure research appendix

Supporting measurement for [package-restructure.md](package-restructure.md)
(epic #6053). Eight agents swept the repository on 2026-08-11: one on gate
mechanics and the blast surface a file move creates, seven on the individual
packages. Each reported a package inventory, the shared core that cannot
move, a proposed move order, the hazards it verified by reading the tree,
and a family table.

How to read this appendix:

- **Counts are a snapshot, not a contract.** The tree moved under two of the
  agents while they measured it (see the `go/cmd/eshu` and `go/internal/query`
  inventory notes). Re-run the census against a pinned commit before
  executing any move.
- **Family grades** are `clean` (measured zero cross-family coupling),
  `tangled` (a real dependency that must be resolved first), or
  `shared-core` (stays in the package root). The family table columns are
  `proposed destination <- filename prefix [file counts] grade`.
- Several fields end mid-sentence, each marked `[…truncated at source]`.
  The research tooling capped each field's length; nothing was trimmed when
  the reports were committed here.
- **What is editorial.** This intro, the section headings, and anything
  marked *Correction made when this appendix was committed* or *Completed when
  this appendix was committed* were written while moving the research out of
  the issue comments. Later editorial updates are marked *Status update made
  YYYY-MM-DD*. Every other word is the agents' own. The only other changes made
  during that migration were mechanical: the outer code fence came off,
  escaped newlines became real ones, and literal asterisks and angle brackets
  were escaped or backticked so they render.

## Gate mechanics and restructure blast surface

### 1. The existing 500-line filelength gate — dual implementation, and the nolint convention

There are **two independent implementations of the same 500-line cap**, which must both be treated as the template:

**A. The authoritative CI enforcement — a golangci-lint Go plugin.**
`tools/golangci-lint-filelength/filelength.go:14-138` is a `package main` plugin (`go build -buildmode=plugin`) exposing an `analysis.Analyzer` named `filelength`. It runs in `LoadModeSyntax`, streams each file with `bufio.Scanner` (`filelength.go:126-133`), and reports any non-test, non-generated, non-vendor, non-testdata file over `maxFileLines = 500` (`filelength.go:29,71-113`). It is wired into `go/.golangci.yml:53-54,77-82` as `linters.settings.custom.filelength`, pointing at the built `.so` (`../tools/golangci-lint-filelength/filelength.so`), and runs as part of the `go-lint` gate's `golangci-lint run` in CI (`test.yml` job `go-core`).

**B. A bash duplicate used for pre-commit/local/pre-pr.** `scripts/dev/precommit-go.sh:163-177` (`filecap`, changed-files variant) and `:264-` (`filecap-all`, whole-tree variant, explicitly documented at `:266-270` as mirroring the plugin's `skip()` exactly) reimplement the same 500-line count with `wc -l`, honoring the same exclusions. `filecap` is the `.pre-commit-config.yaml:110-115` hook (`go-file-cap`); `filecap-all` is what the local `ci-gates` runner invokes for the registry's `go-file-cap` gate (`specs/ci-gates.v1.yaml:107-124`).

**Nolint marker convention** (both implementations honor it, confirmed at `precommit-go.sh:164,169` and `:279` "honours an explicit `//nolint:filelength` marker"): a standard golangci-lint suppression comment placed **on the file's `package` declaration line**, followed by `//` and a justification stating the line count and why splitting is wrong for this file, e.g.:

```go
package crossrepo //nolint:filelength // 509 lines: cross-repo resolution logic. Consolidating ...
```

(the package clause of `go/internal/reducer/crossrepo/cross_repo_resolution.go`). There are **27 files** carrying this marker repo-wide (`rg -c "nolint:filelength" -g '*.go' go/` → 27 files, 27 markers). This is documented centrally at `go/.golangci.yml:568-572` for the largest instance (`go/internal/telemetry/instruments.go`).

A directory-size gate should follow the identical pattern: a Go-plugin analyzer (or a bash script mirroring it for local/pre-pr) plus a `//nolint:<gate-name>` (or equivalent) escape hatch requiring a stated justification — not a bare suppression.

### 2. CI gate registry + workflow path filters — the two-layer trap, quantified

**Registry:** `specs/ci-gates.v1.yaml` (2559 lines) is the single source of truth mapping changed paths → gates, validated by `scripts/verify-ci-gates-registry.sh` → `go/cmd/ci-gates validate` (Go tool in `go/cmd/ci-gates`, backed by `go/internal/cigates/*`). `go-file-cap` (`:107-124`) and `package-docs` (`:126-144`) both trigger broadly on `go/**` — no per-package filter — so a directory-size gate registered the same way is inherently immune to the two-layer trap.

**Two-layer trap mechanics, confirmed in code:** `.github/workflows/*.yml` files independently hand-duplicate `dorny/paths-filter` glob lists, and `go/internal/cigates/pathfilter.go:273-422` (`checkPathFilterCoverage`, run only under `--drift`) is the machine cross-check the repo already built for exactly this class of drift (doc comment at `:280-285`: "the registry's triggers and the workflows' hand-duplicated dorny filters are two independently-edited lists with no prior machine link"). Critically, it **only checks literal (non-glob) triggers** (`isLiteralTrigger`, `pathfilter.go:263-271`) — a glob trigger like `go/internal/reducer/**` is not cross-checked this way at all; it relies on the workflow author having typed the equivalent glob. `--drift` is not run by default `verify-ci-gates-registry.sh` (only the plain integrity check is); it must be invoked explicitly.

**Counts of trigger-path lines referencing the five packages** (verified twice, manually and by script — matched):

- `specs/ci-gates.v1.yaml`: **57 trigger-path lines** referencing `go/internal/{query,reducer,mcp,parser,collector}/`.
- Nine `.github/workflows/*.yml` files carry **38 path-filter lines** for the same five packages: `static-contract-gates.yml` (18), `golden-corpus-gate.yml` (5), `ifa-determinism-gate.yml` (5), `race-graph-writes.yml` (2), `replay-coverage-gate.yml` (2), `frontend.yml` (2), `payload-usage-manifest.yml` (2), `verify-replay-tier.yml` (1), `reducer-contention-gate.yml` (1).
- `test.yml` itself has **no path filters** — `go-fmt`, `go-lint`, `go-build`, `go-vet`, `go-file-cap`, `package-docs` all run unconditionally on every push/PR via `test.yml`'s `go-core`/`verify-contracts` jobs, so those specific gates are unaffected by any restructure.

**The concrete move-sensitive list** — literal (non-glob) single-file triggers naming an exact path, which a `git mv` silently stops matching unless every citation is updated by hand:

| File | Cited in registry (`specs/ci-gates.v1.yaml`) | Cited in workflow |
|---|---|---|
| `go/internal/collector/git_snapshot_entity_buckets.go` | :1928 | `static-contract-gates.yml:159` |
| `go/internal/reducer/materialized_edge_families.go` | :1957 | `static-contract-gates.yml:190` |
| `go/internal/reducer/shared_projection.go` | :1958 | `static-contract-gates.yml:191` |
| `go/internal/reducer/sqlrelationship/sql_relationship_materialization.go` | :1959, :1997, :2057 | `static-contract-gates.yml:192`, `ifa-determinism-gate.yml:68` |
| `go/internal/reducer/sqlrelationship/sql_relationship_embedded_query.go` | :1960, :1998, :2058 | `static-contract-gates.yml:193`, `ifa-determinism-gate.yml:69` |
| `go/internal/reducer/sqlrelationship/sql_relationship_metadata.go` | :1961 | `static-contract-gates.yml:194` |
| `go/internal/reducer/gcp_resource_materialization.go` | :1991 | `ifa-determinism-gate.yml:30` |
| `go/internal/reducer/gcp_resource_materialization_teeth.go` | :1992 | `ifa-determinism-gate.yml:31` |
| `go/internal/reducer/gcp_resource_materialization_teeth_off.go` | :1993 | `ifa-determinism-gate.yml:32` |
| `go/internal/reducer/intent.go` | :2106 | `static-contract-gates.yml:214` |

That's 10 distinct files, each needing 2–4 independent edits (dup'd across two gates in some cases) to survive a move — exactly the shape `checkPathFilterCoverage` was built to catch, but only if `--drift` is actually run.

### 3. Generated artifacts / manifests embedding go file paths

- **`docs/public/observability/telemetry-coverage.md`** (944 lines) is the biggest blast-surface item found: **473 rows** cite an exact `go/internal/{reducer,query,mcp,parser,collector}/<file>.go[:line]` path in its stage tables (e.g. `telemetry-coverage.md:44,48,49,52-58`). This is not silent, though: `scripts/lib/telemetry-coverage-row-check.sh:73-89` (`path_target_exists`) is check "(3b)" of the `telemetry-coverage` gate (registry id `telemetry-coverage`, `specs/ci-gates.v1.yaml:828-852`, triggers `go/internal/**` broadly) and hard-fails if a cited file/glob no longer exists on disk — explicitly built (comment `:30-34`) because a renamed file "leaves no diff signal" for the sibling new-stage check to catch. A restructure will make this gate loudly fail on every moved file until the doc's path column is rewritten in lockstep; it is a real chunk of migration work, not a hidden risk.
- **`go/internal/payloadusage/paths.go:99,111,118,126,134,143`** hardcodes `go/internal/{reducer,projector,query,storage/postgres,relationships,replay/offlinetier}` as fixed directory constants for the `payload-usage-manifest` gate, and resolves decode-seam files via a **non-recursive** `filepath.Glob(dir/"factschema_decode*.go")` (documented at `paths.go:25-32`, `:106-110`, `:119-125`). If reducer/query files matching that glob pattern get moved into a new subdirectory, the flat glob will silently stop finding them — this is a real, currently-undetected risk (no existence/coverage check parallel to the telemetry doc's `path_target_exists` was found for this glob).
    - *Correction made when this appendix was committed:* the `filepath.Glob` calls are in `go/internal/payloadusage/load.go` — `resolveDecodeFiles` at `:37-64` (reducer) and `resolveOptionalDecodeFiles` at `:95-113` (projector, query, loader, relationships, replay). The cited `paths.go` lines are the `Paths` field comments describing that behavior. The reducer glob does error on a zero match (`no decode seam files matched`), and the other five deliberately accept an empty match while their families migrate. The hazard the research names still stands for a PARTIAL move, which is silent on every surface.
- **`go/internal/capabilitycatalog/data/surface-inventory.generated.json`** (614 entries) is safe — it records route/tool `name`/`category`/`readiness` only, no file paths, and is derived by importing `go/internal/mcp` as a Go package (`go/cmd/capability-inventory/main.go:17` etc.) rather than scanning file paths, so it survives an internal file move as long as the package import path (`go/internal/mcp`) itself doesn't change.
- `.eshu-doc-state/` (staleness tracking referenced by `.claude/hooks/eshu-doc-staleness.sh`, `scripts/check-docs-stale.sh`) is gitignored/local-only, not a committed artifact — low risk.
- Two more doc-citation gates exist that cite paths under these trees and would need the same lockstep treatment: `query-doc-commit-refs` (`specs/ci-gates.v1.yaml:1075-1094`, triggers `go/internal/query/*.md`, `go/internal/queryplan/*.md`) and `doc-citations` (`:1096-1125`). *Completed when this appendix was committed, from the registry as it reads today:* `doc-citations` has eight triggers; the four that name content are `docs/public/languages/**`, `docs/public/reference/parity-closure-matrix.md`, `go/internal/**/*_test.go` and `tests/fixtures/**` (the other four are its own scripts, its baseline file, and the registry). None of them reach `docs/internal/**`. `measurement-citations` does trigger on `docs/internal/**` and runs against this file, but it only matches ledger claims shaped `N/M trials|runs` or a `Measurement:` line. So no gate checks the `file:line` citations in this appendix — re-verify them against a pinned commit before acting on one.

## go/internal/projector and go/internal/coordinator

**Inventory:** projector: 188 .go (94 non-test / 94 test approx, plus 4 evidence .md + doc.go/README.md/AGENTS.md); coordinator: 124 .go (62 non-test / 62 test approx, plus doc.go/README.md/AGENTS.md).

**Summary:** Both packages are flat (no existing subdirectories). projector (188 .go files, ~194 total incl. docs) clusters into roughly 25 families; the ~20 single-cloud-provider `*_materialization_intents` / `*_correlation_intents` families (aws, azure, gcp, ec2, s3, iam, kubernetes, observability, security, container, semantic, and ~14 singleton domains) are measured CLEAN internally — zero cross-family calls, each only reachable from the central scope_generation_intents.go dispatcher (43 measured call sites) plus shared-core helpers (canonical, runtime, `factschema_decode_*`, payload.go, reducer_intent_fact_index.go — all of which must stay in root, ~70 files combined). The decisive complication for projector is external, not internal: canonical.go and every family's Row/type definitions are exported and consumed by 182 files outside the package (go/internal/storage/cypher, storage/postgres, replay/offlinetier, ifa, and four cmd/ binaries), so even a 'clean' family move forces an import-path + qualifier update across that external surface — plus one hardcoded exact-file CI gate trigger on canonical.go (#5531) that will silently go stale if not updated in the same PR. coordinator (124 .go files) clusters into ~20 per-provider families, each mechanically split into a self-contained _scheduler.go half (a WorkPlanner type implementing a root-defined interface — genuinely extractable, Go's structural interfaces make cross-package satisfaction work) and a _service.go half that is a set of methods on the shared `Service` struct — which Go's language rules forbid moving out of the package without first redesigning Service into composed sub-types. coordinator's own external blast radius is small (3 consumer files) but concentrated: go/cmd/workflow-coordinator/main.go names every provider's WorkPlanner type in its wiring. Two shared-core helper files (owned_package_target_helpers.go, target_priority.go) are measurably shared by two otherwise-unrelated families (package_registry and vulnerability_intelligence) via functions literally named for both providers in the same file.

The first coordinator family now lives under `internal/coordinator/cicdrun`.
Its 207-line scheduler and 73-line test had no cross-family symbol use: every
unexported declaration was file-local, and production imports were limited to
`plannercontract`, facts, scope, and workflow contracts. Root keeps
`cicd_run_service.go`, the structural planner interface, scheduling position,
clock-derived plan key, and durable admission.

The Loki scheduler now follows that boundary under
`internal/coordinator/lokiplanner`. The child owns request validation, target
filtering, and deterministic workflow-row construction. Root keeps
`loki_service.go`, the structural interface, service scheduling order,
clock-derived plan key, tenant and egress filtering, durable admission, retries,
and telemetry.

**Shared core:** projector root MUST retain: canonical\* (18f, buildCanonicalMaterialization + CanonicalMaterialization struct — called from tfstate/package_registry/oci_registry/runtime), runtime_\* (15f, orchestrator), service.go/service_logging.go/service_superseded.go (Service type + lifecycle), stage_\*.go (9f, pipeline phases), factschema_decode_\*.go (9f, decode helpers reused across >=2 unrelated families — measured decodeAWSResource used by both iam and observability families), payload.go/entity_metadata.go (generic payload readers, used by 15+ files), reducer_intent_fact_index.go (consumed by every intents family), scope_generation_intents.go (the fan-out hub with 43 measured build-call sites to every family), and failure_classification.go/decisions.go/retry.go/work_errors.go/schema_version_admission.go (generic infra). CRITICAL ADDITIONAL FACT: canonical.go's exported Row types (EntityRow, FileRow  […truncated at source]

**Move order:** projector (safest-first): (1) oci_registry_\* and tfstate_\* and container_image_identity and semantic_entity — single-owner leaf families with no internal cross-family calls, though each still requires updating their known external consumers (oci_registry: go/internal/storage/cypher/oci_registry_canonical_writer\*.go; tfstate: go/internal/storage/cypher/tfstate_canonical_writer\*.go, go/internal/storage/postgres/drift_\*.go, go/internal/replay/offlinetier/tfstate_\*.go, go/cmd/{ingester,bootstrap-index,projector}/terraform_state_ownership\*.go, go/internal/relationships/tfstatebackend/canonicalwriter/\*). (2) single-cloud-provider intents families (aws, azure, gcp, ec2, s3, iam, kubernetes, observa  […truncated at source]

**Hazards:** (1) CI PATH PIN, EXACT FILE (projector): `.github/workflows/static-contract-gates.yml:160` and `specs/ci-gates.v1.yaml:1929` (job 'content-entity-bucket-sync', gate id content-entity-bucket-sync, #5531) both hardcode the exact path `go/internal/projector/canonical.go` as a trigger. If canonical.go is split or moved into a `canonical/` subdirectory, this exact-path trigger goes stale silently and the gate stops firing on canonical-logic changes — matches the repo's own documented 'gate selection has two layers' false-green class. MUST update both files in the same PR that touches canonical.go's location. (2) CI PATH GLOBS, projector (safe as-is): `.github/workflows/verify-replay-tier.yml:27`, `.github/workflows/payload-usage-manifest.yml:14`, `.github/workflows/race-graph-writes.yml:11,25` (also runs `go test ... ./internal/projector/...` literally at line 58/920), `.github/workflows/golden-corpus-gate.yml:17`, and `specs/ci-gates.v1.yaml:464,914,1696,2442` all use `go/internal/projector/**`, which still matches subdirectories after a move — low risk, but the literal `./internal/projector/...` go-test invocation in race-graph-writes.yml:58 and ci-gates.v1.yaml:920 continues to work only because Go's `...` wildcard also covers subpackages; confirm this after the first family moves. (3) EXTERNAL GO IMPORT BLAST RADIUS (projector, the largest hazard): 182 files outside go/internal/projector import it and reference exported symbols by package-qualified name (`projector.EntityRow`,  […truncated at source]

**Families:**

```
  KEEP IN ROOT (shared-core, not extractable) <- projector: canonical* (canonical.go, canonical_builder.go, c [7 non-test / 11 test = 18] shared-core
  KEEP IN ROOT (shared-core orchestrator) <- projector: runtime_* (runtime.go, runtime_logging.go, runtim [6 non-test / 9 test = 15] shared-core
  service.go/service_logging.go/service_superseded.go stay in root; service_catalog_correlation_intents -> servicecatalog subpackage <- projector: service* (service.go, service_logging.go, service [4 non-test / 5 test = 9 (6 core + 2 cata] tangled
  KEEP IN ROOT (pipeline stage functions called directly from runtime.go) <- projector: stage_* (stage_entities.go, stage_facts.go, stage [5 non-test / 4 test = 9] shared-core
  KEEP IN ROOT (decode helpers) <- projector: factschema_decode_* (aws, codedataflow, codegraph [8 non-test / 1 test = 9] shared-core
  KEEP IN ROOT <- projector: payload.go + entity_metadata.go (generic fact-pay [2 non-test / 0 test] shared-core
  KEEP IN ROOT <- projector: reducer_intent_fact_index* (reducer_intent_fact_i [1 non-test / 2 test = 3] shared-core
  KEEP IN ROOT (hub) <- projector: scope_generation_intents* (fan-out dispatcher) [1 non-test / 3 test = 4] shared-core
  aws <- projector: aws_* (aws_cloud_image_materialization_intents, a [4 non-test / 2 test = 6] clean
  ec2 <- projector: ec2_* (block_device_kms_posture, instance_identit [5 non-test / 4 test = 9] clean
  s3 <- projector: s3_* (external_principal_grant, internet_exposure [3 non-test / 3 test = 6] clean
  iam <- projector: iam_* (can_assume, instance_profile_role — *_mate [2 non-test / 2 test = 4] clean
  azure <- projector: azure_* (relationship, resource — *_materializati [2 non-test / 1 test = 3] clean
  gcp <- projector: gcp_* (relationship, resource — *_materialization [2 non-test / 0 test = 2] clean
  kubernetes <- projector: kubernetes_* (correlation_intents, namespace_mate [3 non-test / 2 test = 5] clean
  packageregistry <- projector: package_registry_* (canonical.go/_artifact.go/_ev [7 non-test / 5 test = 12] tangled
  ociregistry <- projector: oci_registry_* (canonical.go + cassette/input_inv [1 non-test / 4 test = 5] clean
  tfstate <- projector: tfstate_* (canonical.go, canonical_types.go, owne [3 non-test / 5 test = 8] clean
  containeridentity <- projector: container_image_identity_intents (+ cicd/dockerfi [1 non-test / 4 test = 5] clean
  semanticentity <- projector: semantic_entity_intents (+ elixir/go/tsx test var [1 non-test / 4 test = 5] clean
  observability <- projector: observability_coverage_* (correlation_intents, ma [2 non-test / 2 test = 4] clean
  security <- projector: security_* (alert_reconciliation_intents, group_r [2 non-test / 2 test = 4] clean
  one subpackage per intent family (cicdrun, cloudinventory, codeflow x3, crossplane, deadletter, searchdocsweeper, incident, multicloud, rds, sbom, secrets, supplychain, workload) OR keep the 1-2 file singletons in root to avoid 15+ one-file packages <- projector: singleton *_materialization_intents / *_correlati [~20 non-test / ~16 test = ~36] clean
  KEEP IN ROOT <- projector: failure_classification.go + decisions.go + retry. [5 non-test / 3 test = 8] shared-core
  KEEP IN ROOT <- coordinator: service.go + config.go + metrics.go + governanc [5 non-test / 3 test = 8] shared-core
  KEEP IN ROOT <- coordinator: owned_package_target_helpers.go + derived_targe [3 non-test / 3 test = 6] shared-core
  one subpackage per provider (grafana, jira, loki, tempo, vaultlive, prometheusmimir, ociregistry, sbomattestation, scannerworker, securityalert, cicdrun, packageregistry, pagerduty, gcp, awsscheduled, componentextension, vulnerabilityintelligence), implementing the root-defined Planner interface (Go's structural interface satisfaction keeps this compiling across the package boundary) <- coordinator: per-provider _scheduler.go halves (self-contain [~17 non-test / ~17 test = ~34] clean
  MUST STAY IN ROOT unless Service is redesigned into composed per-provider sub-structs (separate design decision, not a file move) <- coordinator: per-provider _service.go halves — methods on th [~30 non-test / ~35 test = ~65] tangled
  KEEP IN ROOT <- coordinator: workflow_tenant_grants* + installed_advisory_ta [7 non-test / 5 test = 12] shared-core
```

## go/internal/parser

**Inventory:** 259 .go files at go/internal/parser root (47 non-test / 212 test), out of 993 total .go files in the go/internal/parser tree. The other 734 files already live in 34 extracted, self-contained subpackages (c, cfg, cloudformation, cpp, csharp, dart, dataflowemit, dbtsql, dockerfile, elixir, golang, goldenaudit, gomod, gradle, groovy, haskell, hcl, interproc, java, javascript, json, kotlin, maven, nodelockfile, perl, php, python, pythondep, ruby, rust, scala, shared, sql, summary, swift, taint, valueflow, yaml — each already `package <name>`, confirmed via package declarations).

**Summary:** go/internal/parser's 259 flat root .go files (47 non-test / 212 test) are architecturally unlike a typical "prefix families waiting to be moved" package. 34 language/domain subpackages (golang, python, javascript, java, php, ruby, rust, scala, kotlin, csharp, cpp, c, dart, elixir, swift, hcl, json, yaml, sql, groovy, haskell, perl, dockerfile, gradle, gomod, maven, nodelockfile, cloudformation, dbtsql, pythondep, shared, cfg, interproc, taint, valueflow, dataflowemit, summary, goldenaudit) already exist under it holding 734 files of real per-language parsing logic, each its own `package <name>` — that extraction already happened. What remains flat in root is, by measurement: (a) the shared dispatcher core (Engine, Registry, Runtime, Options) that ~50+ files elsewhere in the repo import directly and that root doc.go/README.md explicitly document as the intentional "parent dispatcher" languages must not import back; (b) 27 `<lang>_language.go` glue files, each 1-4 thin `func (e *Engine) parseX/preScanX` methods (verified 100% via grep) that are called from nowhere except engine.go's two `switch definition.Language` blocks and their own file — zero cross-language coupling was found, but every one is bound to Engine by Go method-receiver rules, so none can be relocated without first adopting the already-defined-but-unwired LanguageProvider interface (registry.go/engine.go already branch on `Definition.Provider != nil`, but none of the 27 built-ins populate it — a real, larger refactor, not a file move); and (c) roughly 200 same-package (package parser) black-box `engine_<lang>_*_test.go`/`<lang>_*_test.go` files that integration-test the dispatcher per language and depend on unexported root test helpers. Cleanest first move (if pursued) is prefix-normalizing stragglers (js_cfg_dataflow_test.go, cargo_dependency_test.go, jenkins_groovy_golden_fixture_test.go, fastify_threading_bench_test.go) and converting per-language tests into external _test packages inside the existing subpackage directories after exporting the shared test helpers — never moving engine.go/registry\*.go/runtime\*.go/the _language.go glue. Two production spec ledgers (specs/language-feature-parity-ledger.v1.yaml, specs/parser-backing-ledger.v1.yaml, enforced by scripts/verify-parser-relationship-kit.sh) and .golangci.yml per-file lint exclusions hardcode literal paths to dozens of these root files and must be updated in the same change as any move.

**Shared core:** engine.go (defines `type Engine struct`, `NewEngine`, `Engine.ParsePath`/`PreScanPaths` — the exported entry points ~40 other Eshu packages import directly), registry.go + registry_definitions.go (the `Registry`/`Definition` catalog the task asked to "note" — it is metadata-only: `Definition.Provider LanguageProvider` exists as a real, wired-but-unused extension seam (engine.go:280-284,387-388 branch on `definition.Provider != nil`) yet none of the 27 built-in `defaultDefinitions()` entries populate `Provider`; all 27 built-ins still dispatch through concrete `*Engine` methods via two big `switch definition.Language` blocks in engine.go, not through the interface), runtime.go/runtime_dependencies.go (worker-pool `Runtime` shared by every language's parser pool), options.go, language_provider.go (the unused-by-defaults `LanguageProvider` interface), scip_parser.go/scip_support.go (cross-l  […truncated at source]

**Move order:** This package differs fundamentally from a typical flat-file split: measurement shows essentially nothing at the root is a "just move it" family, because production logic (the `<lang>_language.go` glue) is method-bound to `*Engine` and the huge `engine_<lang>_*_test.go` corpus is a same-package (`package parser`, not `parser_test`) black-box suite that depends on unexported root helpers (`writeTestFile`, `repoFixturePath`, `ensureParentDirectory`, `osWriteFile` in testhelpers_test.go/engine_test.go). Recommended order if this package is ever split, safest first: (1) rename-only prefix normalization with zero code change — fold the non-conforming stragglers (js_cfg_dataflow_test.go, js_parent_  […truncated at source]

**Hazards:** CONFIRMED, cite-able hazards for any restructure of go/internal/parser root files:

1. specs/language-feature-parity-ledger.v1.yaml (385 lines) — the single biggest hazard. It hardcodes exact source_files:/test_files: arrays for ~30 language-feature entries, and the large majority of those literal paths are root parser files being discussed here (e.g. line 107-108 listed go/internal/parser/go_language.go, go/internal/parser/engine_test.go, go/internal/parser/go_embedded_sql_test.go, the last of which #6062 moved to go/internal/parser/golang/go_embedded_sql_test.go; line 130 listed go/internal/parser/php_language_test.go, which #6062 moved to go/internal/parser/php/php_language_test.go; similar entries for every other language). Moving/renaming ANY of these paths without updating this spec breaks it.

2. specs/parser-backing-ledger.v1.yaml (128 lines) — a second ledger with the same literal-path pattern (e.g. line 39 go/internal/parser/dockerfile_language.go, line 62 go/internal/parser/hcl_language.go, line 68-69 hcl_terraform_test.go/hcl_terragrunt_test.go).

3. scripts/verify-parser-relationship-kit.sh (+ scripts/lib/parser_relationship_language_ledger.sh, confirmed at scripts/verify-parser-relationship-kit.sh:1-60) cross-checks those two ledgers against the real diff (git diff --name-only "$base"...HEAD) — it is diff-scoped, so a bulk rename touching many of these paths in one PR will trigger many ledger-mismatch findings at once unless the ledgers are edited in the same commit. Test fixtures for this script already exist at scripts/lib/test-verify-parser-relationship-kit-parser-backing-ledger\*.yaml and already reference p  […truncated at source]

**Families:**

```
  (stays in package root) <- engine.go / registry*.go / runtime*.go / options.go / langua [~47 non-test / ~19 test (~66 total)] shared-core
  golang (already exists as extracted logic package; these are the remaining root-level Go-specific files) <- go_ / engine_go_ [3 non-test (go_language.go glue, go_pack] tangled
  javascript (subpackage already exists) <- javascript_ / engine_javascript_ / js_ / fastify_ [1 non-test (javascript_language.go) / ~3] tangled
  python (already exists) <- python_ / engine_python_ [2 non-test (python_language.go, python_d] tangled
  kotlin (already exists) <- kotlin_ / engine_kotlin_ [1 non-test (kotlin_language.go) / 16 tes] tangled
  php (already exists) <- php_ / php_language_* [1 non-test (php_language.go) / 17 test] tangled
  java (already exists) <- java_ / engine_java_ [2 non-test (java_language.go, java_metad] tangled
  swift (already exists) <- swift_ / engine_swift_ [1 non-test (swift_language.go) / 6 test] tangled
  yaml (already exists) <- yaml_ / engine_yaml_ [1 non-test (yaml_language.go) / 7 test] tangled
  hcl (already exists) <- hcl_ [1 non-test (hcl_language.go) / 4 test] tangled
  csharp (already exists) <- csharp_ [1 non-test / 4 test] tangled
  cpp (already exists) <- cpp_ [1 non-test / 4 test] tangled
  ruby (already exists) <- ruby_ [1 non-test / 4 test] tangled
  elixir (already exists) <- elixir_ [1 non-test / 4 test] tangled
  rust (already exists) <- rust_ / cargo_dependency_test.go [1 non-test / 4 test] tangled
  scala (already exists) <- scala_ [1 non-test / 3 test] tangled
  sql (already exists) <- sql_ [1 non-test / 3 test] tangled
  json (already exists) <- json_ / dbt_sql_lineage.go [2 non-test (json_language.go, dbt_sql_li] tangled
  perl + haskell (two existing subpackages; the single glue file must split) <- perl_haskell_language.go / engine_perl_ / engine_haskell_ [1 non-test (one file implementing BOTH p] tangled
  groovy (already exists) <- groovy_ / jenkins_groovy_golden_fixture_test.go [1 non-test / 2 test] tangled
  c (already exists) <- c_ [1 non-test / 1 test] tangled
  nugetproject (no existing subpackage; free functions, not Engine methods) <- nuget_project_ [1 non-test / 2 test] tangled
  dart, dockerfile, gradle, gomod, nodelockfile, maven (all already exist) <- dart_ / dockerfile_ / gradle_ / gomod_ / node_lockfile_ / ma [1 non-test each / 0 root test (tested vi] tangled
```

## go/internal/collector

**Inventory:** 250 .go files at go/internal/collector root (247 package collector, 3 package collector_test) / 250 total, no separate test-only count at top level — see per-family files column for non-test/test split. (3,544 .go files exist under go/internal/collector including all 42 already-organized subpackages; this inventory covers only the 250 flat root files.)

**Summary:** The 250 flat root files split into ~22 filename-prefix families, but the two biggest structural facts are: (1) three families that look git-repo-specific — service.go, claimed_service\*, and git_source_types.go — are actually the package's shared, externally-consumed core (claimed_service\* alone backs ~15 other non-git collector kinds via `collector.ClaimedService`), and must stay in the root `collector` package; (2) the three largest git-specific families (git_snapshot 64 files, git_selection 39 files, git_source) form a real, measured production-code (non-test) 3-way import cycle, so they cannot each become an independent subpackage without a genuine dependency-inversion refactor first. The evidence-based recommendation is a single new `gitrepo` subpackage absorbing all git-repo-specific families as one unit now (safe, mirrors kuberneteslive/cicdrun/terraformstate as a sibling), with git_documentation, git_observability, git_submodule, git_workflow, git_tfstate, git_service(catalog), git_codeowners, and scheduled_sync_config marked as genuinely clean leaves that can be pulled into their own nested subpackages inside `gitrepo` without waiting for that refactor — five of those leaf families (codeowners/submodule/tfstate/servicecatalog/discovery) additionally require disambiguated names because they are thin wiring adapters over already-existing sibling packages of the same conceptual name, confirmed via their own import statements. Concrete hazards found: a CI gate and a capability-matrix fixture_ref both hardcode exact filenames inside families this restructure would move (git_snapshot_entity_buckets.go, git_documentation_facts_test.go), and roughly a dozen exported Load\*/selector symbols are called by qualified name from three external cmd/ binaries and need either an import-path update or a root alias.

**Shared core:** Three things must stay in the root `collector` package and cannot be cleanly extracted without breaking external callers or creating import cycles:

1. **service.go family** (service.go, service_observation.go) — defines `Service`, `Source`, `Committer` interfaces. This is the entry point: cmd/collector-git/service.go, cmd/ingester/wiring.go, and cmd/bootstrap-index/wiring.go all construct `collector.Service{...}` directly. Production code in service.go itself makes ~0 real cross-family calls (verified: the one apparent match, `RecordGenerationDeadLetter`, is a call through a field, not a package-family symbol) — it is a clean orchestrator, but it is the seam everything else plugs into, so it stays put.

2. **claimed_service\* family** (20 files) — defines `ClaimedService`, `ClaimedCommitter`, `ClaimedSource`, `StreamErrorClaimedCommitter`. Verified via `rg -o 'collector\.[A-Z]\w*'` acros  […truncated at source]

**Move order:** Given the measured production-code (non-test) cycle graph, a single flat "each family becomes its own top-level subpackage" pass is not safe — git_snapshot, git_selection, and git_source form a real 3-way production import cycle (git_snapshot->git_source 7 files, git_source->git_snapshot 2 files, git_snapshot->git_selection 4 files, git_selection->git_snapshot 5 files, git_source->git_selection 3 files, git_selection->git_source 4 files), and git_refs<->git_selection, webhook<->git_selection, webhook<->priority, claimed<->fair are smaller cycles layered on top.

Recommended two-phase move:

Phase 1 (safe now, one new umbrella package `go/internal/collector/gitrepo`): move every git-repo-spec  […truncated at source]

**Hazards:**

1. `.github/workflows/static-contract-gates.yml:159` and `specs/ci-gates.v1.yaml:1928` (generated from the workflow file, keep in lockstep) hardcode `go/internal/collector/git_snapshot_entity_buckets.go` as the exact trigger path for the `bucketsync` gate. Moving that one file (part of git_snapshot's entity sub-cluster) into `gitrepo/snapshot/entity_buckets.go` silently stops the gate from firing on changes to it — update the path filter in the same PR that moves the file.

2. `specs/surface-inventory.v1.yaml:47` hardcodes `go/internal/collector/git_documentation_facts_test.go` as a `fixture_refs` entry for the "documentation" capability-matrix row (owned by internal/collector/confluence). Moving that file breaks the fixture reference for a capability-matrix row; must be updated in lockstep per the "Claim Evidence Lives In Known Locations" rule in CLAUDE.md.

3. Three collector-kind path filters in static-contract-gates.yml and ci-gates.v1.yaml use `go/internal/collector/**` (recursive glob) for the `edge`, `evidence`, and golden-corpus-gate triggers — these are safe under internal reorganization since `**` covers new subdirectories.

4. Naming collisions confirmed by import inspection, not guesswork: git_codeowners_facts\*.go imports `collector/codeowners` (already exists), git_submodule_facts\*.go imports `collector/submodule` (already exists), git_tfstate_backend_warnings.go imports `collector/terraformstate` (already exists), git_service_catalog_facts\*.go imports `collector  […truncated at source]

**Families:**

```
  stays in root (collector) <- service (service*.go) [2/5] shared-core
  stays in root (collector) <- claimed (claimed_service*.go, claimed_multi_source_host*.go) [10/10] shared-core
  stays in root (collector) <- git_source (git_source_types.go defines GitSource/Repository [5/5] shared-core
  stays in root (collector) <- git_fact (git_fact_builder*.go: buildStreamingGeneration, st [2/4] shared-core
  stays in root (collector) or gitfailure <- failure (failure.go, failure_retryable.go) [2/1] clean
  gitrepo/snapshot (nested under a new gitrepo umbrella package) <- git_snapshot (scip/native/dataflow/parse/entity/delta/taint/ [24/40] tangled
  gitrepo/selection <- git_selection (native/copy/discovery/sharding/ignore/pinned_ [21/18] tangled
  gitrepo/gitdocs <- git_documentation (archive/diagram/docx/pptx/xlsx/notebook/o [22/19] clean
  gitrepo/gitobs <- git_observability (facts/log_routes/metrics/trace_routes) [1/5] clean
  gitrepo/gitsubmodule (NOT 'submodule' — collides with existing collector/submodule/ parser pkg it imports) <- git_submodule (facts, pinned_sha) — wraps sibling collector/ [2/3] clean
  gitrepo/workflowimage <- git_workflow (image evidence/facts) [1/2] clean
  gitrepo/gittfstate (NOT 'tfstate' — collides with existing collector/terraformstate/ pkg it imports) <- git_tfstate (backend_warnings) + root tfstate_candidate.go ( [2/2] clean
  gitrepo/gitsvccatalog (NOT 'servicecatalog' — collides with existing collector/servicecatalog/ pkg it imports) <- git_service (service_catalog_facts, golden) — wraps sibling  [1/2] clean
  gitrepo/gitcodeowners (NOT 'codeowners' — collides with existing collector/codeowners/ pkg it imports) <- git_codeowners (facts) — wraps sibling collector/codeowners  [1/2] clean
  gitrepo/gitrefs <- git_refs (selection helpers) [1/2] tangled
  gitrepo/gittracked <- git_tracked (files, ignored discovery) [1/3] tangled
  gitrepo/gitdiscovery (NOT 'discovery' — collides with existing collector/discovery/ pkg it imports) OR stays in root if the alias/re-export cost of moving an externally-called Load* function isn't worth it <- discovery_advisory.go / discovery_env.go — wraps sibling col [2/3] tangled
  gitrepo/scheduledsync <- scheduled_sync_config (LoadScheduledSyncConfig — also called [1/1] clean
  gitrepo/priority <- priority_selector (PriorityRepositorySelector — also called  [1/1] tangled
  gitrepo/webhook <- webhook_trigger_selector / webhook_trigger_handoff_config (W [2/1] tangled
  gitrepo/generation or stays root (claimed<->generation is a shared-core-adjacent bidirectional edge) <- generation_dead_letter [1/1] tangled
  gitrepo/fairdispatch <- fair_claim_dispatcher [1/1] tangled
  mostly stays in root (governance/whole-package tests) — parser_fact_export.go, git_followup_facts.go, git_content_fact_envelopes.go could move into gitrepo since they build git-specific fact envelopes <- singletons: parser_fact_export.go, git_followup_facts.go(+it [6/4 (+3 collector_test)] shared-core
```

## go/internal/query

**Inventory:** 1903 .go files at HEAD 311bdc563 (877 non-test / 1026 test) — re-verified 2x with plain `ls *.go | wc -l` at the end of the session, both times consistent. Note: an early-session count of the same directory returned 1883/871/1012; git status showed the working tree clean throughout (no uncommitted changes to go/internal/query), so the discrepancy is most likely a corrupted/stale first read rather than a real file-count change (this repo's own memory notes document rg/ls output corruption on wide scans). Re-run the census against a pinned commit in an isolated worktree before finalizing any design doc that cites exact counts.

**Summary:** go/internal/query is Eshu's entire HTTP query surface: one root `query` package with ~60 independently-mountable domain Handler types (APIRouter struct, handler.go:148-208) fanning out to roughly 25-30 real prefix families ranging from 183 files (supply chain) down to single-digit clusters. The architecture is already well-suited to this split -- each domain owns its own `<Domain>Handler` struct with its own `Mount(mux)` method, called from one central dispatcher -- so most families ARE extractable. Measured (not guessed) evidence found: package_registry, content_reader, and the code (dead-code/relationships/call-graph/imports) cluster are clean with near-zero cross-family symbol leakage; supply_chain is huge but Handler-isolated and clean at the top level; impact (deployment blast-radius, 95 files -- a different domain from supply_chain's own "impact" vulnerability sub-cluster despite the name collision) is genuinely tangled, with repository, service, and deployment_trace families calling its unexported helper functions directly. True shared core is narrow and identifiable: handler.go's APIRouter/Mount/Write\* helpers, ports.go's GraphQuery/ContentStore interfaces, contract.go's envelope types, the unexported capabilityMatrix registration map that all 40 `contract_<domain>.go` files write into via init(), and openapi.go's spec-assembly point that string-concatenates a constant from every one of the 101 openapi_paths_\*.go files. Two large all-test families (graph_read_error, 17 files; auth_scoped_routes, 41 files) are cross-cutting integration sweeps that construct many different domain Handlers in one file and cannot be scoped to any single subpackage. The most consequential hazards are non-recursive `find`/`rg --max-depth 1`/`go test ./internal/query` invocations in five to six verify-\*.sh scripts and two CI gate definitions -- several of which will produce a false-green PASS (not a failure) the moment a matched file or test moves into a subdirectory, and one (verify-openapi.sh) explicitly documents a prior incident (#5934) with the identical failure shape. Separately, 284 files outside the package import `query.<Type>` directly, 86 of them MCP tool-dispatch files, which sets the real scope of any Handler-type relocation. A minor caveat: this session's file-count snapshots drifted between an early read (1883/871/1012) and later reads (1903/877/1026) on a directory git reported as clean throughout -- most likely a stale/corrupted first read rather than a real change, but worth re-verifying against a pinned commit before the design doc is finalized.

**Shared core:** Confirmed (not guessed) shared-core surface that must stay in the package root regardless of how families are split:

1. handler.go -- APIRouter struct (handler.go:148-208, ~60 pointer fields, one per domain Handler) + Mount(mux) dispatcher (handler.go:211-478) that calls every domain's own `<Handler>.Mount(mux)` method + the WriteJSON/WriteError/WriteSuccess/WriteErrorEnvelope/WriteContractError/ReadJSON/QueryParam/QueryParamInt/PathParam response-envelope helpers (handler.go:15-125), used package-wide.
2. ports.go -- GraphQuery and ContentStore interfaces (ports.go:12-95) plus shared row-shape structs (RepositoryContentCoverage, RepositoryLanguageCount, etc., ports.go:96-151). Every domain Handler struct embeds `Neo4j GraphQuery` and/or `Content ContentStore` as fields (confirmed on SupplyChainHandler, ImpactHandler, CodeHandler, RepositoryHandler, EntityHandler).
3. contract.go --  […truncated at source]

**Move order:** Phase 0 (prerequisite, before any file moves): decide the Handler-type re-export strategy. 284 files outside go/internal/query (confirmed via `rg -l 'eshu-hq/eshu/go/internal/query"' --glob '*.go' -g '!internal/query/**'`, including 86 in internal/mcp/dispatch_\*.go alone) reference `query.<Type>` directly -- e.g. `query.SupplyChainHandler`, `query.ImpactHandler`. Moving an exported Handler type into a subpackage either needs a root-level type alias (`type SupplyChainHandler = supplychain.SupplyChainHandler`) so those 284 files keep compiling unchanged, or a coordinated rename across all of them in the same change. Decide this before Phase 1.

Phase 1 (clean, move first): package_registry  […truncated at source]

**Hazards:** All confirmed by direct grep/read this session, not assumed:

1. specs/ci-gates.v1.yaml:2166 and scripts/verify-replay-coverage-gate.sh:51 both hardcode `go test ./internal/query -run 'Test(AuthorizationReplayCoverageContract|SecretsIAMPostureSummaryScopedGrant|AuthMiddlewareWithScopedTokensAllowsSecretsIAMRoutes)'` -- a single, non-recursive package path. The three named tests live in authz_replay_coverage_contract_test.go and secrets_iam_authz_test.go (confirmed via rg -l on the func names). If those files move into a subpackage, `go test ./internal/query -run <pattern>` silently matches ZERO tests and exits 0 ("no tests to run"), a false green, not a failure.
2. The same false-green class recurs in scripts/verify-hosted-governance-proof.sh:58,60,96,98; scripts/verify-ask-eshu-local-proof.sh:192,229; scripts/verify-hosted-governance-remote-compose-proof.sh:61,92; scripts/verify-query-plan-profile.sh:53; scripts/verify-query-plan-regression.sh:9 -- all use `go test ./internal/query -run '<pattern>'` non-recursively. Every one needs auditing for which subpackage its matched tests will land in, before any move.
3. scripts/verify-route-coverage.sh:112 -- `find "$query_dir" "$api_dir" -maxdepth 1 -name '*.go' ! -name '*_test.go'`. A hard maxdepth-1 flat scan. Any handler .go file moved into a subdirectory silently drops out of route-coverage checking -- the gate will keep reporting full coverage while no longer seeing the moved file's routes at all.
4. scripts/verify-  […truncated at source]

**Families:**

```
  supplychain <- supply_chain_* (owned by SupplyChainHandler) [~183 total (~91 non-test / ~86 test) acr] clean
  impact <- impact_* (owned by ImpactHandler, deployment/blast-radius im [95 total (38 non-test / 57 test)] tangled
  code <- code_* (owned by CodeHandler: dead-code, call graph, relatio [~172 total (~68 non-test / ~104 test) ac] clean
  contentread <- content_reader_* (concrete ContentStore implementation) [42 total (26 non-test / 16 test)] clean
  packagereg <- package_registry_* [32 total (13 non-test / 19 test)] clean
  repository <- repository_* (owned by RepositoryHandler) [~67 total (~27 non-test / ~40 test) acro] tangled
  entity <- entity_* (owned by EntityHandler) [32 total (~12 non-test / ~20 test) acros] tangled
  authidentity (or one subdir per handler: localidentity, browsersession, adminidentity, adminprovider, signinpolicy) <- local_identity_/browser_session_/admin_identity_/admin_provi [~50 total measured (local_identity 18, b] shared-core
  semantic <- semantic_search_* / semantic_* (SemanticSearchHandler + Sema [~47 total (semantic_search 31 [13/18], o] tangled
  langquery <- language_query_* (LanguageQueryHandler) [29 total (5 non-test / 24 test)] tangled
  service <- service_story_/service_query_/service_catalog_ (ServiceCatal [~43 total (service_story 28 [12/16], ser] tangled
  incident <- incident_context_* (IncidentHandler) [~47 total (incident_context 28 [19/9], o] tangled
  investigation <- investigation_packet_/investigation_workflow_ [26 total (investigation_packet 19 [12/7]] tangled
  visualization <- visualization_packet_ (VisualizationHandler) [~18 total (visualization_packet 11 [7/4]] tangled
  playbook <- query_playbook_ (QueryPlaybookHandler) [~15 total (query_playbook 11 [7/4], plus] tangled
  iac <- iac_management_/iac_resources_ (IaCHandler) [~32 total (iac_management 11 [6/5], iac_] tangled
  cloud <- cloud_inventory_/cloud_resource_/cloud_runtime_ (CloudInvent [~48 total (cloud_inventory 15 [4/11], cl] tangled
  workitem <- work_item_ (WorkItemHandler) [~21 total (work_item 13 [8/5], plus ~8 g] tangled
  cicd <- ci_cd_ (CICDHandler) [~14-20 total (ci_cd 14 [6/8], plus gener] tangled
  n/a -- special-cased, see shared_core <- openapi_paths_* (per-domain OpenAPI path-fragment constants  [101 total, all non-test] shared-core
  n/a -- stays in root or moves to a dedicated integration-test package that imports every subpackage <- graph_read_error_* and auth_scoped_routes_* (cross-cutting a [graph_read_error 17 (0/17) + auth_scoped] shared-core
  n/a -- must stay in root unless capabilityMatrix gets an exported registration API <- contract_* (per-domain capabilityMatrix registration glue, o [43 total (40 non-test / 3 test)] shared-core
```

## go/cmd/eshu

**Inventory:** 233 .go files total as of this read (121 src / 112 test) — NOTE: was 230 at task assignment; grew by 3 mid-session (evidence_bundle_live.go, evidence_bundle_live_cmd_test.go, evidence_bundle_api_parity_test.go) via a legitimately merged commit (4345b3c32, PR #6025) that landed on origin/main and fast-forwarded this checkout between my first and second `ls`. git status is clean; this is not an uncommitted/concurrent-edit hazard, but it proves the package is a moving target — re-run this inventory immediately before executing any move, do not treat this snapshot as frozen.

**Summary:** go/cmd/eshu is package main (233 .go files as of this read, 121 src/112 test — grew from the 230 stated at task start via a legitimate mid-session merge, see file_count), so the literal "subdirectory per family" pattern used elsewhere in the repo does not apply here: all files in this directory must stay one package, one binary. The real restructure lever is extracting business logic into new `go/internal/cli/<family>` packages and leaving thin cobra RunE wrappers behind in the flat directory — which matches AGENTS.md's own stated anti-pattern rule (business logic doesn't belong in RunE) and the fact that internal/eshulocal already exists as a lower-layer primitive that cmd/eshu's local_host/local_graph files build on top of.

Prefix-family clustering surfaces roughly 20 command families ranging from 2 files (evidence_packet) to 26 files (vuln_scan), plus one large 31-file cluster (local_host + local_graph + local_content_search + local_iac_reachability) that is a genuine, measured bidirectional import cycle — local_host.go aliases and calls into local_graph.go's managedLocalGraph/graphAddress/graphEnvOverrides, while local_graph_bootstrap.go and local_graph_process.go take a localHostRuntimeConfig parameter and call mergeEnvironment, both defined in local_host\*.go. That cycle, plus this cluster's status as the dependency every other subcommand family reaches into (graph.go, service.go, scan.go, admin, vuln_scan all call into it), makes it unmoveable as separate packages without either merging it into one internal/cli/localsupervisor unit or doing a real dependency-inversion redesign — it is not a mechanical file move.

Everything else measured cleanly resolves to a directed acyclic graph, confirmed by symbol-level cross-referencing (not guessed): mcp_setup sits as a second, narrower shared-core layer with zero outbound family dependencies but five inbound consumers (assistant_guidance, hosted_setup, hosted_onboard, first_run, service.go registration); hosted_setup depends on mcp_setup+scan; hosted_onboard depends on hosted_setup+first_run+mcp_setup; demo depends on first_run; competitive_parity_cmd is a 3-way capstone over first_run+operator_digest+evidence_packet. vuln_scan, component, docs, investigation, evidence_bundle, evidence_packet, assistant_hook_preflight, graph_install, freshness, operator_digest, admin, and change_impact are all clean leaves or near-leaves with at most one trivial outbound dependency.

**CORRECTION (#6059, during execution).** At least one entry in that sentence is wrong, and the method behind it can under-report, so treat every `clean` grade here as unverified until re-probed.

graph_install is the proven case: it needs `printJSON` and, from the local_host/local_graph supervisor cluster, `localGraphReadVersion`. #6059 extracted it anyway by injecting the latter as a `VersionReader` parameter — see `go/internal/cli/graphinstall`.

Two ways a compile probe of a family in isolation reports a clean seam that is not one. The Go compiler stops after 10 errors, so a family needing more than ten symbols yields a list truncated at ten that looks complete. And if the build fails for any reason that is not an undefined symbol — a `//go:embed` asset left behind when only `.go` files were copied, which is what happened to graph_install — a probe filtering for `undefined:` matches nothing and reads as self-contained. Copy the embed assets, pass `-gcflags=-e`, and treat any non-`undefined:` build error as an inconclusive probe rather than a clean result.

Re-measured counts for the other families were posted to #6059 during execution. They are not reproduced here: no probe script or output is committed to back them, and unverified numbers in this file would be read as measured by the next extraction. Re-probe the family you are about to move. Six committed artifacts hardcode paths or test-run targets against this package's current flat layout (two CI gate files, one lint exception file, one backend-conformance spec, one evidence-continuity spec, one verify script) and must be updated in lockstep with any extraction — none of them are covered by a broad enough glob to survive a move into go/internal/cli/\*\* unmodified.

**Shared core:** Two distinct shared-core layers, both measured by grepping for cross-family symbol references (not guessed):

1. Classic CLI-wiring shared core (root.go, service.go, output.go, client.go, contract.go, config.go/config_cmd.go/config_validate.go, repository_selector.go, workspace.go, main.go, doc.go): apiClientFromCmd (basic.go:368) is called from 25 files, commandExitError (contract.go:8) from 22, resolveConfigValue from 6, eshuExec from 6, printJSON from 9. These files define the cobra rootCmd, APIClient, exit-code contract, and env-config store that every family leans on. They cannot move — they ARE the package-main entry surface (AGENTS.md: root.go:31-32 SilenceUsage/SilenceErrors, root.go:35 os.Setenv(ESHU_RUNTIME_DB_TYPE) mutating the process env for every child exec, service.go's syscall.Exec launch paths).

**CORRECTION (#6059, during execution).** "Cannot move" is true of these files, not of everything in them. `repository_selector.go` was listed here as shared core and is still in the flat directory, but only its flag half had to stay: the matching rules and the repository listing behind them moved to `go/internal/cli/reposelector`, leaving the `--repo` / `--repo-id` reads behind. Read a shared-core entry as "this file stays", not as "nothing in this file is extractable" — the test is whether a symbol touches cobra, the process environment, or the exit-code contract, and that is a per-symbol question.

2. The local-authoritative supervisor cluster (local_host\*.go, local_graph\*  […truncated at source]

**Move order:** Package main constraint first: go/cmd/eshu/\*.go can never be split into subdirectories and stay part of the eshu binary (Go requires one package per directory and cmd/eshu is package main). The only real move is extracting business logic into new `internal/cli/<name>` packages, leaving thin cobra RunE wrappers behind in the flat directory — which matches AGENTS.md's own stated anti-pattern rule (business logic doesn't belong in RunE) and the fact that internal/eshulocal already exists as a lower-layer primitive that cmd/eshu's local_host/local_graph files build on top of.

Order below is for that internal/cli extraction, cleanest-and-most-depended-upon first, since later families import earlie  […truncated at source]

**Hazards:** All confirmed by direct rg against the live tree (not the corrupted first pass — a combined `rg -rln -g '*.yaml' -g '*.yml'` mangled "cmd/eshu" into "n"/"ln" fragments in its output, matching this repo's known 'batched rg corrupted output' failure mode; re-ran each target file individually and got clean, verified results).

1. specs/ci-gates.v1.yaml:1269-1271 — three EXACT file-path gate triggers: "go/cmd/eshu/mcp_setup_doc_lockstep_test.go", "go/cmd/eshu/mcp_setup_snippet.go", "go/cmd/eshu/mcp_setup.go". If mcp_setup logic moves into internal/cli/mcpsetup, either these files disappear (the gate silently stops triggering — a false-green per the repo's own "Gate selection false-green has two layers" pattern) or the moved files must be re-added at their new path AND the gate's triggers list must add the new internal/cli/mcpsetup/\*\* path.
2. specs/ci-gates.v1.yaml:1848 — broad "go/cmd/eshu/\*\*" trigger. This still matches anything that stays under go/cmd/eshu/, but will NOT match anything moved to go/internal/cli/\*\*, so every internal/cli package created by this restructure needs its own new trigger entry (or an added parent glob) in this same gate registry file, or changes to extracted logic go untested by this gate.
3. specs/backend-conformance.v1.yaml:98-101 — `go_test: ./cmd/eshu -run TestLocalAuthoritative{Startup,CallChainSynthetic,TransitiveCallersSynthetic,DeadCodeSynthetic}Envelope`. These are exactly the perf/conformance tests living in local_authoritative_\*_test.go, wh  […truncated at source]

**Families:**

```
  N/A — must stay one unit; if pulled out, one package e.g. localsupervisor <- local_host + local_graph + local_content_search + local_iac_ [31 (15 host / 9 graph / 2 content-search] tangled
  internal/cli/vulnscan <- vuln_scan [26 (10 src / 16 test)] clean
  internal/cli/component <- component (incl. component.go) [17 (9 src / 8 test)] clean
  internal/cli/docs <- docs (incl. docs.go) [12 (7 src / 5 test)] clean
  internal/cli/firstrun <- first_run [15 (10 src / 5 test)] clean
  internal/cli/mcpsetup <- mcp_setup [11 (6 src / 5 test)] shared-core
  internal/cli/hostedsetup <- hosted_setup [4 (3 src / 1 test)] clean
  internal/cli/hostedonboard <- hosted_onboard [6 (4 src / 2 test — hosted_onboard_rules] clean
  internal/cli/demo <- demo (incl. demo.go) [10 (5 src / 5 test)] clean
  internal/cli/assistguide <- assistant_guidance [5 (3 src / 2 test)] clean
  internal/cli/hookpreflight <- assistant_hook_preflight [3 (1 src / 2 test)] clean
  internal/cli/graphinstall <- graph_install [7 (3 src / 4 test)] clean -- CORRECTED: not clean. Needs printJSON (wrapper-only) and localGraphReadVersion from the local_host/local_graph supervisor cluster. Extracted anyway in #6059 by injecting a VersionReader parameter; see the note under the summary above.
  internal/cli/freshness <- freshness (incl. freshness.go) [7 (4 src / 3 test)] clean
  internal/cli/opdigest <- operator_digest [4 (2 src / 2 test)] clean
  internal/cli/evidbundle <- evidence_bundle [5 (2 src / 3 test — grew from 2 to 5 fil] clean
  internal/cli/evidpacket <- evidence_packet [2 (1 src / 1 test)] clean
  internal/cli/investigation <- investigation_cmd [4 (2 src / 2 test)] clean
  internal/cli/admin <- admin (incl. admin.go) [5 (2 src / 3 test)] clean
  internal/cli/parity <- competitive_parity_cmd [2 (1 src / 1 test)] tangled
  internal/cli/changeimpact <- change_impact + change_plan [3 (2 src / 1 test)] clean
  internal/cli/report <- report_cmd [3 (1 src / 2 test)] clean
  internal/cli/servicereport <- service_report_cmd [2 (1 src / 1 test) — unrelated to report] clean
  N/A — rides with local cluster <- nornicdb_*_test.go [3 (test-only, 0 src)] tangled
  stays in go/cmd/eshu (root wiring, not extractable — package main entry surface) <- root.go / service.go / output.go / client.go / contract.go / [~40 (cobra root wiring, APIClient, print] shared-core
  internal/cli/<verb> per family, case-by-case (not individually measured this pass) <- diagnostics + diagnostics_classify, ecosystem, analyze, map, [~35 residual small (1-4 file) single-ver] tangled
```

## go/internal/mcp

**Inventory:** 338 total (.go) / 208 are _test.go; roughly 130 production files.

**Summary:** go/internal/mcp (338 .go files, 208 of them tests) splits cleanly into two layers with very different extraction difficulty. The tool-REGISTRATION layer (43 `<domain>Tools() []ToolDefinition` constructors, one per family, all called exactly once from `ReadOnlyTools()` in types.go) is genuinely clean: measured via symbol-count sampling, there are zero lateral family-to-family calls among the `Tools()` constructors. Most `tools_<domain>.go` + `dispatch_<domain>.go` + their tests can become real subpackages with only their Route func's single call site in a shared file to fix.

The DISPATCH/routing layer is where the tangle actually lives, and it is worse than the flat 338-file count suggests. Three concrete, measured problems: (1) `dispatch_repositories.go` (46 case arms) is a hidden second-tier router that fans out to 13 other families' Route funcs despite its filename claiming to be about repositories -- only ~10 of its cases are genuinely repository-scoped; (2) `dispatch_status.go` similarly smuggles in the work_item and fact_schema_version families' routing; (3) 26 of the ~130 production files (the whole "codeintel" cluster -- codebase, code_flow, code_topic, code_quality, dead_code, cross_repo_dead_code, structural_inventory, import_dependencies, call_graph_metrics, reachability, route_to_caller, graph_summary_packet, contract_impact, prechange_impact -- plus incident_context, component_extensions, collector_extraction_readiness, runtime, repository_language, content, context) have NO `dispatch_<domain>.go` file at all: their routing is inline inside root dispatch.go's 490-line switch statement, confirmed by pairing every tools_\*.go against its expected dispatch_\*.go sibling. Those families' registration halves are movable today; their routing halves are not, until someone extracts a per-family Route func out of the shared switch.

Root/shared-core is large -- roughly 70-90 of the 338 files (server/transport/dispatch spine, the two hidden hub files, the read_surface_\*/route_serves_data_registry\*/kind_\* consumer-existence and fact-kind gates, the summaries formatting layer, and ~35-40 package-wide contract/parity/authz test files with no owning production file) -- and none of it should move. The single largest hazard for this specific package is not code, it is specs/: roughly 80 hardcoded `go test ./internal/mcp[/-run '...']` references across evidence-continuity.v1.yaml, surface-inventory.v1.yaml, capability-matrix.v1.yaml, product-claims.v1.yaml, and the per-domain capability-matrix/\*.yaml files, several of them `-run`-pinned to exact test names. Every one of those either needs `/...` added (recursive test discovery, low severity) or its exact path/test-name updated (moved test, high severity: a `-run` regex with zero matches passes silently rather than failing).

**Shared core:** Must stay in package root (go/internal/mcp), confirmed by direct measurement, not by guess:

1. **Server/transport/dispatch spine** (~21 files): server.go, server_sse.go, transport_auth.go, transport_auth_metrics.go, types.go (the tool registry entry point -- `ReadOnlyTools()` fan-in-calls all 43 `<domain>Tools()` constructors, one call each, verified zero lateral family-to-family calls via `rg -o '\w+Tools\(\)'` symbol-count sampling), doc.go, dispatch.go (490 lines; `resolveRoute` is the master router -- calls ~18 family Route funcs directly plus a large inline switch for the "codeintel" cluster), dispatch_args.go, dispatch_envelope.go, dispatch_values.go, dispatch_timeout(.go/_test.go), dispatch_test.go, test_helpers_test.go, run_readonly(.go/_test.go), plus their \*_test.go siblings.

2. **Two hidden secondary hub files** -- the most important measured finding: `dispatch_repositories.

**Move order:** Wave 1 (safest, zero measured cross-family calls, Route func already isolated and called directly from root dispatch.go's top-level fan-out list): documentation, cloud (inventory+runtime_drift), semantic (evidence+search), relationships/relationship_edges, ecosystem, freshness, query_playbooks, investigation (packets+workflows), visualization, ask, service (catalog+intelligence+selector+story, as ONE package -- selector is shared inside the cluster).

Wave 2 (clean at the tools_\*.go/dispatch_\*.go symbol level, but their Route func's only caller is the dispatch_repositories.go hub -- move the family, then fix the one call site in the hub to call `pkg.Route(...)`): package_registry, cicd, cont  […truncated at source]

**Hazards:** All citations verified via rg against the live tree at HEAD (main, ddabec9fb).

**CI/gate commands that will silently stop covering (or false-pass) subpackage tests** -- these all invoke `go test ./internal/mcp` WITHOUT the `/...` suffix, so once family tests move into `go/internal/mcp/<family>/*_test.go`, Go will not run them and these gates will look green while testing nothing for the moved family:

- .github/workflows/mcp-schema-drift.yml:137 -- `go test ./internal/mcp/ -run '^TestReadOnlyTools$' -count=1` (fine only if TestReadOnlyTools, currently in tools_test.go, stays root)
- .github/workflows/mcp-schema-drift.yml:199 -- `go test ./internal/mcp/ -count=1 -timeout 120s` (this is the main package test-run step; MUST become `./internal/mcp/...`)
- scripts/security_intelligence_release_gate.sh:277 -- `./internal/mcp` in the `pkgs` array passed to `go test "${pkgs[@]}"`
- scripts/verify-hosted-governance-proof.sh:66,104 -- `go test ./internal/mcp -run "${mcp_pattern}"` where `mcp_pattern` (line 45) pins 7 specific test names spanning the status/governance, component_extensions, collector_status, ingester_status, and hosted_readiness families simultaneously -- a `-run` regex against a package with zero matching tests exits 0 with "no tests to run" (silent false pass), not a failure
- scripts/test-verify-hosted-governance-proof.sh:20 -- self-test greps the literal string `"go test ./internal/mcp"` from a captured log; changing the command to add `/...` breaks this fixed-strin  […truncated at source]

**Families:**

```
  docs <- dispatch_documentation*, tools_documentation* [4 prod / 13 test] clean
  supplychain <- dispatch_supply_chain*, tools_supply_chain* [4 prod / 10 test] tangled
  relationships <- dispatch_relationships*, dispatch_relationship_edges*, tools [3 prod / 6 test] clean
  semantic <- dispatch_semantic_evidence*, dispatch_semantic_search*, tool [4 prod / 4 test] clean
  security <- dispatch_security_alert_aggregates*, tools_security*, tools_ [3 prod / 5 test (+security_alert_reconci] tangled
  pkgregistry <- dispatch_package_registry*, tools_package_registry* [4 prod / 4 test] tangled
  investigation <- dispatch_investigation_packets*, dispatch_investigation_work [4 prod / 4 test (+investigation_workflow] clean
  infra <- dispatch_infra_search*, dispatch_infra_resource_aggregates*, [4 prod / 4 test] tangled
  cloud <- dispatch_cloud_inventory*, dispatch_cloud_runtime_drift*, to [4 prod / 4 test (+aws_runtime_drift_test] clean
  cicd <- dispatch_cicd*, tools_cicd* [4 prod / 4 test] tangled
  repository (only the ~10 truly repo-specific cases) -- dispatch_repositories.go itself must NOT move as-is <- dispatch_repositories.go, dispatch_repository_files*, tools_ [4 prod / 5 test] tangled
  iac <- dispatch_iac*, tools_iac* [2 prod / 3 test (+terraform_config_state] clean
  containerimage <- dispatch_container_image_aggregates*, tools_container_image_ [2 prod / 3 test] tangled
  admission <- dispatch_admission_decisions*, tools_admission_decisions* [2 prod / 3 test] tangled
  sbom <- dispatch_sbom_attachment_aggregates*, tools_sbom_attachment_ [2 prod / 2 test] tangled
  playbooks <- dispatch_query_playbooks*, tools_query_playbooks*, query_pla [2 prod / 4 test (+demo_playbook_parity_t] clean
  observability <- dispatch_observability_coverage*, tools_observability_covera [2 prod / 2 test] tangled
  kubernetes <- dispatch_kubernetes*, tools_kubernetes* [2 prod / 2 test (+noop_content_store_k8s] tangled
  incident <- dispatch_incident_context*, tools_incident_context* [1 prod / 3 test] tangled
  freshness <- dispatch_freshness*, tools_freshness*, freshness_parity*.go [2 prod / 2 test (+2 freshness_parity orp] clean
  codeowners <- dispatch_codeowners*, tools_codeowners* [2 prod / 1 test] tangled
  secretsiam <- dispatch_secrets_iam*, tools_secrets_iam* [2 prod / 1 test] tangled
  ecosystem <- dispatch_ecosystem*, tools_ecosystem* [2 prod / 1 test] clean
  ask <- dispatch_ask*, tools_ask* [2 prod / 1 test (+cookbook_contract_test] clean
  visualization <- dispatch_visualization*, tools_visualization* [2 prod / 1 test (+visualization_packet_s] clean
  workitem <- tools_work_item*, dead_letters_test.go [1 prod / 3 test] tangled
  service <- dispatch_service_catalog*, dispatch_service_selector*, dispa [5 prod / ~10 test] tangled
  codeintel <- tools_codebase*, tools_code_flow*, tools_code_topic*, tools_ [~17 prod / ~17 test] tangled
  misc / split further per name <- tools_component_extensions*, tools_collector_extraction_read [~7 prod / ~10 test] tangled
```

*Status update made 2026-08-27:* The inventory above preserves the filenames
measured on 2026-08-11. The 23-definition registration family now lives under
`internal/mcp/ecosystem/`; `tools.go` owns canonical assembly while sibling
files own the split definitions. Root `dispatch_ecosystem.go` remains in
`internal/mcp`.

## go/internal/reducer

**Inventory:** 1269 .go files total (536 non-test / 733 test), spanning ~420 distinct 3-token filename-prefix clusters.

**Summary:** go/internal/reducer has 1269 .go files (536 non-test / 733 test) that cluster into roughly 420 filename-prefix families, a handful of which (supply_chain_impact at 125 files, code_call_materialization at 94, container_image_identity at 71) dominate the package. I measured cross-family symbol coupling for the ~30 largest families by extracting each family's exported func/type/const names and grep-matching them against the rest of the package (excluding comment-only hits and a blocklist of the convergent Handler-interface methods every family implements independently: Handle/Retryable/FailureClass/Error/Unwrap/DomainDefinition, which were the dominant source of false-positive "coupling" in raw output). After filtering, the great majority of the top families — container_image_identity, ci_cd_run, security_alert_reconciliation, iam_can_perform, eshu_search_document, secrets_iam_trust, secrets_iam_graph, sbom_attestation_attachment, aws_cloud_runtime, terraform_config_state, semantic_entity_materialization, cloud_inventory_admission, observability_coverage_\*, the per-cloud-resource domains, and the six independent code-intelligence domains (taint evidence, root verdicts, function summary, reachability projection, interproc evidence, value flow) — show zero real coupling to any other domain family beyond the shared root hub (Domain constant, DomainDefinition, Intent, Handler interface, and for two of them the shared_projection worker harness). This means the great majority of families are legitimately clean extraction candidates. I found three concrete exceptions that need explicit handling before any move: (1) code_call_materialization genuinely depends on an unexported context type and resolver registry owned by code_call_language — mechanical but requires exporting an API first; (2) supply_chain_impact and supply_chain_suppression have real bidirectional type coupling (SupplyChainImpactFinding used by suppression; SupplyChainSuppressionState/Decision used by impact) — a true circular-dependency risk that must be resolved by merging them into one subpackage or hoisting the shared types to root; (3) three files literally named service_materialization_{docs,vulnerabilities,incidents}.go actually implement methods on ServiceCatalogCorrelationHandler and have no independent domain of their own — proof that filename-prefix clustering alone is not safe and every cluster needs the same measure-first treatment before it's moved. Separately, and more consequentially for a future repo-level extraction than anything found inside the package: nearly 400 files across roughly 15 other internal packages (internal/projector, internal/storage/postgres, internal/storage/cypher, internal/ifa, internal/replay/\*, internal/query, internal/workflow, cmd/reducer, cmd/ingester, and others) import github.com/eshu-hq/eshu/go/internal/reducer directly, and while most of that is limited to the shared Domain constant, several packages (internal/storage/postgres in particular) depend on family-specific exported types by name (reducer.SecretsIAMTrustChainLoadStats, reducer.ValueFlowProgramInput, reducer.ContainerImageIdentityTransaction, and likely more) — any family extraction plan has to grep the whole module, not just this package, before moving that family's types out of the reducer root.

**Shared core:** The package is a hub-and-spoke design, and the hub is unambiguous and small relative to the package: (1) registry.go (389 lines) — Registry, DomainDefinition, OwnershipShape, Handler/HandlerFunc types; every family's own `<domain>DomainDefinition()` builder returns this struct. (2) registry_additive_domains.go (404 lines) + defaults_registry.go + defaults_handlers.go + defaults_domain_catalog.go + defaults.go + defaults_test.go + defaults_additive_domains\*.go (11 files: azure, cloud_nodes, cloud_posture, cloud_relationships, correlation, crossplane, gcp, incident_code, secrets_drift, supply_chain) — the wiring layer that calls every single family's `<domain>DomainDefinition()` function; this is the highest-fan-in code in the package (confirmed: it references DomainContainerImageIdentity, DomainCICDRunCorrelation, and dozens more by name). (3) domain.go (125 lines) — the knownDomains map enum  […truncated at source]

*Correction made when this appendix was committed:* the invariant below — every
subpackage imports root, root imports nothing family-specific — does not hold as
written, and a reviewer caught it. Root's own wiring names family symbols:
`defaults_additive_domains_correlation.go:66-67` calls
`containerImageIdentityDomainDefinition()` and builds `ContainerImageIdentityHandler{}`,
while the family needs root's `DomainDefinition`, `Domain`, `Intent` and `Handler`
(`container_image_identity.go:52,73`). Moving the family makes root import the child
and the child import root, which Go rejects. See the acyclic-boundary prerequisite in
the design doc; the families below stay measured-clean, but they are not movable until
that boundary exists.

**Move order:** 1) Do nothing to the shared-core file group (domain.go, intent.go, registry.go, registry_additive_domains.go, defaults\*.go including defaults_additive_domains\*.go, runtime.go, service.go, reducer_fact_batch_insert\*.go, materialized_edge_families.go, doc.go/README.md/AGENTS.md) and the shared_projection\*.go harness (26 files) — these stay in the reducer package root permanently; every subpackage will import root, root imports nothing family-specific. (cross_scope_dependencies.go was originally listed here too; it moved to go/internal/reducer/crossscope/dependencies.go instead, #6061 — see the row below.) 2) Extract the verified-clean, zero-inbound-coupling families first, in ascending risk order: iam_can_perform, secrets_iam_trust, secrets_iam_graph, sbom_attestation_attachment, aws_cloud_runtime, terraform_config_sta  […truncated at source]

> **Correction, landed with #6061.** `reducer_fact_batch_insert*.go` did not stay in the root — it moved into `internal/reducer/factwrite`. See the correction note at the end of this file.

**Hazards:** CI path filters with hardcoded per-file (not glob) triggers — these go dark silently if the named file moves without updating both the generator source and the regenerated workflow: specs/ci-gates.v1.yaml:1957-1961 and .github/workflows/static-contract-gates.yml:190-194 (materialized_edge_families.go, shared_projection.go, sql_relationship_materialization.go, sql_relationship_embedded_query.go, sql_relationship_metadata.go); specs/ci-gates.v1.yaml:2106 and static-contract-gates.yml:214 (intent.go); specs/ci-gates.v1.yaml:1991-1993,1997-1998 and .github/workflows/ifa-determinism-gate.yml:30-32,68-69 (gcp_resource_materialization.go, gcp_resource_materialization_teeth.go, gcp_resource_materialization_teeth_off.go, sql_relationship_materialization.go, sql_relationship_embedded_query.go). specs/ci-gates.v1.yaml is the generator source for .github/workflows/\*.yml (per this repo's generator-script-discipline convention); a file move must be reflected in both or the gate is regenerated stale. Broader glob triggers (go/internal/reducer/\*\*) in reducer-contention-gate.yml:27, race-graph-writes.yml:10,24, verify-replay-tier.yml:24, golden-corpus-gate.yml:18, payload-usage-manifest.yml:12, static-contract-gates.yml:189, ifa-determinism-gate.yml, specs/ci-gates.v1.yaml:463,749,913,936,1697,2384,2439 will keep matching regardless of subdirectory depth and need no change. Hardcoded script paths (live defaults, not just comments): scripts/verify-edge-source-tool-coverage.sh:38 defaults ESHU_  […truncated at source]

**Families:**

```
  supplychain (impact sub-scope) <- supply_chain_impact [63/62 (125 total)] tangled
  supplychain (suppression sub-scope, same subpackage as impact) <- supply_chain_suppression [4/11 (15 total)] tangled
  codecall (materialization sub-scope) <- code_call_materialization [25/69 (94 total)] tangled
  codecall (language-resolver sub-scope) <- code_call_language [18/12 (30 total)] tangled
  containerimage <- container_image_identity (+ container_image_* satellites) [18/53 (71) + 10 satellite files = 81 tot] clean
  cicdrun <- ci_cd_run [9/19 (28 total)] clean
  codecall (projection sub-scope) or codecallproj <- code_call_projection [7/12 (19 total)] clean
  secalert <- security_alert_reconciliation [10/8 (18 total)] clean
  repodepproj <- repo_dependency_projection [7/11 (18 total)] clean
  iamcan <- iam_can_perform [8/10 (18 total)] clean
  searchdoc <- eshu_search_document [7/10 (17 total)] clean
  secretsiam (trust sub-scope) <- secrets_iam_trust [9/6 (15 total)] clean
  secretsiam (graph sub-scope, or its own iamgraph) <- secrets_iam_graph [3/5 (8 total)] clean
  sbomattest <- sbom_attestation_attachment [6/7 (13 total)] clean
  svccatalog <- service_catalog_correlation [6/6 (12) PLUS the mislabeled service_mat] tangled
  fold into svccatalog; no independent ServiceMaterializationHandler/Domain exists <- service_materialization [8/? — 3 of the 8 files (docs, vulnerabil] shared-core
  awscloudruntime <- aws_cloud_runtime [5/7 (12 total)] clean
  tfconfigstate <- terraform_config_state [4/7 (11 total)] clean
  stays in root (reducer package) — this IS the shared core <- defaults_additive_domains (+ registry.go, registry_additive_ [11/0 additive-domains files, ~2000+ comb] shared-core
  stays in root <- domain.go / intent.go [2 files] shared-core; cross_scope_dependencies.go moved to go/internal/reducer/crossscope/dependencies.go (#6061), it did not stay in root
  stays in root, or a tiny leaf subpackage (e.g. projshared) both projection subpackages import <- shared_projection (worker/runner/partition/batch/unroutable) [11/15 (26 total)] shared-core
  semanticentity <- semantic_entity_materialization [2/8 (10 total)] clean
  crossrepo <- cross_repo_resolution + cross_repo_evidence + cross_repo_int [2/8 + 2/5 + ~2 (roughly 19 total under c] clean
  codeimportrepo <- code_import_repo [5/5 (10 total)] clean
  stays in root — generic Postgres batch-insert helper (reducer_fact_batch_insert.go, _versioned.go) used package-wide, not domain-specific <- reducer_fact_batch (batch insert helpers) [2/6 (8 total)] shared-core
  obscoverage <- observability_coverage_correlation / observability_coverage_ [4/4 + 4/2 (14 total)] clean
  cloudinventory <- cloud_inventory_admission [3/5 (8 total)] clean
  one subpackage each (codetaint, coderootverdicts, codefuncsummary, codereachability, codeinterproc, codevalueflow) OR bundle as siblings under a codeintel/ parent if the design doc wants fewer top-level dirs <- code_taint_evidence / code_root_verdicts / code_function_sum [7 + 6 + 5 + 5 + 6 + 3 = 32 files across ] clean
  secgroup <- security_group_reachability / security_group_cidr / security [~14 total (3-4 files each)] clean
  gcpresource <- gcp_resource_materialization (+ _teeth.go / _teeth_off.go) [3/3 (6 total)] clean
  one subpackage per cloud resource family (awsrelationship, azurerelationship, gcprelationship, ec2identity, s3exposure, rdsposture, k8scorrelation, ...) or a shared cloudposture/ parent <- aws_relationship_materialization / azure_relationship_materi [~60 files across ~15 small per-cloud-res] clean
  not assessed individually — apply the same measure-before-move discipline per cluster <- long tail (~350+ prefix clusters of 1-4 files each) [~600 files not itemized above] tangled
```

**Correction, landed with #6061.** The "stays in the reducer package root
permanently" (Move order) and "not domain-specific" (Families,
`reducer_fact_batch` row) characterization of `reducer_fact_batch_insert.go`
and `reducer_fact_batch_insert_versioned.go` does not hold: both files moved
into `go/internal/reducer/factwrite` (#6061 PR4), leaving type aliases and
forwarders behind in `reducer_fact_write_compat.go`.
