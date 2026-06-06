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

type fakeStatusReader struct {
	snapshot statuspkg.RawSnapshot
	err      error
}

func (f fakeStatusReader) ReadStatusSnapshot(_ context.Context, _ time.Time) (statuspkg.RawSnapshot, error) {
	if f.err != nil {
		return statuspkg.RawSnapshot{}, f.err
	}
	return f.snapshot, nil
}

func TestStatusHandlerLegacyIndexStatusAlias(t *testing.T) {
	t.Parallel()

	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC),
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/index-status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d", got, want)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := payload["status"], "healthy"; got != want {
		t.Fatalf("payload[status] = %#v, want %#v", got, want)
	}
	terraformState, ok := payload["terraform_state"].(map[string]any)
	if !ok {
		t.Fatalf("payload[terraform_state] missing or wrong type: %#v", payload["terraform_state"])
	}
	warningSummary, ok := terraformState["warning_summary"].([]any)
	if !ok || len(warningSummary) != 0 {
		t.Fatalf("terraform_state.warning_summary = %#v, want empty array", terraformState["warning_summary"])
	}
}

func TestStatusHandlerStatusIndexExposesTerraformStateWarningSummary(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 18, 9, 0, 0, 0, time.UTC)
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: now,
				TerraformStateRecentWarnings: []statuspkg.TerraformStateLocatorWarning{
					{SafeLocatorHash: "hash-a", BackendKind: "s3", WarningKind: "state_missing", Reason: "s3_not_found", Source: "source-a", ObservedAt: now},
					{SafeLocatorHash: "hash-b", BackendKind: "s3", WarningKind: "state_missing", Reason: "s3_not_found", Source: "source-b", ObservedAt: now.Add(time.Second)},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/index", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/status/index status = %d, want %d", got, want)
	}

	var payload struct {
		TerraformState struct {
			WarningSummary []struct {
				WarningKind string `json:"warning_kind"`
				Reason      string `json:"reason"`
				ScopeClass  string `json:"scope_class"`
				Count       int    `json:"count"`
			} `json:"warning_summary"`
		} `json:"terraform_state"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, body=%s", err, rec.Body.String())
	}
	if got := len(payload.TerraformState.WarningSummary); got != 1 {
		t.Fatalf("warning_summary = %d rows, want 1; body=%s", got, rec.Body.String())
	}
	row := payload.TerraformState.WarningSummary[0]
	if row.WarningKind != "state_missing" ||
		row.Reason != "s3_not_found" ||
		row.ScopeClass != "s3" ||
		row.Count != 2 {
		t.Fatalf("warning_summary[0] = %+v, want state_missing/s3_not_found/s3 count=2", row)
	}
}

func TestStatusHandlerLegacyIngesterAliases(t *testing.T) {
	t.Parallel()

	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC),
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	listReq := httptest.NewRequest(http.MethodGet, "/api/v0/ingesters", nil)
	listRec := httptest.NewRecorder()
	mux.ServeHTTP(listRec, listReq)

	if got, want := listRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/ingesters status = %d, want %d", got, want)
	}

	detailReq := httptest.NewRequest(http.MethodGet, "/api/v0/ingesters/repository", nil)
	detailRec := httptest.NewRecorder()
	mux.ServeHTTP(detailRec, detailReq)

	if got, want := detailRec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/ingesters/repository status = %d, want %d", got, want)
	}

	var detailPayload map[string]any
	if err := json.Unmarshal(detailRec.Body.Bytes(), &detailPayload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := detailPayload["ingester"], "repository"; got != want {
		t.Fatalf("payload[ingester] = %#v, want %#v", got, want)
	}
}

func TestStatusHandlerCollectorsRouteExposesCoordinatorInstances(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 4, 14, 12, 0, 0, 0, time.UTC)
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: now,
				Coordinator: &statuspkg.CoordinatorSnapshot{
					CollectorInstances: []statuspkg.CollectorInstanceSummary{{
						InstanceID:     "collector-git-default",
						CollectorKind:  "git",
						Mode:           "continuous",
						Enabled:        true,
						Bootstrap:      true,
						ClaimsEnabled:  false,
						LastObservedAt: now,
						UpdatedAt:      now,
					}},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/collectors", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/status/collectors status = %d, want %d", got, want)
	}

	var payload map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := int(payload["count"].(float64)), 1; got != want {
		t.Fatalf("payload[count] = %d, want %d", got, want)
	}
	collectors, ok := payload["collectors"].([]any)
	if !ok || len(collectors) != 1 {
		t.Fatalf("payload[collectors] = %#v, want one collector", payload["collectors"])
	}
	collector, ok := collectors[0].(map[string]any)
	if !ok {
		t.Fatalf("payload[collectors][0] = %#v, want object", collectors[0])
	}
	if got, want := collector["mode"], "continuous"; got != want {
		t.Fatalf("collector[mode] = %#v, want %#v", got, want)
	}
}

func TestStatusHandlerCollectorsRouteExposesDirectRuntimeEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 6, 10, 45, 0, 0, time.UTC)
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: now,
				Coordinator: &statuspkg.CoordinatorSnapshot{
					CollectorInstances: []statuspkg.CollectorInstanceSummary{{
						InstanceID:     "collector-aws-claims",
						CollectorKind:  "aws",
						Mode:           "continuous",
						Enabled:        true,
						Bootstrap:      true,
						ClaimsEnabled:  true,
						LastObservedAt: now.Add(-10 * time.Minute),
						UpdatedAt:      now.Add(-9 * time.Minute),
					}},
				},
				AWSCloudScans: []statuspkg.AWSCloudScanStatus{{
					CollectorInstanceID: "collector-aws-direct",
					AccountID:           "123456789012",
					Region:              "us-east-1",
					ServiceKind:         "lambda",
					Status:              "succeeded",
					CommitStatus:        "committed",
					LastObservedAt:      now.Add(-2 * time.Minute),
					UpdatedAt:           now.Add(-1 * time.Minute),
				}},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/status/collectors", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/status/collectors status = %d, want %d", got, want)
	}

	var payload struct {
		Count      int `json:"count"`
		Collectors []struct {
			InstanceID     string   `json:"instance_id"`
			CollectorKind  string   `json:"collector_kind"`
			StatusCategory string   `json:"status_category"`
			RuntimeMode    string   `json:"runtime_mode"`
			Enabled        bool     `json:"enabled"`
			Evidence       []string `json:"evidence_sources"`
		} `json:"collectors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := payload.Count, 2; got != want {
		t.Fatalf("payload.count = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	byID := map[string]struct {
		category string
		mode     string
		enabled  bool
		evidence []string
	}{}
	for _, collector := range payload.Collectors {
		byID[collector.InstanceID] = struct {
			category string
			mode     string
			enabled  bool
			evidence []string
		}{
			category: collector.StatusCategory,
			mode:     collector.RuntimeMode,
			enabled:  collector.Enabled,
			evidence: collector.Evidence,
		}
	}
	registered := byID["collector-aws-claims"]
	if got, want := registered.category, "coordinator_managed"; got != want {
		t.Fatalf("coordinator category = %q, want %q", got, want)
	}
	direct := byID["collector-aws-direct"]
	if got, want := direct.category, "unregistered"; got != want {
		t.Fatalf("direct category = %q, want %q", got, want)
	}
	if got, want := direct.mode, "direct"; got != want {
		t.Fatalf("direct runtime_mode = %q, want %q", got, want)
	}
	if direct.enabled {
		t.Fatal("direct enabled = true, want false because direct evidence does not prove coordinator configuration")
	}
	if len(direct.evidence) != 1 || direct.evidence[0] != "aws_cloud_scan_status" {
		t.Fatalf("direct evidence = %#v, want aws_cloud_scan_status", direct.evidence)
	}
}
