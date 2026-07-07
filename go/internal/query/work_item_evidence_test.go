// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// recordingWorkItemEvidenceStore is a fake WorkItemEvidenceStore that already
// holds decoded rows (it has no raw-fact/decode-drop concept; see
// TestBuildWorkItemEvidencePageDerivesTruncationFromFetchedFactsNotDecodedRows
// for that scenario). It mirrors PostgresWorkItemEvidenceStore's pagination
// contract for an all-decode-succeeds fetch: filter.Limit is the "+1"
// lookahead fetch bound, so the visible window is filter.Limit-1 rows.
type recordingWorkItemEvidenceStore struct {
	rows       []WorkItemEvidenceRow
	lastFilter WorkItemEvidenceFilter
}

func (s *recordingWorkItemEvidenceStore) ListWorkItemEvidence(
	_ context.Context,
	filter WorkItemEvidenceFilter,
) (WorkItemEvidencePage, error) {
	s.lastFilter = filter
	rows := append([]WorkItemEvidenceRow(nil), s.rows...)
	visibleLimit := filter.Limit - 1
	if visibleLimit < 0 {
		visibleLimit = 0
	}
	truncated := len(rows) > visibleLimit
	if truncated {
		rows = rows[:visibleLimit]
	}
	page := WorkItemEvidencePage{Rows: rows, Truncated: truncated}
	if truncated && len(rows) > 0 {
		page.NextCursorFactID = rows[len(rows)-1].FactID
	}
	return page, nil
}

type unusedWorkItemEvidenceQueryer struct{}

func (unusedWorkItemEvidenceQueryer) QueryContext(
	context.Context,
	string,
	...any,
) (*sql.Rows, error) {
	return nil, fmt.Errorf("query must not run for invalid filters")
}

func TestWorkItemListEvidenceRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &WorkItemHandler{Evidence: &recordingWorkItemEvidenceStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/work-items/evidence?limit=10",
		"/api/v0/work-items/evidence?work_item_key=OPS-123",
	} {
		target := target
		t.Run(target, func(t *testing.T) {
			t.Parallel()

			req := httptest.NewRequest(http.MethodGet, target, nil)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)

			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
			}
		})
	}
}

