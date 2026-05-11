package tfconfigstate

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/correlation/engine"
	"github.com/eshu-hq/eshu/go/internal/correlation/model"
	"github.com/eshu-hq/eshu/go/internal/correlation/rules"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
)

func sampleAnchor() tfstatebackend.CommitAnchor {
	return tfstatebackend.CommitAnchor{
		RepoID:           "repo-1",
		ScopeID:          "repo:repo-1@abcdef",
		CommitID:         "abcdef",
		CommitObservedAt: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC),
		BackendKind:      "s3",
		LocatorHash:      "hash-1",
	}
}

func TestBuildCandidatesSkipsNonDriftedAddresses(t *testing.T) {
	t.Parallel()

	rows := []AddressedRow{
		{
			Address:      "aws_s3_bucket.app",
			ResourceType: "aws_s3_bucket",
			Config:       &ResourceRow{Address: "aws_s3_bucket.app", ResourceType: "aws_s3_bucket"},
			State:        &ResourceRow{Address: "aws_s3_bucket.app", ResourceType: "aws_s3_bucket"},
		},
	}
	got := BuildCandidates(rows, sampleAnchor(), "state_snapshot:s3:hash-1")
	if len(got) != 0 {
		t.Fatalf("BuildCandidates() = %d candidates, want 0", len(got))
	}
}

func TestBuildCandidatesEmitsOnePerDriftedAddress(t *testing.T) {
	t.Parallel()

	rows := []AddressedRow{
		{
			Address:      "aws_s3_bucket.app",
			ResourceType: "aws_s3_bucket",
			State:        &ResourceRow{Address: "aws_s3_bucket.app", ResourceType: "aws_s3_bucket"},
		},
		{
			Address:      "aws_iam_role.svc",
			ResourceType: "aws_iam_role",
			Config:       &ResourceRow{Address: "aws_iam_role.svc", ResourceType: "aws_iam_role"},
		},
	}
	got := BuildCandidates(rows, sampleAnchor(), "state_snapshot:s3:hash-1")
	if len(got) != 2 {
		t.Fatalf("BuildCandidates() = %d, want 2", len(got))
	}
	if got[0].CorrelationKey != "aws_iam_role.svc" {
		t.Fatalf("BuildCandidates()[0].CorrelationKey = %q, want sorted-by-address first", got[0].CorrelationKey)
	}
	if got[1].CorrelationKey != "aws_s3_bucket.app" {
		t.Fatalf("BuildCandidates()[1].CorrelationKey = %q", got[1].CorrelationKey)
	}
}

func TestBuildCandidatesAttachesAddressAndDriftKindAtoms(t *testing.T) {
	t.Parallel()

	rows := []AddressedRow{
		{
			Address:      "aws_iam_role.svc",
			ResourceType: "aws_iam_role",
			Config:       &ResourceRow{Address: "aws_iam_role.svc", ResourceType: "aws_iam_role"},
		},
	}
	got := BuildCandidates(rows, sampleAnchor(), "state_snapshot:s3:hash-1")
	if len(got) != 1 {
		t.Fatalf("BuildCandidates() = %d, want 1", len(got))
	}
	if err := got[0].Validate(); err != nil {
		t.Fatalf("Candidate.Validate() error = %v", err)
	}

	var addressAtom, kindAtom *model.EvidenceAtom
	for i := range got[0].Evidence {
		atom := &got[0].Evidence[i]
		if atom.EvidenceType == EvidenceTypeDriftAddress {
			addressAtom = atom
		}
		if atom.EvidenceType == EvidenceTypeDriftKind {
			kindAtom = atom
		}
	}
	if addressAtom == nil {
		t.Fatal("missing drift address atom")
	}
	if kindAtom == nil {
		t.Fatal("missing drift kind atom")
	}
	if kindAtom.Value != string(DriftKindAddedInConfig) {
		t.Fatalf("drift kind atom value = %q, want %q", kindAtom.Value, DriftKindAddedInConfig)
	}
}

