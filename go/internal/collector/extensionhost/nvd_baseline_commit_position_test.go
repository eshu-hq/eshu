// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// testNVDBaselineMidInstantScope is a chunk that resumes inside the instant
// 2025-10-18T21:10:00.120Z at CVE-2025-10000 (eshu-hq/eshu#10809).
const (
	testNVDBaselineInstantMicros   = int64(1760821800120000)
	testNVDBaselineMidInstantScope = "vuln-intel://nvd/_baseline/baseline-2026-11/2025/1760821800120000/CVE-2025-10000"
)

func positionPayload(advanceTo int64, cveID string, complete bool) map[string]any {
	payload := testNVDBaselineVerdictPayload()
	payload["nvd_baseline_advance_to_unix_us"] = advanceTo
	if cveID != "" {
		payload[nvdBaselineAdvanceToCVEIDPayloadKey] = cveID
	}
	payload["nvd_baseline_year_complete"] = complete
	return payload
}

func baselineWriterArgsFor(t *testing.T, scopeID string, payload map[string]any) []any {
	t.Helper()
	source := testNVDBaselineSource(t, testNVDBaselinePolicy(t))
	item := testWorkItem()
	item.ScopeID = scopeID
	result := completeResult(item)
	envelopes := []facts.Envelope{testNVDWindowSnapshotEnvelope(payload)}
	collected := source.collectedGeneration(item, result, envelopes, "")
	if err := source.applyCommitPolicy(item, result.Generation.ObservedAt, envelopes, &collected); err != nil {
		t.Fatalf("applyCommitPolicy() error = %v, want a decision", err)
	}
	if collected.StateWriter == nil {
		return nil
	}
	recorder := &affectedCommitExecutor{}
	if err := collected.StateWriter(context.Background(), recorder); err != nil {
		t.Fatalf("StateWriter() error = %v", err)
	}
	return recorder.advanceArgs(t)
}

// TestNVDBaselineCommitPolicyRequiresStrictPositionAdvance pins the position
// contract (eshu-hq/eshu#10809): the resume position is the row (advance_to,
// advance_to CVE ID) compared in byte order, an incomplete verdict must move
// strictly past the chunk start in that order, a complete one may equal it,
// and none may move behind it. A chunk that resumes inside an instant advances
// by CVE ID alone, and CVE-2025-10000 sorts before CVE-2025-9999.
func TestNVDBaselineCommitPolicyRequiresStrictPositionAdvance(t *testing.T) {
	t.Parallel()

	const instant = testNVDBaselineInstantMicros
	cases := []struct {
		name     string
		scope    string
		advance  int64
		cveID    string
		complete bool
		armed    bool
	}{
		{"id past start inside the instant", testNVDBaselineMidInstantScope, instant, "CVE-2025-9999", false, true},
		{"later instant, empty id", testNVDBaselineMidInstantScope, instant + 1000, "", false, true},
		{"empty id at the start of the instant", "vuln-intel://nvd/_baseline/baseline-2026-11/2025/1760821800120000", instant, "CVE-2025-1", false, true},
		{"incomplete at the same position", testNVDBaselineMidInstantScope, instant, "CVE-2025-10000", false, false},
		{"complete at the same position", testNVDBaselineMidInstantScope, instant, "CVE-2025-10000", true, true},
		{"id behind start inside the instant", testNVDBaselineMidInstantScope, instant, "CVE-2025-1", false, false},
		{"complete behind start inside the instant", testNVDBaselineMidInstantScope, instant, "CVE-2025-1", true, false},
		{"empty id is behind a CVE ID in the same instant", testNVDBaselineMidInstantScope, instant, "", false, false},
		{"lowercase id is not canonical", testNVDBaselineMidInstantScope, instant, "cve-2025-99999", false, false},
		{"padded id is not canonical", testNVDBaselineMidInstantScope, instant, " CVE-2025-99999", false, false},
		{"id inside a sub-millisecond stamp", testNVDBaselineMidInstantScope, instant + 500, "CVE-2025-99999", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			args := baselineWriterArgsFor(t, tc.scope, positionPayload(tc.advance, tc.cveID, tc.complete))
			if armed := args != nil; armed != tc.armed {
				t.Fatalf("armed = %v, want %v (args %v)", armed, tc.armed, args)
			}
			if args != nil {
				if args[2] != tc.advance || args[3] != tc.cveID {
					t.Fatalf("resume = (%v, %v), want the committed position (%d, %q)", args[2], args[3], tc.advance, tc.cveID)
				}
			}
		})
	}
}

