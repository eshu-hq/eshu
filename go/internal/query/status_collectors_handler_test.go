// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
)

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

func TestStatusHandlerCollectorsRouteExplainsAWSHealthEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 14, 30, 0, 0, time.UTC)
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
						ClaimsEnabled:  true,
						LastObservedAt: now.Add(-20 * time.Minute),
						UpdatedAt:      now.Add(-19 * time.Minute),
					}},
				},
				AWSCloudScans: []statuspkg.AWSCloudScanStatus{
					{
						CollectorInstanceID: "collector-aws-claims",
						AccountID:           "123456789012",
						Region:              "us-east-1",
						ServiceKind:         "iam",
						Status:              "succeeded",
						CommitStatus:        "committed",
						FailureClass:        "commit_failure",
						LastObservedAt:      now.Add(-4 * time.Minute),
						LastCompletedAt:     now.Add(-3 * time.Minute),
						LastSuccessfulAt:    now.Add(-3 * time.Minute),
						UpdatedAt:           now.Add(-3 * time.Minute),
					},
					{
						CollectorInstanceID: "collector-aws-failed",
						AccountID:           "123456789012",
						Region:              "us-west-2",
						ServiceKind:         "ec2",
						Status:              "failed_retryable",
						CommitStatus:        "failed",
						FailureClass:        "throttled",
						LastObservedAt:      now.Add(-2 * time.Minute),
						UpdatedAt:           now.Add(-1 * time.Minute),
					},
				},
				CollectorFactEvidence: []statuspkg.CollectorFactEvidence{{
					InstanceID:       "collector-aws-claims",
					CollectorKind:    "aws",
					EvidenceSource:   "source_facts",
					SourceSystems:    []string{"aws"},
					ObservationCount: 17,
					LastObservedAt:   now.Add(-3 * time.Minute),
					UpdatedAt:        now.Add(-2 * time.Minute),
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
		Collectors []struct {
			InstanceID string   `json:"instance_id"`
			Health     string   `json:"health"`
			Evidence   []string `json:"evidence_sources"`
			Detail     string   `json:"detail"`
		} `json:"collectors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	byID := map[string]struct {
		health   string
		evidence []string
		detail   string
	}{}
	for _, collector := range payload.Collectors {
		byID[collector.InstanceID] = struct {
			health   string
			evidence []string
			detail   string
		}{
			health:   collector.Health,
			evidence: collector.Evidence,
			detail:   collector.Detail,
		}
	}
	healthy := byID["collector-aws-claims"]
	if got, want := healthy.health, "observed"; got != want {
		t.Fatalf("healthy AWS collector health = %q, want %q; body=%s", got, want, rec.Body.String())
	}
	if !stringSliceContains(healthy.evidence, "aws_cloud_scan_status") ||
		!stringSliceContains(healthy.evidence, "source_facts") {
		t.Fatalf("healthy AWS evidence = %#v, want scan status and source facts", healthy.evidence)
	}
	if strings.Contains(healthy.detail, "commit_failure") {
		t.Fatalf("healthy AWS detail = %q, want stale commit failure omitted", healthy.detail)
	}
	failed := byID["collector-aws-failed"]
	if got, want := failed.health, "degraded"; got != want {
		t.Fatalf("failed AWS collector health = %q, want %q; body=%s", got, want, rec.Body.String())
	}
	if !strings.Contains(failed.detail, "aws_cloud_scan_status") ||
		!strings.Contains(failed.detail, "failure_class=throttled") {
		t.Fatalf("failed AWS detail = %q, want evidence source and failure class", failed.detail)
	}
}

func TestStatusHandlerCollectorsRouteExposesPersistedFactEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 10, 15, 0, 0, time.UTC)
	handler := &StatusHandler{
		StatusReader: fakeStatusReader{
			snapshot: statuspkg.RawSnapshot{
				AsOf: now,
				Coordinator: &statuspkg.CoordinatorSnapshot{
					CollectorInstances: []statuspkg.CollectorInstanceSummary{{
						InstanceID:     "collector-documentation",
						CollectorKind:  "documentation",
						Mode:           "continuous",
						Enabled:        true,
						ClaimsEnabled:  true,
						LastObservedAt: now.Add(-15 * time.Minute),
						UpdatedAt:      now.Add(-14 * time.Minute),
					}},
				},
				CollectorFactEvidence: []statuspkg.CollectorFactEvidence{{
					InstanceID:       "collector-documentation",
					CollectorKind:    "documentation",
					EvidenceSource:   "source_facts",
					SourceSystems:    []string{"confluence"},
					ObservationCount: 4,
					LastObservedAt:   now.Add(-3 * time.Minute),
					UpdatedAt:        now.Add(-2 * time.Minute),
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
	if body := rec.Body.String(); strings.Contains(body, "payload") ||
		strings.Contains(body, "source_uri") ||
		strings.Contains(body, "source_record_id") {
		t.Fatalf("collector status leaked payload identifier fields: %s", body)
	}

	var payload struct {
		ClassificationBasis string `json:"classification_basis"`
		Collectors          []struct {
			InstanceID       string   `json:"instance_id"`
			CollectorKind    string   `json:"collector_kind"`
			Health           string   `json:"health"`
			Evidence         []string `json:"evidence_sources"`
			SourceSystems    []string `json:"source_systems"`
			ObservationCount int      `json:"observation_count"`
		} `json:"collectors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := payload.ClassificationBasis, "workflow coordinator registration plus direct status and persisted fact evidence"; got != want {
		t.Fatalf("classification_basis = %q, want %q", got, want)
	}
	if len(payload.Collectors) != 1 {
		t.Fatalf("collectors len = %d, want 1", len(payload.Collectors))
	}
	collector := payload.Collectors[0]
	if got, want := collector.Health, "observed"; got != want {
		t.Fatalf("collector health = %q, want %q", got, want)
	}
	if got, want := collector.ObservationCount, 4; got != want {
		t.Fatalf("collector observation_count = %d, want %d", got, want)
	}
	for _, want := range []string{"workflow_coordinator", "source_facts"} {
		if !stringSliceContains(collector.Evidence, want) {
			t.Fatalf("collector evidence = %#v, want %q", collector.Evidence, want)
		}
	}
	if !stringSliceContains(collector.SourceSystems, "confluence") {
		t.Fatalf("collector source_systems = %#v, want confluence", collector.SourceSystems)
	}
}

