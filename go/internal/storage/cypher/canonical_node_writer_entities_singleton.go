package cypher

import (
	"fmt"
)

// canonicalEntityRowNeedsSingletonFallback decides whether a canonical entity
// row must be written via a per-row parameterized singleton instead of the
// UNWIND-batched fast path. Historically this returned true when row values
// contained literal Cypher keyword substrings ("shortestpath",
// "allshortestpaths", "remove ") on the theory that NornicDB's Cypher parser
// might confuse them with reserved syntax. The check fired for ~17000 Function
// entities per K8s native run (Kubernetes is a Go codebase with many comments
// and identifiers containing the English word "remove"); each routed row
// became a slow per-statement executeCompoundMatchMerge call and dominated
// canonical_write CPU per the profile in ADR row 1809; the wall-clock
// measurement of removing them is in ADR row 1815.
//
// Per the NornicDB-side regression test
// TestUnwindMergeChainBatch_EshuSingletonFallbackUnnecessary at NornicDB-New
// pkg/cypher/unwind_merge_chain_eshu_canonical_test.go, parameterized UNWIND-
// batched cypher handles those substrings safely: parameters are bound
// separately from cypher text per the Bolt protocol, so parameter values
// containing Cypher keywords never become cypher syntax. Eshu emits all
// canonical entity writes via Bolt parameters (entity_id, file_path,
// generation_id, and the props map), so the parser-confusion theory has no
// remaining surface to defend.
//
// The function and its dispatch infrastructure remain in place as a future
// hook in case a NornicDB regression reintroduces a real parameter-handling
// vulnerability, but it now returns false for every row so the UNWIND-batched
// path stays engaged. Re-introducing a trigger here must be accompanied by an
// updated NornicDB regression guard proving the trigger actually catches
// unsafe behavior.
func canonicalEntityRowNeedsSingletonFallback(label string, row map[string]any) bool {
	_ = label
	_ = row
	return false
}

func canonicalEntitySingletonFallbackMode(label string, row map[string]any) string {
	return PhaseGroupModeExecuteOnly
}

func canonicalEntitySingletonFallbackName(mode string) string {
	if mode == PhaseGroupModeGroupedSingleton {
		return "grouped_singleton"
	}
	return "singleton_parameterized"
}

func canonicalNodeEntitySingletonWithContainmentStatement(
	label string,
	filePath string,
	row map[string]any,
	summary string,
	scopeID string,
	generationID string,
) Statement {
	mode := canonicalEntitySingletonFallbackMode(label, row)
	if summary == "" {
		summary = fmt.Sprintf(
			"label=%s rows=1 entity_id=%v fallback=%s containment=inline",
			label,
			row["entity_id"],
			canonicalEntitySingletonFallbackName(mode),
		)
	}
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    fmt.Sprintf(canonicalNodeEntitySingletonUpsertWithContainmentTemplate, label),
		Parameters: map[string]any{
			"file_path":                        filePath,
			"entity_id":                        row["entity_id"],
			"props":                            row["props"],
			"generation_id":                    row["generation_id"],
			StatementMetadataPhaseKey:          CanonicalPhaseEntities,
			StatementMetadataEntityLabelKey:    label,
			StatementMetadataPhaseGroupModeKey: mode,
			StatementMetadataSummaryKey:        summary,
			StatementMetadataScopeIDKey:        scopeID,
			StatementMetadataGenerationIDKey:   generationID,
		},
	}
}