// TestNVDBaselineCommitPolicyReadsSeedGenerationAndStaysSafeWithAnOldCollector
// proves the two optional verdict keys reach the writer, and that a collector
// that sends neither (every collector before eshu-hq/eshu-sdk#97) commits as
// it did before: empty CVE ID, stamp 0 so the rollback guard is skipped.
func TestNVDBaselineCommitPolicyReadsSeedGenerationAndStaysSafeWithAnOldCollector(t *testing.T) {
	t.Parallel()

	payload := positionPayload(testNVDBaselineChunkStartMicros+1000, "", false)
	payload[nvdBaselineYearGeneratedPayloadKey] = int64(1761000000000000)
	args := baselineWriterArgsFor(t, testNVDBaselineScopeForCommit, payload)
	if args == nil || args[5] != int64(1761000000000000) {
		t.Fatalf("stamped verdict args = %v, want the seed generation at arg 5", args)
	}
	old := baselineWriterArgsFor(t, testNVDBaselineScopeForCommit, testNVDBaselineVerdictPayload())
	if old == nil || old[3] != "" || old[5] != int64(0) {
		t.Fatalf("old-collector verdict args = %v, want empty CVE ID and stamp 0", old)
	}
	for name, bad := range map[string]any{"negative": int64(-5), "string": "now", "float64 round trip ok": float64(1761000000000000)} {
		payload := positionPayload(testNVDBaselineChunkStartMicros+1000, "", false)
		payload[nvdBaselineYearGeneratedPayloadKey] = bad
		armed := baselineWriterArgsFor(t, testNVDBaselineScopeForCommit, payload) != nil
		if want := name == "float64 round trip ok"; armed != want {
			t.Fatalf("seed stamp %s armed = %v, want %v", name, armed, want)
		}
	}
	nonString := positionPayload(testNVDBaselineChunkStartMicros+1000, "", false)
	nonString[nvdBaselineAdvanceToCVEIDPayloadKey] = int64(7)
	if baselineWriterArgsFor(t, testNVDBaselineScopeForCommit, nonString) != nil {
		t.Fatal("a non-string CVE ID armed the writer, want the checkpoint held")
	}
}

func baselineSeedPolicy(t *testing.T) (*NVDBaselineCommitPolicy, *sdkmetric.ManualReader, *bytes.Buffer) {
	t.Helper()
	var logs bytes.Buffer
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	policy, err := NewNVDBaselineCommitPolicy("scorecard-instance", instruments, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err != nil {
		t.Fatalf("NewNVDBaselineCommitPolicy() error = %v", err)
	}
	return policy, reader, &logs
}

func seedRotationCounts(t *testing.T, reader *sdkmetric.ManualReader) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	counts := map[string]int64{}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, m := range scopeMetrics.Metrics {
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok || m.Name != "eshu_dp_nvd_baseline_seed_rotations_total" {
				continue
			}
			for _, point := range sum.DataPoints {
				direction, _ := point.Attributes.Value("direction")
				counts[direction.AsString()] += point.Value
			}
		}
	}
	return counts
}

func armedBaselineWriter(t *testing.T, policy *NVDBaselineCommitPolicy, item workflow.WorkItem) func(context.Context, *affectedCommitExecutor) error {
	t.Helper()
	payload := positionPayload(testNVDBaselineChunkStartMicros+1000, "", false)
	payload[nvdBaselineYearGeneratedPayloadKey] = int64(1761000000000000)
	decision, err := policy.CommitPolicy(item, testObservedAt(), []facts.Envelope{testNVDWindowSnapshotEnvelope(payload)})
	if err != nil || decision.StateWriter == nil {
		t.Fatalf("CommitPolicy() = (%+v, %v), want an armed writer", decision, err)
	}
	return func(ctx context.Context, exec *affectedCommitExecutor) error { return decision.StateWriter(ctx, exec) }
}

// TestNVDBaselineCommitPolicyRefusesSeedRollbackInTheWriter pins the rollback
// path (eshu-hq/eshu#10810): a guard that touches no row returns an error
// carrying class nvd_baseline_seed_rollback, counts one rollback, and WARNs
// with the class. Nothing is counted as a committed chunk.
func TestNVDBaselineCommitPolicyRefusesSeedRollbackInTheWriter(t *testing.T) {
	t.Parallel()

	policy, reader, logs := baselineSeedPolicy(t)
	item := testWorkItem()
	item.ScopeID = testNVDBaselineScopeForCommit
	writer := armedBaselineWriter(t, policy, item)
	err := writer(context.Background(), &affectedCommitExecutor{counts: []int64{0, 0}})
	var classified interface{ FailureClass() string }
	if !errors.As(err, &classified) || classified.FailureClass() != FailureClassNVDBaselineSeedRollback {
		t.Fatalf("writer error = %v, want class %q", err, FailureClassNVDBaselineSeedRollback)
	}
	if FailureClassNVDBaselineSeedRollback != "nvd_baseline_seed_rollback" {
		t.Fatalf("failure class = %q, want nvd_baseline_seed_rollback", FailureClassNVDBaselineSeedRollback)
	}
	if got := seedRotationCounts(t, reader); got["rollback"] != 1 || got["forward"] != 0 {
		t.Fatalf("rotation counts = %v, want exactly one rollback", got)
	}
	logged := logs.String()
	if !strings.Contains(logged, `"level":"WARN"`) || !strings.Contains(logged, "refused a seed rollback") ||
		!strings.Contains(logged, FailureClassNVDBaselineSeedRollback) {
		t.Fatalf("WARN log missing the rollback refusal, got: %s", logged)
	}
	policy.SettleClaimCommitted(context.Background(), item)
	if strings.Contains(logs.String(), "nvd baseline chunk committed") {
		t.Fatal("a refused generation logged a committed chunk")
	}
}

