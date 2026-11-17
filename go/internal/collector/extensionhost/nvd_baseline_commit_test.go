// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// testNVDBaselineScopeForCommit is the 2002 head chunk of the
// baseline-2026-11 run: the year file replayed from its beginning.
const testNVDBaselineScopeForCommit = "vuln-intel://nvd/_baseline/baseline-2026-11/2002/1009843200000000"

func testNVDBaselineVerdictPayload() map[string]any {
	return map[string]any{
		"coverage_descriptor":             map[string]any{"version": "nvd.v1"},
		"nvd_baseline_advance_to_unix_us": int64(1041379200000000),
		"nvd_baseline_year_sha256":        "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855",
		"nvd_baseline_year_complete":      false,
		"nvd_baseline_should_advance":     true,
	}
}

func testNVDBaselineSource(t *testing.T, policy ClaimCommitPolicy) *Source {
	t.Helper()
	source, err := NewSource(Config{
		Manifest:            testManifest(),
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              &recordingRunner{},
		Clock:               testObservedAt,
		ClaimCommitPolicy:   policy,
	})
	if err != nil {
		t.Fatalf("NewSource() error = %v, want nil", err)
	}
	return source
}

func testNVDBaselinePolicy(t *testing.T) *NVDBaselineCommitPolicy {
	t.Helper()
	policy, err := NewNVDBaselineCommitPolicy("scorecard-instance", nil, nil)
	if err != nil {
		t.Fatalf("NewNVDBaselineCommitPolicy() error = %v, want nil", err)
	}
	return policy
}

// TestBaselineCommitArmsCheckpointWriterInTransaction is the R5 checkpoint
// cell: a baseline generation whose satellite-stamped verdict advances arms
// the checkpoint writer, and the writer runs the checkpoint
// compare-and-advance inside the generation's own commit transaction —
// never the window cursor table, never outside the transaction.
func TestBaselineCommitArmsCheckpointWriterInTransaction(t *testing.T) {
	t.Parallel()

	source := testNVDBaselineSource(t, testNVDBaselinePolicy(t))
	item := testWorkItem()
	item.ScopeID = testNVDBaselineScopeForCommit
	result := completeResult(item)
	envelopes := []facts.Envelope{testNVDWindowSnapshotEnvelope(testNVDBaselineVerdictPayload())}
	collected := source.collectedGeneration(item, result, envelopes, "")
	if collected.StateWriter != nil {
		t.Fatal("mapping StateWriter != nil before the policy, want the policy to own checkpoint arming")
	}
	if err := source.applyCommitPolicy(item, result.Generation.ObservedAt, envelopes, &collected); err != nil {
		t.Fatalf("applyCommitPolicy() error = %v, want the checkpoint armed", err)
	}
	if collected.StateWriter == nil {
		t.Fatal("StateWriter = nil for an advancing baseline verdict, want the checkpoint writer armed")
	}
	recorder := &affectedCommitExecutor{}
	if err := collected.StateWriter(context.Background(), recorder); err != nil {
		t.Fatalf("StateWriter() error = %v", err)
	}
	if len(recorder.queries) != 1 {
		t.Fatalf("StateWriter() ran %d statements, want one checkpoint advance", len(recorder.queries))
	}
	if !strings.Contains(recorder.queries[0], "vulnerability_nvd_baseline_checkpoints") {
		t.Fatalf("StateWriter() statement does not target the checkpoint table: %q", recorder.queries[0])
	}
	if strings.Contains(recorder.queries[0], "vulnerability_nvd_window_cursors") {
		t.Fatalf("baseline StateWriter() touches the window cursor table: %q", recorder.queries[0])
	}
	args := recorder.args[0]
	if len(args) < 7 {
		t.Fatalf("StateWriter() args = %v, want (run, year, resume, cve id, sha, stamp, complete, ...)", args)
	}
	if args[0] != "baseline-2026-11" || args[1] != int64(2002) {
		t.Fatalf("StateWriter() identity args = %v, want (baseline-2026-11, 2002)", args[:2])
	}
	if args[2] != int64(1041379200000000) {
		t.Fatalf("StateWriter() resume arg = %v, want the committed chunk end", args[2])
	}
}

