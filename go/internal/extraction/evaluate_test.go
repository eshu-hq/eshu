// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extraction

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// metCriteria returns a complete checklist with every criterion met, used as the
// baseline for tests that then flip individual criteria.
func metCriteria() []CriterionResult {
	out := make([]CriterionResult, 0, len(orderedCriteria))
	for _, criterion := range orderedCriteria {
		out = append(out, CriterionResult{Criterion: criterion, State: Met, Detail: "documented"})
	}
	return out
}

// withCriterion returns a copy of the checklist with one criterion overridden.
func withCriterion(base []CriterionResult, criterion Criterion, state State, detail string) []CriterionResult {
	out := make([]CriterionResult, len(base))
	copy(out, base)
	for i := range out {
		if out[i].Criterion == criterion {
			out[i] = CriterionResult{Criterion: criterion, State: state, Detail: detail}
		}
	}
	return out
}

func TestEvaluateClassifications(t *testing.T) {
	naCriteria := make([]CriterionResult, 0, len(orderedCriteria))
	for _, criterion := range orderedCriteria {
		naCriteria = append(naCriteria, CriterionResult{Criterion: criterion, State: NotApplicable, Detail: "core collector"})
	}

	tests := []struct {
		name          string
		profile       Profile
		want          Classification
		wantBlockers  []Criterion
		rationaleHint string
	}{
		{
			name: "core-owned stays keep-in-tree",
			profile: Profile{
				Family:              scope.CollectorGit,
				DisplayName:         "Git",
				CorrelationCritical: true,
				Criteria:            naCriteria,
			},
			want:          KeepInTree,
			rationaleHint: "Correlation-critical",
		},
		{
			name: "correlation-critical wins even with unmet criteria",
			profile: Profile{
				Family:              scope.CollectorTerraformState,
				DisplayName:         "Terraform state",
				CorrelationCritical: true,
				Criteria:            withCriterion(metCriteria(), RuntimeBehavior, Unmet, "no hosted path"),
			},
			want: KeepInTree,
		},
		{
			name: "reference candidate with completed boundary proof",
			profile: Profile{
				Family:                scope.CollectorPagerDuty,
				DisplayName:           "PagerDuty",
				BoundaryProofComplete: true,
				Extracted:             false,
				Criteria:              metCriteria(),
			},
			want:          ExtractionCandidate,
			rationaleHint: "boundary proof is complete",
		},
		{
			name: "candidate without boundary proof",
			profile: Profile{
				Family:      scope.CollectorJira,
				DisplayName: "Jira",
				Criteria:    metCriteria(),
			},
			want:          ExtractionCandidate,
			rationaleHint: "has not been completed",
		},
		{
			name: "blocked by schema when fact contract is unmet",
			profile: Profile{
				Family:      scope.CollectorJira,
				DisplayName: "Jira",
				Criteria:    withCriterion(metCriteria(), FactContract, Unmet, "fact kinds co-evolve with reducer admission"),
			},
			want:          Blocked,
			wantBlockers:  []Criterion{FactContract},
			rationaleHint: "blocked by schema",
		},
		{
			name: "blocked by runtime when hosted behavior is unmet",
			profile: Profile{
				Family:      scope.CollectorGrafana,
				DisplayName: "Grafana",
				Criteria:    withCriterion(metCriteria(), RuntimeBehavior, Unmet, "no bounded hosted path yet"),
			},
			want:          Blocked,
			wantBlockers:  []Criterion{RuntimeBehavior},
			rationaleHint: "blocked by runtime",
		},
		{
			name: "external ready when proven and extracted",
			profile: Profile{
				Family:                scope.CollectorPagerDuty,
				DisplayName:           "PagerDuty",
				BoundaryProofComplete: true,
				Extracted:             true,
				Criteria:              metCriteria(),
			},
			want:          ExternalReady,
			rationaleHint: "runs out of tree",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.profile)
			if got.Classification != tt.want {
				t.Fatalf("Evaluate(%s) classification = %q, want %q", tt.name, got.Classification, tt.want)
			}
			if !got.Classification.Valid() {
				t.Fatalf("Evaluate(%s) produced invalid classification %q", tt.name, got.Classification)
			}
			if len(got.Criteria) != len(orderedCriteria) {
				t.Fatalf("Evaluate(%s) returned %d criteria, want %d", tt.name, len(got.Criteria), len(orderedCriteria))
			}
			for i, criterion := range orderedCriteria {
				if got.Criteria[i].Criterion != criterion {
					t.Fatalf("Evaluate(%s) criterion[%d] = %q, want canonical order %q", tt.name, i, got.Criteria[i].Criterion, criterion)
				}
			}
			if tt.want != Blocked && len(got.Blockers) != 0 {
				t.Fatalf("Evaluate(%s) reported blockers for non-blocked classification: %+v", tt.name, got.Blockers)
			}
			if len(tt.wantBlockers) > 0 {
				assertBlockers(t, got.Blockers, tt.wantBlockers)
			}
			if tt.rationaleHint != "" && !strings.Contains(got.Rationale, tt.rationaleHint) {
				t.Fatalf("Evaluate(%s) rationale = %q, want it to contain %q", tt.name, got.Rationale, tt.rationaleHint)
			}
		})
	}
}

