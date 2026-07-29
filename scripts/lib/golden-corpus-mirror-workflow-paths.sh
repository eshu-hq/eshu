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
require_workflow_path "static ecosystem corpus inputs" "tests/fixtures/ecosystems/**"
# The orchestrator sources these; an edit to the mutex or a fixture/timing lib
# changes what the gate does, so each must trigger it. Without this the lock
# itself was in no trigger list at all - its only test would never have run.
require_workflow_path "golden-corpus libs"             "scripts/lib/golden-corpus-*.sh"
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
# these five back non-trivial, non-error asserted MCP shapes (real
# required_response_fields, not just an expected_error_contains guard):
# environment backs compare_environments; exposure backs
# trace_exposure_path/dispatch_taint_path/dispatch_reaching_def; doctruth
# backs get_documentation_finding_inventory/list_documentation_findings/etc.;
# serviceintel(+http) backs get_service_intelligence_report. (By contrast,
# ask/askwiring/answerguardrail/answernarration were checked and excluded:
# the only asserted "ask" shape pins expected_error_contains "ask is not
# enabled" — that guard lives in the already-covered internal/query package,
# ESHU_ASK_ENABLED is never set in this gate, so the real ask/answer-narration
# pipeline never executes here.)
require_workflow_path "environment alias normalization (#5538)"   "go/internal/environment/**"
require_workflow_path "code-to-cloud exposure taint (#5538)"      "go/internal/exposure/**"
require_workflow_path "documentation truth extraction (#5538)"    "go/internal/doctruth/**"
require_workflow_path "service intelligence report (#5538)"       "go/internal/serviceintel/**"
require_workflow_path "service intelligence HTTP adapter (#5538)" "go/internal/serviceintelhttp/**"
