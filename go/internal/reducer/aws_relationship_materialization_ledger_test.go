// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestAWSRelationshipMaterializationLedgerRecordsBeforeWrite proves the
// handler records the ledger BEFORE writing graph edges when a Ledger is
// wired, so the ledger stays a superset of graph edges even if the write
// crashes between the two calls.
func TestAWSRelationshipMaterializationLedgerRecordsBeforeWrite(t *testing.T) {
	t.Parallel()

	source := resourceEnvelope("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn", "arn:aws:lambda:us-east-1:111122223333:function:fn")
	target := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/abc", "arn:aws:kms:us-east-1:111122223333:key/abc")
	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	writer := &recordingCloudResourceEdgeWriter{}
	ledger := &fakeProjectedSourceLedger{}
	handler := AWSRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{source, target, rel}},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), awsRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if ledger.recordCalls != 1 {
		t.Fatalf("record calls = %d, want 1", ledger.recordCalls)
	}
	wantUID := cloudResourceUID("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn")
	if len(ledger.recordedUIDs) != 1 || ledger.recordedUIDs[0] != wantUID {
		t.Fatalf("recorded uids = %v, want [%s]", ledger.recordedUIDs, wantUID)
	}
	if ledger.recordedSource != awsRelationshipEvidenceSource {
		t.Fatalf("recorded evidence source = %q, want %q", ledger.recordedSource, awsRelationshipEvidenceSource)
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

// TestAWSRelationshipMaterializationLedgerRetractUsesLedgerUIDs proves retract
// enumerates uids from the ledger and calls the anchored-delete method, not the
// old whole-scope retract.
func TestAWSRelationshipMaterializationLedgerRetractUsesLedgerUIDs(t *testing.T) {
	t.Parallel()

	source := resourceEnvelope("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn", "arn:aws:lambda:us-east-1:111122223333:function:fn")
	target := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/abc", "arn:aws:kms:us-east-1:111122223333:key/abc")
	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	writer := &recordingCloudResourceEdgeWriter{}
	ledger := &fakeProjectedSourceLedger{listUIDs: []string{"uid-1", "uid-2"}}
	handler := AWSRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{source, target, rel}},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), awsRelationshipIntent()); err != nil {
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

// TestAWSRelationshipMaterializationLedgerSkipsRetractOnFirstGeneration proves
// the retract (and therefore the ledger list/prune) is skipped on the scope's
// first generation even when a Ledger is wired.
func TestAWSRelationshipMaterializationLedgerSkipsRetractOnFirstGeneration(t *testing.T) {
	t.Parallel()

	source := resourceEnvelope("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn", "arn:aws:lambda:us-east-1:111122223333:function:fn")
	target := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/abc", "arn:aws:kms:us-east-1:111122223333:key/abc")
	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	writer := &recordingCloudResourceEdgeWriter{}
	ledger := &fakeProjectedSourceLedger{}
	handler := AWSRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{source, target, rel}},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}

	if _, err := handler.Handle(context.Background(), awsRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractByUIDsCalls != 0 {
		t.Fatalf("retractByUIDs calls = %d, want 0 on first generation", writer.retractByUIDsCalls)
	}
	if ledger.pruneCalls != 0 {
		t.Fatalf("prune calls = %d, want 0 on first generation", ledger.pruneCalls)
	}
	if writer.writeCalls != 1 {
		t.Fatalf("write calls = %d, want 1", writer.writeCalls)
	}
	if ledger.recordCalls != 1 {
		t.Fatalf("record calls = %d, want 1 (record still happens even when retract is skipped)", ledger.recordCalls)
	}
}

// TestAWSRelationshipMaterializationNilLedgerPreservesOldRetractPath proves
// that when Ledger is nil, the handler falls back to the pre-ledger
// whole-scope retract, never calling the anchored by-uids method.
func TestAWSRelationshipMaterializationNilLedgerPreservesOldRetractPath(t *testing.T) {
	t.Parallel()

	source := resourceEnvelope("111122223333", "us-east-1", "aws_lambda_function",
		"arn:aws:lambda:us-east-1:111122223333:function:fn", "arn:aws:lambda:us-east-1:111122223333:function:fn")
	target := resourceEnvelope("111122223333", "us-east-1", "aws_kms_key",
		"arn:aws:kms:us-east-1:111122223333:key/abc", "arn:aws:kms:us-east-1:111122223333:key/abc")
	rel := awsRelationshipEnvelope(map[string]any{
		"account_id":         "111122223333",
		"region":             "us-east-1",
		"relationship_type":  "USES_KMS_KEY",
		"source_resource_id": "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"source_arn":         "arn:aws:lambda:us-east-1:111122223333:function:fn",
		"target_resource_id": "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_arn":         "arn:aws:kms:us-east-1:111122223333:key/abc",
		"target_type":        "aws_kms_key",
	})

	writer := &recordingCloudResourceEdgeWriter{}
	handler := AWSRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{source, target, rel}},
		EdgeWriter:           writer,
		Ledger:               nil,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), awsRelationshipIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("old retract calls = %d, want 1 when Ledger is nil", writer.retractCalls)
	}
	if writer.retractByUIDsCalls != 0 {
		t.Fatalf("retractByUIDs calls = %d, want 0 when Ledger is nil", writer.retractByUIDsCalls)
	}
}

