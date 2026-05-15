package query

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type hardcodedSecretInvestigationContentStore struct {
	fakePortContentStore
	rows     []hardcodedSecretFindingRow
	requests []hardcodedSecretInvestigationRequest
}

func (s *hardcodedSecretInvestigationContentStore) investigateHardcodedSecrets(
	_ context.Context,
	req hardcodedSecretInvestigationRequest,
) ([]hardcodedSecretFindingRow, error) {
	s.requests = append(s.requests, req)
	return append([]hardcodedSecretFindingRow(nil), s.rows...), nil
}

func TestHandleHardcodedSecretInvestigationReturnsRedactedPromptPacket(t *testing.T) {
	t.Parallel()

	store := &hardcodedSecretInvestigationContentStore{
		rows: []hardcodedSecretFindingRow{
			{
				RepoID:       "repo-1",
				RelativePath: "cmd/api/config.go",
				Language:     "go",
				LineNumber:   42,
				LineText:     `apiToken := "sk_live_1234567890abcdef"`,
				FindingKind:  "api_token",
				Confidence:   "high",
				Severity:     "high",
			},
			{
				RepoID:       "repo-1",
				RelativePath: "cmd/api/config_test.go",
				Language:     "go",
				LineNumber:   7,
				LineText:     `password := "example-password"`,
				FindingKind:  "password_literal",
				Confidence:   "medium",
				Severity:     "medium",
				Suppressed:   true,
				Suppressions: []string{"test_file"},
			},
		},
	}
	handler := &CodeHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/security/secrets/investigate",
		bytes.NewBufferString(`{"repo_id":"repo-1","limit":1,"include_suppressed":true}`),
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

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if got, want := envelope.Truth.Capability, "security.hardcoded_secrets"; got != want {
		t.Fatalf("truth.capability = %q, want %q", got, want)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("data type = %T, want map", envelope.Data)
	}
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	findings := data["findings"].([]any)
	if got, want := len(findings), 1; got != want {
		t.Fatalf("findings length = %d, want %d", got, want)
	}
	first := findings[0].(map[string]any)
	excerpt := first["redacted_excerpt"].(string)
	if strings.Contains(excerpt, "sk_live_1234567890abcdef") {
		t.Fatalf("redacted_excerpt leaked secret: %q", excerpt)
	}
	if !strings.Contains(excerpt, "[REDACTED]") {
		t.Fatalf("redacted_excerpt = %q, want redaction marker", excerpt)
	}
	if got, want := first["finding_kind"], "api_token"; got != want {
		t.Fatalf("finding_kind = %#v, want %#v", got, want)
	}
	sourceHandle := first["source_handle"].(map[string]any)
	if got, want := sourceHandle["repo_id"], "repo-1"; got != want {
		t.Fatalf("source_handle.repo_id = %#v, want %#v", got, want)
	}
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["query_shape"], "content_secret_investigation"; got != want {
		t.Fatalf("coverage.query_shape = %#v, want %#v", got, want)
	}
	if got, want := coverage["suppressed_count"], float64(1); got != want {
		t.Fatalf("coverage.suppressed_count = %#v, want %#v", got, want)
	}
}

func TestHandleHardcodedSecretInvestigationRejectsHugeOffset(t *testing.T) {
	t.Parallel()

	handler := &CodeHandler{Content: &hardcodedSecretInvestigationContentStore{}, Profile: ProfileLocalAuthoritative}
	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/code/security/secrets/investigate",
		bytes.NewBufferString(`{"offset":10001}`),
	)
	w := httptest.NewRecorder()

	handler.handleHardcodedSecretInvestigation(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, w.Body.String())
	}
}