// TestBaselineCommitNeverArmsOrAdvancesTheWindowCursor is the R5 golden
// trace: a baseline generation carrying a window advance verdict arms no
// window cursor writer, and the commit policy refuses the claim fail-closed
// — the cursor CAS never sees a baseline candidate, and nothing commits.
func TestBaselineCommitNeverArmsOrAdvancesTheWindowCursor(t *testing.T) {
	t.Parallel()

	source := testNVDBaselineSource(t, testNVDBaselinePolicy(t))
	item := testWorkItem()
	item.ScopeID = testNVDBaselineScopeForCommit
	result := completeResult(item)
	envelopes := []facts.Envelope{testNVDWindowSnapshotEnvelope(testNVDWindowVerdictPayload(true))}
	collected := source.collectedGeneration(item, result, envelopes, "")
	if collected.StateWriter != nil {
		t.Fatal("mapping StateWriter != nil for a baseline scope with a window verdict: the window cursor would advance with baseline facts")
	}
	err := source.applyCommitPolicy(item, result.Generation.ObservedAt, envelopes, &collected)
	if err == nil {
		t.Fatal("applyCommitPolicy() error = nil for a baseline generation that would arm the window cursor, want a fail-closed refusal")
	}
	failure, ok := err.(extensionFailure)
	if !ok {
		t.Fatalf("applyCommitPolicy() error type = %T, want extensionFailure", err)
	}
	if failure.class != FailureClassNVDBaselineWindowWriterArmed {
		t.Fatalf("refusal class = %q, want %q", failure.class, FailureClassNVDBaselineWindowWriterArmed)
	}
	if failure.terminal {
		t.Fatal("refusal terminal = true, want retryable: the next plan replays the same chunk")
	}
}

// TestBaselineCommitRefusedEmitsThroughProductionPath is the F6 regression
// cell (eshu-hq/eshu#10762): a baseline generation carrying a window advance
// verdict fails fail-closed through the production commit path with the
// refused outcome counted and the WARN logged. The coverage row promises
// both, so the wired policy must run instead of a silent pre-check.
func TestBaselineCommitRefusedEmitsThroughProductionPath(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v, want nil", err)
	}
	policy, err := NewNVDBaselineCommitPolicy("scorecard-instance", instruments, logger)
	if err != nil {
		t.Fatalf("NewNVDBaselineCommitPolicy() error = %v, want nil", err)
	}
	source := testNVDBaselineSource(t, policy)
	item := testWorkItem()
	item.ScopeID = testNVDBaselineScopeForCommit
	result := completeResult(item)
	envelopes := []facts.Envelope{testNVDWindowSnapshotEnvelope(testNVDWindowVerdictPayload(true))}
	collected := source.collectedGeneration(item, result, envelopes, "")
	err = source.applyCommitPolicy(item, result.Generation.ObservedAt, envelopes, &collected)
	if err == nil {
		t.Fatal("applyCommitPolicy() error = nil for a baseline generation that would arm the window cursor, want a fail-closed refusal")
	}
	failure, ok := err.(extensionFailure)
	if !ok {
		t.Fatalf("applyCommitPolicy() error type = %T, want extensionFailure", err)
	}
	if failure.class != FailureClassNVDBaselineWindowWriterArmed {
		t.Fatalf("refusal class = %q, want %q", failure.class, FailureClassNVDBaselineWindowWriterArmed)
	}
	if failure.terminal {
		t.Fatal("refusal terminal = true, want retryable: the next plan replays the same chunk")
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	var refused int64
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != "eshu_dp_nvd_baseline_advance_total" {
				continue
			}
			data, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("advance metric data type = %T, want Sum[int64]", metric.Data)
			}
			for _, point := range data.DataPoints {
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == "outcome" && attr.Value.AsString() == telemetry.NVDBaselineOutcomeRefused {
						refused += point.Value
					}
				}
			}
		}
	}
	if refused != 1 {
		t.Fatalf("refused advance outcomes = %d, want 1 through the production commit path", refused)
	}
	logged := logs.String()
	if !strings.Contains(logged, `"level":"WARN"`) || !strings.Contains(logged, "nvd baseline generation refused by commit policy, nothing committed") {
		t.Fatalf("WARN log missing the fail-closed refusal, got: %s", logged)
	}
}

