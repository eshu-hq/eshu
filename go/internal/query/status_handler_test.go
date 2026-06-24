// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

func (f fakeStatusReader) ReadStatusSnapshotFiltered(
	ctx context.Context,
	asOf time.Time,
	_ statuspkg.SnapshotSelection,
) (statuspkg.RawSnapshot, error) {
	return f.ReadStatusSnapshot(ctx, asOf)
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
					{SafeLocatorHash: "hash-a", BackendKind: "s3", WarningKind: "state_missing", Reason: "s3_not_found", Source: "source-a", SourceHandle: "state_snapshot:s3:hash-a", ObservedAt: now},
					{SafeLocatorHash: "hash-b", BackendKind: "s3", WarningKind: "state_missing", Reason: "s3_not_found", Source: "source-b", SourceHandle: "state_snapshot:s3:hash-b", ObservedAt: now.Add(time.Second)},
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
				WarningKind   string `json:"warning_kind"`
				Reason        string `json:"reason"`
				ScopeClass    string `json:"scope_class"`
				Severity      string `json:"severity"`
				Actionability string `json:"actionability"`
				Count         int    `json:"count"`
			} `json:"warning_summary"`
			RecentWarnings []struct {
				SafeLocatorHash string `json:"safe_locator_hash"`
				WarningKind     string `json:"warning_kind"`
				Reason          string `json:"reason"`
				Severity        string `json:"severity"`
				Actionability   string `json:"actionability"`
				Source          string `json:"source"`
				SourceHandle    string `json:"source_handle"`
			} `json:"recent_warnings"`
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
		row.Severity != "blocking" ||
		row.Actionability != "blocking_evidence" ||
		row.Count != 2 {
		t.Fatalf("warning_summary[0] = %+v, want state_missing/s3_not_found/s3 count=2", row)
	}
	if got := len(payload.TerraformState.RecentWarnings); got != 2 {
		t.Fatalf("recent_warnings = %d rows, want 2; body=%s", got, rec.Body.String())
	}
	first := payload.TerraformState.RecentWarnings[0]
	if first.WarningKind != "state_missing" ||
		first.Reason != "s3_not_found" ||
		first.Severity != "blocking" ||
		first.Actionability != "blocking_evidence" ||
		first.Source != "source-a" ||
		first.SourceHandle != "state_snapshot:s3:hash-a" ||
		first.SafeLocatorHash != "hash-a" {
		t.Fatalf("recent_warnings[0] = %+v, want actionable state_missing row for hash-a", first)
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
						Domain:      "observability_coverage_materialization",
						Outstanding: 5,
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
	if got, want := payload.AWSMaterialization.Pending, 9; got != want {
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
	if got, want := len(payload.AWSMaterialization.Domains), 3; got != want {
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

func TestStatusHandlerIndexStatusUsesDistinctBlockedWorkItems(t *testing.T) {
	t.Parallel()

	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: time.Date(2026, 6, 6, 11, 25, 0, 0, time.UTC),
				DomainBacklogs: []statuspkg.DomainBacklog{
					{
						Domain:      "security_group_reachability_materialization",
						Outstanding: 1,
					},
				},
				QueueBlockages: []statuspkg.QueueBlockage{
					{
						Stage:          "reducer",
						Domain:         "security_group_reachability_materialization",
						ConflictDomain: "readiness",
						ConflictKey:    "security_group_rule_uid:canonical_nodes_committed:sg-1",
						Blocked:        1,
					},
					{
						Stage:          "reducer",
						Domain:         "security_group_reachability_materialization",
						ConflictDomain: "readiness",
						ConflictKey:    "security_group_endpoint_uid:canonical_nodes_committed:sg-1",
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
			Domains []struct {
				Domain  string `json:"domain"`
				Blocked int    `json:"blocked"`
			} `json:"domains"`
		} `json:"aws_materialization"`
		QueueBlockages []struct {
			Domain         string `json:"domain"`
			ConflictDomain string `json:"conflict_domain"`
			ConflictKey    string `json:"conflict_key"`
			Blocked        int    `json:"blocked"`
		} `json:"queue_blockages"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, body=%s", err, rec.Body.String())
	}
	if got, want := payload.AWSMaterialization.Pending, 1; got != want {
		t.Fatalf("aws_materialization.pending = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := payload.AWSMaterialization.Blocked, 1; got != want {
		t.Fatalf("aws_materialization.blocked = %d, want distinct blocked work items %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := len(payload.AWSMaterialization.Domains), 1; got != want {
		t.Fatalf("aws_materialization.domains len = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := payload.AWSMaterialization.Domains[0].Blocked, 1; got != want {
		t.Fatalf("aws_materialization.domains[0].blocked = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := len(payload.QueueBlockages), 2; got != want {
		t.Fatalf("queue_blockages len = %d, want %d; body=%s", got, want, rec.Body.String())
	}
	if got, want := payload.QueueBlockages[0].ConflictDomain, "readiness"; got != want {
		t.Fatalf("queue_blockages[0].conflict_domain = %q, want %q", got, want)
	}
	if got, want := payload.QueueBlockages[0].Blocked, 1; got != want {
		t.Fatalf("queue_blockages[0].blocked = %d, want distinct blocked work items %d", got, want)
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
