#!/usr/bin/env bash
# require_workflow_path assertions for scripts/test-verify-golden-corpus-gate.sh,
# extracted to its own golden-corpus-*.sh lib chunk (naming convention: see
# golden-corpus-mirror-matcher.sh's header) so the mirror stays under the
# repo's 500-line file rule as the assertion count grows. Sourced by the
# mirror only, after golden-corpus-mirror-matcher.sh (defines require_in) and
# after ${workflow} is set.
#
# #5596 established the pattern this file continues: the workflow's
# on.pull_request.paths filter must trigger the golden-corpus gate on every
# source dir whose changes can alter emitted facts, graph/content projection,
# or query/MCP response truth the gate asserts. A dir missing here means a PR
# that touches only that dir ships an unverified change (a false-green) until
# the unconditional push-to-main trigger catches it post-merge — the wrong
# side of the review boundary. #5538 found go/internal/relationships/** had
# already regained this coverage as an incidental side effect of an unrelated
# PR, then used the same test to audit and close the rest of the class.
#
# Anchored off comment lines for the same reason every other mirror assertion
# is: a YAML `#` comment naming a path would otherwise satisfy the assertion
# while the real filter entry is gone.
require_workflow_path() {
	local label="$1" path_glob="$2"
	require_in "golden-corpus-gate.yml paths filter (${label})" "${workflow}" "- '${path_glob}'"
}

# --- established (#5596) ----------------------------------------------------
require_workflow_path "collector fact emission"        "go/internal/collector/**"
require_workflow_path "parser fact emission"           "go/internal/parser/**"
require_workflow_path "projector graph writes"         "go/internal/projector/**"
require_workflow_path "reducer graph writes"           "go/internal/reducer/**"
require_workflow_path "query response shapes"          "go/internal/query/**"
require_workflow_path "storage layer"                  "go/internal/storage/**"
require_workflow_path "relationship resolution (#5596)" "go/internal/relationships/**"
require_workflow_path "fact-kind schemas (#5596)"       "sdk/go/factschema/**"
# The fact-emitting command packages (service wiring that assembles the
# collectors/ingester/reducer/projector) must trigger the gate too — a change
# under go/cmd/collector-aws-cloud/service.go can alter emitted facts as much
# as go/internal/collector (#5686 review).
require_workflow_path "collector command wiring"       "go/cmd/collector-**"
require_workflow_path "bootstrap-index fact seeding"   "go/cmd/bootstrap-index/**"
require_workflow_path "ingester fact emission"         "go/cmd/ingester/**"
require_workflow_path "projector runtime"              "go/cmd/projector/**"
require_workflow_path "reducer runtime"                "go/cmd/reducer/**"
require_workflow_path "api query surface"              "go/cmd/api/**"
# Both were already IN the workflow's paths filter and the ci-gates registry
# but had never been asserted by this mirror -- a pre-existing gap (present on
# main too), found during the #5538 second review round. The gate command
# itself (go/cmd/golden-corpus-gate/**) builds the orchestrator binary that
# drives every stage this file asserts; go/internal/demospec backs the
# fixed-corpus repository/demo-answer inputs the gate replays. Without an
# assertion here, either path silently falling out of the workflow (a typo, a
# rename, a merge conflict) would pass this mirror with nothing to catch it.
require_workflow_path "golden-corpus-gate command (#5538 gap)" "go/cmd/golden-corpus-gate/**"
require_workflow_path "mock Prometheus range source" "go/cmd/mock-prometheus-mimir/**"
require_workflow_path "mock OpenAI-compatible Ask source" "go/cmd/mock-openai-compatible/**"
require_workflow_path "demo-spec fixed corpus inputs (#5538 gap)" "go/internal/demospec/**"
require_workflow_path "static ecosystem corpus inputs" "tests/fixtures/ecosystems/**"
# The orchestrator sources these; an edit to the mutex or a fixture/timing lib
# changes what the gate does, so each must trigger it. Without this the lock
# itself was in no trigger list at all - its only test would never have run.
require_workflow_path "golden-corpus libs"             "scripts/lib/golden-corpus-*.sh"
require_workflow_path "golden-corpus lib tests"        "scripts/lib/test-golden-corpus-*.sh"
require_workflow_path "live gate mutex"                "scripts/lib/live-gate-lock.sh"

# --- #5538 wider audit -------------------------------------------------------
# Same "can a change here alter projected truth?" test (#5538's own framing),
# applied to every package `go list -deps` confirms is actually compiled into
# the six binaries verify-golden-corpus-gate.sh builds: bootstrap-index,
# projector, reducer, api, mcp-server, golden-corpus-gate.

