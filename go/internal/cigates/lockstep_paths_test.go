// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

// probePaths are docs-set files named by Go tests without ever being read:
// pure path vectors for selection logic, never asserted content. The
// discovery test verifies the named-only claim; a file that gains a reader
// must move to the lockstep manifest instead. Bare filenames resolve only in
// read position or as map keys, so glob vectors and fixture names never land
// here. A probe that starts being read flips to a read demand automatically.
var probePaths = []string{
	".agents/skills/eshu-code-review/SKILL.md",
	"AGENTS.md",
	"CHANGELOG.md",
	"CONTRIBUTING.md",
	"README.md",
	"apps/console/README.md",
	"docs/internal/agent-guide.md",
	"docs/internal/writing-for-agents.md",
	"docs/openapi/README.md",
	"docs/public/contributing-language-support.md",
	"docs/public/extend/reference-pagerduty-extension.md",
	"docs/public/guides/collector-authoring.md",
	"docs/public/guides/fixture-ecosystems.md",
	"docs/public/guides/mcp-guide.md",
	"docs/public/index.md",
	"docs/public/observability/dashboards/eshu-operator-overview.json",
	"docs/public/reference/capability-catalog.md",
	"docs/public/reference/ci-gates.md",
	"docs/public/reference/code-coverage-shield.json",
	"docs/public/reference/code-coverage.md",
	"docs/public/reference/environment-compose-tests.md",
	"docs/public/reference/investigation-evidence-packet.md",
	"docs/public/reference/local-testing.md",
	"docs/public/reference/local-testing/quick-verification-matrix.md",
	"docs/public/reference/local-testing/remote-representative-acceptance.md",
	"docs/public/reference/local-testing/verification-gates.md",
	"docs/public/reference/operator-digest.md",
	"expected/gemini-cli/GEMINI.md",
	"go/README.md",
	"go/cmd/collector-cicd-run/README.md",
	"go/cmd/ci-gates/README.md",
	"go/cmd/pr-watch/AGENTS.md",
	"go/internal/README.md",
	"go/internal/capabilitycatalog/README.md",
	"go/internal/collector/repo/README.md",
	"go/internal/coordinator/README.md",
	"go/internal/envregistry/README.md",
	"go/internal/facts/README.md",
	"go/internal/mcp/README.md",
	"go/internal/parser/README.md",
	"go/internal/parser/AGENTS.md",
	"go/internal/parser/rust/README.md",
	"go/internal/projector/README.md",
	"go/internal/query/README.md",
	"go/internal/relationships/README.md",
	"go/internal/runtime/README.md",
	"go/internal/status/README.md",
	"go/internal/storage/README.md",
	"go/internal/storage/cypher/README.md",
	"go/internal/storage/neo4j/README.md",
	"go/internal/storage/postgres/README.md",
	"go/internal/telemetry/README.md",
	"package-lock.json",
	"package.json",
	"sdk/go/factschema/azure/v1/README.md",
	"scripts/README.md",
	"scripts/lib/ifa_scheduled_inventory.json",
	"skill-fragments/operating-standard.md",
	"tsconfig.json",
	"tsconfig.node.json",
}

