// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/trace/noop"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// helperProcessEnv switches this test binary into the fake extension role.
const helperProcessEnv = "ESHU_TEST_EXTENSION_HELPER"

// TestExtensionHelperProcess is not a test of its own. The worker test below
// runs this test binary as the component's process extension; with
// helperProcessEnv set it reads the SDK request from stdin, echoes the claim,
// and answers retryable with retry_after_seconds=120.
func TestExtensionHelperProcess(t *testing.T) {
	if os.Getenv(helperProcessEnv) != "retryable" {
		return
	}
	var request extensionhost.Request
	if err := json.NewDecoder(os.Stdin).Decode(&request); err != nil {
		os.Exit(2)
	}
	result := sdkcollector.Result{
		ProtocolVersion: request.ProtocolVersion,
		State:           sdkcollector.ResultRetryable,
		Claim:           request.Claim,
		Generation: sdkcollector.Generation{
			ID:         request.Claim.GenerationID,
			ObservedAt: time.Now().UTC(),
		},
		Statuses: []sdkcollector.Status{{
			Class:             sdkcollector.StatusFailure,
			FailureClass:      "rate_limited",
			RetryAfterSeconds: 120,
			SourceLatencyMS:   300,
		}},
	}
	if err := json.NewEncoder(os.Stdout).Encode(result); err != nil {
		os.Exit(3)
	}
	os.Exit(0)
}

// TestBuildClaimedServiceRecordsExtensionStatusAndHonorsRetryAfter proves the
// worker sets the status recorder and honors the provider retry delay end to
// end: buildClaimedService runs the real process runner against a helper
// extension, the status fixture moves the component-extension instruments,
// one bounded log line is written, and the retryable claim stays hidden for at
// least the requested 120 seconds with the throttle counter observing it.
func TestBuildClaimedServiceRecordsExtensionStatusAndHonorsRetryAfter(t *testing.T) {
	t.Parallel()

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v, want nil", err)
	}
	componentHome := t.TempDir()
	installEnabledComponent(t, componentHome, "process", writeHelperExtensionConfig(t, executable))
	inst, reader := grantInstruments(t)
	logs := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(logs, nil))

	service, err := buildClaimedService(&fakeExecQueryer{}, mapEnv(map[string]string{
		envComponentHome:            componentHome,
		envComponentTrustMode:       component.TrustModeAllowlist,
		envComponentAllowIDs:        "dev.eshu.examples.scorecard",
		envComponentAllowPublishers: "eshu-hq",
		envComponentCoreVersion:     "dev",
		envCollectorInstanceID:      "scorecard-local",
	}), noop.NewTracerProvider().Tracer("test"), inst, logger)
	if err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil", err)
	}

	now := time.Now().UTC().Truncate(time.Second)
	item := helperWorkItem(now)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	store := &workerClaimStore{item: item, cancel: cancel}
	service.ControlStore = store
	service.Committer = workerCommitter{}
	service.ClaimIDFunc = func() string { return item.CurrentClaimID }
	service.Clock = func() time.Time { return now }

	if err := service.Run(ctx); err != nil {
		t.Fatalf("Run() error = %v, want nil", err)
	}
	if store.retryableCalls != 1 {
		t.Fatalf("FailClaimRetryable calls = %d, want 1 (terminal=%d, class=%q)",
			store.retryableCalls, store.terminalCalls, store.failureClass)
	}
	if got, want := store.visibleAt.Sub(now), 120*time.Second; got < want {
		t.Fatalf("retry visible after %s, want at least %s", got, want)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v, want nil", err)
	}
	if got := counterWith(rm, contract.MetricComponentExtensionResults, "failure_class", "rate_limited"); got != 1 {
		t.Fatalf("%s{failure_class=rate_limited} = %d, want 1", contract.MetricComponentExtensionResults, got)
	}
	if got := counterWith(rm, contract.MetricComponentExtensionStatusRecords, "collector_kind", "scorecard"); got != 1 {
		t.Fatalf("%s{collector_kind=scorecard} = %d, want 1", contract.MetricComponentExtensionStatusRecords, got)
	}
	if got := counterWith(rm, "eshu_dp_workflow_claim_provider_throttle_total", "outcome", "retry_after_honored"); got != 1 {
		t.Fatalf("provider throttle{outcome=retry_after_honored} = %d, want 1", got)
	}

	var resultLine string
	for _, line := range strings.Split(logs.String(), "\n") {
		if strings.Contains(line, `"msg":"component extension result"`) {
			resultLine = line
		}
	}
	if resultLine == "" {
		t.Fatalf("no component extension result log line in %s", logs.String())
	}
	if !strings.Contains(resultLine, `"extension_status.state":"retryable"`) ||
		!strings.Contains(resultLine, `"extension_status.retry_after_seconds":120`) {
		t.Fatalf("result log line = %s, want retryable with retry_after_seconds 120", resultLine)
	}
	for _, leaked := range []string{item.ScopeID, item.CollectorInstanceID, item.WorkItemID, item.CurrentClaimID} {
		if strings.Contains(resultLine, leaked) {
			t.Fatalf("result log line carries identity value %q: %s", leaked, resultLine)
		}
	}
}

