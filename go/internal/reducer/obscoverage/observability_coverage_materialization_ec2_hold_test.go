// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package obscoverage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
	reducercontract "github.com/eshu-hq/eshu/go/internal/reducer/contract"
	"github.com/eshu-hq/eshu/go/internal/reducer/gpphase"
)

// ec2InstancePostureFact builds the ec2_instance_posture fact that makes
// BuildInstanceNodeMaterializationReducerIntent enqueue the EC2 instance-node
// domain for a scope generation. Its payload mirrors the posture fact in
// testdata/cassettes/obscoverage/ifa-observability-coverage-family.json.
func ec2InstancePostureFact(factID, instanceID string) facts.Envelope {
	return facts.Envelope{
		FactID:   factID,
		FactKind: cloud.EC2InstancePostureFactKind,
		Payload: map[string]any{
			"account_id":    testCoverageAccount,
			"region":        testCoverageRegion,
			"resource_type": "aws_ec2_instance",
			"instance_id":   instanceID,
			"arn":           "arn:aws:ec2:us-east-1:111122223333:instance/" + instanceID,
			"state":         "running",
		},
	}
}

// awsPhaseOnlyLookup reports the AWS CloudResource phase committed and the EC2
// instance-node phase absent, the state a scope with no posture fact stays in
// forever because nothing ever publishes that second phase.
func awsPhaseOnlyLookup(key gpphase.PhaseKey, _ gpphase.Phase) (bool, bool) {
	if strings.Contains(string(key.AcceptanceUnitID), "ec2_instance_node_materialization:") {
		return false, false
	}
	return true, true
}

// failingFactLoader fails every read so the posture probe's error path is
// observable.
type failingFactLoader struct{ err error }

func (f failingFactLoader) ListFacts(context.Context, string, string) ([]facts.Envelope, error) {
	return nil, f.err
}

func TestObservabilityCoverageMaterializationProceedsWithoutPostureFacts(t *testing.T) {
	t.Parallel()

	// A per-service cloudwatchlogs scope carries no ec2_instance_posture fact,
	// so no ec2_instance_node_materialization intent exists and its phase is
	// never published. Waiting on it can never converge, and the miss is a
	// non-counting retry class, so it would never dead-letter either (#9055).
	writer := &recordingObservabilityCoverageEdgeWriter{}
	handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:           &stubFactLoader{envelopes: ec2AlarmCoverageFixture()},
		EdgeWriter:           writer,
		ReadinessLookup:      awsPhaseOnlyLookup,
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	result, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent())
	if err != nil {
		t.Fatalf("Handle() error = %v, want nil: a scope with no posture fact must not wait on the EC2 node phase", err)
	}
	if result.Status != reducercontract.ResultStatusSucceeded {
		t.Fatalf("status = %q, want succeeded", result.Status)
	}
	if writer.writeCalls != 1 || len(writer.writtenRows) != 1 {
		t.Fatalf("writes = %d rows = %d, want 1 call with the 1 exact COVERS row", writer.writeCalls, len(writer.writtenRows))
	}
}

func TestObservabilityCoverageMaterializationWritesWhenPostureAndEC2PhaseReady(t *testing.T) {
	t.Parallel()

	loader := &stubFactLoader{envelopes: append(ec2AlarmCoverageFixture(), ec2InstancePostureFact("fact-posture", "i-0123456789abcdef0"))}
	writer := &recordingObservabilityCoverageEdgeWriter{}
	handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:           loader,
		EdgeWriter:           writer,
		ReadinessLookup:      readyLookup(true, true),
		PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
	}

	if _, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent()); err != nil {
		t.Fatalf("Handle() error = %v, want nil", err)
	}
	if len(writer.writtenRows) != 1 {
		t.Fatalf("written COVERS rows = %d, want 1", len(writer.writtenRows))
	}
	// A committed EC2 phase needs no posture probe: only the coverage load runs.
	if loader.calls != 1 {
		t.Fatalf("fact loads = %d, want 1 (no posture probe once the EC2 phase is ready)", loader.calls)
	}
}

func TestObservabilityCoverageMaterializationFailsWhenPostureProbeFails(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("fact store unavailable")
	writer := &recordingObservabilityCoverageEdgeWriter{}
	handler := ObservabilityCoverageMaterializationHandler{
		FactLoader:      failingFactLoader{err: sentinel},
		EdgeWriter:      writer,
		ReadinessLookup: awsPhaseOnlyLookup,
	}

	_, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent())
	if !errors.Is(err, sentinel) {
		t.Fatalf("Handle() error = %v, want the posture probe's fact-load error", err)
	}
	var notReady observabilityCoverageNodesNotReadyError
	if errors.As(err, &notReady) {
		t.Fatalf("a failed posture probe must not be reported as a readiness wait: %v", err)
	}
	if writer.writeCalls != 0 || writer.retractCalls != 0 {
		t.Fatalf("no graph writes allowed when the posture probe fails: write=%d retract=%d", writer.writeCalls, writer.retractCalls)
	}
}

// TestObservabilityCoverageMaterializationLogsEC2HoldDecision is not parallel:
// it swaps the process-wide slog default to capture the completion log.
func TestObservabilityCoverageMaterializationLogsEC2HoldDecision(t *testing.T) {
	cases := []struct {
		name     string
		envelope []facts.Envelope
		lookup   gpphase.ReadinessLookup
		want     string
	}{
		{"no posture fact", ec2AlarmCoverageFixture(), awsPhaseOnlyLookup, ec2NodeHoldNotRequired},
		{"posture and EC2 phase ready", append(ec2AlarmCoverageFixture(), ec2InstancePostureFact("fact-posture", "i-0123456789abcdef0")), readyLookup(true, true), ec2NodeHoldSatisfied},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			defer slog.SetDefault(previous)

			handler := ObservabilityCoverageMaterializationHandler{
				FactLoader:           &stubFactLoader{envelopes: tc.envelope},
				EdgeWriter:           &recordingObservabilityCoverageEdgeWriter{},
				ReadinessLookup:      tc.lookup,
				PriorGenerationCheck: func(context.Context, string, string) (bool, error) { return true, nil },
			}
			if _, err := handler.Handle(context.Background(), observabilityCoverageMaterializationIntent()); err != nil {
				t.Fatalf("Handle() error = %v, want nil", err)
			}
			var entry map[string]any
			for _, line := range bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte("\n")) {
				var candidate map[string]any
				if json.Unmarshal(line, &candidate) == nil && candidate["msg"] == "observability coverage materialization completed" {
					entry = candidate
				}
			}
			if entry == nil {
				t.Fatalf("no completion log line in %q", logs.String())
			}
			if got := entry["ec2_node_hold"]; got != tc.want {
				t.Fatalf("ec2_node_hold = %v, want %q", got, tc.want)
			}
		})
	}
}