func TestWorkItemListEvidenceUsesBoundedStoreAndCursor(t *testing.T) {
	t.Parallel()

	store := &recordingWorkItemEvidenceStore{
		rows: []WorkItemEvidenceRow{
			{
				FactID:             "fact-1",
				FactKind:           "work_item.external_link",
				Provider:           "jira_cloud",
				ScopeID:            "jira:site:example",
				WorkItemKey:        "OPS-123",
				ProviderWorkItemID: "10001",
				URLFingerprint:     "sha256:abc",
				URLPresent:         true,
				URLRedacted:        true,
				AnchorClass:        "github_pull_request",
				EvidenceState:      WorkItemEvidenceStateExactProviderFact,
			},
			{FactID: "fact-2", FactKind: "work_item.record", WorkItemKey: "OPS-123"},
		},
	}
	handler := &WorkItemHandler{Evidence: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/work-items/evidence?work_item_key=OPS-123&external_url=https://github.com/example/app/pull/42?token=secret&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.WorkItemKey, "OPS-123"; got != want {
		t.Fatalf("WorkItemKey = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}
	if store.lastFilter.URLFingerprint == "" {
		t.Fatal("URLFingerprint is blank, want external_url normalized into a fingerprint")
	}
	if strings.Contains(store.lastFilter.URLFingerprint, "secret") {
		t.Fatalf("URLFingerprint = %q, want no private URL material", store.lastFilter.URLFingerprint)
	}

	var resp struct {
		Evidence        []WorkItemEvidenceRow `json:"evidence"`
		Count           int                   `json:"count"`
		Limit           int                   `json:"limit"`
		Truncated       bool                  `json:"truncated"`
		MissingEvidence bool                  `json:"missing_evidence"`
		States          []string              `json:"states"`
		NextCursor      map[string]string     `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Evidence), 1; got != want {
		t.Fatalf("len(evidence) = %d, want %d", got, want)
	}
	if got := resp.Evidence[0].URLFingerprint; got == "" || strings.Contains(got, "github.com") {
		t.Fatalf("URLFingerprint = %q, want fingerprint only", got)
	}
	if resp.MissingEvidence {
		t.Fatal("missing_evidence = true, want false")
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_fact_id"], "fact-1"; got != want {
		t.Fatalf("next_cursor.after_fact_id = %q, want %q", got, want)
	}
}

func TestWorkItemEvidenceEmptyResultReportsMissingEvidence(t *testing.T) {
	t.Parallel()

	handler := &WorkItemHandler{Evidence: &recordingWorkItemEvidenceStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/work-items/evidence?work_item_key=OPS-404&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		MissingEvidence bool     `json:"missing_evidence"`
		States          []string `json:"states"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if !resp.MissingEvidence {
		t.Fatal("missing_evidence = false, want true")
	}
	if got, want := strings.Join(resp.States, ","), WorkItemEvidenceStateMissingEvidence; got != want {
		t.Fatalf("states = %q, want %q", got, want)
	}
}

func TestNormalizeWorkItemEvidenceFilterBoundsLimitAndFreshness(t *testing.T) {
	t.Parallel()

	cutoff := time.Date(2026, time.June, 1, 12, 0, 0, 0, time.UTC)
	got := normalizeWorkItemEvidenceFilter(WorkItemEvidenceFilter{
		ScopeID:            " jira:site:example ",
		ProjectKey:         " ops ",
		WorkItemKey:        " ops-123 ",
		ProviderWorkItemID: " 10001 ",
		ObservedAfter:      cutoff,
		AfterFactID:        " fact-1 ",
		Limit:              25,
	})

	if got.ScopeID != "jira:site:example" {
		t.Fatalf("ScopeID = %q, want trimmed scope", got.ScopeID)
	}
	if got.ProjectKey != "OPS" {
		t.Fatalf("ProjectKey = %q, want uppercase Jira project key", got.ProjectKey)
	}
	if got.WorkItemKey != "OPS-123" {
		t.Fatalf("WorkItemKey = %q, want uppercase Jira issue key", got.WorkItemKey)
	}
	if got.ProviderWorkItemID != "10001" {
		t.Fatalf("ProviderWorkItemID = %q, want trimmed id", got.ProviderWorkItemID)
	}
	if got.AfterFactID != "fact-1" {
		t.Fatalf("AfterFactID = %q, want trimmed cursor", got.AfterFactID)
	}
	if !got.ObservedAfter.Equal(cutoff) {
		t.Fatalf("ObservedAfter = %s, want %s", got.ObservedAfter, cutoff)
	}
	if got.Limit != 25 {
		t.Fatalf("Limit = %d, want 25", got.Limit)
	}
}

func TestPostgresWorkItemEvidenceStoreRejectsUnboundedFilter(t *testing.T) {
	t.Parallel()

	store := NewPostgresWorkItemEvidenceStore(unusedWorkItemEvidenceQueryer{})
	_, err := store.ListWorkItemEvidence(context.Background(), WorkItemEvidenceFilter{Limit: 10})
	if err == nil {
		t.Fatal("ListWorkItemEvidence() error = nil, want scope error")
	}
	if !strings.Contains(err.Error(), "scope_id, project_key, work_item_key, provider_work_item_id, url_fingerprint, or observed_after is required") {
		t.Fatalf("error = %q, want scope requirement", err.Error())
	}
}

func TestWorkItemEvidenceRowsClassifyStatesWithoutPrivatePayloads(t *testing.T) {
	t.Parallel()

	rows := buildWorkItemEvidenceRows([]workItemEvidenceFactRow{
		{
			FactID:           "unsupported-link",
			FactKind:         "work_item.external_link",
			ScopeID:          "jira:site:example",
			GenerationID:     "generation-1",
			SourceConfidence: "reported",
			ObservedAt:       "2026-06-01T12:00:00Z",
			Payload: map[string]any{
				"provider":               "jira_cloud",
				"work_item_key":          "OPS-123",
				"url":                    "https://private.example.invalid/path",
				"url_fingerprint":        "sha256:abc",
				"url_present":            true,
				"url_redacted":           true,
				"provider_support_state": "unsupported_provider",
			},
		},
		{
			FactID:       "permission-hidden",
			FactKind:     "work_item.record",
			ScopeID:      "jira:site:example",
			GenerationID: "generation-1",
			Payload: map[string]any{
				"provider":              "jira_cloud",
				"provider_work_item_id": "10124",
				"work_item_key":         "OPS-124",
				"evidence_state":        WorkItemEvidenceStatePermissionHidden,
			},
		},
	})

	if got, want := rows[0].EvidenceState, WorkItemEvidenceStateUnsupportedLinkType; got != want {
		t.Fatalf("unsupported link state = %q, want %q", got, want)
	}
	if got, want := rows[1].EvidenceState, WorkItemEvidenceStatePermissionHidden; got != want {
		t.Fatalf("permission state = %q, want %q", got, want)
	}
	if rows[0].RawURL != "" {
		t.Fatalf("RawURL = %q, want private URL omitted", rows[0].RawURL)
	}
	if rows[0].URLFingerprint == "" {
		t.Fatal("URLFingerprint is blank, want retained fingerprint")
	}
}

func TestWorkItemEvidenceSpanAttributesSummarizeBoundedCounts(t *testing.T) {
	t.Parallel()

	attrs := workItemEvidenceSpanAttributes([]WorkItemEvidenceRow{
		{EvidenceState: WorkItemEvidenceStateStaleEvidence},
		{EvidenceState: WorkItemEvidenceStatePermissionHidden},
		{EvidenceState: WorkItemEvidenceStateRejectedUnsafePayload},
		{EvidenceState: WorkItemEvidenceStateUnsupportedLinkType},
	}, true)
	got := map[string]string{}
	for _, attr := range attrs {
		got[string(attr.Key)] = attr.Value.Emit()
	}

	for key, want := range map[string]string{
		telemetry.SpanAttrWorkItemEvidenceQueryCount:                 "1",
		telemetry.SpanAttrWorkItemEvidenceResultCount:                "4",
		telemetry.SpanAttrWorkItemEvidenceStaleCount:                 "1",
		telemetry.SpanAttrWorkItemEvidencePermissionHiddenCount:      "1",
		telemetry.SpanAttrWorkItemEvidenceRejectedUnsafePayloadCount: "1",
		telemetry.SpanAttrWorkItemEvidenceUnsupportedLinkTypeCount:   "1",
		telemetry.SpanAttrWorkItemEvidenceMissingCount:               "0",
		telemetry.SpanAttrWorkItemEvidenceTruncated:                  "true",
	} {
		if got[key] != want {
			t.Fatalf("attribute %s = %q, want %q", key, got[key], want)
		}
	}
}
