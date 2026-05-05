package reducer

import "testing"

func TestDeployableUnitRulePackSelectsSupportedFamilyPacks(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		provenance []string
		want       string
	}{
		{
			name:       "docker compose",
			provenance: []string{"docker_compose_runtime"},
			want:       "docker_compose",
		},
		{
			name:       "kustomize",
			provenance: []string{"kustomize_resource"},
			want:       "kustomize",
		},
		{
			name:       "cloudformation",
			provenance: []string{"cloudformation_template"},
			want:       "cloudformation",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			candidate := WorkloadCandidate{
				RepoID:       "repo-1",
				RepoName:     "service-a",
				Provenance:   tt.provenance,
				Confidence:   0.9,
				WorkloadName: "service-a",
			}
			if got := deployableUnitRulePack(candidate).Name; got != tt.want {
				t.Fatalf("deployableUnitRulePack(...).Name = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestDeployableUnitStructuralEvidenceMapsAdditionalSupportedFamilies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		provenance []string
		wantType   string
		wantKey    string
		wantValue  string
	}{
		{
			name:       "docker compose",
			provenance: []string{"docker_compose_runtime"},
			wantType:   "docker_compose",
			wantKey:    "service",
			wantValue:  "service-a",
		},
		{
			name:       "kustomize",
			provenance: []string{"kustomize_resource"},
			wantType:   "kustomize",
			wantKey:    "resource",
			wantValue:  "service-a",
		},
		{
			name:       "cloudformation",
			provenance: []string{"cloudformation_template"},
			wantType:   "cloudformation",
			wantKey:    "stack",
			wantValue:  "service-a",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			candidate := WorkloadCandidate{
				RepoID:       "repo-1",
				RepoName:     "service-a",
				Provenance:   tt.provenance,
				Confidence:   0.9,
				WorkloadName: "service-a",
			}
			evidence := deployableUnitStructuralEvidence(Intent{
				IntentID:     "intent-1",
				SourceSystem: "git",
				ScopeID:      "scope-1",
			}, candidate, "service-a", 0.9)
			if len(evidence) != 1 {
				t.Fatalf("len(deployableUnitStructuralEvidence(...)) = %d, want 1", len(evidence))
			}
			if got := evidence[0].EvidenceType; got != tt.wantType {
				t.Fatalf("EvidenceType = %q, want %q", got, tt.wantType)
			}
			if got := evidence[0].Key; got != tt.wantKey {
				t.Fatalf("Key = %q, want %q", got, tt.wantKey)
			}
			if got := evidence[0].Value; got != tt.wantValue {
				t.Fatalf("Value = %q, want %q", got, tt.wantValue)
			}
		})
	}
}
