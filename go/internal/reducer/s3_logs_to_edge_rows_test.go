// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// s3BucketResourceEnvelope builds an aws_resource fact envelope for one scanned
// S3 bucket, matching the shape the S3 scanner emits: resource_type
// aws_s3_bucket, an arn:aws:s3:::<name> ARN, resource_id equal to the ARN, and
// the bucket name as a correlation anchor.
func s3BucketResourceEnvelope(account, region, name string) facts.Envelope {
	arn := "arn:aws:s3:::" + name
	return facts.Envelope{
		FactKind: facts.AWSResourceFactKind,
		Payload: map[string]any{
			"account_id":          account,
			"region":              region,
			"resource_type":       "aws_s3_bucket",
			"resource_id":         arn,
			"arn":                 arn,
			"name":                name,
			"correlation_anchors": []string{arn, name, "s3://" + name},
		},
	}
}

// s3PostureEnvelope builds an s3_bucket_posture fact envelope for one bucket,
// with the given access-log target bucket name (blank means logging disabled).
func s3PostureEnvelope(account, region, name, loggingTarget string) facts.Envelope {
	arn := "arn:aws:s3:::" + name
	return facts.Envelope{
		FactKind: facts.S3BucketPostureFactKind,
		Payload: map[string]any{
			"account_id":            account,
			"region":                region,
			"bucket_arn":            arn,
			"bucket_name":           name,
			"logging_target_bucket": loggingTarget,
		},
	}
}

func s3BucketUID(account, region, name string) string {
	return cloudResourceUID(account, region, "aws_s3_bucket", "arn:aws:s3:::"+name)
}

func TestExtractS3LogsToEdgeRowsResolvesScannedTarget(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		s3BucketResourceEnvelope("111111111111", "us-east-1", "orders"),
		s3BucketResourceEnvelope("111111111111", "us-east-1", "orders-logs"),
	}
	postures := []facts.Envelope{
		s3PostureEnvelope("111111111111", "us-east-1", "orders", "orders-logs"),
	}

	rows, tally, _, err := ExtractS3LogsToEdgeRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractS3LogsToEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	if got, want := rows[0]["source_uid"], s3BucketUID("111111111111", "us-east-1", "orders"); got != want {
		t.Fatalf("source_uid = %v, want %v", got, want)
	}
	if got, want := rows[0]["target_uid"], s3BucketUID("111111111111", "us-east-1", "orders-logs"); got != want {
		t.Fatalf("target_uid = %v, want %v", got, want)
	}
	if got, want := rows[0]["relationship_type"], "LOGS_TO"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
	if got, want := rows[0]["resolution_mode"], s3LogsToModeName; got != want {
		t.Fatalf("resolution_mode = %v, want %v", got, want)
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("totalSkipped() = %d, want 0", tally.totalSkipped())
	}
}

func TestExtractS3LogsToEdgeRowsLoggingDisabledIsNoEdgeNoSkip(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		s3BucketResourceEnvelope("111111111111", "us-east-1", "orders"),
	}
	// Blank logging target = logging disabled. Not an edge, not a skip-error.
	postures := []facts.Envelope{
		s3PostureEnvelope("111111111111", "us-east-1", "orders", ""),
	}

	rows, tally, _, err := ExtractS3LogsToEdgeRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractS3LogsToEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for logging disabled", len(rows))
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("totalSkipped() = %d, want 0 for logging disabled (not a skip-error)", tally.totalSkipped())
	}
}

func TestExtractS3LogsToEdgeRowsUnresolvedTargetSkips(t *testing.T) {
	t.Parallel()

	// The source bucket scanned, but the central log bucket "central-logs" was
	// not scanned in this scope (cross-account / out-of-scope). No dangling
	// node, skip + count by target_unresolved.
	resources := []facts.Envelope{
		s3BucketResourceEnvelope("111111111111", "us-east-1", "orders"),
	}
	postures := []facts.Envelope{
		s3PostureEnvelope("111111111111", "us-east-1", "orders", "central-logs"),
	}

	rows, tally, _, err := ExtractS3LogsToEdgeRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractS3LogsToEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for unresolved target", len(rows))
	}
	if got := tally.skipped[s3LogsToSkipTargetUnresolved]; got != 1 {
		t.Fatalf("skipped[target_unresolved] = %d, want 1", got)
	}
	if tally.totalSkipped() != 1 {
		t.Fatalf("totalSkipped() = %d, want 1", tally.totalSkipped())
	}
}

func TestExtractS3LogsToEdgeRowsUnresolvedSourceSkips(t *testing.T) {
	t.Parallel()

	// The target log bucket scanned but the source bucket emitting the posture
	// fact did not scan as a node. The whole posture statement cannot anchor an
	// edge; counted once by source_unresolved.
	resources := []facts.Envelope{
		s3BucketResourceEnvelope("111111111111", "us-east-1", "orders-logs"),
	}
	postures := []facts.Envelope{
		s3PostureEnvelope("111111111111", "us-east-1", "orders", "orders-logs"),
	}

	rows, tally, _, err := ExtractS3LogsToEdgeRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractS3LogsToEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for unresolved source", len(rows))
	}
	if got := tally.skipped[s3LogsToSkipSourceUnresolved]; got != 1 {
		t.Fatalf("skipped[source_unresolved] = %d, want 1", got)
	}
}

