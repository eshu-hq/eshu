// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingCICDRunCorrelationStore struct {
	rows       []CICDRunCorrelationRow
	lastFilter CICDRunCorrelationFilter
}

var errUnexpectedContentHydration = errors.New("unexpected workflow artifact content hydration")

func (s *recordingCICDRunCorrelationStore) ListCICDRunCorrelations(
	_ context.Context,
	filter CICDRunCorrelationFilter,
) ([]CICDRunCorrelationRow, error) {
	s.lastFilter = filter
	// Filter by RepositoryID when set (simulating the real Postgres store's
	// WHERE payload->>'repository_id' = $3 predicate). When the filter does
	// not constrain RepositoryID, return all rows — matching the pre-existing
	// behavior every existing test caller relies on.
	if filter.RepositoryID != "" {
		out := make([]CICDRunCorrelationRow, 0, len(s.rows))
		for _, row := range s.rows {
			if row.RepositoryID == filter.RepositoryID {
				out = append(out, row)
			}
		}
		return out, nil
	}
	return append([]CICDRunCorrelationRow(nil), s.rows...), nil
}

type workflowPathOnlyContentStore struct {
	fakePortContentStore
	getFileContentCalls int
}

func (s *workflowPathOnlyContentStore) GetFileContent(
	context.Context,
	string,
	string,
) (*FileContent, error) {
	s.getFileContentCalls++
	return nil, errUnexpectedContentHydration
}