// TestBaselineCommitHoldsCheckpointWithoutVerdict pins the hold shape: no
// snapshot, or should-advance false, arms nothing — the facts still
// commit, the checkpoint holds, and the next plan replays the same chunk.
func TestBaselineCommitHoldsCheckpointWithoutVerdict(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		envelopes []facts.Envelope
	}{
		{name: "no snapshot fact"},
		{
			name: "no baseline keys",
			envelopes: []facts.Envelope{testNVDWindowSnapshotEnvelope(
				map[string]any{"coverage_descriptor": map[string]any{"version": "nvd.v1"}},
			)},
		},
		{
			name: "should advance false",
			envelopes: []facts.Envelope{testNVDWindowSnapshotEnvelope(func() map[string]any {
				payload := testNVDBaselineVerdictPayload()
				payload["nvd_baseline_should_advance"] = false
				return payload
			}())},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			source := testNVDBaselineSource(t, testNVDBaselinePolicy(t))
			item := testWorkItem()
			item.ScopeID = testNVDBaselineScopeForCommit
			result := completeResult(item)
			collected := source.collectedGeneration(item, result, tc.envelopes, "")
			if err := source.applyCommitPolicy(item, result.Generation.ObservedAt, tc.envelopes, &collected); err != nil {
				t.Fatalf("applyCommitPolicy() error = %v, want hold (facts commit, checkpoint holds)", err)
			}
			if collected.StateWriter != nil {
				t.Fatalf("StateWriter != nil for %s, want no writer (checkpoint holds)", tc.name)
			}
		})
	}
}

// TestBaselineCommitNotCoveredOnUnusableVerdict is the R3 sha cell: a
// stamped identity that covers nothing new — blank sha, malformed sha, or
// an advance behind the chunk start, or an incomplete advance at it —
// commits its facts but arms no checkpoint writer, so the chunk replays
// under a verifiable identity.
func TestBaselineCommitNotCoveredOnUnusableVerdict(t *testing.T) {
	t.Parallel()

	mutations := []struct {
		name   string
		mutate func(map[string]any)
	}{
		{name: "blank sha", mutate: func(payload map[string]any) { payload["nvd_baseline_year_sha256"] = "" }},
		{name: "malformed sha", mutate: func(payload map[string]any) { payload["nvd_baseline_year_sha256"] = "not-a-sha" }},
		{name: "incomplete advance at start", mutate: func(payload map[string]any) { payload["nvd_baseline_advance_to_unix_us"] = int64(1009843200000000) }},
		{name: "missing advance", mutate: func(payload map[string]any) { delete(payload, "nvd_baseline_advance_to_unix_us") }},
	}
	for _, tc := range mutations {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			source := testNVDBaselineSource(t, testNVDBaselinePolicy(t))
			item := testWorkItem()
			item.ScopeID = testNVDBaselineScopeForCommit
			result := completeResult(item)
			payload := testNVDBaselineVerdictPayload()
			tc.mutate(payload)
			envelopes := []facts.Envelope{testNVDWindowSnapshotEnvelope(payload)}
			collected := source.collectedGeneration(item, result, envelopes, "")
			if err := source.applyCommitPolicy(item, result.Generation.ObservedAt, envelopes, &collected); err != nil {
				t.Fatalf("applyCommitPolicy() error = %v, want not-covered (facts commit, checkpoint holds)", err)
			}
			if collected.StateWriter != nil {
				t.Fatalf("StateWriter != nil for %s, want no writer", tc.name)
			}
		})
	}
}

