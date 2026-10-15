// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

// queryErrorTextLeakCeilings is the per-file ceiling of error-text leak sites
// for TestNoNewQueryErrorTextLeakSites, keyed by path relative to
// go/internal/query. The counts are the guard's own first run on origin/main
// at 862aaa6ef; the admin/ files, #7739's reopen.go too, are converted. A
// file absent from the map must have zero sites: every file under impact/
// (first #7674 slice), iac/ and supply/chain/ (second slice), the small
// packages, package/registry/, and secrets/ (third slice), and codequery/,
// entity/, and querycontract/ (fourth slice), and repository/ (the
// repository slice) was converted and is not listed.
// Lower a ceiling (or delete the entry) when a change removes sites; never
// raise one. The final #7674 slice deletes this map.
var queryErrorTextLeakCeilings = map[string]int{
	"admission_decisions.go":                 2,
	"contentread/selector_error.go":          1,
	"evidence_bundle_live.go":                2,
	"evidence_citation.go":                   2,
	"evidence.go":                            1,
	"graph_entity_inventory.go":              3,
	"images.go":                              1,
	"infra_ecosystem_overview.go":            1,
	"infra_graph_summary_packet.go":          2,
	"infra_relationship_filter.go":           1,
	"infra_resource_aggregates_handler.go":   2,
	"infra.go":                               1,
	"investigation_packet_api_deployable.go": 3,
	"investigation_packet_api_drift.go":      2,
	"investigation_packet_supply_chain.go":   2,
	"operator_control_plane.go":              1,
	"relationships_catalog.go":               2,
	"status_answer_narration.go":             1,
	"status_collector_readiness.go":          1,
	"status_freshness_causality.go":          1,
	"status_governance.go":                   2,
	"status_hosted_readiness.go":             1,
	"status_operations.go":                   2,
	"status_scoped.go":                       1,
	"status_semantic_extraction.go":          1,
	"status.go":                              4,
	"tag_history.go":                         1,
}