func writeHelperExtensionConfig(t *testing.T, executable string) string {
	t.Helper()
	path := t.TempDir() + "/config.yaml"
	raw := `host:
  sourceSystem: openssf-scorecard
  scope:
    id: github.com/example/widgets
    kind: repository
process:
  command: ` + executable + `
  args:
    - -test.run=^TestExtensionHelperProcess$
  env:
    - ` + helperProcessEnv + `=retryable
`
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func helperWorkItem(now time.Time) workflow.WorkItem {
	return workflow.WorkItem{
		WorkItemID:          "work-scorecard-widgets-1",
		RunID:               "run-scorecard",
		CollectorKind:       scope.CollectorKind("scorecard"),
		CollectorInstanceID: "scorecard-local",
		SourceSystem:        "openssf-scorecard",
		ScopeID:             "github.com/example/widgets",
		TenantID:            "tenant-1",
		WorkspaceID:         "workspace-1",
		SubjectClass:        "repository",
		PolicyRevisionHash:  "policy-hash",
		AcceptanceUnitID:    "acceptance-unit",
		SourceRunID:         "generation-1",
		GenerationID:        "generation-1",
		FairnessKey:         "openssf-scorecard:widgets",
		Status:              workflow.WorkItemStatusClaimed,
		AttemptCount:        1,
		CurrentClaimID:      "claim-widgets-1",
		CurrentFencingToken: 7,
		CurrentOwnerID:      "worker-1",
		LeaseExpiresAt:      now.Add(2 * time.Minute),
		VisibleAt:           now.Add(-time.Minute),
		LastClaimedAt:       now.Add(-time.Second),
		CreatedAt:           now.Add(-10 * time.Minute),
		UpdatedAt:           now.Add(-time.Second),
	}
}

// workerClaimStore hands out one claimed item and records the failure.
type workerClaimStore struct {
	item           workflow.WorkItem
	cancel         context.CancelFunc
	claimed        bool
	retryableCalls int
	terminalCalls  int
	failureClass   string
	visibleAt      time.Time
}

func (s *workerClaimStore) ClaimNextEligible(
	_ context.Context, _ workflow.ClaimSelector, now time.Time, _ time.Duration,
) (workflow.WorkItem, workflow.Claim, bool, error) {
	if s.claimed {
		return workflow.WorkItem{}, workflow.Claim{}, false, nil
	}
	s.claimed = true
	return s.item, workflow.Claim{
		ClaimID:        s.item.CurrentClaimID,
		WorkItemID:     s.item.WorkItemID,
		FencingToken:   s.item.CurrentFencingToken,
		OwnerID:        s.item.CurrentOwnerID,
		Status:         workflow.ClaimStatusActive,
		ClaimedAt:      now.Add(-time.Second),
		HeartbeatAt:    now.Add(-time.Second),
		LeaseExpiresAt: s.item.LeaseExpiresAt,
		CreatedAt:      now.Add(-time.Second),
		UpdatedAt:      now,
	}, true, nil
}

func (s *workerClaimStore) HeartbeatClaim(context.Context, workflow.ClaimMutation) error { return nil }

func (s *workerClaimStore) CompleteClaim(context.Context, workflow.ClaimMutation) error {
	s.cancel()
	return nil
}

func (s *workerClaimStore) ReleaseClaim(context.Context, workflow.ClaimMutation) error {
	s.cancel()
	return nil
}

func (s *workerClaimStore) FailClaimRetryable(_ context.Context, mutation workflow.ClaimMutation) error {
	s.retryableCalls++
	s.failureClass = mutation.FailureClass
	s.visibleAt = mutation.VisibleAt
	s.cancel()
	return nil
}

func (s *workerClaimStore) FailClaimTerminal(_ context.Context, mutation workflow.ClaimMutation) error {
	s.terminalCalls++
	s.failureClass = mutation.FailureClass
	s.cancel()
	return nil
}

// workerCommitter drains and discards a generation; the retryable path never
// commits.
type workerCommitter struct{}

func (workerCommitter) CommitScopeGeneration(
	context.Context, scope.IngestionScope, scope.ScopeGeneration, <-chan facts.Envelope,
) error {
	return nil
}

func (workerCommitter) CommitClaimedScopeGeneration(
	_ context.Context, _ workflow.ClaimMutation, _ scope.IngestionScope, _ scope.ScopeGeneration,
	envelopes <-chan facts.Envelope,
) error {
	for range envelopes {
	}
	return nil
}

// counterWith sums an Int64 counter's points whose attribute key equals value.
func counterWith(rm metricdata.ResourceMetrics, name, key, value string) int64 {
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || m.Name != name {
				continue
			}
			for _, dp := range sum.DataPoints {
				if got, ok := dp.Attributes.Value(attributeKey(key)); ok && got.AsString() == value {
					total += dp.Value
				}
			}
		}
	}
	return total
}
