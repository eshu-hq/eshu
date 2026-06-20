package competitiveparity

// DefaultExpectations returns the #3265 parity surface set.
func DefaultExpectations() []Expectation {
	return []Expectation{
		{
			ID:           "first_run_report",
			DisplayName:  "First-run report",
			PeerBaseline: "graphify-style report readability",
			Commands:     []string{"first-run", "first-run report"},
			Exercises:    []string{"first_run_report_artifact"},
			Docs: []DocExpectation{{
				Path:       "docs/public/reference/first-run-evidence.md",
				Terms:      []string{"first-run-evidence", "redacted"},
				TruthTerms: []string{"truth"},
			}},
			Quality: qualityExpectations("docs/public/reference/first-run-evidence.md", map[QualityDimensionID][]string{
				QualityDimensionActionability:        {"Next commands", "Recommended follow-ups"},
				QualityDimensionEvidenceClarity:      {"Missing evidence", "complete", "partial", "stale", "failed"},
				QualityDimensionReproducibility:      {"first-run report", "--from", "--format json"},
				QualityDimensionReaderUsefulness:     {"Outcome", "Runtime shape", "First query", "Diagnosis"},
				QualityDimensionPeerBaselineCoverage: {"human-readable onboarding artifact", "support/debug packet"},
			}),
			RelatedIssues: []IssueRef{{
				Number: 1770,
				Reason: "First-run report and onboarding evidence proof.",
			}},
		},
		{
			ID:           "operator_digest",
			DisplayName:  "Operator digest artifact",
			PeerBaseline: "graphify-style report readability",
			Commands:     []string{"report"},
			ConsolePages: []string{"ServiceReportPage"},
			Exercises:    []string{"operator_digest_artifact"},
			Docs: []DocExpectation{{
				Path:       "docs/public/reference/operator-digest.md",
				Terms:      []string{"operator_digest.v1", "unsupported"},
				TruthTerms: []string{"truth"},
			}},
			Quality: qualityExpectations("docs/public/reference/operator-digest.md", map[QualityDimensionID][]string{
				QualityDimensionActionability:        {"suggested_questions", "next_calls", "question_limit"},
				QualityDimensionEvidenceClarity:      {"missing evidence", "truncation", "unsupported", "stale"},
				QualityDimensionReproducibility:      {"operator_digest.v1", "source_refs", "route names", "MCP tool"},
				QualityDimensionReaderUsefulness:     {"what Eshu knows", "what it does not know", "asking next"},
				QualityDimensionPeerBaselineCoverage: {"first-five-minutes onboarding", "incident handoff"},
			}),
			RelatedIssues: []IssueRef{{
				Number: 2455,
				Reason: "Operator digest report and artifact proof.",
			}},
		},
		{
			ID:           "investigation_evidence_packet",
			DisplayName:  "Investigation evidence packet",
			PeerBaseline: "CodeGraphContext-style portable artifact usability",
			Commands:     []string{"investigation export", "evidence-packet-dogfood"},
			APIRoutes: []string{
				"GET /api/v0/investigation-workflows",
				"POST /api/v0/investigation-workflows/resolve",
				"GET /api/v0/investigations/supply-chain/impact/packet",
				"GET /api/v0/investigations/deployable-unit/packet",
				"GET /api/v0/investigations/drift/packet",
			},
			MCPTools: []string{
				"list_investigation_workflows",
				"resolve_investigation_workflow",
				"export_supply_chain_impact_packet",
				"export_deployable_unit_packet",
				"export_cloud_runtime_drift_packet",
			},
			ConsolePages: []string{"VulnDetailPage", "ImpactPage", "CloudDriftPage"},
			Exercises: []string{
				"investigation_evidence_packet_artifact",
				"evidence_packet_dogfood_fixture",
			},
			Docs: []DocExpectation{{
				Path:       "docs/public/reference/investigation-evidence-packet.md",
				Terms:      []string{"investigation_evidence_packet.v2", "source-backed"},
				TruthTerms: []string{"missing_evidence"},
			}},
			Quality: qualityExpectations("docs/public/reference/investigation-evidence-packet.md", map[QualityDimensionID][]string{
				QualityDimensionActionability:        {"reproduce", "exact route/tool/command"},
				QualityDimensionEvidenceClarity:      {"missing_evidence", "partial", "truncated", "unsupported_reasons"},
				QualityDimensionReproducibility:      {"investigation_evidence_packet.v2", "packet_id", "source_fact_ids", "citation handles"},
				QualityDimensionReaderUsefulness:     {"what was observed", "what the reducer decided", "graph answers"},
				QualityDimensionPeerBaselineCoverage: {"instant local artifact", "portable"},
			}),
			RelatedIssues: []IssueRef{{
				Number: 3139,
				Reason: "Investigation evidence packet contract and shipped families.",
			}, {
				Number: 3143,
				Reason: "Evidence packet dogfood benchmark and scorer.",
			}, {
				Number: 3238,
				Reason: "Packet API, MCP, and console surfaces shipped across every target surface.",
			}},
		},
		{
			ID:           "capability_catalog",
			DisplayName:  "Capability catalog surfaces",
			PeerBaseline: "GitNexus-style agent workflow discoverability",
			APIRoutes:    []string{"GET /api/v0/capabilities", "GET /api/v0/surface-inventory"},
			MCPTools:     []string{"get_capability_catalog"},
			ConsolePages: []string{"CapabilityMatrixPage", "SurfaceInventoryPage"},
			Exercises:    []string{"capability_catalog_artifacts"},
			Docs: []DocExpectation{{
				Path:       "docs/public/reference/capability-catalog.md",
				Terms:      []string{"capability catalog", "general_availability", "implemented"},
				TruthTerms: []string{"maturity"},
			}},
			Quality: qualityExpectations("docs/public/reference/capability-catalog.md", map[QualityDimensionID][]string{
				QualityDimensionActionability:        {"known_gaps", "linked_issues"},
				QualityDimensionEvidenceClarity:      {"proof_signals", "maturity", "stale overlay entries"},
				QualityDimensionReproducibility:      {"generated to", "go run ./cmd/capability-inventory", "GET /api/v0/capabilities"},
				QualityDimensionReaderUsefulness:     {"what gaps remain", "where it is exposed", "what proves it"},
				QualityDimensionPeerBaselineCoverage: {"MCP tool", "Console", "prompt-ready"},
			}),
			RelatedIssues: []IssueRef{{
				Number: 2715,
				Reason: "Capability catalog API, MCP, and console surface proof.",
			}},
		},
	}
}

func qualityExpectations(path string, termsByDimension map[QualityDimensionID][]string) []QualityExpectation {
	order := []QualityDimensionID{
		QualityDimensionActionability,
		QualityDimensionEvidenceClarity,
		QualityDimensionReproducibility,
		QualityDimensionReaderUsefulness,
		QualityDimensionPeerBaselineCoverage,
	}
	displayNames := map[QualityDimensionID]string{
		QualityDimensionActionability:        "Actionability",
		QualityDimensionEvidenceClarity:      "Evidence clarity",
		QualityDimensionReproducibility:      "Reproducibility",
		QualityDimensionReaderUsefulness:     "Reader usefulness",
		QualityDimensionPeerBaselineCoverage: "Peer-baseline coverage",
	}
	out := make([]QualityExpectation, 0, len(order))
	for _, dimension := range order {
		terms := termsByDimension[dimension]
		signals := make([]QualitySignal, 0, len(terms))
		for _, term := range terms {
			signals = append(signals, QualitySignal{SourcePath: path, Term: term})
		}
		out = append(out, QualityExpectation{
			Dimension:   dimension,
			DisplayName: displayNames[dimension],
			Signals:     signals,
		})
	}
	return out
}
