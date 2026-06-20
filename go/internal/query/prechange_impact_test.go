package query

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPreChangeImpactNormalizesFileListIntoAnswerPacket(t *testing.T) {
	t.Parallel()

	store := fakePortContentStore{entities: []EntityContent{
		{
			EntityID:     "entity-auth",
			EntityName:   "resolveGitHubAppAuth",
			EntityType:   "Function",
			RepoID:       "repo-1",
			RelativePath: "go/internal/collector/reposync/auth.go",
			Language:     "go",
			StartLine:    44,
			EndLine:      88,
		},
		{
			EntityID:     "entity-new",
			EntityName:   "newWorkspaceLock",
			EntityType:   "Function",
			RepoID:       "repo-1",
			RelativePath: "go/internal/collector/reposync/workspace_lock.go",
			Language:     "go",
			StartLine:    12,
			EndLine:      30,
		},
	}}
	handler := &ImpactHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := `{
		"repo_id":"repo-1",
		"base_ref":"main",
		"head_ref":"feature/pre-change",
		"changes":[
			{"path":"go/internal/collector/reposync/auth.go","status":"modified"},
			{"old_path":"go/internal/collector/reposync/lock.go","path":"go/internal/collector/reposync/workspace_lock.go","status":"renamed"},
			{"path":"go/internal/collector/reposync/deleted.go","status":"deleted"}
		],
		"limit":10
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/pre-change", bytes.NewBufferString(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	envelope := decodePreChangeEnvelope(t, rec)
	if got, want := envelope.Truth.Capability, preChangeImpactCapability; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	data := envelope.Data.(map[string]any)
	if got, want := data["workflow"], "pre_change_impact"; got != want {
		t.Fatalf("workflow = %#v, want %#v", got, want)
	}
	if got, want := int(data["changed_file_count"].(float64)), 3; got != want {
		t.Fatalf("changed_file_count = %d, want %d", got, want)
	}
	changeSet := data["change_set"].(map[string]any)
	if got, want := changeSet["base_ref"], "main"; got != want {
		t.Fatalf("base_ref = %#v, want %#v", got, want)
	}
	files := data["changed_files"].([]any)
	renamed := preChangeFileByStatus(t, files, "renamed")
	if got, want := renamed["status"], "renamed"; got != want {
		t.Fatalf("renamed status = %#v, want %#v", got, want)
	}
	if got, want := renamed["old_path"], "go/internal/collector/reposync/lock.go"; got != want {
		t.Fatalf("renamed old_path = %#v, want %#v", got, want)
	}
	codeSurface := data["code_surface"].(map[string]any)
	if got, want := int(codeSurface["symbol_count"].(float64)), 2; got != want {
		t.Fatalf("symbol_count = %d, want %d", got, want)
	}
	missing := data["missing_evidence"].([]any)
	if got, want := len(missing), 1; got != want {
		t.Fatalf("missing_evidence count = %d, want %d", got, want)
	}
	if got, want := missing[0].(map[string]any)["reason"], "deleted_path_requires_prior_generation"; got != want {
		t.Fatalf("missing_evidence reason = %#v, want %#v", got, want)
	}
	packet := data["answer_packet"].(map[string]any)
	if got, want := packet["prompt_family"], "pre_change.impact"; got != want {
		t.Fatalf("answer_packet.prompt_family = %#v, want %#v", got, want)
	}
	if got, want := packet["partial"], true; got != want {
		t.Fatalf("answer_packet.partial = %#v, want %#v", got, want)
	}
}

func TestPreChangeImpactAllowsEmptyDiff(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Content: fakePortContentStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := `{"repo_id":"repo-1","base_ref":"main","head_ref":"main","changed_paths":[]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/pre-change", bytes.NewBufferString(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	envelope := decodePreChangeEnvelope(t, rec)
	data := envelope.Data.(map[string]any)
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["state"], "empty_diff"; got != want {
		t.Fatalf("coverage.state = %#v, want %#v", got, want)
	}
	if got, want := data["truncated"], false; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	packet := data["answer_packet"].(map[string]any)
	if got, want := packet["supported"], true; got != want {
		t.Fatalf("answer_packet.supported = %#v, want %#v", got, want)
	}
	if got, want := packet["partial"], false; got != want {
		t.Fatalf("answer_packet.partial = %#v, want %#v", got, want)
	}
}

func TestPreChangeImpactRejectsRefsWithoutChangedInput(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Content: fakePortContentStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := `{"repo_id":"repo-1","base_ref":"main","head_ref":"feature/pre-change"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/pre-change", bytes.NewBufferString(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("changed_paths or changes")) {
		t.Fatalf("error body does not mention changed input requirement: %s", rec.Body.String())
	}
}

func TestPreChangeImpactCodeSurfaceBackendUnavailableReturns503(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := `{"repo_id":"repo-1","topic":"authentication"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/pre-change", bytes.NewBufferString(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusServiceUnavailable; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
}

func TestPreChangeImpactReportsHighFanoutTruncation(t *testing.T) {
	t.Parallel()

	store := fakePortContentStore{entities: []EntityContent{
		{EntityID: "entity-a", EntityName: "a", EntityType: "Function", RepoID: "repo-1", RelativePath: "a.go"},
		{EntityID: "entity-b", EntityName: "b", EntityType: "Function", RepoID: "repo-1", RelativePath: "b.go"},
		{EntityID: "entity-c", EntityName: "c", EntityType: "Function", RepoID: "repo-1", RelativePath: "c.go"},
	}}
	handler := &ImpactHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := `{"repo_id":"repo-1","changed_paths":["a.go","b.go","c.go"],"limit":2}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/pre-change", bytes.NewBufferString(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	envelope := decodePreChangeEnvelope(t, rec)
	data := envelope.Data.(map[string]any)
	if got, want := data["truncated"], true; got != want {
		t.Fatalf("truncated = %#v, want %#v", got, want)
	}
	packet := data["answer_packet"].(map[string]any)
	if got, want := packet["partial"], true; got != want {
		t.Fatalf("answer_packet.partial = %#v, want %#v", got, want)
	}
}

func TestPreChangeImpactRejectsUnsafeChangedPaths(t *testing.T) {
	t.Parallel()

	handler := &ImpactHandler{Content: fakePortContentStore{}, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	for _, body := range []string{
		`{"repo_id":"repo-1","changed_paths":["/tmp/auth.go"]}`,
		`{"repo_id":"repo-1","changed_paths":["../auth.go"]}`,
		`{"repo_id":"repo-1","changes":[{"path":"go/auth.go","old_path":"../old.go","status":"renamed"}]}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/pre-change", bytes.NewBufferString(body))
		req.Header.Set("Accept", EnvelopeMIMEType)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)

		if got, want := rec.Code, http.StatusBadRequest; got != want {
			t.Fatalf("status = %d, want %d body=%s request=%s", got, want, rec.Body.String(), body)
		}
	}
}