# The gate starts eshu-mcp-server and asserts "B-7(c) MCP query truth" against
# it directly (start_bg mcp-server ...; curl .../health), yet neither the
# binary's cmd package nor the tool-surface package it wraps was ever in this
# filter — the gate's own MCP assertion was unverifiable at PR review time.
require_workflow_path "MCP server binary (#5538)"      "go/cmd/mcp-server/**"
require_workflow_path "MCP tool surface (#5538)"       "go/internal/mcp/**"

# The pure, no-I/O assertion core behind the already-triggered
# go/cmd/golden-corpus-gate/** — every Evaluate* function that turns an
# observed value into a pass/fail Finding. A regression here is a false
# negative in the verifier itself, so it must trigger the gate it verifies.
require_workflow_path "gate assertion core (#5538)"    "go/internal/goldengate/**"

# Graph write contract: the Cypher builders, merge builders, and EnsureSchema
# constraint/index contract every backend adapter shares, plus the
# concurrency/backpressure/schema-compat control wired into
# reducer/projector/bootstrap-index around it. A regression in any of these
# directly changes what lands in — or whether writes even complete against —
# the graph the gate diffs.
require_workflow_path "graph write contract (#5538)"      "go/internal/graph/**"
require_workflow_path "graph owner-ledger gate (#5538)"    "go/internal/graphowner/**"
require_workflow_path "graph schema compat gate (#5538)"   "go/internal/graphschemacompat/**"
require_workflow_path "graph write backpressure (#5538)"   "go/internal/graphbackpressure/**"

# The durable envelope/queue/replay contracts spanning "emit facts -> enqueue
# work" in the pipeline's own description (CLAUDE.md): facts is the Envelope
# every collector/parser fact rides from collection through the queue into
# projector and reducer; queue is the shared work-item lifecycle; replay is
# the canonical cassette-replay core all 18 B-10 cassette collectors this gate
# replays depend on.
require_workflow_path "fact envelope contract (#5538)"     "go/internal/facts/**"
require_workflow_path "queue work-item contract (#5538)"   "go/internal/queue/**"
require_workflow_path "cassette replay core (#5538)"       "go/internal/replay/**"

# Content projection, the pipeline's own explicit third stage after graph
# projection (CLAUDE.md: "reducer -> graph/content projection -> query
# surface"). verify-golden-corpus-gate.sh wires ESHU_CONTENT_STORE_DSN for
# exactly this write path.
require_workflow_path "content write contract (#5538)"       "go/internal/content/**"
require_workflow_path "content reference extraction (#5538)" "go/internal/contentrefs/**"

# Canonical identity and edge-vocabulary producers — the same class #5596
# already fixed for relationships (a package is a clear yes when it "produces
# the evidence facts the reducer projects"). terraformschema is literally the
# schema data layer internal/relationships depends on to bootstrap its own
# Terraform extractors, so it was one dependency-hop away from #5596's fix
# without ever gaining coverage itself.
require_workflow_path "code-edge provenance vocabulary (#5538)"   "go/internal/codeprovenance/**"
require_workflow_path "package identity normalization (#5538)"    "go/internal/packageidentity/**"
require_workflow_path "repository identity normalization (#5538)" "go/internal/repositoryidentity/**"
require_workflow_path "source_tool edge vocabulary (#5538)"       "go/internal/sourcetool/**"
require_workflow_path "terraform schema data layer (#5538)"      "go/internal/terraformschema/**"

# Bounded reachability check (#5538 review): does any package feed a response
# the B-12 snapshot's query_shapes actually asserts? Checked, not assumed —
# these packages back non-trivial, non-error asserted MCP shapes (real
# required_response_fields, not just an expected_error_contains guard):
# environment backs compare_environments; exposure backs
# trace_exposure_path/dispatch_taint_path/dispatch_reaching_def; doctruth
# backs get_documentation_finding_inventory/list_documentation_findings/etc.;
# serviceintel(+http) backs get_service_intelligence_report. Ask is enabled
# against a credential-free local provider and executes a real bounded tool
# loop, so its engine, wiring, guardrail, and narration-posture packages must
# trigger the same gate.
require_workflow_path "environment alias normalization (#5538)"   "go/internal/environment/**"
require_workflow_path "code-to-cloud exposure taint (#5538)"      "go/internal/exposure/**"
require_workflow_path "documentation truth extraction (#5538)"    "go/internal/doctruth/**"
require_workflow_path "service intelligence report (#5538)"       "go/internal/serviceintel/**"
require_workflow_path "service intelligence HTTP adapter (#5538)" "go/internal/serviceintelhttp/**"
require_workflow_path "Ask reasoning engine" "go/internal/ask/**"
require_workflow_path "Ask runtime wiring" "go/internal/askwiring/**"
require_workflow_path "Ask publish guardrails" "go/internal/answerguardrail/**"
require_workflow_path "Ask narration posture" "go/internal/answernarration/**"

