package query

import (
	"bytes"
	"database/sql/driver"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestResolveEntityFallsBackToJavaAnnotationContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"annotation-1", "repo-1", "src/Logged.java", "Annotation", "Logged",
					int64(2), int64(2), "java", "@Logged", []byte(`{"kind":"applied","target_kind":"method_declaration"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"Logged","type":"annotation","repo_id":"repo-1"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	entities, ok := resp["entities"].([]any)
	if !ok || len(entities) != 1 {
		t.Fatalf("entities = %#v, want one content-backed entity", resp["entities"])
	}

	entity, ok := entities[0].(map[string]any)
	if !ok {
		t.Fatalf("entity type = %T, want map[string]any", entities[0])
	}

	if got, want := entity["semantic_summary"], "Annotation Logged is applied to a method declaration."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}

	profile, ok := entity["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("entity[semantic_profile] type = %T, want map[string]any", entity["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "applied_annotation"; got != want {
		t.Fatalf("entity[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := entity["story"], "Annotation Logged is applied to a method declaration. Defined in src/Logged.java (java)."; got != want {
		t.Fatalf("entity[story] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextFallsBackToJavaAnnotationContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"annotation-1", "repo-1", "src/Logged.java", "Annotation", "Logged",
					int64(2), int64(2), "java", "@Logged", []byte(`{"kind":"applied","target_kind":"method_declaration"}`),
				},
			},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"method-1", "repo-1", "src/Logged.java", "Function", "handle",
					int64(4), int64(8), "java", "@Logged\nvoid handle() {}", []byte(`{"method_kind":"instance"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/annotation-1/context", nil)
	req.SetPathValue("entity_id", "annotation-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	if got, want := resp["semantic_summary"], "Annotation Logged is applied to a method declaration."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	if got, want := resp["story"], "Annotation Logged is applied to a method declaration. Defined in src/Logged.java (java)."; got != want {
		t.Fatalf("resp[story] = %#v, want %#v", got, want)
	}
	profile, ok := resp["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("resp[semantic_profile] type = %T, want map[string]any", resp["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "applied_annotation"; got != want {
		t.Fatalf("resp[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
}

func TestResolveEntityFallsBackToPythonAssignmentTypeAnnotationContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"annotation-1", "repo-1", "src/settings.py", "TypeAnnotation", "timeout",
					int64(3), int64(3), "python", "timeout: int = 30", []byte(`{"type":"int","annotation_kind":"assignment"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"timeout","type":"type_annotation","repo_id":"repo-1"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	entities, ok := resp["entities"].([]any)
	if !ok || len(entities) != 1 {
		t.Fatalf("entities = %#v, want one content-backed entity", resp["entities"])
	}

	entity, ok := entities[0].(map[string]any)
	if !ok {
		t.Fatalf("entity type = %T, want map[string]any", entities[0])
	}

	if got, want := entity["semantic_summary"], "TypeAnnotation timeout is annotated as int."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
	if got, want := entity["story"], "TypeAnnotation timeout is annotated as int. Defined in src/settings.py (python)."; got != want {
		t.Fatalf("entity[story] = %#v, want %#v", got, want)
	}
	profile, ok := entity["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("entity[semantic_profile] type = %T, want map[string]any", entity["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "type_annotation"; got != want {
		t.Fatalf("entity[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := profile["annotation_kind"], "assignment"; got != want {
		t.Fatalf("entity[semantic_profile][annotation_kind] = %#v, want %#v", got, want)
	}
	pythonSemantics, ok := entity["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("entity[python_semantics] type = %T, want map[string]any", entity["python_semantics"])
	}
	if got, want := pythonSemantics["surface_kind"], "type_annotation"; got != want {
		t.Fatalf("entity[python_semantics][surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := pythonSemantics["annotation_kind"], "assignment"; got != want {
		t.Fatalf("entity[python_semantics][annotation_kind] = %#v, want %#v", got, want)
	}
}

func TestResolveEntityFallsBackToPythonParameterTypeAnnotationContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"annotation-1", "repo-1", "src/app.py", "TypeAnnotation", "name",
					int64(3), int64(3), "python", "def greet(name: str) -> str:", []byte(`{"type":"str","annotation_kind":"parameter","context":"greet"}`),
				},
			},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodPost,
		"/api/v0/entities/resolve",
		bytes.NewBufferString(`{"name":"name","type":"type_annotation","repo_id":"repo-1"}`),
	)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	entities, ok := resp["entities"].([]any)
	if !ok || len(entities) != 1 {
		t.Fatalf("entities = %#v, want one content-backed entity", resp["entities"])
	}

	entity, ok := entities[0].(map[string]any)
	if !ok {
		t.Fatalf("entity type = %T, want map[string]any", entities[0])
	}

	if got, want := entity["semantic_summary"], "TypeAnnotation name is a parameter annotation for greet with type str."; got != want {
		t.Fatalf("entity[semantic_summary] = %#v, want %#v", got, want)
	}
	if got, want := entity["story"], "TypeAnnotation name is a parameter annotation for greet with type str. Defined in src/app.py (python)."; got != want {
		t.Fatalf("entity[story] = %#v, want %#v", got, want)
	}
	profile, ok := entity["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("entity[semantic_profile] type = %T, want map[string]any", entity["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "parameter_type_annotation"; got != want {
		t.Fatalf("entity[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := profile["annotation_kind"], "parameter"; got != want {
		t.Fatalf("entity[semantic_profile][annotation_kind] = %#v, want %#v", got, want)
	}
	pythonSemantics, ok := entity["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("entity[python_semantics] type = %T, want map[string]any", entity["python_semantics"])
	}
	if got, want := pythonSemantics["surface_kind"], "parameter_type_annotation"; got != want {
		t.Fatalf("entity[python_semantics][surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := pythonSemantics["annotation_kind"], "parameter"; got != want {
		t.Fatalf("entity[python_semantics][annotation_kind] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextFallsBackToPythonAssignmentTypeAnnotationContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"annotation-1", "repo-1", "src/settings.py", "TypeAnnotation", "timeout",
					int64(3), int64(3), "python", "timeout: int = 30", []byte(`{"type":"int","annotation_kind":"assignment"}`),
				},
			},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/annotation-1/context", nil)
	req.SetPathValue("entity_id", "annotation-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	if got, want := resp["semantic_summary"], "TypeAnnotation timeout is annotated as int."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	if got, want := resp["story"], "TypeAnnotation timeout is annotated as int. Defined in src/settings.py (python)."; got != want {
		t.Fatalf("resp[story] = %#v, want %#v", got, want)
	}
	profile, ok := resp["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("resp[semantic_profile] type = %T, want map[string]any", resp["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "type_annotation"; got != want {
		t.Fatalf("resp[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := profile["annotation_kind"], "assignment"; got != want {
		t.Fatalf("resp[semantic_profile][annotation_kind] = %#v, want %#v", got, want)
	}
	pythonSemantics, ok := resp["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("resp[python_semantics] type = %T, want map[string]any", resp["python_semantics"])
	}
	if got, want := pythonSemantics["surface_kind"], "type_annotation"; got != want {
		t.Fatalf("resp[python_semantics][surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := pythonSemantics["annotation_kind"], "assignment"; got != want {
		t.Fatalf("resp[python_semantics][annotation_kind] = %#v, want %#v", got, want)
	}
}

func TestGetEntityContextFallsBackToPythonParameterTypeAnnotationContentEntity(t *testing.T) {
	t.Parallel()

	db := openContentReaderTestDB(t, []contentReaderQueryResult{
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{
				{
					"annotation-1", "repo-1", "src/app.py", "TypeAnnotation", "name",
					int64(3), int64(3), "python", "def greet(name: str) -> str:", []byte(`{"type":"str","annotation_kind":"parameter","context":"greet"}`),
				},
			},
		},
		{
			columns: []string{
				"entity_id", "repo_id", "relative_path", "entity_type", "entity_name",
				"start_line", "end_line", "language", "source_cache", "metadata",
			},
			rows: [][]driver.Value{},
		},
	})

	handler := &EntityHandler{Content: NewContentReader(db)}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/v0/entities/annotation-1/context", nil)
	req.SetPathValue("entity_id", "annotation-1")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", w.Code, w.Body.String())
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}

	if got, want := resp["semantic_summary"], "TypeAnnotation name is a parameter annotation for greet with type str."; got != want {
		t.Fatalf("resp[semantic_summary] = %#v, want %#v", got, want)
	}
	if got, want := resp["story"], "TypeAnnotation name is a parameter annotation for greet with type str. Defined in src/app.py (python)."; got != want {
		t.Fatalf("resp[story] = %#v, want %#v", got, want)
	}
	profile, ok := resp["semantic_profile"].(map[string]any)
	if !ok {
		t.Fatalf("resp[semantic_profile] type = %T, want map[string]any", resp["semantic_profile"])
	}
	if got, want := profile["surface_kind"], "parameter_type_annotation"; got != want {
		t.Fatalf("resp[semantic_profile][surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := profile["annotation_kind"], "parameter"; got != want {
		t.Fatalf("resp[semantic_profile][annotation_kind] = %#v, want %#v", got, want)
	}
	pythonSemantics, ok := resp["python_semantics"].(map[string]any)
	if !ok {
		t.Fatalf("resp[python_semantics] type = %T, want map[string]any", resp["python_semantics"])
	}
	if got, want := pythonSemantics["surface_kind"], "parameter_type_annotation"; got != want {
		t.Fatalf("resp[python_semantics][surface_kind] = %#v, want %#v", got, want)
	}
	if got, want := pythonSemantics["annotation_kind"], "parameter"; got != want {
		t.Fatalf("resp[python_semantics][annotation_kind] = %#v, want %#v", got, want)
	}
}
