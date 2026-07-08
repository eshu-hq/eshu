// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// ec2Alarm2CoverageFixture builds a second, independent alarm/instance
// coverage pair so ledger tests can prove multi-source retract behavior.
func ec2Alarm2CoverageFixture() []facts.Envelope {
	instance := awsResourceFact("fact-ec2-2", "aws_ec2_instance", "i-2222222222abcdef0",
		"arn:aws:ec2:us-east-1:111122223333:instance/i-2222222222abcdef0", "web-2", false)
	alarm := awsResourceFact("fact-alarm-2", "aws_cloudwatch_alarm",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high-2",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high-2", "cpu-high-2", false)
	rel := alarmObservesMetricFact("fact-rel-2",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high-2", "metric-2",
		[]map[string]any{{"name": "InstanceId", "value": "i-2222222222abcdef0"}})
	return []facts.Envelope{instance, alarm, rel}
}

// TestObservabilityCoverageMaterializationLedgerRecordsBeforeWrite proves the
// handler records the ledger BEFORE writing graph edges when a Ledger is
// wired, keyed by the observability (alarm) uid, not the target uid.
func TestObservabilityCoverageMaterializationLedgerRecordsBeforeWrite(t *testing.T) {
	t.Parallel()

	writer := &recordingObservabilityCoverageEdgeWriter{}
	ledger := &fakeProjectedSourceLedger{}
	handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2AlarmCoverageFixture()},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if ledger.recordCalls != 1 {
		t.Fatalf("record calls = %d, want 1", ledger.recordCalls)
	}
	alarmUID := cloudResourceUID(testCoverageAccount, testCoverageRegion, "aws_cloudwatch_alarm",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high")
	if len(ledger.recordedUIDs) != 1 || ledger.recordedUIDs[0] != alarmUID {
		t.Fatalf("recorded uids = %v, want [%s] (the observability/alarm uid, not the target)", ledger.recordedUIDs, alarmUID)
	}
	if ledger.recordedSource != observabilityCoverageEvidenceSource {
		t.Fatalf("recorded evidence source = %q, want %q", ledger.recordedSource, observabilityCoverageEvidenceSource)
	}
	if len(ledger.callOrder) < 3 || ledger.callOrder[0] != "list" || ledger.callOrder[1] != "prune" || ledger.callOrder[2] != "record" {
		t.Fatalf("call order = %v, want [list prune record]", ledger.callOrder)
	}
}

// TestObservabilityCoverageMaterializationLedgerRetractUsesLedgerUIDs proves
// retract enumerates uids from the ledger and calls the anchored-delete
// method, not the old whole-scope retract.
func TestObservabilityCoverageMaterializationLedgerRetractUsesLedgerUIDs(t *testing.T) {
	t.Parallel()

	writer := &recordingObservabilityCoverageEdgeWriter{}
	ledger := &fakeProjectedSourceLedger{listUIDs: []string{"uid-1", "uid-2"}}
	handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2AlarmCoverageFixture()},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent()); err != nil {
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

// TestObservabilityCoverageMaterializationNilLedgerPreservesOldRetractPath
// proves the pre-ledger whole-scope retract still runs when Ledger is nil.
func TestObservabilityCoverageMaterializationNilLedgerPreservesOldRetractPath(t *testing.T) {
	t.Parallel()

	writer := &recordingObservabilityCoverageEdgeWriter{}
	handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2AlarmCoverageFixture()},
		EdgeWriter:           writer,
		Ledger:               nil,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent()); err != nil {
		t.Fatalf("Handle returned error: %v", err)
	}
	if writer.retractCalls != 1 {
		t.Fatalf("old retract calls = %d, want 1 when Ledger is nil", writer.retractCalls)
	}
	if writer.retractByUIDsCalls != 0 {
		t.Fatalf("retractByUIDs calls = %d, want 0 when Ledger is nil", writer.retractByUIDsCalls)
	}
}

// TestObservabilityCoverageMaterializationLedgerLeakSafetyAcrossGenerations
// proves generation N+1's retract anchors on the ledger's full prior source
// (alarm) set, not just the current generation's resolved set, when the
// second alarm's coverage fact disappears between generations.
func TestObservabilityCoverageMaterializationLedgerLeakSafetyAcrossGenerations(t *testing.T) {
	t.Parallel()

	writer := &recordingObservabilityCoverageEdgeWriter{}
	ledger := newStatefulProjectedSourceLedger()

	envelopesGen1 := append(append([]facts.Envelope{}, ec2AlarmCoverageFixture()...), ec2Alarm2CoverageFixture()...)
	gen1Handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: envelopesGen1},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return false, nil },
	}
	gen1Intent := observabilityCoverageMaterializationIntent()
	gen1Intent.GenerationID = "gen-1"
	if _, err := gen1Handler.Handle(context.Background(), gen1Intent); err != nil {
		t.Fatalf("gen1 Handle returned error: %v", err)
	}
	if writer.writeCalls != 1 || len(writer.writtenRows) != 2 {
		t.Fatalf("gen1 write calls = %d rows = %d, want 1 call / 2 rows", writer.writeCalls, len(writer.writtenRows))
	}

	// Generation 2: the second alarm's coverage fact is gone.
	gen2Handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2AlarmCoverageFixture()},
		EdgeWriter:           writer,
		Ledger:               ledger,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}
	gen2Intent := observabilityCoverageMaterializationIntent()
	gen2Intent.GenerationID = "gen-2"
	if _, err := gen2Handler.Handle(context.Background(), gen2Intent); err != nil {
		t.Fatalf("gen2 Handle returned error: %v", err)
	}

	alarm1UID := cloudResourceUID(testCoverageAccount, testCoverageRegion, "aws_cloudwatch_alarm",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high")
	alarm2UID := cloudResourceUID(testCoverageAccount, testCoverageRegion, "aws_cloudwatch_alarm",
		"arn:aws:cloudwatch:us-east-1:111122223333:alarm:cpu-high-2")

	if writer.retractByUIDsCalls != 1 {
		t.Fatalf("retractByUIDs calls = %d, want 1", writer.retractByUIDsCalls)
	}
	gotUIDs := append([]string(nil), writer.retractByUIDsUids...)
	sort.Strings(gotUIDs)
	wantUIDs := []string{alarm1UID, alarm2UID}
	sort.Strings(wantUIDs)
	if len(gotUIDs) != len(wantUIDs) {
		t.Fatalf("retract anchored on %v, want the full PRIOR ledger set %v", gotUIDs, wantUIDs)
	}
	for i := range gotUIDs {
		if gotUIDs[i] != wantUIDs[i] {
			t.Fatalf("retract anchored on %v, want the full PRIOR ledger set %v", gotUIDs, wantUIDs)
		}
	}
}
