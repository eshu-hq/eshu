package reducer

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/correlation/drift/tfconfigstate"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

type stubBackendQuery struct {
	rows []tfstatebackend.TerraformBackendRow
}

func (s *stubBackendQuery) ListTerraformBackendsByLocator(
	_ context.Context, _ string, _ string,
) ([]tfstatebackend.TerraformBackendRow, error) {
	out := make([]tfstatebackend.TerraformBackendRow, len(s.rows))
	copy(out, s.rows)
	return out, nil
}

type stubDriftLoader struct {
	rows []tfconfigstate.AddressedRow
}

func (s *stubDriftLoader) LoadDriftEvidence(
	_ context.Context, _ string, _ tfstatebackend.CommitAnchor,
) ([]tfconfigstate.AddressedRow, error) {
	out := make([]tfconfigstate.AddressedRow, len(s.rows))
	copy(out, s.rows)
	return out, nil
}

func validIntent() Intent {
	return Intent{
		IntentID:        "intent-1",
		ScopeID:         "state_snapshot:s3:hash-1",
		GenerationID:    "gen-1",
		SourceSystem:    "collector/terraform-state",
		Domain:          DomainConfigStateDrift,
		Cause:           "test drift intent",
		RelatedScopeIDs: []string{"state_snapshot:s3:hash-1"},
		EnqueuedAt:      time.Now(),
		AvailableAt:     time.Now(),
		Status:          IntentStatusClaimed,
	}
}

