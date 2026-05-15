package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEvidenceHandlerBuildsCitationPacketFromFileAndEntityHandles(t *testing.T) {
	t.Parallel()

	store := &citationPacketContentStore{
		files: map[evidenceCitationFileKey]FileContent{
			{repoID: "repo-service", relativePath: "cmd/api/main.go"}: {
				RepoID:       "repo-service",
				RelativePath: "cmd/api/main.go",
				CommitSHA:    "abc123",
				Content:      "package main\n\nfunc main() {\n\tstartAPI()\n}\n",
				ContentHash:  "sha256:file",
				LineCount:    6,
				Language:     "go",
				ArtifactType: "source",
			},
		},
		entities: map[string]*EntityContent{
			"entity:service:handler": {
				EntityID:     "entity:service:handler",
				RepoID:       "repo-service",
				RelativePath: "internal/api/handler.go",
				EntityType:   "function",
				EntityName:   "HandleCheckout",
				StartLine:    12,
				EndLine:      16,
				Language:     "go",
				SourceCache:  "func HandleCheckout() {\n\tcallService()\n}\n",
			},
		},
	}
	handler := &EvidenceHandler{Content: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := map[string]any{
		"subject":  map[string]any{"type": "service", "id": "workload:checkout-api"},
		"question": "Explain this service and cite the proof.",
		"limit":    10,
		"handles": []map[string]any{
			{
				"kind":            "file",
				"repo_id":         "repo-service",
				"relative_path":   "cmd/api/main.go",
				"start_line":      1,
				"end_line":        4,
				"evidence_family": "source",
				"reason":          "API entry point",
			},
			{
				"kind":            "entity",
				"entity_id":       "entity:service:handler",
				"evidence_family": "source",
				"reason":          "Request handler",
			},
		},
	}
	reqBody, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/evidence/citations", bytes.NewReader(reqBody))
	req.Header.Set("Accept", EnvelopeMIMEType)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	if got, want := store.fileBatchCalls, 1; got != want {
		t.Fatalf("fileBatchCalls = %d, want %d", got, want)
	}
	if got, want := store.entityBatchCalls, 1; got != want {
		t.Fatalf("entityBatchCalls = %d, want %d", got, want)
	}
	if got := store.singleFileLineCalls + store.singleEntityCalls; got != 0 {
		t.Fatalf("single handle reads = %d, want 0", got)
	}

	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	if envelope.Truth == nil {
		t.Fatal("truth envelope is nil, want capability metadata")
	}
	if got, want := envelope.Truth.Capability, "evidence_citation.packet"; got != want {
		t.Fatalf("truth capability = %q, want %q", got, want)
	}
	data, ok := envelope.Data.(map[string]any)
	if !ok {
		t.Fatalf("envelope data type = %T, want map", envelope.Data)
	}
	citations, ok := data["citations"].([]any)
	if !ok {
		t.Fatalf("citations type = %T, want array", data["citations"])
	}
	if got, want := len(citations), 2; got != want {
		t.Fatalf("citations length = %d, want %d", got, want)
	}

	first := citations[0].(map[string]any)
	if got, want := first["repo_id"], "repo-service"; got != want {
		t.Fatalf("first.repo_id = %#v, want %#v", got, want)
	}
	if got, want := first["relative_path"], "cmd/api/main.go"; got != want {
		t.Fatalf("first.relative_path = %#v, want %#v", got, want)
	}
	if got, want := first["start_line"], float64(1); got != want {
		t.Fatalf("first.start_line = %#v, want %#v", got, want)
	}
	if got, want := first["end_line"], float64(4); got != want {
		t.Fatalf("first.end_line = %#v, want %#v", got, want)
	}
	if got, want := first["excerpt"], "package main\n\nfunc main() {\n\tstartAPI()"; got != want {
		t.Fatalf("first.excerpt = %#v, want %#v", got, want)
	}

	second := citations[1].(map[string]any)
	if got, want := second["entity_id"], "entity:service:handler"; got != want {
		t.Fatalf("second.entity_id = %#v, want %#v", got, want)
	}
	if got, want := second["entity_name"], "HandleCheckout"; got != want {
		t.Fatalf("second.entity_name = %#v, want %#v", got, want)
	}
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["resolved_count"], float64(2); got != want {
		t.Fatalf("coverage.resolved_count = %#v, want %#v", got, want)
	}
	if got, want := coverage["truncated"], false; got != want {
		t.Fatalf("coverage.truncated = %#v, want %#v", got, want)
	}
}

func TestEvidenceHandlerCitationPacketRejectsEmptyHandles(t *testing.T) {
	t.Parallel()

	handler := &EvidenceHandler{Content: &citationPacketContentStore{}, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v0/evidence/citations", bytes.NewReader([]byte(`{"handles":[]}`)))
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusBadRequest; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
}

func TestEvidenceHandlerCitationPacketReportsMissingAndTruncatedHandles(t *testing.T) {
	t.Parallel()

	store := &citationPacketContentStore{
		files: map[evidenceCitationFileKey]FileContent{
			{repoID: "repo-service", relativePath: "README.md"}: {
				RepoID:       "repo-service",
				RelativePath: "README.md",
				Content:      "line one\nline two\nline three\n",
				ContentHash:  "sha256:readme",
				LineCount:    3,
				Language:     "markdown",
				ArtifactType: "documentation",
			},
		},
	}
	handler := &EvidenceHandler{Content: store, Profile: ProfileProduction}
	mux := http.NewServeMux()
	handler.Mount(mux)

	body := map[string]any{
		"limit": 1,
		"handles": []map[string]any{
			{"kind": "file", "repo_id": "repo-service", "relative_path": "README.md"},
			{"kind": "file", "repo_id": "repo-service", "relative_path": "missing.md"},
			{"kind": "entity", "entity_id": "entity:missing"},
		},
	}
	reqBody, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v0/evidence/citations", bytes.NewReader(reqBody))
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := envelope.Data.(map[string]any)
	citations := data["citations"].([]any)
	if got, want := len(citations), 1; got != want {
		t.Fatalf("citations length = %d, want %d", got, want)
	}
	coverage := data["coverage"].(map[string]any)
	if got, want := coverage["truncated"], true; got != want {
		t.Fatalf("coverage.truncated = %#v, want %#v", got, want)
	}
	if got, want := coverage["input_handle_count"], float64(3); got != want {
		t.Fatalf("coverage.input_handle_count = %#v, want %#v", got, want)
	}
}

func TestContentReaderEvidenceCitationFilesHydratesBatch(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"repo_id", "relative_path", "commit_sha", "content", "content_hash",
				"line_count", "language", "artifact_type",
			},
			rows: [][]driver.Value{
				{
					"repo-service", "README.md", "abc123", "# Service\n", "sha256:readme",
					int64(1), "markdown", "documentation",
				},
				{
					"repo-service", "cmd/api/main.go", "abc123", "package main\n", "sha256:main",
					int64(1), "go", "source",
				},
			},
		},
	})

	reader := NewContentReader(db)
	files, err := reader.evidenceCitationFiles(t.Context(), []evidenceCitationFileLookup{
		{RepoID: "repo-service", RelativePath: "README.md"},
		{RepoID: "repo-service", RelativePath: "cmd/api/main.go"},
		{RepoID: "repo-service", RelativePath: "README.md"},
		{RepoID: "", RelativePath: "ignored"},
	})
	if err != nil {
		t.Fatalf("evidenceCitationFiles() error = %v, want nil", err)
	}
	if got, want := len(files), 2; got != want {
		t.Fatalf("len(files) = %d, want %d", got, want)
	}
	readme := files[evidenceCitationFileKey{repoID: "repo-service", relativePath: "README.md"}]
	if got, want := readme.ArtifactType, "documentation"; got != want {
		t.Fatalf("README artifact type = %q, want %q", got, want)
	}
	mainFile := files[evidenceCitationFileKey{repoID: "repo-service", relativePath: "cmd/api/main.go"}]
	if got, want := mainFile.Language, "go"; got != want {
		t.Fatalf("main.go language = %q, want %q", got, want)
	}
}

