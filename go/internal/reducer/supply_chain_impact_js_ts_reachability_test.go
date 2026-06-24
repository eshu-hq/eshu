// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildSupplyChainImpactFindingsUsesJSTSPackageAPIReachability(t *testing.T) {
	t.Parallel()

	findings := BuildSupplyChainImpactFindings([]facts.Envelope{
		vulnerabilityCVEFact("cve-js-reach", "CVE-2026-118501", 8.8),
		vulnerabilityAffectedPackageFact(
			"affected-js-reach",
			"CVE-2026-118501",
			"pkg:npm/@scope/vulnerable-api",
			"npm",
			"@scope/vulnerable-api",
			"1.2.3",
			"1.3.0",
		),
		packageConsumptionFactWithChain(
			"consume-js-reach",
			"pkg:npm/@scope/vulnerable-api",
			testImpactRepositoryID,
			"1.2.3",
			[]string{"@scope/vulnerable-api"},
			1,
			true,
		),
		jsTSPackageAPIFileFact(
			"file-js-reach",
			testImpactRepositoryID,
			"src/server.ts",
			"typescript",
			[]map[string]any{{
				"name":   "default",
				"alias":  "vulnerableAPI",
				"source": "@scope/vulnerable-api",
			}},
			[]map[string]any{{
				"name":      "vulnerableAPI",
				"full_name": "vulnerableAPI.createServer",
			}},
			nil,
		),
	})

	got := supplyChainImpactFindingsByCVE(findings)["CVE-2026-118501"]
	assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
	if got.Confidence != "exact" {
		t.Fatalf("Confidence = %q, want exact impact confidence", got.Confidence)
	}
	if got.Reachability == nil {
		t.Fatal("Reachability = nil, want JS/TS parser envelope")
	}
	if got.Reachability.State != SupplyChainReachabilityReachable {
		t.Fatalf("Reachability.State = %q, want reachable", got.Reachability.State)
	}
	if got.Reachability.Source != "parser_js_ts" {
		t.Fatalf("Reachability.Source = %q, want parser_js_ts", got.Reachability.Source)
	}
	if got.Reachability.Confidence != "partial" {
		t.Fatalf("Reachability.Confidence = %q, want partial", got.Reachability.Confidence)
	}
	if !stringSliceContains(got.PriorityReasonCodes, "reachable_code_evidence") {
		t.Fatalf("PriorityReasonCodes = %#v, want reachable_code_evidence", got.PriorityReasonCodes)
	}
}

func TestBuildSupplyChainImpactFindingsKeepsJSTSMissingAndAmbiguousEvidenceExplicit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		cveID        string
		packageID    string
		packageName  string
		files        []facts.Envelope
		wantState    SupplyChainReachabilityState
		wantEvidence string
		wantMissing  string
	}{
		{
			name:         "parser evidence exists but no matching package API identity",
			cveID:        "CVE-2026-118502",
			packageID:    "pkg:npm/vulnerable-api",
			packageName:  "vulnerable-api",
			files:        []facts.Envelope{jsTSPackageAPIFileFact("file-js-negative", testImpactRepositoryID, "src/index.js", "javascript", []map[string]any{{"name": "other", "source": "other-api"}}, nil, nil)},
			wantState:    SupplyChainReachabilityUnknown,
			wantEvidence: "package_api_unknown",
			wantMissing:  "javascript/typescript package API evidence missing",
		},
		{
			name:        "similar unscoped import does not prove scoped package identity",
			cveID:       "CVE-2026-118503",
			packageID:   "pkg:npm/@scope/vulnerable-api",
			packageName: "@scope/vulnerable-api",
			files: []facts.Envelope{jsTSPackageAPIFileFact(
				"file-js-ambiguous",
				testImpactRepositoryID,
				"src/index.ts",
				"typescript",
				[]map[string]any{{"name": "vulnerable", "source": "vulnerable-api"}},
				nil,
				nil,
			)},
			wantState:    SupplyChainReachabilityUnknown,
			wantEvidence: "package_api_ambiguous",
			wantMissing:  "javascript/typescript package API identity ambiguous",
		},
		{
			name:         "no parser or SCIP facts keeps missing evidence visible",
			cveID:        "CVE-2026-118504",
			packageID:    "pkg:npm/vulnerable-api",
			packageName:  "vulnerable-api",
			wantState:    SupplyChainReachabilityMissingEvidence,
			wantEvidence: "package_api_missing_evidence",
			wantMissing:  "javascript/typescript parser or SCIP package API evidence missing",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes := []facts.Envelope{
				vulnerabilityCVEFact("cve-"+tc.cveID, tc.cveID, 7.5),
				vulnerabilityAffectedPackageFact(
					"affected-"+tc.cveID,
					tc.cveID,
					tc.packageID,
					"npm",
					tc.packageName,
					"1.2.3",
					"1.3.0",
				),
				packageConsumptionFactWithChain(
					"consume-"+tc.cveID,
					tc.packageID,
					testImpactRepositoryID,
					"1.2.3",
					[]string{tc.packageName},
					1,
					true,
				),
			}
			envelopes = append(envelopes, tc.files...)

			got := supplyChainImpactFindingsByCVE(BuildSupplyChainImpactFindings(envelopes))[tc.cveID]
			assertSupplyChainImpactStatus(t, got, SupplyChainImpactAffectedExact)
			if got.Reachability == nil {
				t.Fatal("Reachability = nil, want JS/TS parser envelope")
			}
			if got.Reachability.State != tc.wantState {
				t.Fatalf("Reachability.State = %q, want %q", got.Reachability.State, tc.wantState)
			}
			if got.Reachability.Evidence != tc.wantEvidence {
				t.Fatalf("Reachability.Evidence = %q, want %q", got.Reachability.Evidence, tc.wantEvidence)
			}
			if !stringSliceContains(got.Reachability.MissingEvidence, tc.wantMissing) {
				t.Fatalf("Reachability.MissingEvidence = %#v, want %q", got.Reachability.MissingEvidence, tc.wantMissing)
			}
			if stringSliceContains(got.PriorityReasonCodes, "reachability_not_called") {
				t.Fatalf("PriorityReasonCodes = %#v, JS/TS parser evidence must not emit not_called", got.PriorityReasonCodes)
			}
		})
	}
}

func jsTSPackageAPIFileFact(
	factID string,
	repositoryID string,
	relativePath string,
	language string,
	imports []map[string]any,
	calls []map[string]any,
	scipCalls []map[string]any,
) facts.Envelope {
	payload := map[string]any{
		"repo_id":       repositoryID,
		"relative_path": relativePath,
		"parsed_file_data": map[string]any{
			"language":            language,
			"imports":             imports,
			"function_calls":      calls,
			"function_calls_scip": scipCalls,
		},
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: factKindFile,
		ScopeID:  repositoryID,
		Payload:  payload,
	}
}
