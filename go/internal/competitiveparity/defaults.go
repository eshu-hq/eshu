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
			},
			MCPTools: []string{"list_investigation_workflows", "resolve_investigation_workflow"},
			Exercises: []string{
				"investigation_evidence_packet_artifact",
				"evidence_packet_dogfood_fixture",
			},
			Docs: []DocExpectation{{
				Path:       "docs/public/reference/investigation-evidence-packet.md",
				Terms:      []string{"investigation_evidence_packet.v2", "source-backed"},
				TruthTerms: []string{"missing_evidence"},
			}},
			RelatedIssues: []IssueRef{{
				Number: 3139,
				Reason: "Investigation evidence packet contract and shipped families.",
			}, {
				Number: 3143,
				Reason: "Evidence packet dogfood benchmark and scorer.",
			}},
			ResidualIssues: []IssueRef{{
				Number: 3238,
				Reason: "Expose investigation evidence packets through every target surface.",
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
			RelatedIssues: []IssueRef{{
				Number: 2715,
				Reason: "Capability catalog API, MCP, and console surface proof.",
			}},
		},
	}
}
