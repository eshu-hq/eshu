// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsruntime

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestClaimedSourceMarksThrottleWarningAsPartial(t *testing.T) {
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	item := awsWorkItem(now)
	statusStore := &stubScanStatusStore{}
	source := ClaimedSource{
		Config: Config{
			CollectorInstanceID: item.CollectorInstanceID,
			Targets: []TargetScope{{
				AccountID:       "123456789012",
				AllowedRegions:  []string{"us-east-1"},
				AllowedServices: []string{awscloud.ServiceIAM},
				Credentials: CredentialConfig{
					Mode: CredentialModeLocalWorkloadIdentity,
				},
			}},
		},
		Credentials: &stubCredentialProvider{lease: &stubCredentialLease{}},
		Scanners: &stubScannerFactory{scanner: stubScanner{
			apiEvents: []stubAPICallEvent{
				{operation: "GetResources", result: "error", throttled: true},
			},
			envelopes: []facts.Envelope{{
				FactKind: facts.AWSWarningFactKind,
				Payload:  map[string]any{"warning_kind": WarningThrottleSustained},
			}},
		}},
		Clock:      func() time.Time { return now },
		ScanStatus: statusStore,
	}

	collected, ok, err := source.NextClaimed(context.Background(), item)
	if err != nil {
		t.Fatalf("NextClaimed() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("NextClaimed() ok = false, want true")
	}
	if got := drainFacts(t, collected.Facts); len(got) != 1 {
		t.Fatalf("fact count = %d, want 1", len(got))
	}
	if len(statusStore.observations) != 1 {
		t.Fatalf("ObserveAWSScan calls = %d, want 1", len(statusStore.observations))
	}
	observation := statusStore.observations[0]
	if observation.Status != awscloud.ScanStatusPartial {
		t.Fatalf("status = %q, want partial", observation.Status)
	}
	if observation.FailureClass != "throttled" {
		t.Fatalf("failure class = %q, want throttled", observation.FailureClass)
	}
	if observation.APICallCount != 1 || observation.ThrottleCount != 1 {
		t.Fatalf("api/throttle counts = %d/%d, want 1/1", observation.APICallCount, observation.ThrottleCount)
	}
}
