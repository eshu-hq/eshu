// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestExtractWorkloadCloudRelationshipRowsPromotesIssue6785DeployableSourceRoleFixture
// proves the extractor's contribution to #6785's golden-corpus USES gap: the
// awscloud cassette's two deployable-source-app-role aws_iam_role facts
// (added alongside the CAN_PERFORM fixture in
// testdata/cassettes/awscloud/supply-chain-demo.json -- one anchored prod,
// one anchored stage, so mcp:compare_environments still reads the role as
// identically present in both deployable-source environments) carry exactly
// the payload shape ExtractWorkloadCloudRelationshipRows requires -- a
// top-level workload_id and environment plus the identity fields
// CAN_PERFORM's own join already proved decode -- and yield exactly two
// explicit_workload_anchor USES rows, one per environment, both keyed to the
// SAME cloud_resource_uid (the two facts share one ARN/resource identity).
// This rules out the extractor as the cause of the live gate's observed zero
// USES edges: the corpus input is provably sufficient once this generation's
// aws_resource facts reach the handler. (What is NOT proven here is WHEN
// they reach it relative to the deployable-source WorkloadInstance nodes'
// own cross-scope materialization. That race is closed by the handler's
// WorkloadInstance readiness gate (workload_cloud_relationship_instance_readiness.go),
// not by this extractor.)
func TestExtractWorkloadCloudRelationshipRowsPromotesIssue6785DeployableSourceRoleFixture(t *testing.T) {
	t.Parallel()

	roleFact := func(factID, environment string) facts.Envelope {
		return workloadCloudAWSResourceEnvelope(factID, map[string]any{
			"arn":                 "arn:aws:iam::123456789012:role/deployable-source-app-role",
			"resource_id":         "arn:aws:iam::123456789012:role/deployable-source-app-role",
			"resource_type":       "aws_iam_role",
			"name":                "deployable-source-app-role",
			"state":               "",
			"account_id":          "123456789012",
			"region":              "us-east-1",
			"service_kind":        "iam",
			"correlation_anchors": []any{"arn:aws:iam::123456789012:role/deployable-source-app-role"},
			"workload_id":         "workload:deployable-source",
			"environment":         environment,
			"attributes":          map[string]any{},
		})
	}

	rows, tally, quarantined, err := ExtractWorkloadCloudRelationshipRows([]facts.Envelope{
		roleFact("aws:123456789012:us-east-1:iam:role:deployable-source-app-role", "prod"),
		roleFact("aws:123456789012:us-east-1:iam:role:deployable-source-app-role:stage-anchor", "stage"),
		// The sibling S3 bucket fact from the same cassette scope carries no
		// workload_id, so it must be skipped rather than promoted or fatal.
		workloadCloudAWSResourceEnvelope("aws:123456789012:us-east-1:s3:bucket:scd-upload-receipts", map[string]any{
			"arn":                 "arn:aws:s3:::scd-upload-receipts",
			"resource_id":         "scd-upload-receipts",
			"resource_type":       "aws_s3_bucket",
			"name":                "scd-upload-receipts",
			"state":               "",
			"account_id":          "123456789012",
			"region":              "us-east-1",
			"service_kind":        "s3",
			"correlation_anchors": []any{"scd-upload-receipts", "arn:aws:s3:::scd-upload-receipts"},
			"attributes":          map[string]any{},
		}),
	})
	if err != nil {
		t.Fatalf("ExtractWorkloadCloudRelationshipRows() error = %v, want nil", err)
	}
	if len(quarantined) != 0 {
		t.Fatalf("quarantined = %v, want none", quarantined)
	}

	if got, want := len(rows), 2; got != want {
		t.Fatalf("len(rows) = %d, want %d (one per environment; the bucket fact carries no workload_id and must be skipped)", got, want)
	}
	// The extractor sorts by "workload_id@environment->cloud_resource_uid",
	// so prod ("p") sorts before stage ("s").
	prodRow, stageRow := rows[0], rows[1]
	for _, row := range []map[string]any{prodRow, stageRow} {
		if got, want := anyToString(row["workload_id"]), "workload:deployable-source"; got != want {
			t.Fatalf("workload_id = %q, want %q", got, want)
		}
		if got, want := anyToString(row["resolution_mode"]), "explicit_workload_anchor"; got != want {
			t.Fatalf("resolution_mode = %q, want %q", got, want)
		}
		if got, want := anyToString(row["relationship_type"]), "USES"; got != want {
			t.Fatalf("relationship_type = %q, want %q", got, want)
		}
		if got := anyToString(row["cloud_resource_uid"]); got == "" {
			t.Fatal("cloud_resource_uid must be populated")
		}
	}
	if got, want := anyToString(prodRow["environment"]), "prod"; got != want {
		t.Fatalf("rows[0].environment = %q, want %q", got, want)
	}
	if got, want := anyToString(stageRow["environment"]), "stage"; got != want {
		t.Fatalf("rows[1].environment = %q, want %q", got, want)
	}
	if got, want := anyToString(prodRow["cloud_resource_uid"]), anyToString(stageRow["cloud_resource_uid"]); got != want {
		t.Fatalf(
			"prod and stage cloud_resource_uid diverged: %q vs %q, want equal (both facts share one role ARN identity)",
			got, want,
		)
	}
	if got, want := tally.skipped[workloadCloudRelationshipSkipMissingWorkloadAnchor], 1; got != want {
		t.Fatalf("skipped[missing_workload_anchor] = %d, want %d (the bucket fact)", got, want)
	}
}