// allowedUnresolvedReads classifies test read sites the scanner cannot
// resolve to a repo file, keyed "test/file_test.go|token" where token is the
// flowing identifier or bare literal. The value names the verified reason:
// temp-dir fixtures, testdata/specs/examples/tests trees (code, never
// docs-set), synthetic vectors that are compared but never opened, and
// sibling source reads. A new entry must be added deliberately; an
// unclassified site fails the discovery test.
var allowedUnresolvedReads = map[string]string{
	"go/cmd/bootstrap-index/wiring_tfstate_resolvers_scan_test.go|path":                                                                   "own-package .go source via readOwnPackageSource (code)",
	"go/cmd/eshu/vuln_scan_fixture_corpus_parser_test.go|path":                                                                            "testdata/vuln_scan_repo_fixtures corpus (code)",
	"go/cmd/eshu/vuln_scan_supply_chain_demo_test.go|package-lock.json":                                                                   "examples/supply-chain-demo fixture (code)",
	"go/cmd/eshu/vuln_scan_supply_chain_demo_test.go|package.json":                                                                        "examples/supply-chain-demo fixture (code)",
	"go/cmd/ingester/wiring_canonical_writer_tfstate_resolvers_scan_test.go|path":                                                         "own-package .go source via readOwnPackageSource (code)",
	"go/cmd/projector/runtime_wiring_tfstate_resolvers_scan_test.go|path":                                                                 "own-package .go source via readOwnPackageSource (code)",
	"go/cmd/reducer/neo4j_wiring_test.go|sourcePath":                                                                                      "sibling .go source read (code)",
	"go/internal/cigates/queue_selection_workflow_test.go|path":                                                                           ".github/workflows file (code, not docs-set)",
	"go/internal/collector/gcpcloud/gcpruntime/bigquery_table_source_test.go|assets_list_bigquery_table.json":                             "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/gcpruntime/claimed_source_test.go|assets_list_page1.json":                                             "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/gcpruntime/compute_network_source_test.go|assets_list_compute_network.json":                           "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/gcpruntime/dns_record_test.go|assets_list_dns_record.json":                                            "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/gcpruntime/iam_policy_test.go|assets_list_iam_policy.json":                                            "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/gcpruntime/image_reference_test.go|assets_list_image_reference.json":                                  "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/gcpruntime/relationship_test.go|assets_list_relationship.json":                                        "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/gcpruntime/service_account_source_test.go|assets_list_service_account.json":                           "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/gcpruntime/source_test.go|assets_list_page1.json":                                                     "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/gcpruntime/source_test.go|assets_list_page2.json":                                                     "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/gcpruntime/telemetry_test.go|assets_list_page1.json":                                                  "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/gcpruntime/telemetry_test.go|assets_list_page2.json":                                                  "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/generation_test.go|assets_list_page1.json":                                                            "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/generation_test.go|assets_list_page2.json":                                                            "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/parse_test.go|assets_list_dns_record.json":                                                            "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/parse_test.go|assets_list_iam_policy.json":                                                            "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/parse_test.go|assets_list_image_reference.json":                                                       "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/parse_test.go|assets_list_page1.json":                                                                 "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/parse_test.go|assets_list_page2.json":                                                                 "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/parse_test.go|assets_list_relationship.json":                                                          "testdata fixture via readFixture (code)",
	"go/internal/collector/gcpcloud/parse_test.go|search_all_resources.json":                                                              "testdata fixture via readFixture (code)",
	"go/internal/collector/pagerduty/reference_component_test.go|complete-result.json":                                                    "examples plus testdata fixture via readReferenceFixtureResult (code)",
	"go/internal/collector/repo/git/hardcoded_secret_golden_test.go|fixturePath":                                                          "tests/fixtures golden tree (code)",
	"go/internal/collector/repo/git/observability_applied_facts_test.go|relativePath":                                                     "tmpdir fixture written then read back (write-then-read)",
	"go/internal/collector/repo/git/selection_copy_commit_policy_test.go|retainedPath":                                                    "synthetic workflow names in a tmpdir git repo (never committed)",
	"go/internal/collector/repo/git/selection_copy_hidden_test.go|rel":                                                                    "synthetic registry-dotfile rel joined under nested/dir, os.Stat only under a t.TempDir() copy root (code, never docs-set)",
	"go/internal/collector/repo/git/snapshot_copy_bound_commit_test.go|workflowPath":                                                      "synthetic workflow name in a tmpdir git repo (never committed)",
	"go/internal/collector/repo/git/snapshot_source_commit_sha_test.go|FileTargets":                                                       "tmpdir repo file written then snapshotted (fixture)",
	"go/internal/cli/demo/runtime_test.go|path":                                                                                           "deploy asset read from the pinned eshu-deploy checkout via ESHU_DEPLOY_CHECKOUT (#8871 in-tree removal)",
	"go/internal/deploy/extraction_contract_test.go|helm/eshu/values.schema.json":                                                         "deploy asset read from the pinned eshu-deploy checkout via ESHU_DEPLOY_CHECKOUT (#8871 in-tree removal)",
	"go/internal/runtime/compose_nornicdb_image_test.go|docs/kubernetes/helm-routing-and-storage-values.md":                               "deploy asset read from the pinned eshu-deploy checkout via ESHU_DEPLOY_CHECKOUT (#8871 in-tree removal)",
	"go/internal/runtime/compose_nornicdb_image_test.go|docs/run-locally/docker-compose.md":                                               "deploy asset read from the pinned eshu-deploy checkout via ESHU_DEPLOY_CHECKOUT (#8871 in-tree removal)",
	"go/internal/correlation/drift/tfconfigstate/fixture_corpus_test.go|path":                                                             "param-dir helper; callers pass testdata corpus dirs (code)",
	"go/internal/demospec/manifest_test.go|cassettePath":                                                                                  "testdata/cassettes tree (code)",
	"go/internal/demospec/manifest_test.go|supply-chain-demo.json":                                                                        "testdata cassette filename joined under a repo fixture root (code)",
	"go/internal/envregistry/coverage_test.go|path":                                                                                       "go source scan list in coreScanFiles (code)",
	"go/internal/ifa/materializededges/family_count_test.go|path":                                                                         "go source claim files, lockstep_test.go and materialized_edge_families.go (code)",
	"go/internal/ifa/materializededges/ledger_halves_test.go|path":                                                                        "specs dir passed as a helper param (code, not docs-set)",
	"go/internal/ifa/materializededges/vault_shared_cell_live_edges_test.go|ifa-authenticates-vault-role-family-expected-edges-live.json": "testdata via root=../testdata (code)",
	"go/internal/ifa/materializededges/vault_shared_cell_live_edges_test.go|ifa-authenticates-vault-role-family-expected-edges.json":      "testdata via root=../testdata (code)",
	"go/internal/ifa/materializededges/vault_shared_cell_live_edges_test.go|ifa-uses-vault-policy-family-expected-edges-live.json":        "testdata via root=../testdata (code)",
	"go/internal/ifa/materializededges/vault_shared_cell_live_edges_test.go|ifa-uses-vault-policy-family-expected-edges.json":             "testdata via root=../testdata (code)",
	"go/internal/mcp/read_surface_domain_semantic_bites_test.go|realPath":                                                                 "specs fact-kind-registry file (code, not docs-set)",
	"go/internal/parser/haskell/characterization_test.go|goldenPath":                                                                      "testdata/characterization goldens (code)",
	"go/internal/parser/javascript/dead_code_node_typescript_fixture_test.go|expected.json":                                               "tests/fixtures/dead-code tree (code)",
	"go/internal/parser/javascript/dead_code_node_typescript_fixture_test.go|path":                                                        "tests/fixtures/dead-code tree (code)",
	"go/internal/query/code_topic_entity_probe_test.go|path":                                                                              "testdata/code_topic_sql goldens joined with a dynamic name (code)",
	"go/internal/query/codequery/deadcode/node_typescript_matrix_test.go|expected.json":                                                   "tests/fixtures/dead-code tree (code)",
	"go/internal/query/codequery/deadcode/node_typescript_matrix_test.go|path":                                                            "tests/fixtures/dead-code tree (code)",
	"go/internal/relationships/relationship_platform_fixture_test.go|path":                                                                "tests/fixtures/relationship_platform via a spread join (code)",
	"go/internal/replay/recordpseudo/corpus_test.go|supply-chain-demo.json":                                                               "testdata/cassettes corpus (code)",
	"go/internal/replay/recordpseudo/engine_test.go|supply-chain-demo.json":                                                               "testdata/cassettes corpus (code)",
	"go/internal/runtime/compose_defaults_test.go|AGENTS.md":                                                                              "repo-root read of AGENTS.md; the file is in the lockstep manifest",
	"go/internal/searchbench/retrieval_proof_test.go|path":                                                                                "dynamic evidence names under searchbench-evidence/; the files are in the lockstep manifest",
	"go/internal/storage/postgres/schema_bootstrap_files_test.go|path":                                                                    "bootstrap SQL under go/internal/storage/postgres/migrations (code)",
	"go/internal/storage/postgres/vulnerability_sql_schema_lockstep_test.go|schemaPath":                                                   "sdk/factschema schema directory (code, not docs-set)",
	"go/internal/storage/postgres/vulnerability_sql_schema_lockstep_test.go|vulnerability.os_package.v1.schema.json":                      "sdk/factschema schema file (code, not docs-set)",
	"go/internal/synth/gcp/demo_profile_test.go|supply-chain-demo.json":                                                                   "testdata expectation path compared as a string, generated at runtime (code)",
}

// manuallyVerifiedLockstep covers lockstep manifest entries no static rule can
// see: doc paths that reach a test through several hops (production const
// alias into a struct field read generically, join chains through a variable
// repo root, table-driven evidence names). The value points at the reading
// test and the chain shape. Every other manifest entry must be
// scanner-detected, or the stale check fails.
var manuallyVerifiedLockstep = map[string]string{
	"AGENTS.md": "compose_defaults_test.go reads the repo-root AGENTS.md through readRepositoryFile(t, root, ...) (bare literal on a multi-arg call); the caller-relative target cannot resolve statically",
	"docs/public/reference/searchbench-evidence/issue-1298-semantic-retrieval-proof-v1.json":           "retrieval_proof_test.go reads table-driven evidence names joined under docs/public/reference/searchbench-evidence/; the per-name filename is a loop variable",
	"docs/public/reference/searchbench-evidence/issue-417-nornicdb-hybrid-retrieval-prototype-v1.json": "retrieval_proof_test.go reads table-driven evidence names joined under docs/public/reference/searchbench-evidence/; the per-name filename is a loop variable",
}