func TestPreChangeImpactDeduplicatesCanonicalPaths(t *testing.T) {
	t.Parallel()

	store := fakePortContentStore{entities: []EntityContent{{
		EntityID:     "entity-auth",
		EntityName:   "resolveGitHubAppAuth",
		EntityType:   "Function",
		RepoID:       "repo-1",
		RelativePath: "go/internal/collector/reposync/auth.go",
	}}}
	handler := &ImpactHandler{Content: store, Profile: ProfileLocalAuthoritative}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := `{"repo_id":"repo-1","changed_paths":["./go/internal/collector/reposync/auth.go","go/internal/collector/reposync/auth.go"]}`
	req := httptest.NewRequest(http.MethodPost, "/api/v0/impact/pre-change", bytes.NewBufferString(body))
	req.Header.Set("Accept", EnvelopeMIMEType)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if got, want := rec.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d body=%s", got, want, rec.Body.String())
	}
	envelope := decodePreChangeEnvelope(t, rec)
	data := envelope.Data.(map[string]any)
	if got, want := int(data["changed_file_count"].(float64)), 1; got != want {
		t.Fatalf("changed_file_count = %d, want %d", got, want)
	}
}

func preChangeFileByStatus(t *testing.T, files []any, status string) map[string]any {
	t.Helper()

	for _, file := range files {
		row := file.(map[string]any)
		if row["status"] == status {
			return row
		}
	}
	t.Fatalf("changed_files missing status %q: %#v", status, files)
	return nil
}

func decodePreChangeEnvelope(t *testing.T, rec *httptest.ResponseRecorder) ResponseEnvelope {
	t.Helper()

	var envelope ResponseEnvelope
	if err := json.Unmarshal(rec.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v; body=%s", err, rec.Body.String())
	}
	if envelope.Truth == nil {
		t.Fatalf("truth envelope is nil; body=%s", rec.Body.String())
	}
	if envelope.Error != nil {
		t.Fatalf("unexpected error envelope: %+v", envelope.Error)
	}
	return envelope
}