func newDriftInstruments(t *testing.T) (*telemetry.Instruments, sdkmetric.Reader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	inst, err := telemetry.NewInstruments(provider.Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	return inst, reader
}

func TestDriftHandlerRejectsWrongDomain(t *testing.T) {
	t.Parallel()

	h := TerraformConfigStateDriftHandler{}
	intent := validIntent()
	intent.Domain = DomainWorkloadIdentity
	_, err := h.Handle(context.Background(), intent)
	if err == nil {
		t.Fatal("Handle(wrong domain) error = nil, want non-nil")
	}
}

func TestDriftHandlerRejectsNonStateSnapshotScope(t *testing.T) {
	t.Parallel()

	h := TerraformConfigStateDriftHandler{
		Resolver: tfstatebackend.NewResolver(nil),
	}
	intent := validIntent()
	intent.ScopeID = "repo:repo-1@abc"
	res, err := h.Handle(context.Background(), intent)
	if err != nil {
		t.Fatalf("Handle() err = %v, want nil (operator-actionable)", err)
	}
	if res.Status != ResultStatusSucceeded {
		t.Fatalf("res.Status = %q, want Succeeded", res.Status)
	}
}

func TestDriftHandlerNoOwnerSucceedsWithoutCounters(t *testing.T) {
	t.Parallel()

	inst, reader := newDriftInstruments(t)
	resolver := tfstatebackend.NewResolver(&stubBackendQuery{}) // empty rows
	h := TerraformConfigStateDriftHandler{
		Resolver:       resolver,
		EvidenceLoader: &stubDriftLoader{},
		Instruments:    inst,
	}
	res, err := h.Handle(context.Background(), validIntent())
	if err != nil {
		t.Fatalf("Handle() err = %v", err)
	}
	if res.Status != ResultStatusSucceeded {
		t.Fatalf("res.Status = %q, want Succeeded", res.Status)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := counterTotal(rm, "eshu_dp_correlation_drift_detected_total"); got != 0 {
		t.Fatalf("drift_detected = %d, want 0 (no owner)", got)
	}
}

func TestDriftHandlerAmbiguousOwnerSucceedsWithoutDriftCounter(t *testing.T) {
	t.Parallel()

	inst, reader := newDriftInstruments(t)
	rows := []tfstatebackend.TerraformBackendRow{
		{
			RepoID: "repo-a", ScopeID: "repo:repo-a@1", CommitID: "aaa",
			CommitObservedAt: time.Now(), BackendKind: "s3", LocatorHash: "hash-1",
		},
		{
			RepoID: "repo-b", ScopeID: "repo:repo-b@1", CommitID: "bbb",
			CommitObservedAt: time.Now(), BackendKind: "s3", LocatorHash: "hash-1",
		},
	}
	resolver := tfstatebackend.NewResolver(&stubBackendQuery{rows: rows})
	h := TerraformConfigStateDriftHandler{
		Resolver:       resolver,
		EvidenceLoader: &stubDriftLoader{},
		Instruments:    inst,
	}
	res, err := h.Handle(context.Background(), validIntent())
	if err != nil {
		t.Fatalf("Handle() err = %v", err)
	}
	if res.Status != ResultStatusSucceeded {
		t.Fatalf("res.Status = %q, want Succeeded", res.Status)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := counterTotal(rm, "eshu_dp_correlation_drift_detected_total"); got != 0 {
		t.Fatalf("drift_detected = %d, want 0 (ambiguous owner)", got)
	}
}

func TestDriftHandlerSingleOwnerEmitsCountersForAllFiveDriftKinds(t *testing.T) {
	t.Parallel()

	inst, reader := newDriftInstruments(t)

	now := time.Now()
	backendRows := []tfstatebackend.TerraformBackendRow{
		{
			RepoID: "repo-a", ScopeID: "repo:repo-a@1", CommitID: "aaa",
			CommitObservedAt: now, BackendKind: "s3", LocatorHash: "hash-1",
		},
	}
	driftRows := []tfconfigstate.AddressedRow{
		{
			Address: "aws_s3_bucket.added_state", ResourceType: "aws_s3_bucket",
			State: &tfconfigstate.ResourceRow{Address: "aws_s3_bucket.added_state", ResourceType: "aws_s3_bucket"},
		},
		{
			Address: "aws_iam_role.added_config", ResourceType: "aws_iam_role",
			Config: &tfconfigstate.ResourceRow{Address: "aws_iam_role.added_config", ResourceType: "aws_iam_role"},
		},
		{
			Address: "aws_s3_bucket.attr_drift", ResourceType: "aws_s3_bucket",
			Config: &tfconfigstate.ResourceRow{
				Address: "aws_s3_bucket.attr_drift", ResourceType: "aws_s3_bucket",
				Attributes: map[string]string{"versioning.enabled": "true"},
			},
			State: &tfconfigstate.ResourceRow{
				Address: "aws_s3_bucket.attr_drift", ResourceType: "aws_s3_bucket",
				Attributes: map[string]string{"versioning.enabled": "false"},
			},
		},
		{
			Address: "aws_lambda_function.removed_state", ResourceType: "aws_lambda_function",
			Config: &tfconfigstate.ResourceRow{Address: "aws_lambda_function.removed_state", ResourceType: "aws_lambda_function"},
			Prior:  &tfconfigstate.ResourceRow{Address: "aws_lambda_function.removed_state", ResourceType: "aws_lambda_function"},
		},
		{
			Address: "aws_iam_policy.removed_config", ResourceType: "aws_iam_policy",
			State: &tfconfigstate.ResourceRow{
				Address: "aws_iam_policy.removed_config", ResourceType: "aws_iam_policy",
				PreviouslyDeclaredInConfig: true,
			},
		},
	}

	h := TerraformConfigStateDriftHandler{
		Resolver:       tfstatebackend.NewResolver(&stubBackendQuery{rows: backendRows}),
		EvidenceLoader: &stubDriftLoader{rows: driftRows},
		Instruments:    inst,
	}
	res, err := h.Handle(context.Background(), validIntent())
	if err != nil {
		t.Fatalf("Handle() err = %v", err)
	}
	if res.Status != ResultStatusSucceeded {
		t.Fatalf("res.Status = %q, want Succeeded", res.Status)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	if got := counterTotal(rm, "eshu_dp_correlation_drift_detected_total"); got != 5 {
		t.Fatalf("drift_detected = %d, want 5 (one per drift kind)", got)
	}

	// Each admitted candidate emits exactly one rule-match increment with
	// the admission-producing rule as the `rule` label. Five admitted
	// candidates produce five increments, not 25: the engine does not
	// surface per-rule match counts, so the contract is "one increment per
	// admission" rather than "one increment per (admission * rule)".
	if got := counterTotal(rm, "eshu_dp_correlation_rule_matches_total"); got != 5 {
		t.Fatalf("rule_matches = %d, want 5 (one increment per admitted candidate)", got)
	}

	assertCounterLabelKeys(t, rm,
		"eshu_dp_correlation_drift_detected_total",
		map[string]struct{}{
			telemetry.MetricDimensionPack:      {},
			telemetry.MetricDimensionRule:      {},
			telemetry.MetricDimensionDriftKind: {},
		},
	)
	assertCounterLabelKeys(t, rm,
		"eshu_dp_correlation_rule_matches_total",
		map[string]struct{}{
			telemetry.MetricDimensionPack: {},
			telemetry.MetricDimensionRule: {},
		},
	)

	driftKinds := collectDriftKindValues(rm)
	expected := map[string]bool{
		"added_in_state":      true,
		"added_in_config":     true,
		"attribute_drift":     true,
		"removed_from_state":  true,
		"removed_from_config": true,
	}
	for _, k := range driftKinds {
		if !expected[k] {
			t.Fatalf("unexpected drift_kind label value %q", k)
		}
	}
	if len(driftKinds) != len(expected) {
		t.Fatalf("drift_kind label values = %v, want one of each of the 5 enum values", driftKinds)
	}
}

func TestDriftHandlerNoMetricLabelsCarryResourceAddress(t *testing.T) {
	t.Parallel()

	inst, reader := newDriftInstruments(t)
	rows := []tfstatebackend.TerraformBackendRow{{
		RepoID: "repo-a", ScopeID: "repo:repo-a@1", CommitID: "aaa",
		CommitObservedAt: time.Now(), BackendKind: "s3", LocatorHash: "hash-1",
	}}
	driftRows := []tfconfigstate.AddressedRow{{
		Address: "aws_iam_role.svc", ResourceType: "aws_iam_role",
		Config: &tfconfigstate.ResourceRow{Address: "aws_iam_role.svc", ResourceType: "aws_iam_role"},
	}}
	h := TerraformConfigStateDriftHandler{
		Resolver:       tfstatebackend.NewResolver(&stubBackendQuery{rows: rows}),
		EvidenceLoader: &stubDriftLoader{rows: driftRows},
		Instruments:    inst,
	}
	if _, err := h.Handle(context.Background(), validIntent()); err != nil {
		t.Fatalf("Handle() err = %v", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_correlation_drift_detected_total" &&
				m.Name != "eshu_dp_correlation_rule_matches_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				iter := dp.Attributes.Iter()
				for iter.Next() {
					kv := iter.Attribute()
					if kv.Value.AsString() == "aws_iam_role.svc" {
						t.Fatalf("metric %q has resource address as a label value", m.Name)
					}
				}
			}
		}
	}
}

// helpers

func counterTotal(rm metricdata.ResourceMetrics, name string) int64 {
	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				total += dp.Value
			}
		}
	}
	return total
}

