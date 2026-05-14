package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type topicInvestigationContentStore struct {
	fakePortContentStore
	rows     []codeTopicEvidenceRow
	requests []codeTopicInvestigationRequest
}

func (s *topicInvestigationContentStore) investigateCodeTopic(
	_ context.Context,
	req codeTopicInvestigationRequest,
) ([]codeTopicEvidenceRow, error) {
	s.requests = append(s.requests, req)
	return append([]codeTopicEvidenceRow(nil), s.rows...), nil
}

func TestHandleCodeTopicInvestigationReturnsRankedEvidenceAndHandles(t *testing.T) {
	t.Parallel()

	store := &topicInvestigationContentStore{
		rows: []codeTopicEvidenceRow{
			{
				SourceKind:   "entity",
				RepoID:       "repo-1",
				RelativePath: "go/internal/collector/reposync/auth.go",
				EntityID:     "entity-auth",
				EntityName:   "resolveGitHubAppAuth",
				EntityType:   "Function",
				Language:     "go",
				StartLine:    44,
				EndLine:      88,
				MatchedTerms: []string{"auth", "github", "repo", "sync"},
				Score:        4,
			},
			{
				SourceKind:   "file",
				RepoID:       "repo-1",
				RelativePath: "go/internal/collector/reposync/workspace_lock.go",
				Language:     "go",
				MatchedTerms: []string{"lock", "workspace"},
				Score:        2,
			},
		},
	}
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/topics/investigate",
		bytes.NewBufferString(`{"topic":"Find the code paths responsible for repo sync authentication and explain how GitHub App auth is resolved.","repo_id":"repo-1","intent":"explain_auth_flow","limit":1}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	if got, want := len(store.requests), 1; got != want {
		t.Fatalf("investigate calls = %d, want %d", got, want)
	}
	if got, want := store.requests[0].RepoID, "repo-1"; got != want {
		t.Fatalf("repo_id = %q, want %q", got, want)
	}
	if got, want := store.requests[0].Limit, 2; got != want {
		t.Fatalf("probe limit = %d, want %d", got, want)
	}
	for _, term := range []string{"repo", "sync", "auth", "github"} {
		if !codeTopicStringSliceContains(store.requests[0].Terms, term) {
			t.Fatalf("terms = %#v, want %q", store.requests[0].Terms, term)
		}
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := envelope.Truth.Capability, "code_search.topic_investigation"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map", envelope.Data)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	groups, ok := data["evidence_groups"].([]any)
	if !ok || len(groups) != 1 {
		t.Fatalf("evidence_groups = %#v, want one trimmed group", data["evidence_groups"])
	}
	group, ok := groups[0].(map[string]any)
	if !ok {
		t.Fatalf("evidence group type = %T, want map", groups[0])
	}
	if got, want := group["relative_path"], "go/internal/collector/reposync/auth.go"; got != want {
		t.Fatalf("relative_path = %#v, want %#v", got, want)
	}
	nextCalls, ok := group["recommended_next_calls"].([]any)
	if !ok || len(nextCalls) == 0 {
		t.Fatalf("recommended_next_calls = %#v, want at least one call", group["recommended_next_calls"])
	}
	callHandles, ok := data["call_graph_handles"].([]any)
	if !ok || len(callHandles) != 1 {
		t.Fatalf("call_graph_handles = %#v, want one entity handle", data["call_graph_handles"])
	}
	coverage, ok := data["coverage"].(map[string]any)
	if !ok {
		t.Fatalf("coverage type = %T, want map", data["coverage"])
	}
	if got, want := coverage["query_shape"], "content_topic_investigation"; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
}

func TestHandleCodeTopicInvestigationExplainsEmptyCoverage(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: &topicInvestigationContentStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/topics/investigate",
		bytes.NewBufferString(`{"topic":"workspace locking clone fetch default branch","limit":25}`),
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["empty"], true; got != want {
		t.Fatalf("coverage.empty = %#v, want %#v", got, want)
	}
	if got, want := int(coverage["searched_term_count"].(float64)), 6; got < want {
		t.Fatalf("searched_term_count = %d, want at least %d", got, want)
	}
	recommendations, ok := data["recommended_next_calls"].([]any)
	if !ok || len(recommendations) == 0 {
		t.Fatalf("recommended_next_calls = %#v, want fallback next calls", data["recommended_next_calls"])
	}
}

func TestHandleCodeTopicInvestigationRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: &topicInvestigationContentStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for name, body := range map[string]string{
		"missing topic": `{"limit":25}`,
		"huge offset":   `{"topic":"repo sync","offset":10001}`,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(http.MethodPost, "/api/v0/code/topics/investigate", bytes.NewBufferString(body))
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if got, want := w.Code, http.StatusBadRequest; got != want {
				t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
			}
		})
	}
}

func TestContentReaderInvestigateCodeTopicUsesOneScoredQuery(t *testing.T) {
	t.Parallel()

	db, recorder := openRecordingContentSearchDB(t, []contentSearchQueryResult{
		{
			columns: []string{
				"source_kind", "repo_id", "relative_path", "entity_id", "entity_name",
				"entity_type", "language", "start_line", "end_line", "matched_terms", "score",
			},
			rows: [][]driver.Value{
				{
					"entity", "repo-1", "go/internal/collector/reposync/auth.go", "entity-auth",
					"resolveGitHubAppAuth", "Function", "go", int64(44), int64(88),
					"auth\x1fgithub\x1frepo\x1fsync", int64(4),
				},
			},
		},
	})
	reader := NewContentReader(db)

	rows, err := reader.investigateCodeTopic(context.Background(), codeTopicInvestigationRequest{
		RepoID: "repo-1",
		Terms:  []string{"repo", "sync", "auth", "github"},
		Limit:  26,
		Offset: 0,
	})
	if err != nil {
		t.Fatalf("investigateCodeTopic() error = %v, want nil", err)
	}
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(rows) = %d, want %d", got, want)
	}
	if got, want := len(recorder.queries), 1; got != want {
		t.Fatalf("queries = %d, want one scored SQL query", got)
	}
	if !strings.Contains(recorder.queries[0], "WITH terms AS") {
		t.Fatalf("query = %q, want scored terms CTE", recorder.queries[0])
	}
	if got, want := recorder.args[0][0], "repo-1"; got != want {
		t.Fatalf("repo arg = %#v, want %#v", got, want)
	}
	if got, want := recorder.args[0][1], "repo\x1fsync\x1fauth\x1fgithub"; got != want {
		t.Fatalf("terms arg = %#v, want %#v", got, want)
	}
}

func codeTopicStringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
