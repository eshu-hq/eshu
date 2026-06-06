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

func TestStatusHandlerIndexStatusExposesAWSMaterializationBuckets(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 6, 10, 45, 0, 0, time.UTC)
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: now,
				DomainBacklogs: []statuspkg.DomainBacklog{
					{
						Domain:      "iam_can_perform_materialization",
						Outstanding: 4,
						InFlight:    1,
						Retrying:    1,
						DeadLetter:  1,
					},
					{
						Domain:      "aws_resource_materialization",
						Outstanding: 2,
					},
					{
						Domain:      "code_call_materialization",
						Outstanding: 9,
						Retrying:    9,
					},
				},
				QueueBlockages: []statuspkg.QueueBlockage{
					{
						Stage:          "reducer",
						Domain:         "iam_can_perform_materialization",
						ConflictDomain: "readiness",
						ConflictKey:    "cloud_resource_uid:canonical_nodes_committed:aws_resource_materialization:scope-1",
						Blocked:        2,
					},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/index-status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/index-status status = %d, want %d", got, want)
	}

	var payload struct {
		AWSMaterialization struct {
			Pending    int `json:"pending"`
			Blocked    int `json:"blocked"`
			Retrying   int `json:"retrying"`
			DeadLetter int `json:"dead_letter"`
			Domains    []struct {
				Domain     string `json:"domain"`
				Pending    int    `json:"pending"`
				Blocked    int    `json:"blocked"`
				Retrying   int    `json:"retrying"`
				DeadLetter int    `json:"dead_letter"`
			} `json:"domains"`
		} `json:"aws_materialization"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, body=%s", err, rec.Body.String())
	}
	if got, want := payload.AWSMaterialization.Pending, 4; got != want {
		t.Fatalf("aws_materialization.pending = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := payload.AWSMaterialization.Blocked, 2; got != want {
		t.Fatalf("aws_materialization.blocked = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := payload.AWSMaterialization.Retrying, 1; got != want {
		t.Fatalf("aws_materialization.retrying = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := payload.AWSMaterialization.DeadLetter, 1; got != want {
		t.Fatalf("aws_materialization.dead_letter = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := len(payload.AWSMaterialization.Domains), 2; got != want {
		t.Fatalf("aws_materialization.domains len = %d, want %d; body=%s", got, want, rec.Body.String())
	}
}

func TestStatusHandlerIndexStatusIncludesAWSMaterializationBeyondBacklogCap(t *testing.T) {
	t.Parallel()

	highVolumeDomains := []statuspkg.DomainBacklog{
		{Domain: "code_call_materialization", Outstanding: 20},
		{Domain: "code_file_materialization", Outstanding: 19},
		{Domain: "code_symbol_materialization", Outstanding: 18},
		{Domain: "terraform_state_materialization", Outstanding: 17},
		{Domain: "deployment_correlation_materialization", Outstanding: 16},
	}
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 6, 6, 11, 15, 0, 0, time.UTC),
				DomainBacklogs: append(highVolumeDomains, statuspkg.DomainBacklog{
					Domain:      "iam_can_perform_materialization",
					Outstanding: 3,
				}),
				QueueBlockages: []statuspkg.QueueBlockage{
					{
						Stage:          "reducer",
						Domain:         "iam_can_perform_materialization",
						ConflictDomain: "readiness",
						ConflictKey:    "cloud_resource_uid:canonical_nodes_committed:aws_resource_materialization:scope-1",
						Blocked:        1,
					},
				},
			},
		},
	}

	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/index-status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("GET /api/v0/index-status status = %d, want %d", got, want)
	}

	var payload struct {
		AWSMaterialization struct {
			Pending int `json:"pending"`
			Blocked int `json:"blocked"`
		} `json:"aws_materialization"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, body=%s", err, rec.Body.String())
	}
	if got, want := payload.AWSMaterialization.Pending, 3; got != want {
		t.Fatalf("aws_materialization.pending = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := payload.AWSMaterialization.Blocked, 1; got != want {
		t.Fatalf("aws_materialization.blocked = %d, want %d; body=%s", got, want, rec.Body.String())
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
}