# --- #5538 review follow-up: correcting two wrong exclusions ----------------
# The prior widening round's commit message excluded "reducer correlation" as
# "owned by other lanes or a different capability, not this filter" -- that
# call was wrong: internal/reducer/deployable_unit_correlation.go imports
# internal/correlation (plus its engine/model/rules subpackages) directly, and
# aws_cloud_runtime_drift.go, multi_cloud_runtime_drift.go,
# terraform_config_state_drift.go, and cloud_inventory_admission*.go import
# internal/correlation/{cloudinventory,drift/cloudruntime,drift/multicloud,
# drift/tfconfigstate} directly. rc-1 (CORRELATES_DEPLOYABLE_UNIT, asserted via
# -required-correlations=all) is provably zero without the deployable-unit
# correlation materializer these packages back.
require_workflow_path "deployable-unit + drift correlation (#5538 correction)" "go/internal/correlation/**"

# internal/truth is the canonical truth-contract package (see the ownership
# table in docs/internal/agent-guide.md) imported directly by dozens of
# internal/reducer materialization writers -- including
# kubernetes_namespace_materialization.go, azure_resource_materialization.go,
# and secrets_iam_graph_projection.go -- plus doctruth, relationships, and
# internal/query. KubernetesNamespace node counts, fed by
# kubernetes_namespace_materialization.go, are asserted in the B-12 snapshot's
# node_counts.
require_workflow_path "canonical truth contracts (#5538)" "go/internal/truth/**"

# internal/scope defines IngestionScope/ScopeGeneration, the anchor every
# fact, work item, and graph projection carries; 97 files under
# internal/projector (every *_intents.go) import it directly.
require_workflow_path "ingestion scope + generation anchor (#5538)" "go/internal/scope/**"

# internal/factenvelope is the single collector-SDK-record <-> internal
# envelope <-> factschema decode adapter, imported directly by
# projector/factschema_quarantine.go, projector/schema_version_admission.go,
# and reducer/schemadecode/factschema_decode.go. A fact_kind/version mismatch here is
# silently inert -- the same 0-node-gate-result failure mode
# eshu-golden-corpus-rigor documents for a cassette fact_kind mismatch.
require_workflow_path "fact envelope decode adapter (#5538)" "go/internal/factenvelope/**"

# internal/ghactionsref backs the ReusableWorkflowRepo/ActionRepo/Pinned
# classifiers that relationships/github_actions_evidence.go and
# reducer/crossrepo/cross_repo_evidence_artifacts.go call directly; the
# "github_actions_action_repository" DEPENDS_ON evidence-kind reason those
# classifiers produce is asserted live in the B-12 snapshot's
# content-relationships query shapes.
require_workflow_path "GitHub Actions ref classification (#5538)" "go/internal/ghactionsref/**"

# internal/workflowimage was deliberately left out of the prior widening round
# ("sits adjacent to the container_image_identity lane") -- that call was also
# wrong: collector/git_workflow_image_facts.go imports it directly to emit
# ci.workflow_image_evidence facts, and query/ci_cd_evidence_summary.go
# imports it directly to classify EvidenceClassImageRef/Unresolved/Ambiguous
# into evidence_summary.static_workflow_artifacts.evidence_class, a
# required_response_fields entry on the asserted list_ci_cd_run_correlations
# query shape.
require_workflow_path "GitHub Actions workflow image evidence (#5538 correction)" "go/internal/workflowimage/**"

