// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func s3InternetExposurePostureEnvelope(factID, account, region, name string, payload map[string]any) facts.Envelope {
	arn := "arn:aws:s3:::" + name
	merged := map[string]any{
		"account_id":  account,
		"region":      region,
		"bucket_arn":  arn,
		"bucket_name": name,
	}
	for key, value := range payload {
		merged[key] = value
	}
	return facts.Envelope{
		FactID:   factID,
		FactKind: facts.S3BucketPostureFactKind,
		Payload:  merged,
	}
}

func requireS3InternetExposureRow(t *testing.T, rows []map[string]any, uid string) map[string]any {
	t.Helper()
	for _, row := range rows {
		if row["uid"] == uid {
			return row
		}
	}
	t.Fatalf("no s3 internet-exposure row found for uid %q in %v", uid, rows)
	return nil
}

func TestExtractS3InternetExposureRowsDerivesExposedPublicPolicy(t *testing.T) {
	t.Parallel()

	const account = "111111111111"
	const region = "us-east-1"
	resources := []facts.Envelope{s3BucketResourceEnvelope(account, region, "orders")}
	postures := []facts.Envelope{s3InternetExposurePostureEnvelope(
		"fact-public-policy",
		account,
		region,
		"orders",
		map[string]any{
			"policy_present":          true,
			"policy_grants_public":    true,
			"restrict_public_buckets": false,
		},
	)}

	rows, tally := ExtractS3InternetExposureRows(resources, postures)
	row := requireS3InternetExposureRow(t, rows, s3BucketUID(account, region, "orders"))
	if got, want := row["state"], "exposed"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got, want := row["internet_exposed"], true; got != want {
		t.Fatalf("internet_exposed = %v, want %v", got, want)
	}
	if got, want := row["reason"], "public_policy_allows_public"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
	if got, want := row["source_fact_id"], "fact-public-policy"; got != want {
		t.Fatalf("source_fact_id = %v, want %v", got, want)
	}
	if got, want := tally.decisions["exposed"], 1; got != want {
		t.Fatalf("decisions[exposed] = %d, want %d", got, want)
	}
}

func TestExtractS3InternetExposureRowsDerivesNotExposedWhenPublicPolicyBlocked(t *testing.T) {
	t.Parallel()

	const account = "111111111111"
	const region = "us-east-1"
	resources := []facts.Envelope{s3BucketResourceEnvelope(account, region, "orders")}
	postures := []facts.Envelope{s3InternetExposurePostureEnvelope(
		"fact-public-policy-blocked",
		account,
		region,
		"orders",
		map[string]any{
			"policy_present":          true,
			"policy_grants_public":    true,
			"restrict_public_buckets": true,
		},
	)}

	rows, tally := ExtractS3InternetExposureRows(resources, postures)
	row := requireS3InternetExposureRow(t, rows, s3BucketUID(account, region, "orders"))
	if got, want := row["state"], "not_exposed"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got, want := row["internet_exposed"], false; got != want {
		t.Fatalf("internet_exposed = %v, want %v", got, want)
	}
	if got, want := row["reason"], "public_policy_restricted_by_block_public_access"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
	if got, want := tally.decisions["not_exposed"], 1; got != want {
		t.Fatalf("decisions[not_exposed] = %d, want %d", got, want)
	}
}

func TestExtractS3InternetExposureRowsKeepsUnknownWhenPolicyGrantUnknown(t *testing.T) {
	t.Parallel()

	const account = "111111111111"
	const region = "us-east-1"
	resources := []facts.Envelope{s3BucketResourceEnvelope(account, region, "orders")}
	postures := []facts.Envelope{s3InternetExposurePostureEnvelope(
		"fact-unknown-policy",
		account,
		region,
		"orders",
		map[string]any{
			"policy_present": true,
		},
	)}

	rows, tally := ExtractS3InternetExposureRows(resources, postures)
	row := requireS3InternetExposureRow(t, rows, s3BucketUID(account, region, "orders"))
	if got, want := row["state"], "unknown"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got := row["internet_exposed"]; got != nil {
		t.Fatalf("internet_exposed = %v, want nil for unknown", got)
	}
	if got, want := row["reason"], "policy_public_grant_unknown"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
	if got, want := tally.decisions["unknown"], 1; got != want {
		t.Fatalf("decisions[unknown] = %d, want %d", got, want)
	}
}

