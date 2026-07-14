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

// TestExtractS3ExternalPrincipalGrantRowsQuarantinesMissingRequiredField proves
// an s3_external_principal_grant fact MISSING a required identity key
// (account_id absent, not empty) is quarantined as an input_invalid per-fact
// dead-letter and produces no row, while a valid sibling fact in the same
// batch still projects normally. Before the typed-decode migration, the
// extractor read account_id (and principal_kind/principal_value) via raw
// payloadString lookups that silently defaulted a missing key to "", so the
// malformed fact would have either fabricated a GRANTS_ACCESS_TO edge keyed by
// an empty account_id or been swallowed as an indistinguishable
// missing_identity skip — never a visible, operator-diagnosable dead-letter.
// This test locks the corrected behavior (#4632).
func TestExtractS3ExternalPrincipalGrantRowsQuarantinesMissingRequiredField(t *testing.T) {
	t.Parallel()

	resources := []facts.Envelope{
		s3BucketResourceEnvelope("111111111111", "us-east-1", "orders-artifacts"),
	}
	malformed := facts.Envelope{
		FactID:   "fact-grant-missing-account",
		FactKind: facts.S3ExternalPrincipalGrantFactKind,
		Payload: map[string]any{
			// account_id intentionally absent.
			"region":               "us-east-1",
			"bucket_arn":           "arn:aws:s3:::orders-artifacts",
			"bucket_name":          "orders-artifacts",
			"principal_kind":       "aws_account",
			"principal_value":      "999988887777",
			"grant_outcome":        "cross_account",
			"is_public":            false,
			"is_cross_account":     true,
			"is_service_principal": false,
			"is_unsupported":       false,
		},
	}
	grants := []facts.Envelope{
		malformed,
		s3ExternalPrincipalGrantEnvelope(
			"111111111111",
			"us-east-1",
			"orders-artifacts",
			"aws_account",
			"999988887777",
			"cross_account",
		),
	}

	rows, tally, quarantined, err := ExtractS3ExternalPrincipalGrantRows(resources, grants)
	if err != nil {
		t.Fatalf("ExtractS3ExternalPrincipalGrantRows() error = %v, want nil (per-fact isolation, not batch abort)", err)
	}
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1; the valid fact must still project despite the malformed sibling", len(rows))
	}
	if len(quarantined) != 1 {
		t.Fatalf("len(quarantined) = %d, want 1; the missing-account_id fact must be quarantined", len(quarantined))
	}
	if quarantined[0].factKind != facts.S3ExternalPrincipalGrantFactKind {
		t.Fatalf("quarantined factKind = %q, want %q", quarantined[0].factKind, facts.S3ExternalPrincipalGrantFactKind)
	}
	if quarantined[0].field != "account_id" {
		t.Fatalf("quarantined field = %q, want %q", quarantined[0].field, "account_id")
	}
	if quarantined[0].classification != "input_invalid" {
		t.Fatalf("quarantined classification = %q, want %q", quarantined[0].classification, "input_invalid")
	}
	if tally.totalSkipped() != 0 {
		t.Fatalf("totalSkipped() = %d, want 0; a malformed fact dead-letters through quarantine, it is not counted as a skip", tally.totalSkipped())
	}
}