# The search/semantic family: internal/query/semantic_search.go imports
# searchbench, searchdocs, and searchretrieval directly, and the
# search_semantic_context MCP tool shape (14 required_response_fields) is
# asserted live in the B-12 snapshot. searchbench also imports searchdecay
# directly. searchhybrid/searchrerank/searchembed/searchembedruntime feed
# internal/query/semantic_search_hybrid.go, semantic_search_rerank.go, and
# semantic_search_persisted_vector.go (the same MCP tool's response), plus
# cmd/api and cmd/mcp-server wiring; searchvector feeds
# cmd/reducer/search_vector_build_wiring.go; semanticqueue/semanticpolicy/
# semanticguard/semanticprofile feed the semantic-extraction queue
# (internal/coordinator, internal/storage/postgres) and the same
# mcp-server/api wiring, independent of the ask/answer-narration pipeline that
# stays excluded above. Checked and left OUT: searchbenchrun,
# searchdecaytelemetry, searchnornicdb, searchpostgres, and semanticdocs do
# not appear in `go list -deps` for any of the six binaries this gate builds.
require_workflow_path "search retrieval + doc scoring (#5538)" "go/internal/searchretrieval/**"
require_workflow_path "search document model (#5538)"          "go/internal/searchdocs/**"
require_workflow_path "search benchmark harness (#5538)"       "go/internal/searchbench/**"
require_workflow_path "search recency decay (#5538)"           "go/internal/searchdecay/**"
require_workflow_path "search hybrid fusion (#5538)"            "go/internal/searchhybrid/**"
require_workflow_path "search rerank (#5538)"                   "go/internal/searchrerank/**"
require_workflow_path "search embedding contract (#5538)"       "go/internal/searchembed/**"
require_workflow_path "search embedding provider (#5538)"       "go/internal/searchembedprovider/**"
require_workflow_path "search embedding runtime wiring (#5538)" "go/internal/searchembedruntime/**"
require_workflow_path "search vector build (#5538)"             "go/internal/searchvector/**"
require_workflow_path "semantic extraction queue (#5538)"       "go/internal/semanticqueue/**"
require_workflow_path "semantic policy (#5538)"                 "go/internal/semanticpolicy/**"
require_workflow_path "semantic guard (#5538)"                  "go/internal/semanticguard/**"
require_workflow_path "semantic profile (#5538)"                "go/internal/semanticprofile/**"

# internal/iacreachability backs query/iac.go's dead-IaC analysis; the
# find_dead_code / find_cross_repo_dead_code MCP shapes pin
# data.analysis.iac_reachability_mode="not_modeled_by_code_dead_code" in the
# B-12 snapshot.
require_workflow_path "IaC reachability analysis (#5538)" "go/internal/iacreachability/**"

# internal/rubycontroller backs the shared dead-code query filter --
# query/content_reader_dead_code.go uses rubycontroller.VerdictDowngraded
# directly as a SQL argument -- that find_dead_code/investigate_dead_code/
# find_cross_repo_dead_code all share regardless of language, plus the
# parser/ruby and reducer dead-code-root verdict engine those MCP shapes are
# asserted live against.
require_workflow_path "dead-code verdict engine (#5538)" "go/internal/rubycontroller/**"

# --- #5538 second review round: capability/status/version surface ----------
# Independently re-derived reachability with `go list -deps` over the same
# six binaries and confirmed five more packages directly feed a
# required_response_fields entry the B-12 snapshot asserts live.

# query/capabilities.go imports capabilitycatalog directly and calls
# capabilitycatalog.Load() to build the get_capability_catalog.capabilities
# response; mcp/read_surface_factkind.go imports it directly and calls
# capabilitycatalog.LoadSurfaceInventory() for get_surface_inventory.surfaces.
# Both calls are unconditional (no env gate), unlike the excluded component
# registry readback below.
require_workflow_path "capability + surface catalog (#5538)" "go/internal/capabilitycatalog/**"

# query/collector_extraction_readiness.go imports extraction directly; the
# handler's own doc comment states it "reads no runtime, graph, or registry
# state" -- a static catalog read, unconditional -- backing
# list_collector_extraction_readiness and get_collector_extraction_readiness.
require_workflow_path "collector extraction readiness catalog (#5538)" "go/internal/extraction/**"

# query/status_governance.go imports governanceaudit directly and calls
# GovernanceAudit.Summary() to fill get_hosted_governance_status.audit.
# cmd/api/wiring_router.go and cmd/mcp-server/wiring_router.go both wire a
# real store (newGovernanceAuditStore / pgstatus.NewGovernanceAuditStore)
# unconditionally -- no opt-in flag gates it, unlike governanceauditasync
# (excluded below: that async sink only fires behind a scoped-token/OIDC-bearer
# read this gate's static ESHU_API_KEY auth never resolves).
require_workflow_path "governance audit summary (#5538)" "go/internal/governanceaudit/**"

