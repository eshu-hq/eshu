// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import "testing"

func TestSupplyChainReachabilityStatesPreserveImpactTruth(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		finding     SupplyChainImpactFinding
		wantState   SupplyChainReachabilityState
		wantSource  string
		wantImpact  SupplyChainImpactStatus
		wantMissing string
	}{
		{
			name: "govulncheck symbol reachability is strongest reachable evidence",
			finding: SupplyChainImpactFinding{
				Ecosystem:           "gomod",
				Status:              SupplyChainImpactAffectedExact,
				RuntimeReachability: string(GoVulnReachabilitySymbolReachable),
			},
			wantState:  SupplyChainReachabilityReachable,
			wantSource: "govulncheck",
			wantImpact: SupplyChainImpactAffectedExact,
		},
		{
			name: "govulncheck not called is explicit but not a clean finding",
			finding: SupplyChainImpactFinding{
				Ecosystem:           "gomod",
				Status:              SupplyChainImpactAffectedExact,
				RuntimeReachability: string(GoVulnReachabilityNotCalled),
			},
			wantState:  SupplyChainReachabilityNotCalled,
			wantSource: "govulncheck",
			wantImpact: SupplyChainImpactAffectedExact,
		},
		{
			name: "package manifest evidence stays unknown reachability",
			finding: SupplyChainImpactFinding{
				Ecosystem:           "npm",
				Status:              SupplyChainImpactAffectedExact,
				RuntimeReachability: "package_manifest",
			},
			wantState:  SupplyChainReachabilityUnknown,
			wantSource: "parser",
			wantImpact: SupplyChainImpactAffectedExact,
		},
		{
			name: "missing Go scanner evidence is explicit missing evidence",
			finding: SupplyChainImpactFinding{
				Ecosystem:        "gomod",
				Status:           SupplyChainImpactAffectedExact,
				MissingEvidence:  []string{"govulncheck call-graph evidence missing"},
				RepositoryID:     "repo://example/go",
				ObservedVersion:  "v1.2.3",
				DependencyScope:  "runtime",
				DirectDependency: boolPtr(true),
			},
			wantState:   SupplyChainReachabilityMissingEvidence,
			wantSource:  "govulncheck",
			wantImpact:  SupplyChainImpactAffectedExact,
			wantMissing: "govulncheck call-graph evidence missing",
		},
		{
			name: "missing Go scanner detail adds the required missing evidence",
			finding: SupplyChainImpactFinding{
				Ecosystem:        "gomod",
				Status:           SupplyChainImpactAffectedExact,
				RepositoryID:     "repo://example/go",
				ObservedVersion:  "v1.2.3",
				DependencyScope:  "runtime",
				DirectDependency: boolPtr(true),
			},
			wantState:   SupplyChainReachabilityMissingEvidence,
			wantSource:  "govulncheck",
			wantImpact:  SupplyChainImpactAffectedExact,
			wantMissing: "govulncheck call-graph evidence missing",
		},
		{
			name: "ecosystem without vulnerability reachability scanner is unavailable",
			finding: SupplyChainImpactFinding{
				Ecosystem:        "pypi",
				Status:           SupplyChainImpactAffectedExact,
				RepositoryID:     "repo://example/python",
				ObservedVersion:  "2.0.0",
				DependencyScope:  "runtime",
				DirectDependency: boolPtr(true),
			},
			wantState:  SupplyChainReachabilityUnavailable,
			wantSource: "not_available",
			wantImpact: SupplyChainImpactAffectedExact,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := withSupplyChainReachability(tc.finding)
			if got.Status != tc.wantImpact {
				t.Fatalf("Status = %q, want %q", got.Status, tc.wantImpact)
			}
			if got.Reachability.State != tc.wantState {
				t.Fatalf("Reachability.State = %q, want %q", got.Reachability.State, tc.wantState)
			}
			if got.Reachability.Source != tc.wantSource {
				t.Fatalf("Reachability.Source = %q, want %q", got.Reachability.Source, tc.wantSource)
			}
			if tc.wantMissing != "" && !stringSliceContains(got.Reachability.MissingEvidence, tc.wantMissing) {
				t.Fatalf("Reachability.MissingEvidence = %#v, want %q", got.Reachability.MissingEvidence, tc.wantMissing)
			}
		})
	}
}