type citationPacketContentStore struct {
	fakePortContentStore
	files               map[evidenceCitationFileKey]FileContent
	entities            map[string]*EntityContent
	fileBatchCalls      int
	entityBatchCalls    int
	singleFileLineCalls int
	singleEntityCalls   int
}

func (s *citationPacketContentStore) evidenceCitationFiles(
	_ context.Context,
	lookups []evidenceCitationFileLookup,
) (map[evidenceCitationFileKey]FileContent, error) {
	s.fileBatchCalls++
	results := make(map[evidenceCitationFileKey]FileContent, len(lookups))
	for _, lookup := range lookups {
		key := evidenceCitationFileKey{repoID: lookup.RepoID, relativePath: lookup.RelativePath}
		if file, ok := s.files[key]; ok {
			results[key] = file
		}
	}
	return results, nil
}

func (s *citationPacketContentStore) GetEntityContents(
	_ context.Context,
	entityIDs []string,
) (map[string]*EntityContent, error) {
	s.entityBatchCalls++
	results := make(map[string]*EntityContent, len(entityIDs))
	for _, entityID := range entityIDs {
		if entity, ok := s.entities[entityID]; ok {
			copyEntity := *entity
			results[entityID] = &copyEntity
		}
	}
	return results, nil
}

func (s *citationPacketContentStore) GetFileLines(
	ctx context.Context,
	repoID string,
	relativePath string,
	startLine int,
	endLine int,
) (*FileContent, error) {
	s.singleFileLineCalls++
	return s.fakePortContentStore.GetFileLines(ctx, repoID, relativePath, startLine, endLine)
}

func (s *citationPacketContentStore) GetEntityContent(
	ctx context.Context,
	entityID string,
) (*EntityContent, error) {
	s.singleEntityCalls++
	return s.fakePortContentStore.GetEntityContent(ctx, entityID)
}