# query/status_governance.go, status.go, status_collectors.go, and
# status_operations.go all import internal/status directly; status.go's
# getGovernanceStatus unconditionally calls status.BuildReport/LoadReport to
# fill get_hosted_governance_status.readiness/semantic/aggregates, and the
# same package backs get_ingester_status's and get_index_status's
# coordinator/queue/scope_activity fields.
require_workflow_path "pipeline status readback (#5538)" "go/internal/status/**"

# buildinfo.AppVersion() is called directly from status.go and
# status_operations.go to fill the "version" field required on
# list_collectors, list_ingesters, get_ingester_status, get_index_status,
# get_hosted_readiness, get_capability_catalog, and get_surface_inventory.
require_workflow_path "build version stamp (#5538)" "go/internal/buildinfo/**"

# internal/component was checked again in this round and is DELIBERATELY LEFT
# OUT -- re-verification shows the prior exclusion was correct here (unlike
# "reducer correlation" and workflowimage, which 6a6944fbad's message got
# wrong). Both asserted MCP shapes
# (list_component_extensions/get_component_extension_diagnostics) pin
# expected_error_contains "component extension registry is unavailable",
# which query/component_extensions.go's readbackOrUnavailable produces on its
# `ComponentHome == ""` branch -- BEFORE ever calling
# component.NewRegistry(...).Readback(...). ESHU_COMPONENT_HOME is never set
# by verify-golden-corpus-gate.sh or docker-compose.yaml, so the real registry
# code is unreachable dead code for this gate. internal/coordinator's
# ComponentExtensionWorkPlanner is equally dead: it only plans work for a
# configured component-extension collector instance, and none exists in this
# corpus. See scripts/lib/golden-corpus-filter-exclusions.txt.

# --- #5877 review: two more wrong exclusions found by the exhaustiveness
# audit's own exclusion re-check --------------------------------------------
# internal/boundedset.DedupeSortCap is called directly and unconditionally by
# the already-covered internal/query/sbom_attestation_attachment_rows.go and
# internal/reducer/sbom_attestation_attachment_evidence_bounds.go to compute
# attachments[].component_count, attachments[].component_evidence_truncated,
# and attachments[].component_evidence[].{component_id,purl} -- all asserted
# response fields. It was wrongly filed as "not itself a fact/graph/query
# producer"; it is exactly that, just a shared helper rather than a
# fact-specific one.
require_workflow_path "SBOM attachment dedupe/sort/cap (#5877 correction)" "go/internal/boundedset/**"

# internal/tfstatewarning was excluded as "not one of the 9 credentialed
# collectors this gate replays via cassette" -- factually wrong, terraform-state
# IS one of the cassette-replayed collectors (testdata/cassettes/terraformstate/
# supply-chain-demo.json). internal/status/tfstate.go (already-covered
# internal/status) unconditionally calls tfstatewarning.Classify for every
# recent warning row to build get_index_status's required "terraform_state"
# field (status.go's GroupTerraformStateWarningsByKind/
# SummarizeTerraformStateWarnings). The current cassette records zero
# warning_fact rows, so Classify runs zero times against real data in today's
# corpus -- but the call site is unconditional production code, not gated by
# an env flag or a ComponentHome-style short-circuit, so a future cassette
# change (or a bug in the classification itself once warnings exist) must not
# ship invisibly. Cover it rather than lean on today's empty-corpus accident.
require_workflow_path "terraform-state warning classification (#5877 correction)" "go/internal/tfstatewarning/**"

# --- #6119: URL credential redaction ----------------------------------------
# internal/urlredact is the first redaction package this filter covers, and the
# reason it is covered while internal/redact stays excluded is worth stating
# plainly, because the two look interchangeable from the package name alone.
# internal/redact sanitizes LIVE collector output, and cassette replay bypasses
# live collection entirely (see scripts/lib/golden-corpus-filter-exclusions.txt).
# internal/urlredact is reached on the STATIC-PARSE path instead, which this gate
# really does run: internal/collector/git_service_catalog_facts.go parses
# catalog-info.yaml out of the repo corpus and calls
# servicecatalog.BackstageManifestEnvelopes, and that package's
# stripSensitiveURL/isSafeURL/redactSensitiveText each call
# urlredact.CarriesUserinfo (internal/collector/servicecatalog/envelope.go).
# The corpus carries a matching fixture --
# tests/fixtures/ecosystems/deployable-config/catalog-info.yaml, listed in
# scripts/lib/golden-corpus-fixtures.sh -- so the call site is live here, not
# hypothetical. Widen or narrow what CarriesUserinfo treats as credential-bearing
# and the service-catalog facts this gate projects move with it.
require_workflow_path "URL credential redaction (#6119)" "go/internal/urlredact/**"
