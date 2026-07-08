// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSecurityGroupReachabilityMaterializationLedgerRecordsBeforeWrite proves
// the handler records the ledger BEFORE writing graph edges when a Ledger is
// wired, and that the recorded set is the UNION of both source families: the
// SG CloudResource uid (the SG->rule edge source, keyed "sg_uid" on
// reach.SGRuleEdges) and the SecurityGroupRule uid (the rule->endpoint edge
// source, keyed "rule_uid" on reach.RuleEndpointEdges).
func TestSecurityGroupReachabilityMaterializationLedgerRecordsBeforeWrite(t *testing.T) {
	t.Parallel()

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8",
	))

	writer := &recordingSecurityGroupReachabilityWriter{}
	ledger := &fakeProjectedSourceLedger{}
	handler := SecurityGroupReachabilityMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{anchor, rule}},
		Writer:               writer,
		Ledger:               ledger,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), securityGroupReachabilityIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if ledger.recordCalls != 1 {
		t.Fatalf("record calls = %d, want 1", ledger.recordCalls)
	}

	sgUID := cloudResourceUID(sgAccountID, sgRegion, "aws_ec2_security_group", "sg-0abc")
	ruleUID := securityGroupRuleUID(sgUID, "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8")

	gotUIDs := append([]string(nil), ledger.recordedUIDs...)
	sort.Strings(gotUIDs)
	wantUIDs := []string{sgUID, ruleUID}
	sort.Strings(wantUIDs)
	if len(gotUIDs) != len(wantUIDs) || gotUIDs[0] != wantUIDs[0] || gotUIDs[1] != wantUIDs[1] {
		t.Fatalf("recorded uids = %v, want the union %v (sg_uid + rule_uid)", gotUIDs, wantUIDs)
	}
	if ledger.recordedSource != securityGroupReachabilityEvidenceSource {
		t.Fatalf("recorded evidence source = %q, want %q", ledger.recordedSource, securityGroupReachabilityEvidenceSource)
	}
	// Order must be list -> prune (retract phase) before record (write phase).
	// PriorGenerationCheck returns true (hasPrior), so retract runs first.
	if len(ledger.callOrder) < 3 {
		t.Fatalf("call order too short: %v", ledger.callOrder)
	}
	if ledger.callOrder[0] != "list" || ledger.callOrder[1] != "prune" || ledger.callOrder[2] != "record" {
		t.Fatalf("call order = %v, want [list prune record]", ledger.callOrder)
	}
}

// TestSecurityGroupReachabilityMaterializationLedgerRetractUsesLedgerUIDs
// proves retract enumerates uids from the ledger and calls the anchored-delete
// method, not the old whole-scope retract.
func TestSecurityGroupReachabilityMaterializationLedgerRetractUsesLedgerUIDs(t *testing.T) {
	t.Parallel()

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8",
	))

	writer := &recordingSecurityGroupReachabilityWriter{}
	ledger := &fakeProjectedSourceLedger{listUIDs: []string{"uid-1", "uid-2"}}
	handler := SecurityGroupReachabilityMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{anchor, rule}},
		Writer:               writer,
		Ledger:               ledger,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), securityGroupReachabilityIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractByUIDsCalls != 1 {
		t.Fatalf("retractByUIDs calls = %d, want 1", writer.retractByUIDsCalls)
	}
	if len(writer.retractByUIDsUids) != 2 {
		t.Fatalf("retractByUIDs uids = %v, want 2", writer.retractByUIDsUids)
	}
	if writer.retractCalls != 0 {
		t.Fatalf("old whole-scope retract calls = %d, want 0 when a ledger is wired", writer.retractCalls)
	}
}

// TestSecurityGroupReachabilityMaterializationLedgerSkipsRetractOnFirstGeneration
// proves the retract (and therefore the ledger list/prune) is skipped on the
// scope's first generation even when a Ledger is wired, while the ledger
// record still happens for the write.
func TestSecurityGroupReachabilityMaterializationLedgerSkipsRetractOnFirstGeneration(t *testing.T) {
	t.Parallel()

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8",
	))

	writer := &recordingSecurityGroupReachabilityWriter{}
	ledger := &fakeProjectedSourceLedger{}
	handler := SecurityGroupReachabilityMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{anchor, rule}},
		Writer:               writer,
		Ledger:               ledger,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	if _, err := handler.Handle(context.Background(), securityGroupReachabilityIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractByUIDsCalls != 0 {
		t.Fatalf("retractByUIDs calls = %d, want 0 on first generation", writer.retractByUIDsCalls)
	}
	if ledger.pruneCalls != 0 {
		t.Fatalf("prune calls = %d, want 0 on first generation", ledger.pruneCalls)
	}
	if writer.sgEdgeCalls != 1 || writer.toEdgeCalls != 1 {
		t.Fatalf("edge write calls = sg:%d to:%d, want 1/1", writer.sgEdgeCalls, writer.toEdgeCalls)
	}
	if ledger.recordCalls != 1 {
		t.Fatalf("record calls = %d, want 1 (record still happens even when retract is skipped)", ledger.recordCalls)
	}
}

