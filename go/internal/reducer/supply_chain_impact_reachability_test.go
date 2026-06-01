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