func TestStatusHandlerCollectorsRouteExposesGitRepositoryEvidence(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 6, 7, 12, 30, 0, 0, time.UTC)
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
						LastObservedAt: now.Add(-30 * time.Minute),
						UpdatedAt:      now.Add(-29 * time.Minute),
					}},
				},
				CollectorFactEvidence: []statuspkg.CollectorFactEvidence{{
					InstanceID:       "collector-git-default",
					CollectorKind:    "git",
					EvidenceSource:   "source_facts",
					SourceSystems:    []string{"git"},
					ObservationCount: 217,
					LastObservedAt:   now.Add(-4 * time.Minute),
					UpdatedAt:        now.Add(-3 * time.Minute),
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
		Collectors []struct {
			InstanceID       string   `json:"instance_id"`
			CollectorKind    string   `json:"collector_kind"`
			Health           string   `json:"health"`
			Evidence         []string `json:"evidence_sources"`
			SourceSystems    []string `json:"source_systems"`
			ObservationCount int      `json:"observation_count"`
		} `json:"collectors"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &payload); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if len(payload.Collectors) != 1 {
		t.Fatalf("collectors len = %d, want 1", len(payload.Collectors))
	}
	collector := payload.Collectors[0]
	if got, want := collector.InstanceID, "collector-git-default"; got != want {
		t.Fatalf("instance_id = %q, want %q", got, want)
	}
	if got, want := collector.CollectorKind, "git"; got != want {
		t.Fatalf("collector_kind = %q, want %q", got, want)
	}
	if got, want := collector.Health, "observed"; got != want {
		t.Fatalf("health = %q, want %q", got, want)
	}
	if got, want := collector.ObservationCount, 217; got != want {
		t.Fatalf("observation_count = %d, want %d", got, want)
	}
	if !stringSliceContains(collector.Evidence, "workflow_coordinator") ||
		!stringSliceContains(collector.Evidence, "source_facts") {
		t.Fatalf("evidence_sources = %#v, want workflow_coordinator and source_facts", collector.Evidence)
	}
	if !stringSliceContains(collector.SourceSystems, "git") {
		t.Fatalf("source_systems = %#v, want git", collector.SourceSystems)
	}
}
