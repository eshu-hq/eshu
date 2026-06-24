// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsruntime

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestStartScanStatusClassifiesStaleFenceAsTerminal proves that when the
// storage layer rejects StartAWSScan with awscloud.ErrScanStatusStaleFence,
// the AWS claimed source returns an error that classifies as terminal with
// failure class "stale_fence". The ClaimedService runner uses this to route
// the claim through FailClaimTerminal rather than FailClaimRetryable so the
// orphaned-row symptom in issue #612 cannot manifest as an unbounded retry
// loop on workflow_claims.
func TestStartScanStatusClassifiesStaleFenceAsTerminal(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 24, 18, 0, 0, 0, time.UTC)
	item := awsWorkItem(now)
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
		Scanners:    &stubScannerFactory{scanner: stubScanner{}},
		ScanStatus:  &staleFenceScanStatusStore{},
		Clock:       func() time.Time { return now },
	}

	_, _, err := source.NextClaimed(context.Background(), item)
	if err == nil {
		t.Fatalf("NextClaimed() err = nil, want stale fence error")
	}
	if !errors.Is(err, awscloud.ErrScanStatusStaleFence) {
		t.Fatalf("NextClaimed() err = %v, want errors.Is awscloud.ErrScanStatusStaleFence", err)
	}
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) || classified.FailureClass() != "stale_fence" {
		got := ""
		if classified != nil {
			got = classified.FailureClass()
		}
		t.Fatalf("FailureClass() = %q, want stale_fence (err=%v)", got, err)
	}
	var terminal interface{ TerminalFailure() bool }
	if !errors.As(err, &terminal) || !terminal.TerminalFailure() {
		t.Fatalf("TerminalFailure() = false, want true (err=%v)", err)
	}
}

// staleFenceScanStatusStore returns awscloud.ErrScanStatusStaleFence on
// StartAWSScan so tests can prove the classifier wires the typed error into
// the workflow claim terminal-fail path.
type staleFenceScanStatusStore struct{}

func (staleFenceScanStatusStore) StartAWSScan(context.Context, awscloud.ScanStatusStart) error {
	return fmt.Errorf("start AWS scan status: %w", awscloud.ErrScanStatusStaleFence)
}

func (staleFenceScanStatusStore) ObserveAWSScan(context.Context, awscloud.ScanStatusObservation) error {
	return nil
}

// TestStartScanStatusIncrementsStaleFenceCounter proves that the
// classification path also records the eshu_dp_aws_scan_status_stale_fence_total
// counter so an operator can attribute the runtime symptom of issue #612 to
// the (service, account, region) tuple that produced the rejection.
func TestStartScanStatusIncrementsStaleFenceCounter(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 24, 19, 0, 0, 0, time.UTC)
	item := awsWorkItem(now)
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() err = %v", err)
	}
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
		Scanners:    &stubScannerFactory{scanner: stubScanner{}},
		ScanStatus:  &staleFenceScanStatusStore{},
		Instruments: instruments,
		Clock:       func() time.Time { return now },
	}

	if _, _, err := source.NextClaimed(context.Background(), item); err == nil {
		t.Fatalf("NextClaimed() err = nil, want stale fence error")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() err = %v", err)
	}
	got := awsRuntimeCounterValue(t, rm, "eshu_dp_aws_scan_status_stale_fence_total", map[string]string{
		telemetry.MetricDimensionService:   awscloud.ServiceIAM,
		telemetry.MetricDimensionAccount:   "123456789012",
		telemetry.MetricDimensionRegion:    "us-east-1",
		telemetry.MetricDimensionOperation: "start",
	})
	if got != 1 {
		t.Fatalf("stale fence counter = %d, want 1", got)
	}
}
