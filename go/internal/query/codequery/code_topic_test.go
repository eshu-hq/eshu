// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codequery

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

type topicInvestigationContentStore struct {
	querytestutil.FakePortContentStore
	rows     []CodeTopicEvidenceRow
	requests []CodeTopicInvestigationRequest
	err      error
}

func (s *topicInvestigationContentStore) InvestigateCodeTopic(
	_ context.Context,
	req CodeTopicInvestigationRequest,
) ([]CodeTopicEvidenceRow, error) {
	s.requests = append(s.requests, req)
	if s.err != nil {
		return nil, s.err
	}
	return append([]CodeTopicEvidenceRow(nil), s.rows...), nil
}

func TestHandleCodeTopicInvestigationReturns503UntilSubstringIndexesReady(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{
		Content: &topicInvestigationContentStore{err: ErrContentSubstringIndexesNotReady},
		Profile: ProfileLocalAuthoritative,
	}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/topics/investigate",
		bytes.NewBufferString(`{"topic":"auth"}`),
	)
	rec := httptest.NewRecorder()

	handler.handleTopicInvestigation(rec, req)

	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, rec.Body.String())
	}
}

func TestHandleCodeTopicInvestigationReturnsRankedEvidenceAndHandles(t *testing.T) {
	t.Parallel()

	store := &topicInvestigationContentStore{
		rows: []CodeTopicEvidenceRow{
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
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
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
	packet := requireAnswerPacketCompanion(t, data, "code.topic")
	if got, want := packet["primary_tool"], "investigate_code_topic"; got != want {
		t.Fatalf("answer_packet.primary_tool = %#v, want %#v", got, want)
	}
	if got, want := packet["partial"], true; got != want {
		t.Fatalf("answer_packet.partial = %#v, want truncated packet to be partial", got)
	}
	if calls, ok := packet["recommended_next_calls"].([]any); !ok || len(calls) == 0 {
		t.Fatalf("answer_packet.recommended_next_calls = %#v, want next calls", packet["recommended_next_calls"])
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
	req.Header.Set("Accept", querycontract.EnvelopeMIMEType)
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
	packet := requireAnswerPacketCompanion(t, data, "code.topic")
	if got, want := packet["supported"], true; got != want {
		t.Fatalf("answer_packet.supported = %#v, want %#v", got, want)
	}
	if got, want := packet["partial"], true; got != want {
		t.Fatalf("answer_packet.partial = %#v, want no-evidence packet to be partial", got)
	}
	if summary, ok := packet["summary"].(string); ok && summary != "" {
		t.Fatalf("answer_packet.summary = %q, want no confident summary without evidence", summary)
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

func codeTopicStringSliceContains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// requireAnswerPacketCompanion asserts the answer_packet companion object the
// code.topic routes attach and returns it for route-specific assertions. Twin
// of the same-named helper in package query (answer_packet_route_test.go): a
// _test.go symbol is not importable across the package boundary (#6060).
func requireAnswerPacketCompanion(t *testing.T, data map[string]any, promptFamily string) map[string]any {
	t.Helper()

	packet, ok := data["answer_packet"].(map[string]any)
	if !ok {
		t.Fatalf("answer_packet = %#v, want object", data["answer_packet"])
	}
	if got, want := packet["prompt_family"], promptFamily; got != want {
		t.Fatalf("answer_packet.prompt_family = %#v, want %#v", got, want)
	}
	if got, want := packet["supported"], true; got != want {
		t.Fatalf("answer_packet.supported = %#v, want %#v", got, want)
	}
	if packet["truth_class"] == "" {
		t.Fatalf("answer_packet.truth_class = %#v, want non-empty", packet["truth_class"])
	}
	return packet
}
