package query

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type recordingCICDRunCorrelationStore struct {
	rows       []CICDRunCorrelationRow
	lastFilter CICDRunCorrelationFilter
}

func (s *recordingCICDRunCorrelationStore) ListCICDRunCorrelations(
	_ context.Context,
	filter CICDRunCorrelationFilter,
) ([]CICDRunCorrelationRow, error) {
	s.lastFilter = filter
	return append([]CICDRunCorrelationRow(nil), s.rows...), nil
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
			{CorrelationID: "correlation-2", Provider: "github_actions", RunID: "run-2"},
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
