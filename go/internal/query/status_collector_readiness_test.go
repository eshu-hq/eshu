package query

import (
	"context"
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

	// Evidence freshness must be evaluated against the snapshot's own AsOf so the
	// readiness API agrees with the text and status-JSON surfaces, which already
	// classify against report.AsOf. Pinning the snapshot and its evidence to a
	// fixed past instant keeps this deterministic: a correct classifier reads
	// zero evidence age (fresh -> implemented), while one that uses the handler's
	// wall clock would see the snapshot as long stale and wrongly flip jira to
	// "stale".
	now := time.Date(2025, 6, 16, 12, 0, 0, 0, time.UTC)
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

	// Staleness is evaluated against the snapshot's AsOf with a 24h window, so
	// pin both to fixed instants: evidence observed 48h before the snapshot AsOf
	// must classify as stale regardless of when this test runs.
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
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

// countingStatusReader wraps fakeStatusReader and counts how many times
// ReadStatusSnapshot is called, so tests can assert that a warm cache hit
// does not reach the underlying reader.
type countingStatusReader struct {
	fakeStatusReader
	calls int
}

func (c *countingStatusReader) ReadStatusSnapshot(ctx context.Context, asOf time.Time) (statuspkg.RawSnapshot, error) {
	c.calls++
	return c.fakeStatusReader.ReadStatusSnapshot(ctx, asOf)
}

func (c *countingStatusReader) ReadStatusSnapshotFiltered(ctx context.Context, asOf time.Time, sel statuspkg.SnapshotSelection) (statuspkg.RawSnapshot, error) {
	c.calls++
	return c.fakeStatusReader.ReadStatusSnapshotFiltered(ctx, asOf, sel)
}

// TestCollectorReadinessCacheHitReturnsCached asserts that a second request
// within the TTL window does not trigger a second read of the underlying
// StatusReader (the expensive Postgres query path).
func TestCollectorReadinessCacheHitReturnsCached(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 12, 0, 0, 0, time.UTC)
	reader := &countingStatusReader{
		fakeStatusReader: fakeStatusReader{snapshot: statuspkg.RawSnapshot{
			AsOf: now,
			Coordinator: &statuspkg.CoordinatorSnapshot{
				CollectorInstances: []statuspkg.CollectorInstanceSummary{
					{InstanceID: "collector-jira", CollectorKind: "jira", Enabled: true, ClaimsEnabled: true, LastObservedAt: now, UpdatedAt: now},
				},
			},
			CollectorFactEvidence: []statuspkg.CollectorFactEvidence{
				{InstanceID: "collector-jira", CollectorKind: "jira", EvidenceSource: "reducer_facts", ObservationCount: 4, LastObservedAt: now, UpdatedAt: now},
			},
		}},
	}

	// Both requests must go through the same handler so the cache is shared.
	handler := &StatusHandler{StatusReader: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	do := func() []byte {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v0/status/collector-readiness", nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		return rec.Body.Bytes()
	}

	body1 := do() // cache cold — reader called once
	body2 := do() // cache warm — reader must NOT be called again

	// Both responses must contain the same jira promotion_state.
	byKind1 := collectorReadinessByKind(t, body1)
	byKind2 := collectorReadinessByKind(t, body2)
	if byKind1["jira"]["promotion_state"] != byKind2["jira"]["promotion_state"] {
		t.Fatalf("cached response promotion_state = %v, want %v",
			byKind2["jira"]["promotion_state"], byKind1["jira"]["promotion_state"])
	}

	// Underlying reader must have been called exactly once across both requests.
	if reader.calls != 1 {
		t.Fatalf("reader called %d times, want 1 (second request must hit cache)", reader.calls)
	}
}

// TestCollectorReadinessCacheExpiryRefetches asserts that a request arriving
// after the TTL window has elapsed triggers a fresh read of the StatusReader.
func TestCollectorReadinessCacheExpiryRefetches(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 21, 13, 0, 0, 0, time.UTC)
	reader := &countingStatusReader{
		fakeStatusReader: fakeStatusReader{snapshot: statuspkg.RawSnapshot{AsOf: now}},
	}

	handler := &StatusHandler{StatusReader: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	// Seed the unscoped cache slot with an already-expired entry.
	handler.readinessCache.mu.Lock()
	handler.readinessCache.unscopedEntry.expiry = now.Add(-time.Second) // expired
	handler.readinessCache.mu.Unlock()

	// Request after expiry — cache must be bypassed and reader called.
	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/collector-readiness", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
	}

	if reader.calls != 1 {
		t.Fatalf("reader called %d times after cache expiry, want 1", reader.calls)
	}
}

// TestCollectorReadinessCacheDoesNotCrossScopes is a security regression test.
// An unscoped (admin) request must never warm the cache entry served to a
// scoped (tenant) request. The scoped response redacts per-instance IDs; if the
// two scopes shared a slot, a scoped caller arriving after an admin request
// within the same TTL window would receive the admin (unredacted) payload,
// leaking per-instance identity across auth boundaries.
func TestCollectorReadinessCacheDoesNotCrossScopes(t *testing.T) {
	t.Parallel()

	const privateInstanceID = "collector-private-instance"
	now := time.Date(2026, 6, 21, 14, 0, 0, 0, time.UTC)
	reader := &countingStatusReader{
		fakeStatusReader: fakeStatusReader{snapshot: statuspkg.RawSnapshot{
			AsOf: now,
			Coordinator: &statuspkg.CoordinatorSnapshot{
				CollectorInstances: []statuspkg.CollectorInstanceSummary{{
					InstanceID:     privateInstanceID,
					CollectorKind:  "aws",
					Enabled:        true,
					ClaimsEnabled:  true,
					LastObservedAt: now,
					UpdatedAt:      now,
				}},
			},
		}},
	}

	handler := &StatusHandler{StatusReader: reader}
	mux := http.NewServeMux()
	handler.Mount(mux)

	doAs := func(scoped bool) []byte {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/v0/status/collector-readiness", nil)
		if scoped {
			req = req.WithContext(ContextWithAuthContext(req.Context(),
				AuthContext{Mode: AuthModeScoped, TenantID: "tenant-a", WorkspaceID: "workspace-a"}))
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body=%s)", rec.Code, rec.Body.String())
		}
		return rec.Body.Bytes()
	}

	// Admin (unscoped) request — warms the unscoped cache slot.
	adminBody := doAs(false)
	// Scoped request — must use the scoped slot (cache cold for that scope).
	scopedBody := doAs(true)

	// The admin payload must contain the private instance ID.
	adminByKind := collectorReadinessByKind(t, adminBody)
	if adminByKind["aws"]["instance_id"] == "" {
		t.Fatalf("admin response should contain instance_id for %q, but it was empty", privateInstanceID)
	}

	// The scoped payload must NOT contain the private instance ID.
	scopedByKind := collectorReadinessByKind(t, scopedBody)
	if got := scopedByKind["aws"]["instance_id"]; got != nil && got != "" {
		t.Fatalf("scoped response must redact instance_id, got %q (cross-scope cache leak)", got)
	}

	// Both requests must have hit the reader (different cache slots, both cold).
	if reader.calls != 2 {
		t.Fatalf("reader called %d times, want 2 (one per scope slot)", reader.calls)
	}
}