func TestCICDListRunCorrelationsRequiresScopeAndLimit(t *testing.T) {
	t.Parallel()

	handler := &CICDHandler{Correlations: &recordingCICDRunCorrelationStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, target := range []string{
		"/api/v0/ci-cd/run-correlations?limit=10",
		"/api/v0/ci-cd/run-correlations?repository_id=repo-api",
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

func TestCICDListRunCorrelationsUsesBoundedPostgresStore(t *testing.T) {
	t.Parallel()

	store := &recordingCICDRunCorrelationStore{
		rows: []CICDRunCorrelationRow{
			{
				CorrelationID:   "correlation-1",
				Provider:        "github_actions",
				RunID:           "run-1",
				RunAttempt:      "1",
				RepositoryID:    "repo-api",
				CommitSHA:       "abc123",
				ArtifactDigest:  "sha256:cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
				Outcome:         "exact",
				Reason:          "artifact digest matches one container image identity row",
				CanonicalWrites: 1,
			},
			{CorrelationID: "correlation-2", Provider: "github_actions", RunID: "run-2", RepositoryID: "repo-api"},
		},
	}
	handler := &CICDHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations?repository_id=repo-api&commit_sha=abc123&limit=1",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.RepositoryID, "repo-api"; got != want {
		t.Fatalf("RepositoryID = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.CommitSHA, "abc123"; got != want {
		t.Fatalf("CommitSHA = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.Limit, 2; got != want {
		t.Fatalf("Limit = %d, want %d", got, want)
	}

	var resp struct {
		Correlations []CICDRunCorrelationResult `json:"correlations"`
		Count        int                        `json:"count"`
		Limit        int                        `json:"limit"`
		Truncated    bool                       `json:"truncated"`
		NextCursor   map[string]string          `json:"next_cursor"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := len(resp.Correlations), 1; got != want {
		t.Fatalf("len(correlations) = %d, want %d", got, want)
	}
	if got, want := resp.Correlations[0].RunID, "run-1"; got != want {
		t.Fatalf("RunID = %q, want %q", got, want)
	}
	if !resp.Truncated {
		t.Fatal("truncated = false, want true")
	}
	if got, want := resp.NextCursor["after_correlation_id"], "correlation-1"; got != want {
		t.Fatalf("next_cursor.after_correlation_id = %q, want %q", got, want)
	}
}

func TestCICDListRunCorrelationsHydratesStaticWorkflowArtifactsOnce(t *testing.T) {
	t.Parallel()

	content := &workflowPathOnlyContentStore{
		fakePortContentStore: fakePortContentStore{
			repoFiles: []FileContent{{
				RepoID:       "repo://example/api",
				RelativePath: ".github/workflows/deploy.yml",
				ArtifactType: "github_actions_workflow",
			}},
		},
	}
	handler := &CICDHandler{
		Content:      content,
		Correlations: &recordingCICDRunCorrelationStore{},
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations?repository_id=repo://example/api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got := content.getFileContentCalls; got != 1 {
		t.Fatalf("GetFileContent calls = %d, want 1 bounded workflow evidence hydration", got)
	}

	var resp struct {
		EvidenceSummary cicdRunCorrelationEvidenceSummary `json:"evidence_summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.EvidenceSummary.StaticWorkflowArtifacts.State, "present"; got != want {
		t.Fatalf("static_workflow_artifacts.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.StaticWorkflowArtifacts.Paths, []string{".github/workflows/deploy.yml"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("static_workflow_artifacts.paths = %#v, want %#v", got, want)
	}
	if got, want := resp.EvidenceSummary.StaticWorkflowArtifacts.Reason, "workflow_image_evidence_read_failed"; got != want {
		t.Fatalf("static_workflow_artifacts.reason = %q, want %q", got, want)
	}
}

func TestCICDListRunCorrelationsExplainsStaticWorkflowOnlyEvidence(t *testing.T) {
	t.Parallel()

	store := &recordingCICDRunCorrelationStore{}
	handler := &CICDHandler{
		Content: fakePortContentStore{
			repoFiles: []FileContent{{
				RepoID:       "repo://example/api",
				RelativePath: ".github/workflows/deploy.yml",
				ArtifactType: "github_actions_workflow",
				Content: `name: deploy
on:
  push:
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - run: echo deploy
`,
			}},
		},
		Correlations: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations?repository_id=repo://example/api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Correlations    []CICDRunCorrelationResult        `json:"correlations"`
		EvidenceSummary cicdRunCorrelationEvidenceSummary `json:"evidence_summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got := len(resp.Correlations); got != 0 {
		t.Fatalf("len(correlations) = %d, want 0", got)
	}
	if got, want := resp.EvidenceSummary.StaticWorkflowArtifacts.State, "present"; got != want {
		t.Fatalf("static_workflow_artifacts.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.StaticWorkflowArtifacts.Count, 1; got != want {
		t.Fatalf("static_workflow_artifacts.count = %d, want %d", got, want)
	}
	if got, want := resp.EvidenceSummary.StaticWorkflowArtifacts.Paths, []string{".github/workflows/deploy.yml"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("static_workflow_artifacts.paths = %#v, want %#v", got, want)
	}
	if got, want := resp.EvidenceSummary.LiveRunCorrelations.State, "missing"; got != want {
		t.Fatalf("live_run_correlations.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.LiveRunCorrelations.Reason, "static_workflow_only_live_run_correlation_missing"; got != want {
		t.Fatalf("live_run_correlations.reason = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.RunArtifactEvidence.State, "missing"; got != want {
		t.Fatalf("run_artifact_evidence.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.RunArtifactEvidence.Reason, "static_workflow_only_live_run_correlation_missing"; got != want {
		t.Fatalf("run_artifact_evidence.reason = %q, want %q", got, want)
	}
}

func TestCICDListRunCorrelationsExplainsLiveRunEvidence(t *testing.T) {
	t.Parallel()

	store := &recordingCICDRunCorrelationStore{
		rows: []CICDRunCorrelationRow{{
			CorrelationID: "correlation-1",
			RepositoryID:  "repo://example/api",
			Provider:      "github_actions",
			RunID:         "run-1",
			Outcome:       "exact",
		}},
	}
	handler := &CICDHandler{
		Content:      fakePortContentStore{},
		Correlations: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations?repository_id=repo://example/api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		Count           int                               `json:"count"`
		EvidenceSummary cicdRunCorrelationEvidenceSummary `json:"evidence_summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.Count, 1; got != want {
		t.Fatalf("count = %d, want %d", got, want)
	}
	if got, want := resp.EvidenceSummary.LiveRunCorrelations.State, "present"; got != want {
		t.Fatalf("live_run_correlations.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.LiveRunCorrelations.Count, 1; got != want {
		t.Fatalf("live_run_correlations.count = %d, want %d", got, want)
	}
	if got, want := resp.EvidenceSummary.StaticWorkflowArtifacts.State, "absent"; got != want {
		t.Fatalf("static_workflow_artifacts.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.RunArtifactEvidence.State, "missing"; got != want {
		t.Fatalf("run_artifact_evidence.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.RunArtifactEvidence.Reason, "artifact_or_image_evidence_missing"; got != want {
		t.Fatalf("run_artifact_evidence.reason = %q, want %q", got, want)
	}
}

func TestCICDListRunCorrelationsExplainsNoEvidence(t *testing.T) {
	t.Parallel()

	store := &recordingCICDRunCorrelationStore{}
	handler := &CICDHandler{
		Content:      fakePortContentStore{},
		Correlations: store,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations?repository_id=repo://example/api&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp struct {
		EvidenceSummary cicdRunCorrelationEvidenceSummary `json:"evidence_summary"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if got, want := resp.EvidenceSummary.StaticWorkflowArtifacts.State, "absent"; got != want {
		t.Fatalf("static_workflow_artifacts.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.LiveRunCorrelations.State, "missing"; got != want {
		t.Fatalf("live_run_correlations.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.Reason, "no_ci_cd_evidence_found"; got != want {
		t.Fatalf("evidence_summary.reason = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.RunArtifactEvidence.State, "missing"; got != want {
		t.Fatalf("run_artifact_evidence.state = %q, want %q", got, want)
	}
	if got, want := resp.EvidenceSummary.RunArtifactEvidence.Reason, "no_ci_cd_evidence_found"; got != want {
		t.Fatalf("run_artifact_evidence.reason = %q, want %q", got, want)
	}
}

func TestCICDListRunCorrelationsUsesImageRefAnchor(t *testing.T) {
	t.Parallel()

	store := &recordingCICDRunCorrelationStore{}
	handler := &CICDHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations?image_ref=registry.example.com/team/api:prod&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.ImageRef, "registry.example.com/team/api:prod"; got != want {
		t.Fatalf("ImageRef = %q, want %q", got, want)
	}
}

func TestCICDListRunCorrelationsRequiresProviderForProviderRunID(t *testing.T) {
	t.Parallel()

	handler := &CICDHandler{Correlations: &recordingCICDRunCorrelationStore{}}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations?provider_run_id=12345&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestCICDListRunCorrelationsPassesProviderRunDisambiguator(t *testing.T) {
	t.Parallel()

	store := &recordingCICDRunCorrelationStore{}
	handler := &CICDHandler{Correlations: store}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/ci-cd/run-correlations?provider=github_actions&provider_run_id=12345&limit=10",
		nil,
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.lastFilter.Provider, "github_actions"; got != want {
		t.Fatalf("Provider = %q, want %q", got, want)
	}
	if got, want := store.lastFilter.ProviderRunID, "12345"; got != want {
		t.Fatalf("ProviderRunID = %q, want %q", got, want)
	}
}

func TestCICDRunCorrelationQueryExcludesTombstones(t *testing.T) {
	t.Parallel()

	if !strings.Contains(listCICDRunCorrelationsQuery, "fact.is_tombstone = FALSE") {
		t.Fatalf("listCICDRunCorrelationsQuery must exclude tombstone facts:\n%s", listCICDRunCorrelationsQuery)
	}
}

func TestCICDRunCorrelationQueryFiltersProviderWithRunID(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"fact.payload->>'provider' = $5",
		"fact.payload->>'run_id' = $6",
	} {
		if !strings.Contains(listCICDRunCorrelationsQuery, want) {
			t.Fatalf("listCICDRunCorrelationsQuery missing %q:\n%s", want, listCICDRunCorrelationsQuery)
		}
	}
}

func TestCICDRunCorrelationQueryFiltersImageRef(t *testing.T) {
	t.Parallel()

	if !strings.Contains(listCICDRunCorrelationsQuery, "fact.payload->>'image_ref' = $8") {
		t.Fatalf("listCICDRunCorrelationsQuery must filter image_ref:\n%s", listCICDRunCorrelationsQuery)
	}
}
