// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/sdk/go/factschema"
	reducerderivedv1 "github.com/eshu-hq/eshu/sdk/go/factschema/reducerderived/v1"
)

// TestAWSCloudRuntimeDriftFindingStoreRoundTripsRealEncodedPayload is the
// #5759 follow-up P1-2 regression: a hostile review found
// TestAWSCloudRuntimeDriftRowToNeutralMapsEveryField (go/internal/query)
// hand-built a postgres.AWSCloudRuntimeDriftFindingRow with
// MatchedTerraformConfigFile/ModulePath, MatchedOtherIaCSource,
// ServiceCandidates, EnvironmentCandidates, and DependencyPaths set -- a
// shape the real encode path can never produce, since
// reducerderivedv1.AWSCloudRuntimeDriftFinding has no fields for them and
// factschema.EncodeReducerAWSCloudRuntimeDriftFinding (the SAME function
// go/internal/reducer/awscloud/aws_cloud_runtime_drift_writer.go calls) cannot emit
// what was never in the typed struct. That test was a false green: it
// proved struct-copy correctness for a feature that can never fire.
//
// This test goes through the REAL path instead: build the real typed
// struct (which has no fields for the six values -- they are not just unset,
// they are unsettable here), encode it with the real writer function,
// marshal it exactly as the writer does, and decode it back through
// AWSCloudRuntimeDriftFindingStore.ListActiveFindings (the real read path).
// It asserts every field the writer DOES populate round-trips correctly, and
// that the six fields decode to their Go zero value -- not because a mapper
// dropped them, but because no real payload ever carries them. Both
// go/internal/query's MultiCloudRuntimeDriftFindingRow (the provider-neutral
// aggregate row) and IaCManagementFindingRow (the AWS-specific row) removed/
// never gained fields for the same reason.
func TestAWSCloudRuntimeDriftFindingStoreRoundTripsRealEncodedPayload(t *testing.T) {
	t.Parallel()

	source := reducerderivedv1.AWSCloudRuntimeDriftFinding{
		ReducerDomain:                "aws_cloud_runtime_drift",
		IntentID:                     "intent:aws-lambda-1",
		ScopeID:                      "aws:123456789012:us-east-1:lambda",
		GenerationID:                 "generation:aws-1",
		SourceSystem:                 "aws",
		Cause:                        "aws runtime resource facts observed",
		CanonicalID:                  "canonical:aws_cloud_runtime_drift:arn:aws:lambda:us-east-1:123456789012:function:payments-api:unmanaged_cloud_resource",
		CandidateID:                  "candidate:lambda:payments-api",
		CandidateKind:                "aws_cloud_runtime_drift",
		ARN:                          "arn:aws:lambda:us-east-1:123456789012:function:payments-api",
		FindingKind:                  "unmanaged_cloud_resource",
		ManagementStatus:             "terraform_state_only",
		Confidence:                   0.92,
		CandidateState:               "admitted",
		MatchedTerraformStateAddress: "module.app.aws_lambda_function.payments",
		MissingEvidence:              []string{"terraform_config_resource"},
		WarningFlags:                 []string{"stale_iac_evidence"},
		RecommendedAction:            "restore_config_or_prepare_import_block",
		Evidence:                     []map[string]any{{"id": "evidence:state", "key": "resource_address", "value": "module.app.aws_lambda_function.payments"}},
		PublicationFactKind:          AWSCloudRuntimeDriftFindingFactKind,
	}

	payload, err := factschema.EncodeReducerAWSCloudRuntimeDriftFinding(source)
	if err != nil {
		t.Fatalf("EncodeReducerAWSCloudRuntimeDriftFinding() error = %v, want nil", err)
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal encoded payload: %v", err)
	}

	observedAt := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	db := &fakeExecQueryer{
		queryResponses: []queueFakeRows{
			{rows: [][]any{{
				"fact:aws-lambda-1",
				source.ScopeID,
				source.GenerationID,
				source.SourceSystem,
				observedAt,
				payloadJSON,
			}}},
		},
	}
	store := NewAWSCloudRuntimeDriftFindingStore(db)

	rows, err := store.ListActiveFindings(context.Background(), AWSCloudRuntimeDriftFindingFilter{
		ScopeID: source.ScopeID,
		Limit:   25,
	})
	if err != nil {
		t.Fatalf("ListActiveFindings() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	row := rows[0]

	// Fields the real writer populates must round-trip exactly.
	if got, want := row.ARN, source.ARN; got != want {
		t.Errorf("row.ARN = %q, want %q", got, want)
	}
	if got, want := row.FindingKind, source.FindingKind; got != want {
		t.Errorf("row.FindingKind = %q, want %q", got, want)
	}
	if got, want := row.ManagementStatus, source.ManagementStatus; got != want {
		t.Errorf("row.ManagementStatus = %q, want %q", got, want)
	}
	if got, want := row.MatchedTerraformStateAddress, source.MatchedTerraformStateAddress; got != want {
		t.Errorf("row.MatchedTerraformStateAddress = %q, want %q", got, want)
	}
	if got, want := row.CandidateID, source.CandidateID; got != want {
		t.Errorf("row.CandidateID = %q, want %q", got, want)
	}
	if got, want := len(row.Evidence), 1; got != want {
		t.Fatalf("len(row.Evidence) = %d, want %d", got, want)
	}

	// The six fields no real payload can ever carry must decode to their Go
	// zero value on the real path -- not because this test forces it, but
	// because reducerderivedv1.AWSCloudRuntimeDriftFinding has no field to
	// set them from in the first place.
	if row.MatchedTerraformConfigFile != "" {
		t.Errorf("row.MatchedTerraformConfigFile = %q, want empty (no real producer)", row.MatchedTerraformConfigFile)
	}
	if row.MatchedTerraformModulePath != "" {
		t.Errorf("row.MatchedTerraformModulePath = %q, want empty (no real producer)", row.MatchedTerraformModulePath)
	}
	if row.MatchedOtherIaCSource != "" {
		t.Errorf("row.MatchedOtherIaCSource = %q, want empty (no real producer)", row.MatchedOtherIaCSource)
	}
	if len(row.ServiceCandidates) != 0 {
		t.Errorf("row.ServiceCandidates = %#v, want empty (no real producer)", row.ServiceCandidates)
	}
	if len(row.EnvironmentCandidates) != 0 {
		t.Errorf("row.EnvironmentCandidates = %#v, want empty (no real producer)", row.EnvironmentCandidates)
	}
	if len(row.DependencyPaths) != 0 {
		t.Errorf("row.DependencyPaths = %#v, want empty (no real producer)", row.DependencyPaths)
	}
}