func assertBlockers(t *testing.T, got []CriterionResult, want []Criterion) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("blockers = %+v, want criteria %v", got, want)
	}
	for _, criterion := range want {
		found := false
		for _, blocker := range got {
			if blocker.Criterion == criterion {
				if blocker.State != Unmet {
					t.Fatalf("blocker %s state = %q, want unmet", criterion, blocker.State)
				}
				found = true
			}
		}
		if !found {
			t.Fatalf("blockers %+v missing expected criterion %s", got, criterion)
		}
	}
}

func TestEvaluateFailsClosedOnMissingCriterion(t *testing.T) {
	// A profile that omits proof_surface entirely must not silently pass; the
	// missing criterion is treated as unmet so the family is blocked.
	partial := []CriterionResult{
		{Criterion: SourceCoupling, State: Met},
		{Criterion: FactContract, State: Met},
		{Criterion: ScopeGeneration, State: Met},
		{Criterion: TrustBoundary, State: Met},
		{Criterion: RuntimeBehavior, State: Met},
		{Criterion: ReleaseCadence, State: Met},
	}
	got := Evaluate(Profile{Family: scope.CollectorJira, DisplayName: "Jira", Criteria: partial})
	if got.Classification != Blocked {
		t.Fatalf("classification = %q, want blocked when a criterion is missing", got.Classification)
	}
	if len(got.Criteria) != len(orderedCriteria) {
		t.Fatalf("returned %d criteria, want all %d normalized", len(got.Criteria), len(orderedCriteria))
	}
	last := got.Criteria[len(got.Criteria)-1]
	if last.Criterion != ProofSurface || last.State != Unmet {
		t.Fatalf("missing proof_surface = %+v, want unmet proof_surface", last)
	}
}

func TestEvaluateDeduplicatesCriteria(t *testing.T) {
	dupes := append(metCriteria(), CriterionResult{Criterion: ProofSurface, State: Unmet, Detail: "duplicate that must be ignored"})
	got := Evaluate(Profile{Family: scope.CollectorJira, DisplayName: "Jira", BoundaryProofComplete: true, Criteria: dupes})
	if len(got.Criteria) != len(orderedCriteria) {
		t.Fatalf("returned %d criteria, want %d after dedupe", len(got.Criteria), len(orderedCriteria))
	}
	if got.Classification != ExtractionCandidate {
		t.Fatalf("classification = %q, want extraction_candidate (first proof_surface wins)", got.Classification)
	}
}

func TestEvaluateAppendsProfileRationale(t *testing.T) {
	got := Evaluate(Profile{
		Family:                scope.CollectorPagerDuty,
		DisplayName:           "PagerDuty",
		BoundaryProofComplete: true,
		Criteria:              metCriteria(),
		Rationale:             "In-tree path stays the production correlation source.",
	})
	if !strings.Contains(got.Rationale, "In-tree path stays the production correlation source.") {
		t.Fatalf("rationale = %q, want profile rationale appended", got.Rationale)
	}
}

func TestSortReadinessOrder(t *testing.T) {
	rows := []Readiness{
		{Family: "z-blocked", Classification: Blocked},
		{Family: "a-candidate", Classification: ExtractionCandidate},
		{Family: "m-core", Classification: KeepInTree},
		{Family: "b-external", Classification: ExternalReady},
		{Family: "a-core", Classification: KeepInTree},
	}
	SortReadiness(rows)
	wantOrder := []scope.CollectorKind{"a-core", "m-core", "b-external", "a-candidate", "z-blocked"}
	for i, want := range wantOrder {
		if rows[i].Family != want {
			t.Fatalf("SortReadiness order[%d] = %q, want %q", i, rows[i].Family, want)
		}
	}
}
