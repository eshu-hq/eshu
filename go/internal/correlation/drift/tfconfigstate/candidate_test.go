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

func TestBuildCandidatesAdmitsThroughEngine(t *testing.T) {
	t.Parallel()

	rows := []AddressedRow{
		{
			Address:      "aws_iam_role.svc",
			ResourceType: "aws_iam_role",
			Config:       &ResourceRow{Address: "aws_iam_role.svc", ResourceType: "aws_iam_role"},
		},
	}
	candidates := BuildCandidates(rows, sampleAnchor(), "state_snapshot:s3:hash-1")
	pack := rules.TerraformConfigStateDriftRulePack()
	eval, err := engine.Evaluate(pack, candidates)
	if err != nil {
		t.Fatalf("engine.Evaluate() error = %v", err)
	}
	if len(eval.Results) != 1 {
		t.Fatalf("engine.Evaluate() results = %d, want 1", len(eval.Results))
	}
	if eval.Results[0].Candidate.State != model.CandidateStateAdmitted {
		t.Fatalf(
			"candidate state = %q, want %q",
			eval.Results[0].Candidate.State,
			model.CandidateStateAdmitted,
		)
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
