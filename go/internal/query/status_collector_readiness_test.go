package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

func collectorReadinessByKind(t *testing.T, body []byte) map[string]map[string]any {
	t.Helper()
	var payload struct {
		Readiness []map[string]any `json:"readiness"`
		Count     int              `json:"count"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if payload.Count != len(payload.Readiness) {
		t.Fatalf("count = %d, want %d", payload.Count, len(payload.Readiness))
	}
	byKind := map[string]map[string]any{}
	for _, entry := range payload.Readiness {
		kind, _ := entry["collector_kind"].(string)
		// instances of the same kind: keep the one with an instance id.
		if existing, ok := byKind[kind]; ok {
			if existing["instance_id"] != nil && existing["instance_id"] != "" {
				continue
			}
		}
		byKind[kind] = entry
	}
	return byKind
}

func getCollectorReadinessBody(t *testing.T, reader statuspkg.Reader) []byte {
	t.Helper()
	handler := &StatusHandler{StatusReader: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)
	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/collector-readiness", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

func TestCollectorReadinessNoCollectorsAreUnsupported(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	body := getCollectorReadinessBody(t, fakeStatusReader{snapshot: statuspkg.RawSnapshot{AsOf: now}})
	byKind := collectorReadinessByKind(t, body)

	jira, ok := byKind["jira"]
	if !ok {
		t.Fatalf("readiness missing jira; got kinds %v", keysOf(byKind))
	}
	if jira["promotion_state"] != statuspkg.CollectorPromotionUnsupported {
		t.Fatalf("jira promotion_state = %v, want unsupported", jira["promotion_state"])
	}
	if action, _ := jira["recommended_next_action"].(string); action == "" {
		t.Fatalf("jira recommended_next_action must not be empty")
	}
}

func TestCollectorReadinessClassifiesConfiguredCollectors(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	reader := fakeStatusReader{snapshot: statuspkg.RawSnapshot{
		AsOf: now,
		Coordinator: &statuspkg.CoordinatorSnapshot{
			CollectorInstances: []statuspkg.CollectorInstanceSummary{
				{InstanceID: "collector-jira", CollectorKind: "jira", Enabled: true, ClaimsEnabled: true, LastObservedAt: now, UpdatedAt: now},
				{InstanceID: "collector-pagerduty", CollectorKind: "pagerduty", Enabled: true, ClaimsEnabled: false, LastObservedAt: now, UpdatedAt: now},
				{InstanceID: "collector-sbom", CollectorKind: "sbom_attestation", Enabled: false, LastObservedAt: now, UpdatedAt: now},
				{InstanceID: "collector-aws", CollectorKind: "aws", Enabled: true, ClaimsEnabled: true, LastObservedAt: now, UpdatedAt: now},
			},
		},
		AWSCloudScans: []statuspkg.AWSCloudScanStatus{
			{CollectorInstanceID: "collector-aws", Status: "failed_terminal", CredentialFailed: true, FailureClass: "credential_denied", UpdatedAt: now},
		},
		CollectorFactEvidence: []statuspkg.CollectorFactEvidence{
			{InstanceID: "collector-jira", CollectorKind: "jira", EvidenceSource: "reducer_facts", ObservationCount: 4, LastObservedAt: now, UpdatedAt: now},
		},
	}}

	byKind := collectorReadinessByKind(t, getCollectorReadinessBody(t, reader))
	cases := map[string]string{
		"jira":             statuspkg.CollectorPromotionImplemented,
		"pagerduty":        statuspkg.CollectorPromotionGated,
		"sbom_attestation": statuspkg.CollectorPromotionDisabled,
		"aws":              statuspkg.CollectorPromotionFailed,
	}
	for kind, want := range cases {
		entry, ok := byKind[kind]
		if !ok {
			t.Fatalf("readiness missing %q", kind)
		}
		if entry["promotion_state"] != want {
			t.Errorf("%s promotion_state = %v, want %v", kind, entry["promotion_state"], want)
		}
		if action, _ := entry["recommended_next_action"].(string); action == "" {
			t.Errorf("%s recommended_next_action must not be empty", kind)
		}
	}

	if jira := byKind["jira"]; jira["reducer_readback"] != statuspkg.CollectorReadbackAvailable {
		t.Errorf("jira reducer_readback = %v, want available", jira["reducer_readback"])
	}
}

func TestCollectorReadinessMarksStaleEvidence(t *testing.T) {
	t.Parallel()

	// The handler evaluates staleness against time.Now with a 24h window, so age
	// the evidence relative to now to stay deterministic regardless of run time.
	now := time.Now().UTC()
	old := now.Add(-48 * time.Hour)
	reader := fakeStatusReader{snapshot: statuspkg.RawSnapshot{
		AsOf: now,
		Coordinator: &statuspkg.CoordinatorSnapshot{
			CollectorInstances: []statuspkg.CollectorInstanceSummary{
				{InstanceID: "collector-jira", CollectorKind: "jira", Enabled: true, ClaimsEnabled: true, LastObservedAt: old, UpdatedAt: old},
			},
		},
		CollectorFactEvidence: []statuspkg.CollectorFactEvidence{
			{InstanceID: "collector-jira", CollectorKind: "jira", EvidenceSource: "reducer_facts", ObservationCount: 4, LastObservedAt: old, UpdatedAt: old},
		},
	}}

	jira := collectorReadinessByKind(t, getCollectorReadinessBody(t, reader))["jira"]
	if jira["promotion_state"] != statuspkg.CollectorPromotionStale {
		t.Fatalf("jira promotion_state = %v, want stale", jira["promotion_state"])
	}
	if action, _ := jira["recommended_next_action"].(string); action == "" {
		t.Fatalf("stale jira recommended_next_action must not be empty")
	}
}

func TestCollectorReadinessClaimCapableWithoutEvidenceIsPartial(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	reader := fakeStatusReader{snapshot: statuspkg.RawSnapshot{
		AsOf: now,
		Coordinator: &statuspkg.CoordinatorSnapshot{
			CollectorInstances: []statuspkg.CollectorInstanceSummary{
				{InstanceID: "collector-grafana", CollectorKind: "grafana", Enabled: true, ClaimsEnabled: true, LastObservedAt: now, UpdatedAt: now},
			},
		},
	}}

	grafana := collectorReadinessByKind(t, getCollectorReadinessBody(t, reader))["grafana"]
	if grafana["promotion_state"] != statuspkg.CollectorPromotionPartial {
		t.Fatalf("grafana promotion_state = %v, want partial", grafana["promotion_state"])
	}
	if grafana["reducer_readback"] != statuspkg.CollectorReadbackUnavailable {
		t.Fatalf("grafana reducer_readback = %v, want unavailable", grafana["reducer_readback"])
	}
	if action, _ := grafana["recommended_next_action"].(string); action == "" {
		t.Fatalf("partial grafana recommended_next_action must not be empty")
	}
}

func TestCollectorReadinessEnvelopeShape(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	handler := &StatusHandler{StatusReader: fakeStatusReader{snapshot: statuspkg.RawSnapshot{AsOf: now}}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/collector-readiness", nil)
	req.Header.Set("Accept", "application/eshu.envelope+json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	var envelope struct {
		Data  map[string]any `json:"data"`
		Truth *struct {
			Level     string `json:"level"`
			Basis     string `json:"basis"`
			Freshness struct {
				State string `json:"state"`
			} `json:"freshness"`
		} `json:"truth"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if envelope.Truth == nil {
		t.Fatal("envelope truth is nil")
	}
	if envelope.Truth.Level != string(TruthLevelExact) {
		t.Errorf("truth.level = %q, want %q", envelope.Truth.Level, TruthLevelExact)
	}
	if envelope.Truth.Basis != string(TruthBasisRuntimeState) {
		t.Errorf("truth.basis = %q, want %q", envelope.Truth.Basis, TruthBasisRuntimeState)
	}
	if envelope.Data["readiness"] == nil {
		t.Errorf("envelope data missing readiness")
	}
}

func TestCollectorReadinessRedactsInstanceForScopedCallers(t *testing.T) {
	t.Parallel()

	proofs := []statuspkg.CollectorPromotionProof{{
		CollectorKind:  "jira",
		InstanceID:     "collector-jira",
		PromotionState: statuspkg.CollectorPromotionImplemented,
	}}

	unscoped := collectorReadinessEntries(proofs, false)
	if unscoped[0].InstanceID != "collector-jira" {
		t.Fatalf("unscoped instance id = %q, want collector-jira", unscoped[0].InstanceID)
	}
	scoped := collectorReadinessEntries(proofs, true)
	if scoped[0].InstanceID != "" {
		t.Fatalf("scoped instance id = %q, want empty (redacted)", scoped[0].InstanceID)
	}
}

func keysOf(m map[string]map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}
