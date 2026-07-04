// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func s3ExternalPrincipalGrantEnvelope(
	account,
	region,
	bucket,
	principalKind,
	principalValue,
	outcome string,
) facts.Envelope {
	return facts.Envelope{
		FactKind: facts.S3ExternalPrincipalGrantFactKind,
		Payload: map[string]any{
			"account_id":            account,
			"region":                region,
			"bucket_arn":            "arn:aws:s3:::" + bucket,
			"bucket_name":           bucket,
			"principal_kind":        principalKind,
			"principal_value":       principalValue,
			"principal_account_id":  principalValue,
			"principal_partition":   "aws",
			"grant_outcome":         outcome,
			"is_public":             outcome == "public",
			"is_cross_account":      outcome == "cross_account",
			"is_service_principal":  outcome == "aws_service",
			"is_unsupported":        outcome == "unsupported_principal",
			"source_statement_id":   "stmt-1",
			"correlation_anchors":   []string{"arn:aws:s3:::" + bucket, bucket, principalValue},
			"policy_document":       "must-not-survive",
			"condition":             "must-not-survive",
			"acl_grants":            []string{"must-not-survive"},
			"object_keys":           []string{"must-not-survive"},
			"source_statement_body": "must-not-survive",
		},
	}
}

func TestExtractS3ExternalPrincipalGrantRowsProjectsExactPrincipals(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		s3BucketResourceEnvelope("111111111111", "us-east-1", "orders-artifacts"),
	}
	grants := []facts.Envelope{
		s3ExternalPrincipalGrantEnvelope(
			"111111111111",
			"us-east-1",
			"orders-artifacts",
			"aws_account",
			"999988887777",
			"cross_account",
		),
	}

	rows, tally, _, err := ExtractS3ExternalPrincipalGrantRows(resources, grants)
	if err != nil {
		t.Fatalf("ExtractS3ExternalPrincipalGrantRows() error = %v, want nil", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1", len(rows))
	}
	row := rows[0]
	if got, want := row["source_uid"], s3BucketUID("111111111111", "us-east-1", "orders-artifacts"); got != want {
		t.Fatalf("source_uid = %v, want %v", got, want)
	}
	if got, want := row["principal_kind"], "aws_account"; got != want {
		t.Fatalf("principal_kind = %v, want %v", got, want)
	}
	if got, want := row["principal_value"], "999988887777"; got != want {
		t.Fatalf("principal_value = %v, want %v", got, want)
	}
	if got, want := row["principal_uid"], externalPrincipalUID("aws_account", "999988887777"); got != want {
		t.Fatalf("principal_uid = %v, want %v", got, want)
	}
	if got, want := row["relationship_type"], "GRANTS_ACCESS_TO"; got != want {
		t.Fatalf("relationship_type = %v, want %v", got, want)
	}
	if got, want := row["grant_outcome"], "cross_account"; got != want {
		t.Fatalf("grant_outcome = %v, want %v", got, want)
	}
	for _, forbidden := range []string{"policy_document", "condition", "acl_grants", "object_keys", "source_statement_body"} {
		if _, exists := row[forbidden]; exists {
			t.Fatalf("row contains forbidden raw policy field %q: %#v", forbidden, row)
		}
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("totalSkipped() = %d, want 0", tally.totalSkipped())
	}
}

func TestExtractS3ExternalPrincipalGrantRowsSkipsUnsupportedAndUnresolvedSources(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		s3BucketResourceEnvelope("111111111111", "us-east-1", "orders-artifacts"),
	}
	grants := []facts.Envelope{
		s3ExternalPrincipalGrantEnvelope(
			"111111111111",
			"us-east-1",
			"orders-artifacts",
			"unsupported",
			"AWS",
			"unsupported_principal",
		),
		s3ExternalPrincipalGrantEnvelope(
			"111111111111",
			"us-east-1",
			"missing-source",
			"public",
			"*",
			"public",
		),
	}

	rows, tally, _, err := ExtractS3ExternalPrincipalGrantRows(resources, grants)
	if err != nil {
		t.Fatalf("ExtractS3ExternalPrincipalGrantRows() error = %v, want nil", err)
	}
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for unsupported/unresolved grants", len(rows))
	}
	if got := tally.skipped[s3ExternalPrincipalGrantSkipUnsupportedPrincipal]; got != 1 {
		t.Fatalf("skipped[unsupported_principal] = %d, want 1", got)
	}
	if got := tally.skipped[s3ExternalPrincipalGrantSkipSourceUnresolved]; got != 1 {
		t.Fatalf("skipped[source_unresolved] = %d, want 1", got)
	}
}

func TestExtractS3ExternalPrincipalGrantRowsDeduplicatesAndOrders(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		s3BucketResourceEnvelope("111111111111", "us-east-1", "alpha"),
		s3BucketResourceEnvelope("111111111111", "us-east-1", "beta"),
	}
	forward := []facts.Envelope{
		s3ExternalPrincipalGrantEnvelope("111111111111", "us-east-1", "beta", "public", "*", "public"),
		s3ExternalPrincipalGrantEnvelope("111111111111", "us-east-1", "alpha", "aws_service", "logging.s3.amazonaws.com", "aws_service"),
		s3ExternalPrincipalGrantEnvelope("111111111111", "us-east-1", "alpha", "aws_service", "logging.s3.amazonaws.com", "aws_service"),
	}
	reverse := []facts.Envelope{
		s3ExternalPrincipalGrantEnvelope("111111111111", "us-east-1", "alpha", "aws_service", "logging.s3.amazonaws.com", "aws_service"),
		s3ExternalPrincipalGrantEnvelope("111111111111", "us-east-1", "beta", "public", "*", "public"),
	}

	rowsForward, _, _, err := ExtractS3ExternalPrincipalGrantRows(resources, forward)
	if err != nil {
		t.Fatalf("ExtractS3ExternalPrincipalGrantRows() error = %v, want nil", err)
	}
	rowsReverse, _, _, err := ExtractS3ExternalPrincipalGrantRows(resources, reverse)
	if err != nil {
		t.Fatalf("ExtractS3ExternalPrincipalGrantRows() error = %v, want nil", err)
	}
	if len(rowsForward) != 2 || len(rowsReverse) != 2 {
		t.Fatalf("len(rowsForward)=%d len(rowsReverse)=%d, want 2 each", len(rowsForward), len(rowsReverse))
	}
	for i := range rowsForward {
		if rowsForward[i]["source_uid"] != rowsReverse[i]["source_uid"] ||
			rowsForward[i]["principal_uid"] != rowsReverse[i]["principal_uid"] {
			t.Fatalf("row %d differs by input order: %v vs %v", i, rowsForward[i], rowsReverse[i])
		}
	}
}
