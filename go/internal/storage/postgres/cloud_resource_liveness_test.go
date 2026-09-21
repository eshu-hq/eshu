// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"strings"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestCloudRetractLivenessSQLGuards pins the correctness-critical predicates
// of the #6887 live-check statements: the tombstone exclusion, the
// current-generation join, and the fact-kind scoping. A query that silently
// dropped any of these would either over-delete (missing tombstone or
// generation fence) or scan the wrong kind. The live proof of the full
// statements is TestCloudResourceLivenessLive.
func TestCloudRetractLivenessSQLGuards(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"admission", liveAdmissionCloudUIDsSQL},
		{"ec2_posture", liveEC2PostureUIDsSQL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, want := range []string{
				"fact.is_tombstone = FALSE",
				"scope.active_generation_id = fact.generation_id",
				"ingestion_scopes AS scope",
				"fact_records AS fact",
			} {
				if !strings.Contains(tc.sql, want) {
					t.Errorf("liveness SQL missing %q:\n%s", want, tc.sql)
				}
			}
		})
	}

	if !strings.Contains(liveAdmissionCloudUIDsSQL, cloudRetractAdmissionFactKind) {
		t.Errorf("admission SQL must probe kind %q", cloudRetractAdmissionFactKind)
	}
	if !strings.Contains(liveEC2PostureUIDsSQL, cloudRetractEC2PostureFactKind) {
		t.Errorf("ec2 SQL must probe kind %q", cloudRetractEC2PostureFactKind)
	}
	if !strings.Contains(liveAdmissionCloudUIDsSQL, "= ANY($1::text[])") {
		t.Error("admission SQL must probe the candidate uid array with = ANY")
	}
	if !strings.Contains(liveEC2PostureUIDsSQL, "UNNEST($1::text[], $2::text[], $3::text[], $4::text[], $5::text[])") {
		t.Error("ec2 SQL must zip candidate tuples with multi-arg UNNEST")
	}
}

// TestNormalizeEC2PostureCandidate pins the reader-matching normalization:
// trim, blank type defaults, instance-or-arn reference, and no-identity
// rejection.
func TestNormalizeEC2PostureCandidate(t *testing.T) {
	t.Parallel()

	account, region, rtype, ref, ok := normalizeEC2PostureCandidate(reducercontract.EC2PostureCandidate{
		AccountID: " 111 ", Region: "us-east-1", InstanceID: "i-1",
	})
	if !ok || account != "111" || region != "us-east-1" || rtype != cloudRetractDefaultEC2ResourceType || ref != "i-1" {
		t.Fatalf("got (%q,%q,%q,%q,%v), want trimmed/defaulted identity", account, region, rtype, ref, ok)
	}

	_, _, _, ref, ok = normalizeEC2PostureCandidate(reducercontract.EC2PostureCandidate{
		AccountID: "222", Region: "eu-west-1", ARN: "arn:aws:ec2:::i-arn",
	})
	if !ok || ref != "arn:aws:ec2:::i-arn" {
		t.Fatalf("arn fallback ref = %q, ok = %v; want arn, true", ref, ok)
	}

	if _, _, _, _, ok = normalizeEC2PostureCandidate(reducercontract.EC2PostureCandidate{
		AccountID: "222", Region: "eu-west-1",
	}); ok {
		t.Fatal("identity-free candidate must not normalize")
	}
}
