package competitiveparity

import "testing"

func TestValidatePassesCompleteInventory(t *testing.T) {
	report := Validate(completeInventory(), defaultExpectationsForTest())
	if !report.Pass {
		t.Fatalf("report.Pass = false, want true: %#v", report.Surfaces)
	}
	if got, want := len(report.Surfaces), 4; got != want {
		t.Fatalf("len(report.Surfaces) = %d, want %d", got, want)
	}
	if !surfaceHasResidual(report, "investigation_evidence_packet", 3238) {
		t.Fatalf("investigation_evidence_packet residuals = %#v, want #3238", report.Surfaces)
	}
	if !surfaceHasRelatedIssue(report, "capability_catalog", 2715) {
		t.Fatalf("capability_catalog related issues = %#v, want #2715", report.Surfaces)
	}
}

func TestValidateFailsMissingSurfaceEvidence(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Inventory)
		want    CheckKind
		target  string
		surface string
	}{
		{
			name: "missing CLI command",
			mutate: func(inv *Inventory) {
				inv.Commands = without(inv.Commands, "report")
			},
			want:    CheckCLICommand,
			target:  "report",
			surface: "operator_digest",
		},
		{
			name: "missing API route",
			mutate: func(inv *Inventory) {
				inv.APIRoutes = without(inv.APIRoutes, "GET /api/v0/capabilities")
			},
			want:    CheckAPIRoute,
			target:  "GET /api/v0/capabilities",
			surface: "capability_catalog",
		},
		{
			name: "missing MCP tool",
			mutate: func(inv *Inventory) {
				inv.MCPTools = without(inv.MCPTools, "get_capability_catalog")
			},
			want:    CheckMCPTool,
			target:  "get_capability_catalog",
			surface: "capability_catalog",
		},
		{
			name: "missing console surface",
			mutate: func(inv *Inventory) {
				inv.ConsolePages = without(inv.ConsolePages, "CapabilityMatrixPage")
			},
			want:    CheckConsolePage,
			target:  "CapabilityMatrixPage",
			surface: "capability_catalog",
		},
		{
			name: "stale docs claim",
			mutate: func(inv *Inventory) {
				delete(inv.Docs, "docs/public/reference/capability-catalog.md")
			},
			want:    CheckDoc,
			target:  "docs/public/reference/capability-catalog.md",
			surface: "capability_catalog",
		},
		{
			name: "mismatched truth label",
			mutate: func(inv *Inventory) {
				inv.Docs["docs/public/reference/investigation-evidence-packet.md"] = "portable packet without the expected missing evidence label"
			},
			want:    CheckTruthLabel,
			target:  "missing_evidence",
			surface: "investigation_evidence_packet",
		},
		{
			name: "failed exercise",
			mutate: func(inv *Inventory) {
				inv.Exercises = replaceExercise(inv.Exercises, ExerciseResult{
					ID:     "operator_digest_artifact",
					OK:     false,
					Detail: "artifact validation failed",
				})
			},
			want:    CheckExercise,
			target:  "operator_digest_artifact",
			surface: "operator_digest",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inv := completeInventory()
			tt.mutate(&inv)
			report := Validate(inv, defaultExpectationsForTest())
			if report.Pass {
				t.Fatal("report.Pass = true, want false")
			}
			if !hasFailedCheck(report, tt.surface, tt.want, tt.target) {
				t.Fatalf("missing failed check surface=%s kind=%s target=%s in %#v", tt.surface, tt.want, tt.target, report.Surfaces)
			}
		})
	}
}

func completeInventory() Inventory {
	return Inventory{
		Commands: []string{
			"first-run",
			"first-run report",
			"report",
			"investigation",
			"investigation export",
			"evidence-packet-dogfood",
		},
		APIRoutes: []string{
			"GET /api/v0/capabilities",
			"GET /api/v0/surface-inventory",
			"GET /api/v0/investigation-workflows",
			"POST /api/v0/investigation-workflows/resolve",
		},
		MCPTools: []string{
			"get_capability_catalog",
			"list_investigation_workflows",
			"resolve_investigation_workflow",
		},
		ConsolePages: []string{
			"CapabilityMatrixPage",
			"SurfaceInventoryPage",
			"ServiceReportPage",
		},
		Docs: map[string]string{
			"docs/public/reference/first-run-evidence.md":            "first-run-evidence redacted truth",
			"docs/public/reference/operator-digest.md":               "operator_digest.v1 unsupported truth",
			"docs/public/reference/investigation-evidence-packet.md": "investigation_evidence_packet.v2 missing_evidence source-backed",
			"docs/public/reference/capability-catalog.md":            "capability catalog general_availability implemented maturity",
		},
		Exercises: []ExerciseResult{
			{ID: "first_run_report_artifact", OK: true, Detail: "rendered"},
			{ID: "operator_digest_artifact", OK: true, Detail: "validated"},
			{ID: "investigation_evidence_packet_artifact", OK: true, Detail: "rendered"},
			{ID: "evidence_packet_dogfood_fixture", OK: true, Detail: "scored"},
			{ID: "capability_catalog_artifacts", OK: true, Detail: "decoded"},
		},
	}
}

func defaultExpectationsForTest() []Expectation {
	return DefaultExpectations()
}

func without(values []string, remove string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value != remove {
			out = append(out, value)
		}
	}
	return out
}

func hasFailedCheck(report Report, surface string, kind CheckKind, target string) bool {
	for _, result := range report.Surfaces {
		if result.ID != surface {
			continue
		}
		for _, check := range result.Checks {
			if check.Kind == kind && check.Target == target && check.Status == CheckFail {
				return true
			}
		}
	}
	return false
}

func surfaceHasResidual(report Report, surface string, issue int) bool {
	for _, result := range report.Surfaces {
		if result.ID != surface {
			continue
		}
		for _, residual := range result.ResidualIssues {
			if residual.Number == issue {
				return true
			}
		}
	}
	return false
}

func surfaceHasRelatedIssue(report Report, surface string, issue int) bool {
	for _, result := range report.Surfaces {
		if result.ID != surface {
			continue
		}
		for _, related := range result.RelatedIssues {
			if related.Number == issue {
				return true
			}
		}
	}
	return false
}

func replaceExercise(results []ExerciseResult, replacement ExerciseResult) []ExerciseResult {
	out := make([]ExerciseResult, 0, len(results)+1)
	replaced := false
	for _, result := range results {
		if result.ID == replacement.ID {
			out = append(out, replacement)
			replaced = true
			continue
		}
		out = append(out, result)
	}
	if !replaced {
		out = append(out, replacement)
	}
	return out
}
