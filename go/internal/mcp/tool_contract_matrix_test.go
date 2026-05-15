package mcp

import (
	"os"
	"strings"
	"testing"
)

func TestMCPToolContractMatrixCoversReadOnlyTools(t *testing.T) {
	t.Parallel()

	markdown, err := os.ReadFile("../../../docs/docs/reference/mcp-tool-contract-matrix.md")
	if err != nil {
		t.Fatalf("read MCP tool contract matrix: %v", err)
	}
	content := string(markdown)
	for _, tool := range ReadOnlyTools() {
		rowMarker := "| `" + tool.Name + "` |"
		if !strings.Contains(content, rowMarker) {
			t.Fatalf("contract matrix missing row for %s", tool.Name)
		}
	}
}

func TestMCPPromptEpicDocsDoNotAdvertiseClosedGaps(t *testing.T) {
	t.Parallel()

	staleClaims := map[string][]string{
		"../../../docs/docs/reference/mcp-tool-contract-matrix.md": {
			"class hierarchy/overrides remain tracked by #291",
		},
		"../../../docs/docs/reference/mcp-prompt-surface-audit.md": {
			"| Recursive and hub-function prompts | None yet | Tracked by #360 |",
			"Keep recursive and hub-function prompts quarantined to #360",
		},
		"../../../docs/docs/adrs/2026-05-14-mcp-tool-contract-performance-audit.md": {
			"| Cross-repo service story, onboarding, runbooks | `get_service_story`, `investigate_service` | One-call dossier path from #284; keep using story first | #285 parent epic |",
			"| Symbol discovery and implementation lookup | `find_symbol`, `find_code`, `execute_language_query` | First-class definition lookup is bounded, paged, source-handle backed, and no longer requires raw Cypher for \"where is this implemented?\" prompts | #287 |",
			"| Broad code-topic and implementation investigation | `investigate_code_topic` | First-class content-index investigation returns ranked files, symbols, searched terms, coverage, truncation, and source/relationship follow-up handles without raw Cypher or client-side term guessing | #286 |",
			"| Callers, callees, imports, call chains | `get_code_relationship_story`, `find_function_call_chain`, `investigate_import_dependencies`, `inspect_call_graph_metrics` | Relationship story is bounded, ambiguity-aware, entity-anchored, paged, and exposes optional bounded transitive CALLS traversal; call-chain keeps the dedicated endpoint; import/dependency prompts now have file/module scoped graph reads for imports, importers, package imports, direct Python cycles, and cross-module calls; call-graph metrics now cover recursive and hub-function prompts with repo-scoped graph reads | #288 |",
			"| Dead code and code quality | `find_dead_code`, `find_most_complex_functions`, `inspect_code_quality` | Complexity, long-function, high-argument, and refactoring-candidate prompts use first-class bounded tools with source handles and truncation instead of raw Cypher | #289 |",
			"| Security hardcoded secrets | `investigate_hardcoded_secrets` | First-class redacted content-index investigation with severity, confidence, suppression notes, source handles, paging, and truncation | #292 |",
			"| Deployment, GitOps, and resource tracing | `trace_deployment_chain`, `trace_resource_to_code`, story tools | Service story is one-call; low-level trace paths keep existing caps | #293, #294, #295 |",
			"| Environment comparison | `compare_environments` | Scoped workload/environment route now returns a prompt-ready story packet with shared resources, dedicated resources, evidence, limitations, coverage, and exact next calls | #296 |",
			"| Runtime and indexing status prompts | `get_index_status`, `list_ingesters`, `get_ingester_status` | Cookbook status prompts use shipped MCP tools instead of stale job-status names | #297 |",
			"| Documentation/confluence prompts | story routes plus `build_evidence_citation_packet` | Story-first guidance remains; exact source, docs, manifest, and deployment proof uses bounded citation packets from returned handles | #298 |",
			"| Structural code inventory | `inspect_code_inventory` | First-class content-index path covers functions/classes, file-local entities, top-level rows, dataclasses, documented functions, decorated methods, classes with a method, `super()` calls, and function counts per file with source handles and truncation | #362 |",
			"passes and the cookbook links the remaining first-class gap to #362.",
			"Security prompts remain deliberately unsolved by raw Cypher and are tracked in #292.",
		},
	}

	for path, claims := range staleClaims {
		path, claims := path, claims
		t.Run(path, func(t *testing.T) {
			t.Parallel()

			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			content := string(raw)
			for _, claim := range claims {
				if strings.Contains(content, claim) {
					t.Fatalf("%s still advertises closed MCP gap: %s", path, claim)
				}
			}
		})
	}
}