func TestExtractS3InternetExposureRowsDerivesNotExposedForNoPolicyWithACLPublicAccessBlocked(t *testing.T) {
	t.Parallel()

	const account = "111111111111"
	const region = "us-east-1"
	resources := []facts.Envelope{s3BucketResourceEnvelope(account, region, "orders")}
	postures := []facts.Envelope{s3InternetExposurePostureEnvelope(
		"fact-no-policy-acl-blocked",
		account,
		region,
		"orders",
		map[string]any{
			"policy_present":     false,
			"ignore_public_acls": true,
		},
	)}

	rows, _ := ExtractS3InternetExposureRows(resources, postures)
	row := requireS3InternetExposureRow(t, rows, s3BucketUID(account, region, "orders"))
	if got, want := row["state"], "not_exposed"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got, want := row["internet_exposed"], false; got != want {
		t.Fatalf("internet_exposed = %v, want %v", got, want)
	}
	if got, want := row["reason"], "no_policy_acl_public_access_blocked"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
}

func TestExtractS3InternetExposureRowsKeepsUnknownForPartialPublicAccessBlock(t *testing.T) {
	t.Parallel()

	const account = "111111111111"
	const region = "us-east-1"
	resources := []facts.Envelope{s3BucketResourceEnvelope(account, region, "orders")}
	postures := []facts.Envelope{s3InternetExposurePostureEnvelope(
		"fact-partial-bpa",
		account,
		region,
		"orders",
		map[string]any{
			"policy_present": false,
		},
	)}

	rows, _ := ExtractS3InternetExposureRows(resources, postures)
	row := requireS3InternetExposureRow(t, rows, s3BucketUID(account, region, "orders"))
	if got, want := row["state"], "unknown"; got != want {
		t.Fatalf("state = %v, want %v", got, want)
	}
	if got := row["internet_exposed"]; got != nil {
		t.Fatalf("internet_exposed = %v, want nil for partial public-access-block data", got)
	}
	if got, want := row["reason"], "partial_public_access_block"; got != want {
		t.Fatalf("reason = %v, want %v", got, want)
	}
}

func TestExtractS3InternetExposureRowsSkipsUnresolvedSource(t *testing.T) {
	t.Parallel()

	const account = "111111111111"
	const region = "us-east-1"
	postures := []facts.Envelope{s3InternetExposurePostureEnvelope(
		"fact-unresolved",
		account,
		region,
		"orders",
		map[string]any{"policy_present": false},
	)}

	rows, tally := ExtractS3InternetExposureRows(nil, postures)
	if len(rows) != 0 {
		t.Fatalf("len(rows) = %d, want 0 for unresolved source", len(rows))
	}
	if got, want := tally.skipped[s3InternetExposureSkipSourceUnresolved], 1; got != want {
		t.Fatalf("skipped[source_unresolved] = %d, want %d", got, want)
	}
}

func TestExtractS3InternetExposureRowsDuplicateReplayIsIdempotent(t *testing.T) {
	t.Parallel()

	const account = "111111111111"
	const region = "us-east-1"
	resources := []facts.Envelope{s3BucketResourceEnvelope(account, region, "orders")}
	posture := s3InternetExposurePostureEnvelope(
		"fact-public-policy",
		account,
		region,
		"orders",
		map[string]any{
			"policy_present":          true,
			"policy_grants_public":    true,
			"restrict_public_buckets": true,
		},
	)

	rows, _ := ExtractS3InternetExposureRows(resources, []facts.Envelope{posture, posture})
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 for duplicate replay", len(rows))
	}
}

func TestExtractS3InternetExposureRowsDuplicateBucketChoosesStableFact(t *testing.T) {
	t.Parallel()

	const account = "111111111111"
	const region = "us-east-1"
	resources := []facts.Envelope{s3BucketResourceEnvelope(account, region, "orders")}
	unknown := s3InternetExposurePostureEnvelope(
		"fact-z-unknown",
		account,
		region,
		"orders",
		map[string]any{"policy_present": true},
	)
	blocked := s3InternetExposurePostureEnvelope(
		"fact-a-blocked",
		account,
		region,
		"orders",
		map[string]any{
			"policy_present":          true,
			"policy_grants_public":    true,
			"restrict_public_buckets": true,
		},
	)

	rows, _ := ExtractS3InternetExposureRows(resources, []facts.Envelope{unknown, blocked})
	if len(rows) != 1 {
		t.Fatalf("len(rows) = %d, want 1 for duplicate bucket posture facts", len(rows))
	}
	if got, want := rows[0]["source_fact_id"], "fact-a-blocked"; got != want {
		t.Fatalf("source_fact_id = %v, want stable lowest fact id %v", got, want)
	}
	if got, want := rows[0]["state"], "not_exposed"; got != want {
		t.Fatalf("state = %v, want %v from stable selected fact", got, want)
	}
}