func TestSupplyChainReachabilityPriorityUsesSignalWithoutChangingImpact(t *testing.T) {
	t.Parallel()

	notCalled := withSupplyChainImpactPriority(withSupplyChainReachability(SupplyChainImpactFinding{
		Ecosystem:           "gomod",
		Status:              SupplyChainImpactAffectedExact,
		CVSSScore:           9.8,
		RuntimeReachability: string(GoVulnReachabilityNotCalled),
		ObservedVersion:     "v1.2.3",
	}))
	if notCalled.Status != SupplyChainImpactAffectedExact {
		t.Fatalf("Status = %q, want affected_exact", notCalled.Status)
	}
	if !stringSliceContains(notCalled.PriorityReasonCodes, "reachability_not_called") {
		t.Fatalf("PriorityReasonCodes = %#v, want reachability_not_called", notCalled.PriorityReasonCodes)
	}

	reachable := withSupplyChainImpactPriority(withSupplyChainReachability(SupplyChainImpactFinding{
		Ecosystem:           "gomod",
		Status:              SupplyChainImpactAffectedExact,
		CVSSScore:           9.8,
		RuntimeReachability: string(GoVulnReachabilitySymbolReachable),
		ObservedVersion:     "v1.2.3",
	}))
	if !stringSliceContains(reachable.PriorityReasonCodes, "reachable_code_evidence") {
		t.Fatalf("PriorityReasonCodes = %#v, want reachable_code_evidence", reachable.PriorityReasonCodes)
	}
	if reachable.Status != SupplyChainImpactAffectedExact {
		t.Fatalf("Status = %q, want affected_exact", reachable.Status)
	}
}

func TestSupplyChainReachabilityLongTailEcosystemPackageEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		finding      SupplyChainImpactFinding
		wantSource   string
		wantEvidence string
		wantMissing  string
	}{
		{
			name: "Composer dependency path is reachable package evidence with PHP blockers",
			finding: SupplyChainImpactFinding{
				Ecosystem:           "composer",
				Status:              SupplyChainImpactAffectedExact,
				RuntimeReachability: "package_manifest",
				DependencyPath:      []string{"symfony/http-kernel"},
				DirectDependency:    boolPtr(true),
			},
			wantSource:   "composer",
			wantEvidence: "composer_dependency_path",
			wantMissing:  "php autoload and dynamic dispatch evidence missing",
		},
		{
			name: "RubyGems dependency path is reachable package evidence with metaprogramming blockers",
			finding: SupplyChainImpactFinding{
				Ecosystem:           "rubygems",
				Status:              SupplyChainImpactAffectedExact,
				RuntimeReachability: "package_manifest",
				DependencyPath:      []string{"rails", "actionpack"},
				DirectDependency:    boolPtr(false),
			},
			wantSource:   "bundler",
			wantEvidence: "bundler_dependency_path",
			wantMissing:  "ruby metaprogramming and autoload evidence missing",
		},
		{
			name: "Cargo dependency path is reachable package evidence with macro cfg blockers",
			finding: SupplyChainImpactFinding{
				Ecosystem:           "cargo",
				Status:              SupplyChainImpactAffectedExact,
				RuntimeReachability: "package_manifest",
				DependencyPath:      []string{"serde", "serde_derive", "proc-macro2"},
				DirectDependency:    boolPtr(false),
			},
			wantSource:   "cargo",
			wantEvidence: "cargo_dependency_path",
			wantMissing:  "rust macro and cfg reachability evidence missing",
		},
		{
			name: "NuGet dependency path is reachable package evidence with dotnet blockers",
			finding: SupplyChainImpactFinding{
				Ecosystem:           "nuget",
				Status:              SupplyChainImpactAffectedExact,
				RuntimeReachability: "package_manifest",
				DependencyPath:      []string{"Newtonsoft.Json"},
				DirectDependency:    boolPtr(true),
			},
			wantSource:   "nuget",
			wantEvidence: "nuget_dependency_path",
			wantMissing:  ".net reflection dependency-injection and generated-code evidence missing",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := withSupplyChainReachability(tc.finding)
			if got.Status != SupplyChainImpactAffectedExact {
				t.Fatalf("Status = %q, want affected_exact", got.Status)
			}
			if got.Reachability.State != SupplyChainReachabilityReachable {
				t.Fatalf("Reachability.State = %q, want reachable", got.Reachability.State)
			}
			if got.Reachability.Confidence != "partial" {
				t.Fatalf("Reachability.Confidence = %q, want partial", got.Reachability.Confidence)
			}
			if got.Reachability.Source != tc.wantSource {
				t.Fatalf("Reachability.Source = %q, want %q", got.Reachability.Source, tc.wantSource)
			}
			if got.Reachability.Evidence != tc.wantEvidence {
				t.Fatalf("Reachability.Evidence = %q, want %q", got.Reachability.Evidence, tc.wantEvidence)
			}
			if got.Reachability.LanguageMaturity != "partial" {
				t.Fatalf("Reachability.LanguageMaturity = %q, want partial", got.Reachability.LanguageMaturity)
			}
			if !stringSliceContains(got.Reachability.MissingEvidence, tc.wantMissing) {
				t.Fatalf("Reachability.MissingEvidence = %#v, want %q", got.Reachability.MissingEvidence, tc.wantMissing)
			}
		})
	}
}