// TestWorkloadCloudRelationshipMaterializationHandleConvergesOnReplay proves
// Handle converges when the SAME (scope, generation) runs twice, as a readiness
// retry or a lease reclaim does. It pins both retract branches of
// shouldSkipRetract (#6785 review F2):
//
//   - No prior generation (the single-generation golden-corpus shape): the
//     retract is skipped on both calls and idempotency rests on the writer's
//     MERGE identity (instance, USES, resource), pinned by
//     TestWorkloadCloudRelationshipWriterUsesExistingEndpoints in
//     internal/storage/cypher.
//   - A prior generation exists (every AWS scope that has refreshed once):
//     PriorGenerationCheck is EXISTS(any other generation of the scope), so
//     BOTH calls take the retract branch, deleting every USES edge carrying
//     this scope_id and evidence source, then rewrite. Convergence there comes
//     from scope-wide retract followed by rewrite, not from a skipped retract.
//
// In both shapes each call hands the writer the byte-identical single row.
func TestWorkloadCloudRelationshipMaterializationHandleConvergesOnReplay(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name        string
		hasPrior    bool
		wantRetract int
	}{
		{name: "first generation skips retract", hasPrior: false, wantRetract: 0},
		{name: "prior generation retracts then rewrites each call", hasPrior: true, wantRetract: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			envelope := workloadCloudAWSResourceEnvelope("aws:123456789012:us-east-1:iam:role:deployable-source-app-role", map[string]any{
				"arn":                 "arn:aws:iam::123456789012:role/deployable-source-app-role",
				"resource_id":         "arn:aws:iam::123456789012:role/deployable-source-app-role",
				"resource_type":       "aws_iam_role",
				"name":                "deployable-source-app-role",
				"account_id":          "123456789012",
				"region":              "us-east-1",
				"service_kind":        "iam",
				"correlation_anchors": []any{"arn:aws:iam::123456789012:role/deployable-source-app-role"},
				"workload_id":         "workload:deployable-source",
				"environment":         "prod",
				"attributes":          map[string]any{},
			})

			writer := &recordingWorkloadCloudRelationshipWriter{}
			hasPrior := tc.hasPrior
			handler := WorkloadCloudRelationshipMaterializationHandler{
				FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{envelope}},
				EdgeWriter:           writer,
				ReadinessLookup:      readyLookup(true, true),
				PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return hasPrior, nil },
			}
			intent := workloadCloudRelationshipIntent()
			intent.AttemptCount = 1

			for call := 1; call <= 2; call++ {
				result, err := handler.Handle(context.Background(), intent)
				if err != nil {
					t.Fatalf("Handle() call %d error = %v", call, err)
				}
				if result.CanonicalWrites != 1 {
					t.Fatalf("call %d CanonicalWrites = %d, want 1", call, result.CanonicalWrites)
				}
			}
			if writer.retractCalls != tc.wantRetract {
				t.Fatalf("retractCalls = %d, want %d", writer.retractCalls, tc.wantRetract)
			}
			if writer.writeCalls != 2 || len(writer.writtenRows) != 2 {
				t.Fatalf("writeCalls = %d rows = %d, want 2 and 2 (one row per call)", writer.writeCalls, len(writer.writtenRows))
			}
			first, second := writer.writtenRows[0], writer.writtenRows[1]
			for _, key := range []string{"workload_id", "cloud_resource_uid", "environment", "resolution_mode", "relationship_type"} {
				if anyToString(first[key]) != anyToString(second[key]) {
					t.Fatalf("row[%q] diverged across replay: %q vs %q", key, anyToString(first[key]), anyToString(second[key]))
				}
			}
		})
	}
}
