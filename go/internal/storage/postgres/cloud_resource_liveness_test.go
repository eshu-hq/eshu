// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"strings"
	"testing"

	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
)

// TestCloudRetractLivenessSQLGuards pins the correctness-critical predicates
// of the #6887 live-check statements: the tombstone exclusion, the
// current-generation join, and the fact-kind scoping. A query that silently
// dropped any of these would either over-delete (missing tombstone or
// generation fence) or scan the wrong kind. The live proof of the full
// statements is TestCloudResourceLivenessLive and
// TestCloudResourceLivenessRefusesUndrainedAdmissionLive.
func TestCloudRetractLivenessSQLGuards(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"admission_alive", liveAdmissionCloudUIDsAliveSQL},
		{"admission_fenced", liveAdmissionCloudUIDsFencedSQL},
		{"ec2_posture", liveEC2PostureUIDsSQL},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			generationJoin := "scope.active_generation_id = fact.generation_id"
			if strings.HasPrefix(tc.name, "admission") {
				// #6946: the admission read joins the materialized candidate
				// slice, so the generation fence is spelled on the CTE.
				generationJoin = "scope.active_generation_id = cand.generation_id"
			}
			for _, want := range []string{
				"fact.is_tombstone = FALSE",
				generationJoin,
				"ingestion_scopes AS scope",
				"fact_records AS fact",
			} {
				if !strings.Contains(tc.sql, want) {
					t.Errorf("liveness SQL missing %q:\n%s", want, tc.sql)
				}
			}
		})
	}

	if !strings.Contains(liveAdmissionCloudUIDsFencedSQL, cloudRetractAdmissionFactKind) {
		t.Errorf("admission SQL must probe kind %q", cloudRetractAdmissionFactKind)
	}
	if !strings.Contains(liveEC2PostureUIDsSQL, cloudRetractEC2PostureFactKind) {
		t.Errorf("ec2 SQL must probe kind %q", cloudRetractEC2PostureFactKind)
	}
	if !strings.Contains(liveAdmissionCloudUIDsFencedSQL, "= ANY($1::text[])") {
		t.Error("admission SQL must probe the candidate uid array with = ANY")
	}
	// The admission-drain fence (#6887): both the pre-lock statement and the
	// fenced probe must carry the same predicate — reducer-stage
	// cloud_inventory_admission items, joined to the ACTIVE generation, in
	// the shared nonterminal status list — and the fenced probe must read the
	// fence and the admission rows in ONE statement so they share a snapshot.
	for _, tc := range []struct {
		name string
		sql  string
	}{
		{"pre_lock_fence", undrainedCloudAdmissionSQL},
		{"admission_fenced", liveAdmissionCloudUIDsFencedSQL},
	} {
		for _, want := range []string{
			"fact_work_items AS work",
			"scope.active_generation_id = work.generation_id",
			"work.stage = 'reducer'",
			"work.domain = 'cloud_inventory_admission'",
			"work.status IN " + cloudAdmissionNonterminalStatusList,
		} {
			if !strings.Contains(tc.sql, want) {
				t.Errorf("%s SQL missing %q:\n%s", tc.name, want, tc.sql)
			}
		}
	}
	for _, status := range []string{"'pending'", "'claimed'", "'running'", "'retrying'", "'failed'", "'dead_letter'"} {
		if !strings.Contains(cloudAdmissionNonterminalStatusList, status) {
			t.Errorf("nonterminal status list missing %s", status)
		}
	}
	// #6946: the alive branch must pin the partial-index scan first.
	if !strings.Contains(liveAdmissionCloudUIDsAliveSQL, "WITH cand AS MATERIALIZED") ||
		!strings.Contains(liveAdmissionCloudUIDsFencedSQL, liveAdmissionCloudUIDsAliveSQL) {
		t.Error("admission read must materialize the candidate slice first and the fenced probe must embed that exact read")
	}
	if !strings.Contains(liveAdmissionCloudUIDsFencedSQL, "UNION ALL") ||
		!strings.Contains(liveAdmissionCloudUIDsFencedSQL, "'undrained' AS kind") ||
		!strings.Contains(liveAdmissionCloudUIDsFencedSQL, "'alive' AS kind") {
		t.Error("fenced admission SQL must return fence rows and alive rows from one UNION ALL statement")
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

// TestRequireCloudAdmissionDrainedRejectsNilQueryer pins parity with
// LiveAdmissionCloudUIDs: a nil queryer is an error, never a panic, so a
// miswired pre-lock fence fails closed with an attributable message.
func TestRequireCloudAdmissionDrainedRejectsNilQueryer(t *testing.T) {
	t.Parallel()

	err := RequireCloudAdmissionDrained(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "queryer is required") {
		t.Fatalf("RequireCloudAdmissionDrained(nil) = %v, want the queryer-required error", err)
	}
}