// TestSecurityGroupReachabilityMaterializationNilLedgerPreservesOldRetractPath
// proves that when Ledger is nil, the handler falls back to the pre-ledger
// whole-scope retract, never calling the anchored by-uids method.
func TestSecurityGroupReachabilityMaterializationNilLedgerPreservesOldRetractPath(t *testing.T) {
	t.Parallel()

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8",
	))

	writer := &recordingSecurityGroupReachabilityWriter{}
	handler := SecurityGroupReachabilityMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{anchor, rule}},
		Writer:               writer,
		Ledger:               nil,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), securityGroupReachabilityIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("old retract calls = %d, want 1 when Ledger is nil", writer.retractCalls)
	}
	if writer.retractByUIDsCalls != 0 {
		t.Fatalf("retractByUIDs calls = %d, want 0 when Ledger is nil", writer.retractByUIDsCalls)
	}
}

// TestSecurityGroupReachabilityMaterializationLedgerLeakSafetyAcrossGenerations
// is the key accuracy regression for issue #4881: generation N resolves one
// live rule (ruleA) attached to SG anchor sgA and records the UNION {sgA_uid,
// ruleA_uid} into the ledger. Generation N+1 still carries the sgA resource
// fact (the anchor "resolves"), but ruleA's aws_security_group_rule fact is
// gone — so generation N+1 produces ZERO reachability rows. The anchored
// retract for generation N+1 MUST still target the ledger's PRIOR set
// {sgA_uid, ruleA_uid}, not the current (empty) generation's resolved set;
// otherwise ruleA's now-stale SG->rule and rule->endpoint edges are never
// retracted and leak in the graph forever.
func TestSecurityGroupReachabilityMaterializationLedgerLeakSafetyAcrossGenerations(t *testing.T) {
	t.Parallel()

	anchor := securityGroupAnchorEnvelope("sg-0abc")
	rule := sgReachabilityRuleEnvelope(sgReachabilityRulePayload(
		"sg-0abc", "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8",
	))
	sgUID := cloudResourceUID(sgAccountID, sgRegion, "aws_ec2_security_group", "sg-0abc")
	ruleUID := securityGroupRuleUID(sgUID, "ingress", "tcp", int32(22), int32(22), "cidr_ipv4", "10.0.0.0/8")

	writer := &recordingSecurityGroupReachabilityWriter{}
	ledger := newStatefulProjectedSourceLedger()

	// Generation 1: the anchor and its rule both resolve. First generation for
	// the scope, so PriorGenerationCheck reports no prior and the retract is
	// skipped.
	gen1Handler := SecurityGroupReachabilityMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{anchor, rule}},
		Writer:               writer,
		Ledger:               ledger,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	gen1Intent := securityGroupReachabilityIntent()
	gen1Intent.GenerationID = "gen-1"
	if _, err := gen1Handler.Handle(context.Background(), gen1Intent); err != nil {
		t.Fatalf("gen1 Handle returned error: %v", err)
	}
	if writer.sgEdgeCalls != 1 || writer.toEdgeCalls != 1 {
		t.Fatalf("gen1 edge write calls = sg:%d to:%d, want 1/1", writer.sgEdgeCalls, writer.toEdgeCalls)
	}

	// Generation 2: only the SG anchor resource fact remains; ruleA's rule fact
	// is gone, so no rule node/edges resolve. A prior generation exists now, so
	// the retract runs.
	gen2Handler := SecurityGroupReachabilityMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{anchor}},
		Writer:               writer,
		Ledger:               ledger,
		ReadinessLookup:      allKeyspacesReady(),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	gen2Intent := securityGroupReachabilityIntent()
	gen2Intent.GenerationID = "gen-2"
	if _, err := gen2Handler.Handle(context.Background(), gen2Intent); err != nil {
		t.Fatalf("gen2 Handle returned error: %v", err)
	}

	if writer.retractByUIDsCalls != 1 {
		t.Fatalf("retractByUIDs calls = %d, want 1", writer.retractByUIDsCalls)
	}
	gotUIDs := append([]string(nil), writer.retractByUIDsUids...)
	sort.Strings(gotUIDs)
	wantUIDs := []string{sgUID, ruleUID}
	sort.Strings(wantUIDs)
	if len(gotUIDs) != len(wantUIDs) {
		t.Fatalf("retract anchored on %v, want the full PRIOR ledger set %v (not the current generation's empty set)",
			gotUIDs, wantUIDs)
	}
	for i := range gotUIDs {
		if gotUIDs[i] != wantUIDs[i] {
			t.Fatalf("retract anchored on %v, want the full PRIOR ledger set %v (not the current generation's empty set)",
				gotUIDs, wantUIDs)
		}
	}

	// After gen2, the ledger must be empty for this scope: gen2 pruned the
	// prior set and recorded nothing (its reach was empty), so no stale rows
	// linger that a later retract could double-anchor on.
	afterUIDs, err := ledger.ListSourceUIDsForScopes(context.Background(), securityGroupReachabilityEvidenceSource, []string{gen2Intent.ScopeID})
	if err != nil {
		t.Fatalf("ListSourceUIDsForScopes returned error: %v", err)
	}
	if len(afterUIDs) != 0 {
		t.Fatalf("ledger after gen2 = %v, want empty (gen2 produced no reachability rows to re-record)", afterUIDs)
	}
}