func TestExtractS3LogsToEdgeRowsSelfTargetEmitsEdge(t *testing.T) {
	t.Parallel()

	// A bucket logging to itself is a legal, real S3 configuration (unlike IAM
	// self-assume). Emit the edge. This is the deliberate divergence from the
	// CAN_ASSUME self-loop skip rule.
	resources := []facts.Envelope{
		s3BucketResourceEnvelope("111111111111", "us-east-1", "orders"),
	}
	postures := []facts.Envelope{
		s3PostureEnvelope("111111111111", "us-east-1", "orders", "orders"),
	}

	rows, tally, _, err := ExtractS3LogsToEdgeRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractS3LogsToEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 for self-target (legal config)", len(rows))
	}
	if rows[0]["source_uid"] != rows[0]["target_uid"] {
		t.Fatalf("self-target edge must have equal source and target uid: %v vs %v", rows[0]["source_uid"], rows[0]["target_uid"])
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("totalSkipped() = %d, want 0", tally.totalSkipped())
	}
}

func TestExtractS3LogsToEdgeRowsTwoSourcesSameTargetAreDistinctEdges(t *testing.T) {
	t.Parallel()

	// Two buckets logging to the same central log bucket must produce two
	// distinct edges — no accidental merge or cartesian.
	resources := []facts.Envelope{
		s3BucketResourceEnvelope("111111111111", "us-east-1", "orders"),
		s3BucketResourceEnvelope("111111111111", "us-east-1", "payments"),
		s3BucketResourceEnvelope("111111111111", "us-east-1", "central-logs"),
	}
	postures := []facts.Envelope{
		s3PostureEnvelope("111111111111", "us-east-1", "orders", "central-logs"),
		s3PostureEnvelope("111111111111", "us-east-1", "payments", "central-logs"),
	}

	rows, _, _, err := ExtractS3LogsToEdgeRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractS3LogsToEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2 distinct edges", len(rows))
	}
	target := s3BucketUID("111111111111", "us-east-1", "central-logs")
	seen := map[string]bool{}
	for _, row := range rows {
		if row["target_uid"] != target {
			t.Fatalf("target_uid = %v, want %v", row["target_uid"], target)
		}
		seen[row["source_uid"].(string)] = true
	}
	if !seen[s3BucketUID("111111111111", "us-east-1", "orders")] || !seen[s3BucketUID("111111111111", "us-east-1", "payments")] {
		t.Fatalf("both source buckets must appear once: %v", seen)
	}
}

func TestExtractS3LogsToEdgeRowsDuplicateFactIsIdempotent(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		s3BucketResourceEnvelope("111111111111", "us-east-1", "orders"),
		s3BucketResourceEnvelope("111111111111", "us-east-1", "orders-logs"),
	}
	// The same posture fact delivered twice must still produce one edge.
	postures := []facts.Envelope{
		s3PostureEnvelope("111111111111", "us-east-1", "orders", "orders-logs"),
		s3PostureEnvelope("111111111111", "us-east-1", "orders", "orders-logs"),
	}

	rows, _, _, err := ExtractS3LogsToEdgeRows(resources, postures)
	if err != nil {
		t.Fatalf("ExtractS3LogsToEdgeRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 (idempotent dedupe)", len(rows))
	}
}

func TestExtractS3LogsToEdgeRowsDeterministicOrdering(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		s3BucketResourceEnvelope("111111111111", "us-east-1", "alpha"),
		s3BucketResourceEnvelope("111111111111", "us-east-1", "beta"),
		s3BucketResourceEnvelope("111111111111", "us-east-1", "logs"),
	}
	forward := []facts.Envelope{
		s3PostureEnvelope("111111111111", "us-east-1", "alpha", "logs"),
		s3PostureEnvelope("111111111111", "us-east-1", "beta", "logs"),
	}
	reverse := []facts.Envelope{
		s3PostureEnvelope("111111111111", "us-east-1", "beta", "logs"),
		s3PostureEnvelope("111111111111", "us-east-1", "alpha", "logs"),
	}

	rowsForward, _, _, err := ExtractS3LogsToEdgeRows(resources, forward)
	if err != nil {
		t.Fatalf("ExtractS3LogsToEdgeRows() error = %v, want nil", err)
	}
	rowsReverse, _, _, err := ExtractS3LogsToEdgeRows(resources, reverse)
	if err != nil {
		t.Fatalf("ExtractS3LogsToEdgeRows() error = %v, want nil", err)
	}
	if len(rowsForward) != 2 || len(rowsReverse) != 2 {
		t.Fatalf("len(rowsForward)=%d len(rowsReverse)=%d, want 2 each", len(rowsForward), len(rowsReverse))
	}
	for i := range rowsForward {
		if rowsForward[i]["source_uid"] != rowsReverse[i]["source_uid"] ||
			rowsForward[i]["target_uid"] != rowsReverse[i]["target_uid"] {
			t.Fatalf("row %d differs by input ordering: %v vs %v", i, rowsForward[i], rowsReverse[i])
		}
	}
}

func TestExtractS3LogsToEdgeRowsEmptyInputIsNoOp(t *testing.T) {
	t.Parallel()

	rows, tally, _, err := ExtractS3LogsToEdgeRows(nil, nil)
	if err != nil {
		t.Fatalf("ExtractS3LogsToEdgeRows() error = %v, want nil", err)
	}
	if rows != nil {
		t.Fatalf("rows = %v, want nil for empty input", rows)
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("totalSkipped() = %d, want 0 for empty input", tally.totalSkipped())
	}
}