func assertCounterLabelKeys(t *testing.T, rm metricdata.ResourceMetrics, name string, allowed map[string]struct{}) {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				iter := dp.Attributes.Iter()
				for iter.Next() {
					kv := iter.Attribute()
					if _, ok := allowed[string(kv.Key)]; !ok {
						t.Fatalf(
							"counter %q has unexpected label key %q (allowed: %v)",
							name, string(kv.Key), allowed,
						)
					}
				}
			}
		}
	}
}

func collectDriftKindValues(rm metricdata.ResourceMetrics) []string {
	seen := map[string]struct{}{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_correlation_drift_detected_total" {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				iter := dp.Attributes.Iter()
				for iter.Next() {
					kv := iter.Attribute()
					if string(kv.Key) == telemetry.MetricDimensionDriftKind {
						seen[kv.Value.AsString()] = struct{}{}
					}
				}
			}
		}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	return out
}

// TestParseDriftIntentScope covers the boundaries of the state_snapshot scope
// parser, including the embedded-colon rejection that the locator-hash
// invariant relies on. Locator hashes are hex-safe by construction
// (`go/internal/scope/tfstate.go`); a colon inside the locator hash field
// indicates either a malformed scope or a non-canonical emitter.
func TestParseDriftIntentScope(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		scope       string
		wantBackend string
		wantLocator string
		wantErr     bool
	}{
		{
			name:        "valid_two_segment_scope",
			scope:       "state_snapshot:s3:abc123",
			wantBackend: "s3",
			wantLocator: "abc123",
		},
		{
			name:    "missing_prefix",
			scope:   "repo:repo-1@abc",
			wantErr: true,
		},
		{
			name:    "prefix_only",
			scope:   "state_snapshot:",
			wantErr: true,
		},
		{
			name:    "prefix_plus_backend_no_locator",
			scope:   "state_snapshot:s3:",
			wantErr: true,
		},
		{
			name:    "prefix_plus_separator_no_backend",
			scope:   "state_snapshot::hash",
			wantErr: true,
		},
		{
			name:    "locator_with_embedded_colon_rejected",
			scope:   "state_snapshot:s3:hash:extra",
			wantErr: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			backend, locator, err := parseDriftIntentScope(Intent{ScopeID: tc.scope})
			if tc.wantErr {
				if err == nil {
					t.Fatalf("parseDriftIntentScope(%q) err = nil, want non-nil", tc.scope)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDriftIntentScope(%q) err = %v, want nil", tc.scope, err)
			}
			if backend != tc.wantBackend {
				t.Fatalf("backend = %q, want %q", backend, tc.wantBackend)
			}
			if locator != tc.wantLocator {
				t.Fatalf("locator = %q, want %q", locator, tc.wantLocator)
			}
		})
	}
}
