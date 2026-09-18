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
// own cross-scope materialization -- see the handler's own doc comment: "the
// graph writer still uses MATCH-only endpoint anchoring so missing workload
// instances are a no-op instead of fabricated graph truth." That race is
// repaired by this domain's crossScopeCorrelationReopenDomains membership,
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

// TestWorkloadCloudRelationshipMaterializationHandleConvergesOnReopenedReplay
// is the #6785 idempotency proof for adding this domain to
// crossScopeCorrelationReopenDomains
// (internal/storage/postgres/ingestion_reopen_correlation.go): a reopen
// replays the SAME succeeded work item's SAME (scope, generation) against the
// reducer a second time, not a new generation, so Handle must converge rather
// than duplicate or diverge. Both calls here share the intent unchanged (no
// simulated AttemptCount bump, matching shouldSkipRetract's own
// intent.AttemptCount <= 1 branch, which is what a freshly reopened work item's
// attempt_count = 0 reset -> first claim actually presents) and a
// PriorGenerationCheck stub returning false -- the real corpus shape this
// domain hits in the golden-corpus gate, where the AWS cloud scope has exactly
// one generation. Under that shape shouldSkipRetract skips the retract on
// EVERY call and relies on the writer's static-token MERGE identity alone for
// idempotency (proven separately by
// TestWorkloadCloudRelationshipWriterUsesExistingEndpoints in
// internal/storage/cypher, which pins the Cypher to
// "MERGE (instance)-[rel:USES]->(resource)" with no node-creating MERGE). This
// test proves the Go-side half of that contract: extraction is a pure function
// of the input facts, so both calls hand the writer the byte-identical single
// row -- never a duplicate, a dropped edge, or a different resolution_mode --
// regardless of whether the first call ran before or after the graph's
// WorkloadInstance node materialized.
func TestWorkloadCloudRelationshipMaterializationHandleConvergesOnReopenedReplay(t *testing.T) {
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
	handler := WorkloadCloudRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{envelope}},
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	intent := workloadCloudRelationshipIntent()

	firstResult, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("first Handle() error = %v", err)
	}
	secondResult, err := handler.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("reopened second Handle() error = %v", err)
	}

	if firstResult.CanonicalWrites != 1 || secondResult.CanonicalWrites != 1 {
		t.Fatalf(
			"CanonicalWrites = %d then %d, want 1 then 1 (a reopened replay must re-derive exactly the same row)",
			firstResult.CanonicalWrites, secondResult.CanonicalWrites,
		)
	}
	if writer.writeCalls != 2 {
		t.Fatalf("writeCalls = %d, want 2 (one per Handle call)", writer.writeCalls)
	}
	if writer.retractCalls != 0 {
		t.Fatalf(
			"retractCalls = %d, want 0 (no prior generation on either call, so shouldSkipRetract skips both -- "+
				"idempotency here rests entirely on the writer's static-token MERGE)",
			writer.retractCalls,
		)
	}
	if len(writer.writtenRows) != 2 {
		t.Fatalf("writtenRows accumulated = %d, want 2 (one row per Handle call)", len(writer.writtenRows))
	}
	first, second := writer.writtenRows[0], writer.writtenRows[1]
	for _, key := range []string{"workload_id", "cloud_resource_uid", "environment", "resolution_mode", "relationship_type"} {
		if anyToString(first[key]) != anyToString(second[key]) {
			t.Fatalf(
				"row[%q] diverged across the reopened replay: first=%q second=%q (extraction must be deterministic)",
				key, anyToString(first[key]), anyToString(second[key]),
			)
		}
	}
	if got, want := anyToString(first["workload_id"]), "workload:deployable-source"; got != want {
		t.Fatalf("workload_id = %q, want %q", got, want)
	}
}
