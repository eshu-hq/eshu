package query

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestOpenAPIDeadCodeMentionsHaskellRootsAndLanguageFilter(t *testing.T) {
	var spec map[string]any
	if err := json.Unmarshal([]byte(OpenAPISpec()), &spec); err != nil {
		t.Fatalf("json.Unmarshal(OpenAPISpec()) error = %v, want nil", err)
	}

	paths := mustMapField(t, spec, "paths")
	deadCodePath := mustMapField(t, paths, "/api/v0/code/dead-code")
	deadCodePost := mustMapField(t, deadCodePath, "post")
	description, ok := deadCodePost["description"].(string)
	if !ok {
		t.Fatalf("code/dead-code description = %T, want string", deadCodePost["description"])
	}
	if !strings.Contains(description, "Haskell") {
		t.Fatalf("code/dead-code description = %q, want Haskell root coverage", description)
	}

	requestBody := mustMapField(t, mustMapField(t, deadCodePost, "requestBody"), "content")
	requestJSON := mustMapField(t, requestBody, "application/json")
	schema := mustMapField(t, mustMapField(t, requestJSON, "schema"), "properties")
	language := mustMapField(t, schema, "language")
	languageDescription, ok := language["description"].(string)
	if !ok {
		t.Fatalf("code/dead-code language description = %T, want string", language["description"])
	}
	if !strings.Contains(languageDescription, "haskell") {
		t.Fatalf("code/dead-code language description = %q, want haskell example", languageDescription)
	}
}