// TestAWSRelationshipMaterializationLedgerLeakSafetyAcrossGenerations is the
// key accuracy regression for issue #4858: generation N resolves two distinct
// source Lambda functions (A, B) into edges and records BOTH into the ledger.
// Generation N+1 only resolves source A (B's relationship fact disappeared —
// e.g. the Lambda was deleted or its aws_relationship fact stopped being
// emitted). The anchored retract for generation N+1 MUST still target the
// ledger's PRIOR set {A, B}, not just the current generation's resolved set
// {A}; otherwise B's now-stale edge is never retracted and leaks in the graph
// forever (nothing in the current generation ever points at B again).
func TestAWSRelationshipMaterializationLedgerLeakSafetyAcrossGenerations(t *testing.T) {
	t.Parallel()

	const (
		account = "111122223333"
		region  = "us-east-1"
		fnA     = "arn:aws:lambda:us-east-1:111122223333:function:fn-a"
		fnB     = "arn:aws:lambda:us-east-1:111122223333:function:fn-b"
		kmsKey  = "arn:aws:kms:us-east-1:111122223333:key/abc"
	)
	sourceA := resourceEnvelope(account, region, "aws_lambda_function", fnA, fnA)
	sourceB := resourceEnvelope(account, region, "aws_lambda_function", fnB, fnB)
	target := resourceEnvelope(account, region, "aws_kms_key", kmsKey, kmsKey)
	relA := awsRelationshipEnvelope(map[string]any{
		"account_id": account, "region": region, "relationship_type": "USES_KMS_KEY",
		"source_resource_id": fnA, "source_arn": fnA,
		"target_resource_id": kmsKey, "target_arn": kmsKey, "target_type": "aws_kms_key",
	})
	relB := awsRelationshipEnvelope(map[string]any{
		"account_id": account, "region": region, "relationship_type": "USES_KMS_KEY",
		"source_resource_id": fnB, "source_arn": fnB,
		"target_resource_id": kmsKey, "target_arn": kmsKey, "target_type": "aws_kms_key",
	})
	uidA := cloudResourceUID(account, region, "aws_lambda_function", fnA)
	uidB := cloudResourceUID(account, region, "aws_lambda_function", fnB)

	writer := &recordingCloudResourceEdgeWriter{}
	ledger := newStatefulProjectedSourceLedger()

	// Generation 1: both A and B resolve. First generation for the scope, so
	// PriorGenerationCheck reports no prior and the retract is skipped.
	gen1Handler := AWSRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{sourceA, sourceB, target, relA, relB}},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	gen1Intent := awsRelationshipIntent()
	gen1Intent.GenerationID = "gen-1"
	if _, err := gen1Handler.Handle(context.Background(), gen1Intent); err != nil {
		t.Fatalf("gen1 Handle returned error: %v", err)
	}
	if writer.writeCalls != 1 || len(writer.writtenRows) != 2 {
		t.Fatalf("gen1 write calls = %d rows = %d, want 1 call / 2 rows", writer.writeCalls, len(writer.writtenRows))
	}

	// Generation 2: only A resolves (B's fact is gone). A prior generation
	// exists now, so the retract runs.
	gen2Handler := AWSRelationshipMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: []facts.Envelope{sourceA, target, relA}},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	gen2Intent := awsRelationshipIntent()
	gen2Intent.GenerationID = "gen-2"
	if _, err := gen2Handler.Handle(context.Background(), gen2Intent); err != nil {
		t.Fatalf("gen2 Handle returned error: %v", err)
	}

	if writer.retractByUIDsCalls != 1 {
		t.Fatalf("retractByUIDs calls = %d, want 1", writer.retractByUIDsCalls)
	}
	gotUIDs := append([]string(nil), writer.retractByUIDsUids...)
	sort.Strings(gotUIDs)
	wantUIDs := []string{uidA, uidB}
	sort.Strings(wantUIDs)
	if len(gotUIDs) != len(wantUIDs) {
		t.Fatalf("retract anchored on %v, want the full PRIOR ledger set %v (not just the current generation's %v)",
			gotUIDs, wantUIDs, []string{uidA})
	}
	for i := range gotUIDs {
		if gotUIDs[i] != wantUIDs[i] {
			t.Fatalf("retract anchored on %v, want the full PRIOR ledger set %v (not just the current generation's %v)",
				gotUIDs, wantUIDs, []string{uidA})
		}
	}

	// After gen2, the ledger must reflect only A (B was pruned along with the
	// scope's stale rows, then A alone was re-recorded from gen2's write).
	afterUIDs, err := ledger.ListSourceUIDsForScopes(context.Background(), awsRelationshipEvidenceSource, []string{gen2Intent.ScopeID})
	if err != nil {
		t.Fatalf("ListSourceUIDsForScopes returned error: %v", err)
	}
	if len(afterUIDs) != 1 || afterUIDs[0] != uidA {
		t.Fatalf("ledger after gen2 = %v, want [%s]", afterUIDs, uidA)
	}
}
