// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"context"
	"strings"
	"testing"
)

// TestSecurityGroupReachabilityRetractByUIDsAnchoredDelete proves
// RetractSecurityGroupReachabilityByUIDs emits BOTH anchored-retract families
// — the SG->rule family filtered on `WHERE sg.uid IN $source_uids` and the
// rule->endpoint family filtered on `WHERE rule.uid IN $source_uids` — each
// scoped by the shared scope_id/evidence_source predicates, and never falls
// back to the slow UNWIND + property-map MATCH shape or a whole-scope scan.
func TestSecurityGroupReachabilityRetractByUIDsAnchoredDelete(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSecurityGroupReachabilityWriter(executor, 0)
	if err := writer.RetractSecurityGroupReachabilityByUIDs(
		context.Background(),
		[]string{"sg-a", "rule-a"},
		[]string{"scope-1"},
		sgReachabilityEvidence,
	); err != nil {
		t.Fatalf("RetractSecurityGroupReachabilityByUIDs returned error: %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2 (one per source-label family)", len(executor.calls))
	}

	var joined string
	for _, call := range executor.calls {
		joined += call.Cypher + "\n"
	}

	for _, want := range []string{
		"MATCH (sg:CloudResource)-[rel]->(:SecurityGroupRule)",
		"WHERE sg.uid IN $source_uids",
		"MATCH (rule:SecurityGroupRule)-[rel:TO]->()",
		"WHERE rule.uid IN $source_uids",
		"rel.scope_id IN $scope_ids",
		"rel.evidence_source = $evidence_source",
		"DELETE rel",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("retract by uids cypher missing %q:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "UNWIND $source_uids") || strings.Contains(joined, "{uid: suid}") {
		t.Fatalf("retract by uids cypher must not use the slow UNWIND + property-map MATCH shape:\n%s", joined)
	}

	for _, call := range executor.calls {
		if got := call.Parameters["source_uids"]; got == nil {
			t.Fatalf("source_uids param missing on call: %+v", call)
		}
		if got := call.Parameters["scope_ids"]; got == nil {
			t.Fatalf("scope_ids param missing on call: %+v", call)
		}
		if got := call.Parameters["evidence_source"]; got != sgReachabilityEvidence {
			t.Fatalf("evidence_source param = %v, want %q", got, sgReachabilityEvidence)
		}
	}
}

// TestSecurityGroupReachabilityRetractByUIDsSharesSourceUIDsAcrossFamilies
// proves both anchored-retract statements are issued with the SAME
// $source_uids batch — the caller (the reducer handler) records the UNION of
// sg_uid and rule_uid into one ledger, and this writer must not split that
// union by family before dispatching; each statement's own source-node label
// (:CloudResource vs :SecurityGroupRule) does the filtering.
func TestSecurityGroupReachabilityRetractByUIDsSharesSourceUIDsAcrossFamilies(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSecurityGroupReachabilityWriter(executor, 0)
	uids := []string{"sg-a", "rule-a", "rule-b"}
	if err := writer.RetractSecurityGroupReachabilityByUIDs(
		context.Background(), uids, []string{"scope-1"}, sgReachabilityEvidence,
	); err != nil {
		t.Fatalf("RetractSecurityGroupReachabilityByUIDs returned error: %v", err)
	}
	if len(executor.calls) != 2 {
		t.Fatalf("len(calls) = %d, want 2", len(executor.calls))
	}
	for _, call := range executor.calls {
		got, ok := call.Parameters["source_uids"].([]string)
		if !ok || len(got) != 3 {
			t.Fatalf("source_uids param = %v, want the full shared batch %v", call.Parameters["source_uids"], uids)
		}
	}
}

// TestSecurityGroupReachabilityRetractByUIDsEmptyIsNoOp proves empty source
// uids is a clean no-op (no executor call at all).
func TestSecurityGroupReachabilityRetractByUIDsEmptyIsNoOp(t *testing.T) {
	t.Parallel()

	executor := &recordingExecutor{}
	writer := NewSecurityGroupReachabilityWriter(executor, 0)
	if err := writer.RetractSecurityGroupReachabilityByUIDs(
		context.Background(), nil, []string{"scope-1"}, sgReachabilityEvidence,
	); err != nil {
		t.Fatalf("RetractSecurityGroupReachabilityByUIDs returned error: %v", err)
	}
	if len(executor.calls) != 0 {
		t.Fatalf("len(calls) = %d, want 0 for empty uids", len(executor.calls))
	}
}

// TestSecurityGroupReachabilityRetractByUIDsBatchesUids proves uids beyond the
// batch size split into multiple statement pairs (one SG->rule + one
// rule->endpoint statement per batch).
func TestSecurityGroupReachabilityRetractByUIDsBatchesUids(t *testing.T) {
	t.Parallel()

	uids := make([]string, 1200)
	for i := range uids {
		uids[i] = "uid-" + string(rune('a'+i%26)) + string(rune('0'+i/26))
	}

	executor := &recordingExecutor{}
	writer := NewSecurityGroupReachabilityWriter(executor, 0)
	if err := writer.RetractSecurityGroupReachabilityByUIDs(
		context.Background(), uids, []string{"scope-1"}, sgReachabilityEvidence,
	); err != nil {
		t.Fatalf("RetractSecurityGroupReachabilityByUIDs returned error: %v", err)
	}
	// 1200 uids at 500 batch = ceil(1200/500) = 3 batches, times 2 statements
	// per batch (SG->rule + rule->endpoint) = 6 calls.
	if len(executor.calls) != 6 {
		t.Fatalf("len(calls) = %d, want 6 (3 uid batches x 2 families)", len(executor.calls))
	}
}

// TestSecurityGroupReachabilityRetractByUIDsSatisfiesReducerInterface is a
// compile-time guarantee that the cypher writer satisfies the reducer-owned
// consumer interface shape the materialization handler depends on.
func TestSecurityGroupReachabilityRetractByUIDsSatisfiesReducerInterface(t *testing.T) {
	t.Parallel()

	var _ interface {
		WriteSecurityGroupRuleNodes(ctx context.Context, rows []map[string]any, evidenceSource string) error
		WriteSecurityGroupSGRuleEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
		WriteSecurityGroupRuleEndpointEdges(ctx context.Context, rows []map[string]any, scopeID, generationID, evidenceSource string) error
		RetractSecurityGroupReachability(ctx context.Context, scopeIDs []string, generationID, evidenceSource string) error
		RetractSecurityGroupReachabilityByUIDs(ctx context.Context, sourceUIDs []string, scopeIDs []string, evidenceSource string) error
	} = NewSecurityGroupReachabilityWriter(&recordingExecutor{}, 0)
}
