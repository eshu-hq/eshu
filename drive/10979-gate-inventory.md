# 10979 gate inventory: keep / convert-to-test / delete

Refs #10979. Part of #9315. Worktree `drive/10979` off `origin/main` (ac1389af4c).
Method: every id in `specs/ci-gates.v1.yaml` + `specs/ci-gates.d/*.yaml` + `hygiene_hooks` enumerated 2026-11-19.
Rule (epic): a gate stays iff it judges changed code by running or analyzing it.
Static = compares the tree to committed lists/counts/docs/specs without running the subject code.
`selftest=Y` means the registry row carries a `test_command` (seeded RED/GREEN proof the check catches failures).
`last catch` is the motivating regression/issue or the seeded self-test; NOT-PROVEN marks CI-history lookups in F4.

## Count reconciliation (170 ids, zero unknown)

- 160 gates in `specs/ci-gates.d/gates-01..17.yaml` (all classified below).
- 10 `hygiene_hooks` in `specs/ci-gates.v1.yaml` (all dynamic pre-commit mirrors; all keep; final section).
- 2 `required_status_checks` contexts (`go-core-complete`, `go-race-complete`) are aggregate umbrellas, not gates.
- 15 `merge_group_slim_gates` name existing gates only (no new ids); the `merge-group-slim` job is live on PRs
  (`test.yml:247`, PR-plan gated) and `rg merge_group .github/workflows` is empty: slim rows stay.

## Summary: dynamic=99 static=61 | keep=149 convert-to-test=6 delete-or-wire-candidates=5

No in-tree deletions or conversions are implemented in this turn: every non-keep row needs either
consumer proof (epic acceptance) or arbiter decision 3 (contract-class presumption), recorded as F1/F2.
Conversions additionally touch the CI selector (`go/internal/cigates`), workflow jobs, and generated
`docs/public/reference/ci-gates.md`, so each ships as its own closing-PR item with focused proof.