func TestSupplyChainReachabilityLongTailPartialEvidenceStaysMissingEvidence(t *testing.T) {
	t.Parallel()

	got := withSupplyChainReachability(SupplyChainImpactFinding{
		Ecosystem:           "nuget",
		Status:              SupplyChainImpactAffectedExact,
		RuntimeReachability: "package_manifest",
		DependencyPath:      []string{"Legacy.Project", "Newtonsoft.Json"},
		DirectDependency:    boolPtr(false),
		MissingEvidence: []string{
			"msbuild property unresolved: PackageVersion",
			"nuget project-reference resolution missing",
		},
	})

	if got.Reachability.State != SupplyChainReachabilityMissingEvidence {
		t.Fatalf("Reachability.State = %q, want missing_evidence", got.Reachability.State)
	}
	if got.Reachability.Source != "nuget" {
		t.Fatalf("Reachability.Source = %q, want nuget", got.Reachability.Source)
	}
	if got.Reachability.Evidence != "nuget_dependency_path" {
		t.Fatalf("Reachability.Evidence = %q, want nuget_dependency_path", got.Reachability.Evidence)
	}
	for _, want := range []string{
		"msbuild property unresolved: PackageVersion",
		"nuget project-reference resolution missing",
	} {
		if !stringSliceContains(got.Reachability.MissingEvidence, want) {
			t.Fatalf("Reachability.MissingEvidence = %#v, want %q", got.Reachability.MissingEvidence, want)
		}
	}
}

func assertSupplyChainReachability(
	t *testing.T,
	finding SupplyChainImpactFinding,
	state SupplyChainReachabilityState,
	source string,
	evidence string,
) {
	t.Helper()

	if finding.Reachability == nil {
		t.Fatal("Reachability = nil, want envelope")
	}
	if finding.Reachability.State != state {
		t.Fatalf("Reachability.State = %q, want %q", finding.Reachability.State, state)
	}
	if finding.Reachability.Source != source {
		t.Fatalf("Reachability.Source = %q, want %q", finding.Reachability.Source, source)
	}
	if finding.Reachability.Evidence != evidence {
		t.Fatalf("Reachability.Evidence = %q, want %q", finding.Reachability.Evidence, evidence)
	}
}
