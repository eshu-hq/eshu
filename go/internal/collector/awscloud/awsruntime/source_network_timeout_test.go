// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsruntime

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/catalog"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/cloud"
)

// TestClaimedSourceMarksNetworkTimeoutWarningAsPartial proves that a scan
// carrying only a network_timeout degrade warning (issue #9535) observes
// ScanStatusPartial with failure class network_timeout, following the
// throttle-sustained contract: the scan produced durable warning evidence
// for an incomplete but resumable read.
func TestClaimedSourceMarksNetworkTimeoutWarningAsPartial(t *testing.T) {
	now := time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC)
	item := awsWorkItem(now)
	statusStore := &stubScanStatusStore{}
	source := ClaimedSource{
		Config: Config{
			CollectorInstanceID: item.CollectorInstanceID,
			Targets: []TargetScope{{
				AccountID:       "123456789012",
				AllowedRegions:  []string{"us-east-1"},
				AllowedServices: []string{catalog.ServiceIAM},
				Credentials: CredentialConfig{
					Mode: CredentialModeLocalWorkloadIdentity,
				},
			}},
		},
		Credentials: &stubCredentialProvider{lease: &stubCredentialLease{}},
		Scanners: &stubScannerFactory{scanner: stubScanner{
			envelopes: []facts.Envelope{{
				FactKind: cloud.AWSWarningFactKind,
				Payload:  map[string]any{"warning_kind": WarningNetworkTimeout},
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
	if observation.FailureClass != "network_timeout" {
		t.Fatalf("failure class = %q, want network_timeout", observation.FailureClass)
	}
}
