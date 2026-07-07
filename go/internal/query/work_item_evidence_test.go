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
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
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

func TestWorkItemEvidenceFactKindsMatchRegistrySet(t *testing.T) {
	t.Parallel()

	// The evidence read surface bounds its SQL read to the work_item family the
	// fact-kind registry maps to GET /api/v0/work-items/evidence, MINUS
	// work_item.metadata_warning (deferred to #4887; WorkItemEvidenceRow has no
	// metadata_type/reason field, so surfacing it would strip its contract).
	// facts.WorkItemFactKinds() is the single source of truth for the family, so
	// asserting the read set equals it minus that one kind means a future family
	// addition trips this guard instead of silently drifting.
	want := make([]string, 0)
	for _, kind := range facts.WorkItemFactKinds() {
		if kind == facts.WorkItemMetadataWarningFactKind {
			continue
		}
		want = append(want, kind)
	}
	got := slices.Clone(workItemEvidenceFactKinds)
	slices.Sort(want)
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Fatalf("workItemEvidenceFactKinds = %v, want facts.WorkItemFactKinds() minus metadata_warning = %v", got, want)
	}
	if slices.Contains(got, "work_item.coverage_warning") {
		t.Fatal("workItemEvidenceFactKinds still lists the phantom work_item.coverage_warning (no emitter, no registry row)")
	}
	if !slices.Contains(got, "work_item.issue_type_metadata") {
		t.Fatal("workItemEvidenceFactKinds missing registered read-surface kind \"work_item.issue_type_metadata\"")
	}
	if slices.Contains(got, facts.WorkItemMetadataWarningFactKind) {
		t.Fatal("workItemEvidenceFactKinds must exclude work_item.metadata_warning until #4887 surfaces it with metadata_type/reason")
	}
}

func TestWorkItemEvidenceSurfacesIssueTypeAndExcludesMetadataWarning(t *testing.T) {
	t.Parallel()

	// issue_type_metadata is on the read surface and must decode into an
	// evidence row (carrying the provider issue-type id and its project scope)
	// rather than being dropped at the switch default.
	rows := buildWorkItemEvidenceRows([]workItemEvidenceFactRow{
		{
			FactID:           "issue-type",
			FactKind:         "work_item.issue_type_metadata",
			ScopeID:          "jira:site:example",
			GenerationID:     "generation-1",
			SchemaVersion:    facts.WorkItemSchemaVersionV1,
			SourceConfidence: "reported",
			ObservedAt:       "2026-06-01T12:00:00Z",
			Payload: map[string]any{
				"provider":                 "jira_cloud",
				"issue_type_id":            "10001",
				"project_id":               "10000",
				"redaction_policy_version": "jira_work_item_v1",
			},
		},
	})

	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1 (issue_type_metadata surfaced, not dropped)", len(rows))
	}
	issueType := rows[0]
	if issueType.FactKind != "work_item.issue_type_metadata" {
		t.Fatalf("rows[0].FactKind = %q, want work_item.issue_type_metadata", issueType.FactKind)
	}
	if issueType.Provider != "jira_cloud" {
		t.Fatalf("issue_type_metadata Provider = %q, want jira_cloud", issueType.Provider)
	}
	if issueType.IssueTypeID != "10001" {
		t.Fatalf("issue_type_metadata IssueTypeID = %q, want 10001", issueType.IssueTypeID)
	}
	if issueType.ProjectID != "10000" {
		t.Fatalf("issue_type_metadata ProjectID = %q, want 10000", issueType.ProjectID)
	}

	// metadata_warning is deliberately NOT on the read surface: the read set
	// bounds the SQL query, and WorkItemEvidenceRow has no metadata_type/reason
	// field, so a warning must not present as an ordinary provider fact. The
	// SQL therefore never selects it and it never reaches the row builder. This
	// assertion fails if metadata_warning is added back to the read set before
	// #4887 gives WorkItemEvidenceRow the warning's contract fields.
	if slices.Contains(workItemEvidenceFactKinds, facts.WorkItemMetadataWarningFactKind) {
		t.Fatalf("read set must exclude %s until #4887; including it would surface a warning as an exact_provider_fact with no metadata_type/reason", facts.WorkItemMetadataWarningFactKind)
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