## gates-01.yaml (17 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| go-fmt | dynamic | pre-commit | B | test.yml / go-core | N | precommit-go.sh | keep | runs Go toolchain (build/vet/lint/audit) on code |
| go-lint | dynamic | pre-commit | B | test.yml / go-core | Y | precommit-go.sh | keep | runs Go toolchain (build/vet/lint/audit) on code |
| go-build | dynamic | pre-push | B | test.yml / go-core | N | (go test/toolchain) | keep | runs Go toolchain (build/vet/lint/audit) on code |
| go-vet | dynamic | pre-push | B | test.yml / go-core | N | (go test/toolchain) | keep | runs Go toolchain (build/vet/lint/audit) on code |
| go-file-cap | static-shape | pre-commit | B | test.yml / go-core | Y | precommit-go.sh | keep | Epic stays (500-line cap); seeded self-test; precommit-go touched 2026-11-17 (69b2ec65b7) |
| go-dir-gate | static-shape | pre-commit | B | test.yml / dirgate | Y | precommit-go.sh | keep | Growth-trailer rule, cap family; Go-test self-test (cmd/dirgate) |
| markdown-file-cap | static-shape | pre-commit | B | test.yml / markdown-file-cap | Y | verify-markdown-line-cap.sh | keep | Epic stays; seeded self-test; last verifier touch 2026-09-26 (84b70f5640) |
| scripts-file-cap | static-shape | pre-commit | B | test.yml / scripts-file-cap | Y | verify-tooling-line-cap.sh | keep | Cap family; seeded self-test; #8221 grandfather rows; split 2026-10-16 |
| package-docs | static-shape | pre-pr | B | test.yml / verify-contracts | Y | verify-package-docs.sh | keep | Package-shape rule (doc.go/README/AGENTS.md); seeded self-test |
| migration-immutability | dynamic | pre-push | B | test.yml / verify-contracts | Y | verify-migration-immutability.sh | keep | runs script analyzer on changed code |
| gitignore-rg-parity | dynamic | pre-push | B | verify-agent-hygiene.yml / Agent hygiene gate | Y | verify-gitignore-rg-parity.sh | keep | runs script analyzer on changed code |
| slim-go-build | dynamic | pre-push | B | test.yml / merge-group-slim | N | (CI-only) | keep | Empty local by design; PRs run the identical command via go-build (ci_only) |
| slim-go-vet | dynamic | pre-push | B | test.yml / merge-group-slim | Y | verify-merge-group-slim-vet.sh | keep | runs script analyzer on changed code |
| slim-go-filecap | static-shape | pre-push | B | test.yml / merge-group-slim | N | precommit-go.sh | keep | Slim-lane parity of go-file-cap; merge-group-slim job live on PRs |
| slim-migration-uniqueness | dynamic | pre-push | B | test.yml / merge-group-slim | N | (go test/toolchain) | keep | runs Go tests |
| slim-collector-entrypoints | static-parity | pre-push | B | test.yml / merge-group-slim | Y | verify-collector-entrypoints-generated.s | keep | Same verifier as collector-entrypoints-generated (#2298); dedup is followup F3 |
| slim-doc-split | static-shape | pre-push | B | test.yml / merge-group-slim | N | test-context-stories-doc-split.sh | keep | Context-page line-cap/route-map rule; slim lane live |

## gates-02.yaml (13 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| agent-canon | dynamic | pre-pr | B | verify-agent-hygiene.yml / Agent hygiene gate | Y | verify-agent-canon.sh | keep | runs script analyzer on changed code |
| no-diff-fragments | dynamic | pre-commit | B | verify-agent-hygiene.yml / Agent hygiene gate | Y | verify-no-diff-fragments.sh | keep | runs script analyzer on changed code |
| scope-prefix-single-source | dynamic | pre-commit | B | verify-agent-hygiene.yml / Agent hygiene gate | Y | verify-scope-prefix-single-source.sh | keep | runs script analyzer on changed code |
| filename-stutter | dynamic | pre-commit | B | verify-agent-hygiene.yml / Agent hygiene gate | Y | verify-filename-stutter.sh | keep | runs script analyzer on changed code |
| no-ai-attribution | dynamic | pre-commit | B | verify-agent-hygiene.yml / Agent hygiene gate | N | verify-no-ai-attribution.sh | keep | runs script analyzer on changed code |
| no-private-identifiers | dynamic | pre-commit | B | verify-agent-hygiene.yml / Agent hygiene gate | Y | verify-no-private-identifiers.sh | keep | runs script analyzer on changed code |
| answer-demo-fixtures-sanitized | dynamic | pre-commit | B | verify-agent-hygiene.yml / Agent hygiene gate | Y | verify-answer-demo-fixtures-sanitized.sh | keep | runs script analyzer on changed code |
| license-header | static-shape | pre-commit | B | test.yml / verify-contracts | Y | verify-license-header.sh | keep | Cheap pre-commit shape; seeded self-test; born of #3767/#3844 |
| ci-install-apt-packages | dynamic | pre-pr | B | test.yml / go-core | N | test-ci-install-apt-packages.sh | keep | executes logic self-test |
| ci-go-mod-download-retry | dynamic | pre-pr | B | test.yml / go-core | N | test-ci-go-mod-download-retry.sh | keep | executes logic self-test |
| ci-go-install-retry | dynamic | pre-pr | B | test.yml / go-core | N | test-ci-go-install-retry.sh | keep | executes logic self-test |
| main-health-watcher | dynamic | pre-pr | B | verify-ci-gate-registry.yml / Verify ci-gate registry test m | N | test-main-health.sh | keep | executes logic self-test |
| dirgate-main-alarm | dynamic | pre-pr | B | verify-ci-gate-registry.yml / Verify ci-gate registry test m | N | test-dirgate-main-alarm.sh | keep | executes logic self-test |

## gates-03.yaml (13 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| pre-enqueue-check | dynamic | pre-pr | B | verify-ci-gate-registry.yml / Verify ci-gate registry test m | N | test-pre-enqueue-check.sh | keep | runs script analyzer on changed code |
| openapi-surface | static-contract | pre-pr | B | static-contract-gates.yml / Verify OpenAPI gate | Y | verify-openapi.sh | keep | Seeded self-test RED/GREEN; last catch area #8874 CLI endpoints 2026-10-29 |
| edge-source-tool-coverage | static-contract | pre-pr | B | static-contract-gates.yml / Verify edge source-tool coverage | Y | verify-edge-source-tool-coverage.sh | keep | Seeded self-test; #6468 crossrepo move 2026-09-03 |
| evidence-continuity | static-contract | pre-pr | B | static-contract-gates.yml / Verify evidence continuity gate | Y | verify-evidence-continuity.sh | keep | Seeded self-test; born of #4068 |
| skill-roundtrip | static-parity | pre-pr | B | static-contract-gates.yml / Verify skillgen roundtrip gate | Y | verify-skill-roundtrip.sh | keep | Seeded self-test; born of #3714 |
| skill-workflow-refs | static-config | pre-pr | B | static-contract-gates.yml / Verify skill workflow refs gate | Y | verify-skill-workflow-refs.sh | keep | Seeded self-test; caught agent-kit migration drift 2026-11-15 |
| fact-kind-registry | static-contract | pre-pr | B | static-contract-gates.yml / Verify contract source-of-truth  | Y | verify-fact-kind-registry.sh | keep | Seeded self-test; born of #4078 |
| contract-source-of-truth | static-parity | pre-pr | B | static-contract-gates.yml / Verify contract source-of-truth  | Y | verify-contracttest.sh | keep | Seeded self-test; #6886 restructure 2026-09-20 |
| factschema-diff | static-contract | pre-pr | B | factschema-diff.yml / Verify factschema-diff gate | Y | verify-factschema-diff.sh | keep | Seeded self-test; born of #4569/#4608 |
| payload-usage-manifest | dynamic | pre-pr | B | payload-usage-manifest.yml / Verify payload-usage-manifest g | Y | verify-payload-usage-manifest.sh | keep | runs script analyzer on changed code |
| scorecard-example-conformance | dynamic | pre-pr | B | scorecard-example-conformance.yml / Scorecard example go tes | N |  | keep | runs Go tests |
| sdk-go-collector | dynamic | pre-pr | B | sdk-go.yml / SDK Go: collector | N |  | keep | runs Go tests |
| live-readiness-registration-drift | static-drift | pre-pr | B | static-contract-gates.yml / Verify live-readiness registrati | Y | verify-live-postgres-readiness-drift.sh | keep | Self-test; #9146 ledger sharding 2026-10-27 |

## gates-04.yaml (18 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| sdk-go-factschema | dynamic | pre-pr | B | sdk-go.yml / SDK Go: factschema | N |  | keep | runs Go tests |
| sdk-go-apicontract | dynamic | pre-pr | B | sdk-go.yml / SDK Go: apicontract | N |  | keep | runs Go tests |
| mcp-schema-drift | static-parity | pre-pr | B | mcp-schema-drift.yml / Verify capability inventory | N | precommit-go.sh | keep | Runs precommit-go surface analysis on code; NO self-test; catch history NOT-PROVEN -> F4 |
| mcp-tool-count | dynamic | pre-pr | B | mcp-schema-drift.yml / Verify ReadOnlyTools count | N | (go test/toolchain) | keep | runs Go tests |
| mcp-test-suite | dynamic | pre-pr | B | mcp-schema-drift.yml / Full MCP test suite | N | (go test/toolchain) | keep | runs Go tests |
| mcp-router-eval | dynamic | pre-pr | B | mcp-schema-drift.yml / MCP router eval report | N | (go test/toolchain) | keep | runs Go tests |
| capability-inventory | dynamic | pre-pr | B | mcp-schema-drift.yml / Verify capability inventory | N | capability-stub-satellite.sh | keep | runs Go analyzer on code |
| capability-inventory-docs | dynamic | pre-pr | B | mcp-schema-drift.yml / Verify capability inventory | N | (go test/toolchain) | keep | runs Go analyzer on code |
| parser-relationship-kit | static-contract | pre-pr | B | static-contract-gates.yml / Verify parser relationship kit g | Y | verify-parser-relationship-kit.sh | keep | Seeded self-test; merge-ref diff fix 2026-11-06 (85466d468e) |
| accuracy-golden-gate | dynamic | pre-pr | B | test.yml / verify-contracts | N | verify_accuracy_golden_gate.sh | keep | runs script analyzer on changed code |
| heredoc-budget | static-budget | pre-pr | B | static-contract-gates.yml / Verify heredoc budget gate | Y | (go test/toolchain) | CONVERT-TO-TEST (followup) | Go tool + Go unit test exist (cmd/heredoc-budget); gate wraps `go run`; fold into go test -> F2 |
| query-methodology-static | dynamic | pre-commit | B | test.yml / verify-contracts | Y | verify-query-methodology.sh | keep | runs script analyzer on changed code |
| query-plan-regression | dynamic | pre-pr | B | test.yml / verify-contracts | Y | verify-query-plan-regression.sh | keep | runs script analyzer on changed code |
| scale-corpus-suite | static-contract | pre-pr | B | test.yml / verify-contracts | Y | verify-scale-corpus-suite.sh | keep | Seeded self-test; spec-structure validator |
| scale-benchmark-artifact | static-contract | pre-pr | B | test.yml / verify-contracts | Y | verify-scale-benchmark-artifact.sh | keep | Seeded self-test; born of #3222 |
| capability-budget-proof | static-contract | pre-pr | B | test.yml / verify-contracts | Y | verify-capability-budget-proof.sh | keep | Seeded self-test; born of #4077 |
| collector-entrypoints-generated | static-parity | pre-pr | B | test.yml / verify-contracts | Y | verify-collector-entrypoints-generated.s | keep | Seeded self-test; born of #2298; slim twin dedup -> F3 |
| capability-surface-inventory | static-parity | pre-pr | B | mcp-schema-drift.yml / Verify capability inventory | N | precommit-go.sh | keep | Runs precommit-go surface analysis; NO self-test; catch history NOT-PROVEN -> F4 |

## gates-05.yaml (13 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| telemetry-coverage | static-parity | pre-pr | B | static-contract-gates.yml / Verify telemetry coverage gate | Y | verify-telemetry-coverage.sh | keep | Seeded self-test; merge-ref diff fix 2026-11-06 (c4ef3d2f8e) |
| operator-dashboard | static-parity | pre-pr | B | static-contract-gates.yml / Verify generate-operator-dashboa | Y | generate-operator-dashboard.sh | keep | Seeded self-test; generator+template; #8875 C8 row 2026-11-10 |
| env-registry-doc | static-parity | pre-pr | B | static-contract-gates.yml / Verify environment registry refe | Y | verify-env-registry-doc.sh | keep | Seeded self-test; born of #5028 |
| race-graph-writes | dynamic | pre-pr | B | race-graph-writes.yml / race | N | (go test/toolchain) | keep | runs Go tests |
| reducer-contention | dynamic | pre-pr | B | reducer-contention-gate.yml / reducer contention gate | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| supply-chain-impact-live | dynamic | pre-pr | B | reducer-contention-gate.yml / reducer contention gate | Y | (go test/toolchain) | keep | runs Go tests |
| docs-catalog-metadata | static-docs | pre-pr | B | test.yml / docs-helm-hygiene | Y | verify-docs-catalog.sh | keep | Seeded self-test; #9461 repoint 2026-10-29 |
| required-doc-sections | static-docs | pre-pr | B | test.yml / docs-helm-hygiene | Y | verify-required-doc-sections.sh | keep | Seeded self-test; regression-born #9037 |
| docs-prose-quality | static-docs | pre-pr | a | - / - | Y | verify-docs-prose-quality.sh | DELETE-CANDIDATE (followup) | Advisory, NO CI workflow; burn-down baseline; needs consumer proof -> F1 |
| docs-contradiction | static-docs | pre-pr | a | - / - | Y | verify-docs-contradiction.sh | keep | Advisory BUT wired into pre-push floor (cigates/select.go, pre-push.sh); born of #5340 |
| docs-refs | static-docs | pre-pr | B | static-contract-gates.yml / Verify docs-refs gate | Y | verify-docs-refs.sh | keep | Seeded self-test; #9497 deploy-checkout repoint 2026-10-29 |
| docs-cli-env-refs | dynamic | pre-pr | B | static-contract-gates.yml / Verify docs CLI/env refs gate | Y | verify-docs-cli-env-refs.sh | keep | runs script analyzer on changed code |
| query-doc-commit-refs | static-docs | pre-pr | B | static-contract-gates.yml / Verify query-doc-commit-refs gat | Y | verify-query-doc-commit-refs.sh | keep | Seeded self-test; regression-born #5951 |

## gates-06.yaml (10 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| doc-citations | static-docs | pre-pr | B | static-contract-gates.yml / Verify doc-citations gate | Y | verify-doc-citations.sh | keep | Seeded fixture+repository self-test; #9368 rewire 2026-10-28 |
| moved-file-refs | static-docs | pre-pr | B | static-contract-gates.yml / Verify moved-file-refs gate | Y | verify-moved-file-refs.sh | keep | Seeded self-test; merge-ref diff fix 2026-11-06 |
| measurement-citations | static-docs | pre-pr | B | static-contract-gates.yml / Verify measurement-citations gat | Y | verify-measurement-citations.sh | keep | Seeded self-test; #9370 bare-host reject 2026-10-28 |
| remote-validation-artifacts | static-contract | pre-pr | B | static-contract-gates.yml / Verify remote-validation-artifac | Y | generate-remote-validation-inventory.sh | keep | Seeded self-test; born of #5988 |
| maturity-drift-guard | static-contract | pre-pr | B | static-contract-gates.yml / Verify maturity-drift-guard gate | Y | verify-maturity-drift-guard.sh | keep | Seeded self-test |
| mcp-client-auth-doc | static-docs | pre-pr | a | - / - | Y | verify-mcp-client-auth-doc.sh | WIRE-OR-DELETE (followup) | Advisory, NO CI workflow; Go lockstep test exists (mcpsetup); wire into CI or delete -> F1 |
| docs-build-changed | dynamic | pre-push | B | test.yml / docs-helm-hygiene | Y | verify-docs-build-changed.sh | keep | runs script analyzer on changed code |
| docs-only-ci-skip | dynamic | pre-pr | B | test.yml / docs-helm-hygiene | Y | verify-docs-only-ci-skip.sh | keep | runs script analyzer on changed code |
| frontend-format-changed | dynamic | pre-push | a | frontend.yml / Prettier format check (issue #3763) | Y | verify-frontend-format-changed.sh | keep | runs script analyzer on changed code |
| console-e2e | dynamic | pre-push | B | frontend.yml / Console (apps/console) | Y | precommit-console.sh | keep | runs console test suite |

## gates-07.yaml (10 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| frontend-site | dynamic | pre-push | B | frontend.yml / Marketing site (root Vite) | N | (npm) | keep | runs frontend toolchain (typecheck/test/build/audit) |
| frontend-console-checks | dynamic | pre-push | B | frontend.yml / Console (apps/console) | N | test-run-console-live-e2e.sh | keep | runs frontend toolchain (typecheck/test/build/audit) |
| console-a11y | dynamic | pre-push | B | frontend.yml / Console a11y audit (axe-core) | N | (npm) | keep | runs frontend toolchain (typecheck/test/build/audit) |
| frontend-eslint | dynamic | pre-push | B | frontend.yml / ESLint flat config (issue #3763) | Y | verify-eslint-config.sh | keep | runs script analyzer on changed code |
| npm-audit | dynamic | pre-push | B | frontend.yml / npm audit (fail on high/critical) | Y | verify-npm-audit-exceptions.sh | keep | runs frontend toolchain (typecheck/test/build/audit) |
| gosec-changed | dynamic | pre-push | B | security-scan.yml / gosec (Go static analysis) | Y | precommit-go.sh | keep | runs Go toolchain (build/vet/lint/audit) on code |
| govulncheck | dynamic | pre-push | B | security-scan.yml / govulncheck (Go vulnerability database) | Y | precommit-go.sh | keep | runs Go toolchain (build/vet/lint/audit) on code |
| nancy | dynamic | pre-push | a | security-scan.yml / nancy (Sonatype dep CVE + license scan) | Y | precommit-go.sh | DELETE-CANDIDATE (followup): non-blocking; purpose states it scans nothing without creds; needs run-history proof -> F1 | DELETE-CANDIDATE (followup): non-blocking; purpose states it scans nothing without creds; needs run-history proof -> F1 |
| trivy-fs | dynamic | pre-push | a | security-scan.yml / Trivy filesystem scan (vuln + secret + c | N | trivy-fs-local.sh | keep | runs script analyzer on changed code |
| golden-corpus-mirror | static-mirror | pre-pr | B | golden-corpus-gate.yml / static mirror | N | test-verify-golden-corpus-gate.sh | CONVERT-TO-TEST (followup) | local.command IS the hermetic self-test script; row adds only a CI job -> F2 |

## gates-08.yaml (3 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| golden-corpus-gate | dynamic | manual | a | golden-corpus-gate.yml / corpus-gate | Y | verify-golden-corpus-gate.sh | KEEP-AS-OFF-RECORD (followup): disabled NornicDB cell per owner order #7331; Neo4j cell is the blocking truth; row removal -> F1 | KEEP-AS-OFF-RECORD (followup): disabled NornicDB cell per owner order #7331; Neo4j cell is the blocking truth; row removal -> F1 |
| golden-corpus-gate-neo4j | dynamic | ci-heavy | B | golden-corpus-gate.yml / corpus-gate | Y | verify-golden-corpus-gate.sh | keep | runs script analyzer on changed code |
| live-backend-tests | dynamic | ci-heavy | B | live-backend-tests.yml / live-backend (nornicdb and neo4j) | Y | run-live-backend-tests.sh | keep | runs script analyzer on changed code |

## gates-09.yaml (8 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| live-backend-static-mirror | static-mirror | pre-pr | B | live-backend-tests.yml / static-mirror (ledger and runner pr | Y | verify-live-tests-ledger.sh | keep | Blocking hermetic ledger+runner guard; seeded self-test; #6784 classification |
| live-postgres-readiness | dynamic | ci-heavy | a | live-postgres-readiness.yml / live-postgres-readiness (postg | Y | test-run-live-postgres-readiness-tests.s | keep | executes logic self-test |
| live-postgres-readiness-shard-mirror | static-mirror | pre-pr | B | live-postgres-readiness.yml / live-postgres-readiness hermet | N | test-live-postgres-readiness-shards.py | keep | Blocking hermetic shard contract (cut/budget/union/weights); contract tests run locally |
| live-postgres-migrations | dynamic | ci-heavy | B | live-postgres-migrations.yml / live-postgres-migrations (pos | Y | test-run-live-postgres-migrations.sh | keep | executes logic self-test |
| golden-corpus-differential | dynamic | ci-heavy | B | golden-corpus-gate.yml / differential nornicdb vs neo4j | Y | verify-golden-corpus-gate.sh | keep | runs Go analyzer on code |
| golden-corpus-filter-exhaustive | static-config | pre-pr | B | test.yml / verify-contracts | Y | verify-golden-corpus-filter-exhaustive.s | keep | Seeded self-test; guards CI path-filter completeness; fixed 3 PR failures 2026-11-18 |
| replay-coverage-gate | static-contract | pre-pr | B | replay-coverage-gate.yml / coverage-gate (blocking) | Y | verify-replay-coverage-gate.sh | keep | Seeded self-test; blocking coverage manifest |
| replay-coverage-static-mirror | static-mirror | pre-pr | B | replay-coverage-gate.yml / static mirror | N | (CI-only) | CONVERT-TO-TEST (followup) | Empty local.command; owns CI job only; fold into replay-coverage-gate self-test -> F2 |

## gates-10.yaml (10 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| ifa-contract-layer | dynamic | pre-pr | B | static-contract-gates.yml / Verify Ifa contract-layer gate | Y | (go test/toolchain) | keep | runs Go tests |
| content-entity-bucket-sync | dynamic | pre-pr | B | static-contract-gates.yml / Verify content-entity bucket syn | Y | (go test/toolchain) | keep | runs Go tests |
| collector-content-pin | dynamic | pre-pr | B | test.yml / go-core | Y | (go test/toolchain) | keep | runs Go tests |
| delta-union-scheduled | dynamic | ci-heavy | a | delta-union-scheduled.yml / Delta union proof vs Neo4j and P | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| ifa-scheduled-proofs | dynamic | ci-heavy | a | ifa-scheduled-proofs.yml / Ifá scheduled proof vs Neo4j and  | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| index-key-guard-sweep | dynamic | pre-pr | B | test.yml / go-core | Y | (go test/toolchain) | keep | runs Go tests |
| fips-md5-sql-sweep | dynamic | pre-pr | B | test.yml / go-core | Y | (go test/toolchain) | keep | runs Go tests |
| hot-cypher-source-coverage | dynamic | pre-pr | B | test.yml / go-core | Y | (go test/toolchain) | keep | runs Go tests |
| ask-overlay-inventory-coverage | dynamic | pre-pr | B | test.yml / go-core | N | (go test/toolchain) | keep | runs Go tests |
| ifa-materialized-edge-coverage | dynamic | pre-pr | B | static-contract-gates.yml / Verify Ifa contract-layer gate | Y | (go test/toolchain) | keep | runs Go tests |

## gates-11.yaml (2 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| ifa-determinism | dynamic | pre-pr | B | ifa-determinism-gate.yml / determinism-matrix | Y | test-verify-ifa-determinism.sh | keep | executes logic self-test |
| ifa-dead-letter-matrix | dynamic | pre-pr | B | ifa-determinism-gate.yml / dead-letter-matrix | Y | test-verify-ifa-dead-letter-matrix.sh | keep | executes logic self-test |

## gates-12.yaml (1 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| ifa-fault-injection | dynamic | pre-pr | B | ifa-determinism-gate.yml / fault-injection | Y | test-verify-ifa-fault-injection.sh | keep | executes logic self-test |

## gates-13.yaml (3 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| ifa-static-mirror | static-mirror | pre-pr | a | ifa-determinism-gate.yml / static mirror | N | (CI-only) | keep | Advisory, never blocks; guards stale hand-kept copies + NornicDB rewire |
| ifa-load-saturation | dynamic | pre-pr | B | static-contract-gates.yml / Verify Ifa P5 load/saturation ga | Y | (go test/toolchain) | keep | runs Go tests |
| cassette-author | dynamic | pre-pr | B | static-contract-gates.yml / Verify cassette author gate | Y | verify-cassette-author.sh | keep | runs script analyzer on changed code |

## gates-14.yaml (14 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| ifa-replay-drive | static-mirror | pre-pr | B | static-contract-gates.yml / Verify Ifa replay-drive mirror g | Y | test-verify-ifa-replay-drive.sh | CONVERT-TO-TEST (followup) | local IS the test script and never replays; fold into scheduled proof or drop row -> F2 |
| parserfixture-tests | dynamic | pre-pr | B | test.yml / go-race-complete | N | (go test/toolchain) | keep | runs Go tests |
| authz-scoped-route-tests | dynamic | pre-pr | B | replay-coverage-gate.yml / coverage-gate (blocking) | N | go-test-run-guard.sh | keep | runs script analyzer on changed code |
| code-coverage-report | dynamic | pre-pr | a | code-coverage-report.yml / Generate Go code coverage report | Y | generate-code-coverage-report.sh | keep | runs script analyzer on changed code |
| e2e-tests | dynamic | ci-heavy | B | e2e-tests.yml / test | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| trivy-image | dynamic | ci-heavy | a | security-scan.yml / Trivy image scan (ghcr.io/eshu-hq/eshu) | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| docker-image-build | dynamic | ci-heavy | B | docker-publish.yml / build-and-push-image | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| docker-image-reproducibility | dynamic | ci-heavy | B | docker-publish.yml / verify-reproducibility | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| docker-publish-shape | static-config | pre-pr | B | static-contract-gates.yml / Verify docker-publish shape gate | Y | test-verify-docker-publish-moving-tags.s | keep | Self-test (moving-tags/platforms/promote/resolver); never pushes |
| dockerhub-login | static-config | pre-pr | B | static-contract-gates.yml / Verify Docker Hub login gate | Y | verify-dockerhub-login.sh | keep | Seeded self-test; PR/merge-group Docker Hub guard |
| workflow-script-checkout | static-config | pre-pr | B | static-contract-gates.yml / Verify workflow script checkout  | Y | verify-workflow-script-checkout.sh | keep | Seeded self-test; #9367 plumbing removal 2026-10-28 |
| docker-image-promotion | dynamic | ci-heavy | a | docker-publish.yml / promote-moving-tags | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| release-sbom-attach | dynamic | ci-heavy | a | docker-publish.yml / attach-release-sbom | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| scanner-digest-pins | static-config | pre-pr | B | docker-publish.yml / verify-scanner-digest-pins | N | test-verify-scanner-digest-pins.sh | keep | local IS hermetic self-test; born of #9632; #8878 fallout fix 2026-10-31 |

## gates-15.yaml (17 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| runtime-image-tools | static-config | pre-pr | B | docker-publish.yml / verify-reproducibility | N | test-verify-runtime-image-tools.sh | keep | local IS verifier test script; #10166 tesseract pin 2026-11-05 |
| apk-floors | dynamic | pre-pr | B | docker-publish.yml / verify-apk-floors | Y | verify-apk-floors.sh | keep | runs script analyzer on changed code |
| apk-floors-drift | dynamic | ci-heavy | a | apk-floors-drift.yml / Apk floors drift (daily) | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| scanner-freshness-signal | dynamic | ci-heavy | a | scanner-freshness.yml / Scanner freshness (weekly) | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| ocr-sandbox-proof | dynamic | ci-heavy | a | ocr-sandbox-proof.yml / OCR sandbox proof | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| docker-publish | dynamic | manual | a | docker-publish.yml / build-and-push-image | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| satellite-gates | dynamic | ci-heavy | B | docker-publish.yml / verify-satellite-gates | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| macos-build | dynamic | ci-heavy | a | macos.yml / macos | N | (CI-only) | KEEP-SCHEDULED (followup): 0 of 29 failures macOS-only since 2026-09-21 at ~2800 macos-min/day; delete-case for closing PR -> F1 | KEEP-SCHEDULED (followup): 0 of 29 failures macOS-only since 2026-09-21 at ~2800 macos-min/day; delete-case for closing PR -> F1 |
| root-cause-evidence | static-docs | pre-pr | a | static-contract-gates.yml / Verify root-cause-evidence gate | Y | verify-root-cause-evidence.sh | keep | Seeded self-test; advisory; merge-ref diff fix 2026-11-06 |
| pg-statement-report | static-mirror | pre-pr | B | static-contract-gates.yml / Verify pg-statement-report gate | N | test-pg-statement-report.sh | CONVERT-TO-TEST (followup) | local IS stub-psql contract test already; row adds only a CI job -> F2 |
| collector-cutover-rollback | static-mirror | pre-pr | B | static-contract-gates.yml / Verify collector-cutover-rollbac | N | test-collector-cutover-rollback.sh | CONVERT-TO-TEST (followup) | local IS stubbed order/refusal/idempotence test; #9331; row adds only CI job -> F2 |
| tagged-builds | dynamic | pre-pr | B | static-contract-gates.yml / Verify tagged-builds gate | Y | verify-tagged-builds.sh | keep | runs script analyzer on changed code |
| perf-evidence | dynamic | pre-push | B | test.yml / verify-contracts | Y | precommit-go.sh | keep | runs Go toolchain (build/vet/lint/audit) on code |
| product-claim-ledger | dynamic | pre-pr | B | product-claim-ledger.yml / Verify product claim ledger | N | (go test/toolchain) | keep | runs Go analyzer on code |
| replay-tier | dynamic | ci-heavy | B | verify-replay-tier.yml / Offline replay tier vs real Neo4j | Y | verify-replay-tier.sh | keep | runs script analyzer on changed code |
| go-test-race | dynamic | pre-pr | B | test.yml / go-race-complete | N | (go test/toolchain) | keep | runs Go tests |
| ci-gate-registry | dynamic | pre-pr | B | verify-ci-gate-registry.yml / Verify ci-gate registry + drif | Y | verify-ci-gates-registry.sh | keep | runs script analyzer on changed code |

## gates-16.yaml (5 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| ci-service-mirror-publish | dynamic | manual | a | docker-publish.yml / publish-ci-service-mirrors | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| ci-service-mirror-verify-public | dynamic | manual | a | docker-publish.yml / verify-public-ci-service-mirrors | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |
| cgo-cflags-guard | static-config | pre-push | B | verify-agent-hygiene.yml / Agent hygiene gate | Y | verify-cgo-cflags-guard.sh | keep | Seeded self-test; regression-born #9066 (GCC 15) |
| changelog-unreleased | static-config | pre-push | B | static-contract-gates.yml / Verify changelog-unreleased gate | Y | verify-changelog-unreleased.sh | keep | Seeded self-test; regression-born #9096 (union-merge) |
| nornicdb-ci-off | static-config | pre-push | B | verify-agent-hygiene.yml / Agent hygiene gate | Y | verify-nornicdb-ci-off.py | keep | Seeded self-test; #7331 owner-order guard; gaps closed 2026-11-06 |

## gates-17.yaml (3 gates)

| gate | class | tier | B | ci job | T | verifier | disposition | evidence / last catch |
|---|---|---|---|---|---|---|---|---|
| codex-goal-continuation | dynamic | pre-pr | B | verify-agent-hygiene.yml / Agent hygiene gate | N | test-codex-goal-continue-hook.sh | keep | executes logic self-test |
| ghcr-buildcache-prune | static-config | pre-pr | B | static-contract-gates.yml / Verify GHCR build cache prune ga | N | test-prune-ghcr-buildcache.sh | keep | local IS hermetic self-test; born of #9632 |
| ghcr-buildcache-prune-run | dynamic | ci-heavy | a | ghcr-buildcache-prune.yml / GHCR build cache prune | N | (CI-only) | keep | CI-only proof (needs hosted service, registry, or schedule) |

## hygiene_hooks (10 ids, all dynamic pre-commit mirrors, all keep)

| hook | disposition | reason |
|---|---|---|
| codex-spawn-guard | keep | pre-commit mirror of the agent-canon Codex spawn guard test, also run locally and in CI |
| goal-role-routing | keep | pre-commit mirror of agent-canon goal router behavior, also run in the local test command and CI hygiene workf |
| no-ai-attribution-message | keep | commit-msg-stage variant of the no-ai-attribution gate (same check, different stage) |
| no-private-identifiers-message | keep | commit-msg-stage variant of the no-private-identifiers gate (same check, different stage) |
| frontend-format-staged | keep | staged-file variant of frontend-format-changed (same prettier check, pre-commit stage) |
| frontend-format-verifier-tests | keep | self-test of the frontend-format verifier + hook wiring |
| docs-build-staged | keep | staged-file variant of docs-build-changed (same mkdocs check, pre-commit stage) |
| docs-build-verifier-tests | keep | self-test of the docs-build verifier + hook wiring |
| console-e2e-verifier-tests | keep | self-test of the console-e2e pre-push helper routing |
| naming-glue-gate | keep | model-judged glued-compound directory name check (naming.md rule 3); pre-commit-only by design -- see go/cmd/n |

## Followups for closing PRs

- F1 (delete/wire, each needs consumer proof in its PR): `docs-prose-quality` (advisory, no workflow),
  `mcp-client-auth-doc` (wire into CI or delete; Go lockstep test exists), `nancy` (scans nothing
  without creds per its purpose), `golden-corpus-gate` NornicDB off-cell (#7331), `macos-build`
  (0/29 macOS-only failures; cost case).
- F2 (convert-to-test, one PR each with selector+workflow+docs updates): `heredoc-budget`,
  `golden-corpus-mirror`, `replay-coverage-static-mirror`, `ifa-replay-drive`, `pg-statement-report`,
  `collector-cutover-rollback`.
- F3 (dedup audit): `slim-collector-entrypoints` vs `collector-entrypoints-generated` run the same
  verifier on different lanes; confirm both lanes still needed after queue-select decisions.
- F4 (CI-history lookup): last-caught proof for `mcp-schema-drift`, `capability-surface-inventory`
  (no self-test); query failed-run history before their closing PRs.

## Focused proof (this turn, inventory-only change)

- `bash scripts/verify-ci-gates-registry.sh --drift` → `PASS: ci-gates registry integrity + drift check` (exit 0, 2026-11-19; table adds no registry rows).
- Census: `rg` count of gate ids across `specs/ci-gates.d` = 160; `hygiene_hooks` = 10; total 170.