// TestNVDBaselineCommitPolicySettlesForwardRotationAfterTheCommit proves a
// forward rotation and the committed-chunk INFO line fire only once the commit
// hook runs, never from the writer, and never for a refused generation.
func TestNVDBaselineCommitPolicySettlesForwardRotationAfterTheCommit(t *testing.T) {
	t.Parallel()

	policy, reader, logs := baselineSeedPolicy(t)
	item := testWorkItem()
	item.ScopeID = testNVDBaselineScopeForCommit
	writer := armedBaselineWriter(t, policy, item)
	if err := writer(context.Background(), &affectedCommitExecutor{}); err != nil {
		t.Fatalf("writer error = %v", err)
	}
	if got := seedRotationCounts(t, reader); got["forward"] != 0 {
		t.Fatalf("forward rotation counted before the commit landed: %v", got)
	}
	policy.SettleClaimCommitted(context.Background(), item)
	if got := seedRotationCounts(t, reader); got["forward"] != 1 {
		t.Fatalf("rotation counts = %v, want one forward rotation after the commit", got)
	}
	logged := logs.String()
	if !strings.Contains(logged, `"level":"INFO"`) || !strings.Contains(logged, "nvd baseline chunk committed") ||
		!strings.Contains(logged, `"seed_rotated_forward":true`) {
		t.Fatalf("INFO log missing the committed chunk, got: %s", logged)
	}
	if strings.Contains(logged, testNVDBaselineScopeForCommit) {
		t.Fatalf("committed-chunk log carries the scope id: %s", logged)
	}
	policy.SettleClaimCommitted(context.Background(), item)
	if got := seedRotationCounts(t, reader); got["forward"] != 1 {
		t.Fatalf("a second settle counted again: %v", got)
	}

	refused := testWorkItem()
	refused.WorkItemID = "refused-item"
	refused.ScopeID = testNVDBaselineScopeForCommit
	refusedWriter := armedBaselineWriter(t, policy, refused)
	if err := refusedWriter(context.Background(), &affectedCommitExecutor{}); err != nil {
		t.Fatalf("writer error = %v", err)
	}
	policy.DiscardClaimSettlement(refused)
	policy.SettleClaimCommitted(context.Background(), refused)
	if got := seedRotationCounts(t, reader); got["forward"] != 1 {
		t.Fatalf("a refused generation counted a forward rotation: %v", got)
	}
}

// TestSourceCommitHooksReachTheBaselinePolicySettlement proves the wiring: the
// Source's post-commit and refusal hooks call the wired policy, through the
// combined policy the cmd builds.
func TestSourceCommitHooksReachTheBaselinePolicySettlement(t *testing.T) {
	t.Parallel()

	policy, reader, _ := baselineSeedPolicy(t)
	source := testNVDBaselineSource(t, CombineClaimCommitPolicies(policy))
	item := testWorkItem()
	item.ScopeID = testNVDBaselineScopeForCommit
	if err := armedBaselineWriter(t, policy, item)(context.Background(), &affectedCommitExecutor{}); err != nil {
		t.Fatalf("writer error = %v", err)
	}
	if err := source.ObserveClaimedGenerationCommitted(context.Background(), item); err != nil {
		t.Fatalf("ObserveClaimedGenerationCommitted() error = %v", err)
	}
	if got := seedRotationCounts(t, reader); got["forward"] != 1 {
		t.Fatalf("rotation counts = %v, want the Source commit hook to settle one forward rotation", got)
	}
	if err := armedBaselineWriter(t, policy, item)(context.Background(), &affectedCommitExecutor{}); err != nil {
		t.Fatalf("writer error = %v", err)
	}
	if err := source.ObserveClaimedGenerationRefused(context.Background(), item, collector.CommitRefusal{}); err != nil {
		t.Fatalf("ObserveClaimedGenerationRefused() error = %v", err)
	}
	if err := source.ObserveClaimedGenerationCommitted(context.Background(), item); err != nil {
		t.Fatalf("ObserveClaimedGenerationCommitted() error = %v", err)
	}
	if got := seedRotationCounts(t, reader); got["forward"] != 1 {
		t.Fatalf("rotation counts = %v, want the refusal hook to discard the settlement", got)
	}
}