// TestBaselineCommitPolicyIgnoresForeignScopes proves the policy owns only
// its family: window, static NVD, and GHSA scopes are unhandled and keep
// their existing commit behavior exactly.
func TestBaselineCommitPolicyIgnoresForeignScopes(t *testing.T) {
	t.Parallel()

	for _, scopeID := range []string{
		"vuln-intel://nvd/_window/1767225600000000-1767398400000000",
		"vuln-intel://nvd/modified",
		"vuln-intel://ghsa/reviewed/_window/1793491200000000-1793577600000000",
	} {
		policy := testNVDBaselinePolicy(t)
		item := testWorkItem()
		item.ScopeID = scopeID
		decision, err := policy.CommitPolicy(item, time.Now().UTC(), []facts.Envelope{
			testNVDWindowSnapshotEnvelope(testNVDBaselineVerdictPayload()),
		})
		if err != nil {
			t.Fatalf("CommitPolicy(%s) error = %v, want unhandled", scopeID, err)
		}
		if decision.Handled {
			t.Fatalf("CommitPolicy(%s) Handled = true, want false", scopeID)
		}
	}
}

// TestBaselineCommitPolicyRequiresAnInstanceID pins fail-closed
// construction: a blank instance id is an error, because a checkpoint row
// keyed under an invented run would strand the stream.
func TestBaselineCommitPolicyRequiresAnInstanceID(t *testing.T) {
	t.Parallel()

	if _, err := NewNVDBaselineCommitPolicy("  ", nil, nil); err == nil {
		t.Fatal("NewNVDBaselineCommitPolicy() error = nil for a blank instance, want an error")
	}
}

// TestBaselineCommitRecordsAdvanceOutcomes is the R8 emission cell: one
// decided baseline generation counts exactly one closed outcome on the
// baseline advance counter, with closed run and year labels only.
func TestBaselineCommitRecordsAdvanceOutcomes(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	policy, err := NewNVDBaselineCommitPolicy("scorecard-instance", instruments, nil)
	if err != nil {
		t.Fatalf("NewNVDBaselineCommitPolicy() error = %v, want nil", err)
	}
	decide := func(scopeID string, payload map[string]any) {
		item := testWorkItem()
		item.ScopeID = scopeID
		if _, err := policy.CommitPolicy(item, time.Now().UTC(), []facts.Envelope{testNVDWindowSnapshotEnvelope(payload)}); err != nil {
			t.Fatalf("CommitPolicy() error = %v", err)
		}
	}
	decide(testNVDBaselineScopeForCommit, testNVDBaselineVerdictPayload())
	held := testNVDBaselineVerdictPayload()
	held["nvd_baseline_should_advance"] = false
	decide(testNVDBaselineScopeForCommit, held)
	uncovered := testNVDBaselineVerdictPayload()
	uncovered["nvd_baseline_year_sha256"] = ""
	decide(testNVDBaselineScopeForCommit, uncovered)

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scopeMetrics := range rm.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name != "eshu_dp_nvd_baseline_advance_total" {
				continue
			}
			data, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("advance metric data type = %T, want Sum[int64]", metric.Data)
			}
			for _, point := range data.DataPoints {
				var outcome string
				for _, attr := range point.Attributes.ToSlice() {
					if string(attr.Key) == "outcome" {
						outcome = attr.Value.AsString()
					}
				}
				got[outcome] += point.Value
			}
		}
	}
	for outcome, want := range map[string]int64{"advanced": 1, "held": 1, "not_covered": 1} {
		if got[outcome] != want {
			t.Fatalf("advance outcome %q = %d, want %d (all outcomes %v)", outcome, got[outcome], want, got)
		}
	}
}