func TestBuildCandidatesAdmitsThroughEngineAcrossAllDriftKinds(t *testing.T) {
	t.Parallel()

	allowlisted := AllowlistFor("aws_s3_bucket")[0]

	// One AddressedRow per drift kind. The engine.Evaluate path is the only
	// production exerciser of cross-scope Candidate.Validate; this sweep
	// guards against accidental regressions in the admission shape when a
	// new drift kind is added or the cross-scope evidence pattern changes.
	cases := []struct {
		name string
		row  AddressedRow
		want DriftKind
	}{
		{
			name: "added_in_state",
			row: AddressedRow{
				Address:      "aws_s3_bucket.logs",
				ResourceType: "aws_s3_bucket",
				State:        &ResourceRow{Address: "aws_s3_bucket.logs", ResourceType: "aws_s3_bucket"},
			},
			want: DriftKindAddedInState,
		},
		{
			name: "added_in_config",
			row: AddressedRow{
				Address:      "aws_iam_role.svc",
				ResourceType: "aws_iam_role",
				Config:       &ResourceRow{Address: "aws_iam_role.svc", ResourceType: "aws_iam_role"},
			},
			want: DriftKindAddedInConfig,
		},
		{
			name: "attribute_drift",
			row: AddressedRow{
				Address:      "aws_s3_bucket.app",
				ResourceType: "aws_s3_bucket",
				Config: &ResourceRow{
					Address: "aws_s3_bucket.app", ResourceType: "aws_s3_bucket",
					Attributes: map[string]string{allowlisted: "true"},
				},
				State: &ResourceRow{
					Address: "aws_s3_bucket.app", ResourceType: "aws_s3_bucket",
					Attributes: map[string]string{allowlisted: "false"},
				},
			},
			want: DriftKindAttributeDrift,
		},
		{
			name: "removed_from_state",
			row: AddressedRow{
				Address:      "aws_lambda_function.worker",
				ResourceType: "aws_lambda_function",
				Config:       &ResourceRow{Address: "aws_lambda_function.worker", ResourceType: "aws_lambda_function"},
				Prior:        &ResourceRow{Address: "aws_lambda_function.worker", ResourceType: "aws_lambda_function"},
			},
			want: DriftKindRemovedFromState,
		},
		{
			name: "removed_from_config",
			row: AddressedRow{
				Address:      "aws_iam_role.legacy",
				ResourceType: "aws_iam_role",
				State: &ResourceRow{
					Address: "aws_iam_role.legacy", ResourceType: "aws_iam_role",
					PreviouslyDeclaredInConfig: true,
				},
			},
			want: DriftKindRemovedFromConfig,
		},
	}

	pack := rules.TerraformConfigStateDriftRulePack()
	for _, tc := range cases {
		t.Run(string(tc.want), func(t *testing.T) {
			t.Parallel()
			candidates := BuildCandidates([]AddressedRow{tc.row}, sampleAnchor(), "state_snapshot:s3:hash-1")
			if len(candidates) != 1 {
				t.Fatalf("BuildCandidates() = %d, want 1 for drift kind %q (case %q)",
					len(candidates), tc.want, tc.name)
			}
			eval, err := engine.Evaluate(pack, candidates)
			if err != nil {
				t.Fatalf("engine.Evaluate() error = %v", err)
			}
			if len(eval.Results) != 1 {
				t.Fatalf("engine.Evaluate() results = %d, want 1", len(eval.Results))
			}
			if eval.Results[0].Candidate.State != model.CandidateStateAdmitted {
				t.Fatalf(
					"candidate state for drift kind %q = %q, want %q",
					tc.want,
					eval.Results[0].Candidate.State,
					model.CandidateStateAdmitted,
				)
			}
		})
	}
}

func TestBuildCandidatesCarriesCrossScopeEvidence(t *testing.T) {
	t.Parallel()

	rows := []AddressedRow{
		{
			Address:      "aws_s3_bucket.app",
			ResourceType: "aws_s3_bucket",
			Config: &ResourceRow{
				Address: "aws_s3_bucket.app", ResourceType: "aws_s3_bucket",
				Attributes: map[string]string{"versioning.enabled": "true"},
			},
			State: &ResourceRow{
				Address: "aws_s3_bucket.app", ResourceType: "aws_s3_bucket",
				Attributes: map[string]string{"versioning.enabled": "false"},
			},
		},
	}
	anchor := sampleAnchor()
	stateScope := "state_snapshot:s3:hash-1"
	got := BuildCandidates(rows, anchor, stateScope)
	if len(got) != 1 {
		t.Fatalf("BuildCandidates() = %d, want 1", len(got))
	}
	seenScopes := map[string]bool{}
	for _, atom := range got[0].Evidence {
		seenScopes[atom.ScopeID] = true
	}
	if !seenScopes[anchor.ScopeID] {
		t.Fatalf("missing config-side scope %q in atoms", anchor.ScopeID)
	}
	if !seenScopes[stateScope] {
		t.Fatalf("missing state-side scope %q in atoms", stateScope)
	}
}
