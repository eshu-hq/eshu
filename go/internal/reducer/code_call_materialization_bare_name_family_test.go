package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractCodeCallRowsResolvesRepoUniqueBareCallsAcrossParserFamilies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		repoID       string
		callerPath   string
		calleePath   string
		callerEntity string
		calleeEntity string
		call         map[string]any
	}{
		{
			name:         "c",
			repoID:       "repo-c",
			callerPath:   "service.c",
			calleePath:   "helper.c",
			callerEntity: "content-entity:c-run",
			calleeEntity: "content-entity:c-helper",
			call:         map[string]any{"name": "helper", "full_name": "helper", "line_number": 4},
		},
		{
			name:         "cpp",
			repoID:       "repo-cpp",
			callerPath:   "service.cpp",
			calleePath:   "helper.cpp",
			callerEntity: "content-entity:cpp-run",
			calleeEntity: "content-entity:cpp-helper",
			call:         map[string]any{"name": "helper", "full_name": "helper", "line_number": 4},
		},
		{
			name:         "csharp",
			repoID:       "repo-csharp",
			callerPath:   "Service.cs",
			calleePath:   "Helper.cs",
			callerEntity: "content-entity:csharp-run",
			calleeEntity: "content-entity:csharp-helper",
			call:         map[string]any{"name": "Helper", "full_name": "Helper", "line_number": 4},
		},
		{
			name:         "java",
			repoID:       "repo-java",
			callerPath:   "Service.java",
			calleePath:   "Helper.java",
			callerEntity: "content-entity:java-run",
			calleeEntity: "content-entity:java-helper",
			call:         map[string]any{"name": "helper", "full_name": "helper", "line_number": 4},
		},
		{
			name:         "scala",
			repoID:       "repo-scala",
			callerPath:   "Service.scala",
			calleePath:   "Helper.scala",
			callerEntity: "content-entity:scala-run",
			calleeEntity: "content-entity:scala-helper",
			call:         map[string]any{"name": "helper", "full_name": "helper", "line_number": 4},
		},
		{
			name:         "kotlin",
			repoID:       "repo-kotlin-bare",
			callerPath:   "Service.kt",
			calleePath:   "Helper.kt",
			callerEntity: "content-entity:kotlin-run",
			calleeEntity: "content-entity:kotlin-helper",
			call: map[string]any{
				"name": "helper", "full_name": "helper", "line_number": 4, "lang": "kotlin",
			},
		},
		{
			name:         "rust",
			repoID:       "repo-rust-bare",
			callerPath:   "main.rs",
			calleePath:   "helper.rs",
			callerEntity: "content-entity:rust-run",
			calleeEntity: "content-entity:rust-helper",
			call: map[string]any{
				"name": "helper", "full_name": "helper", "line_number": 4, "lang": "rust",
			},
		},
		{
			name:         "dart",
			repoID:       "repo-dart",
			callerPath:   "service.dart",
			calleePath:   "helper.dart",
			callerEntity: "content-entity:dart-run",
			calleeEntity: "content-entity:dart-helper",
			call:         map[string]any{"name": "helper", "full_name": "helper", "line_number": 4},
		},
		{
			name:         "haskell",
			repoID:       "repo-haskell",
			callerPath:   "Service.hs",
			calleePath:   "Helper.hs",
			callerEntity: "content-entity:haskell-run",
			calleeEntity: "content-entity:haskell-helper",
			call:         map[string]any{"name": "helper", "full_name": "helper", "line_number": 4},
		},
		{
			name:         "perl",
			repoID:       "repo-perl",
			callerPath:   "service.pl",
			calleePath:   "helper.pl",
			callerEntity: "content-entity:perl-run",
			calleeEntity: "content-entity:perl-helper",
			call:         map[string]any{"name": "helper", "full_name": "helper", "line_number": 4},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, rows := ExtractCodeCallRows(
				bareNameRepoEnvelopes(
					tt.repoID,
					tt.callerPath,
					tt.calleePath,
					tt.callerEntity,
					tt.calleeEntity,
					tt.call,
					false,
				),
			)
			if len(rows) != 1 {
				t.Fatalf("len(rows) = %d, want 1", len(rows))
			}
			if got := rows[0]["caller_entity_id"]; got != tt.callerEntity {
				t.Fatalf("caller_entity_id = %#v, want %#v", got, tt.callerEntity)
			}
			if got := rows[0]["callee_entity_id"]; got != tt.calleeEntity {
				t.Fatalf("callee_entity_id = %#v, want %#v", got, tt.calleeEntity)
			}
			if got := rows[0]["callee_file"]; got != tt.calleePath {
				t.Fatalf("callee_file = %#v, want %#v", got, tt.calleePath)
			}
		})
	}
}

func TestExtractCodeCallRowsSkipsAmbiguousRepoBareCallsAcrossParserFamilies(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		repoID     string
		callerPath string
		calleePath string
		call       map[string]any
	}{
		{name: "c", repoID: "repo-c-ambiguous", callerPath: "service.c", calleePath: "helper.c", call: map[string]any{"name": "helper", "full_name": "helper", "line_number": 4}},
		{name: "cpp", repoID: "repo-cpp-ambiguous", callerPath: "service.cpp", calleePath: "helper.cpp", call: map[string]any{"name": "helper", "full_name": "helper", "line_number": 4}},
		{name: "csharp", repoID: "repo-csharp-ambiguous", callerPath: "Service.cs", calleePath: "Helper.cs", call: map[string]any{"name": "Helper", "full_name": "Helper", "line_number": 4}},
		{name: "java", repoID: "repo-java-ambiguous", callerPath: "Service.java", calleePath: "Helper.java", call: map[string]any{"name": "helper", "full_name": "helper", "line_number": 4}},
		{name: "scala", repoID: "repo-scala-ambiguous", callerPath: "Service.scala", calleePath: "Helper.scala", call: map[string]any{"name": "helper", "full_name": "helper", "line_number": 4}},
		{name: "kotlin", repoID: "repo-kotlin-ambiguous", callerPath: "Service.kt", calleePath: "Helper.kt", call: map[string]any{"name": "helper", "full_name": "helper", "line_number": 4, "lang": "kotlin"}},
		{name: "rust", repoID: "repo-rust-ambiguous", callerPath: "main.rs", calleePath: "helper.rs", call: map[string]any{"name": "helper", "full_name": "helper", "line_number": 4, "lang": "rust"}},
		{name: "dart", repoID: "repo-dart-ambiguous", callerPath: "service.dart", calleePath: "helper.dart", call: map[string]any{"name": "helper", "full_name": "helper", "line_number": 4}},
		{name: "haskell", repoID: "repo-haskell-ambiguous", callerPath: "Service.hs", calleePath: "Helper.hs", call: map[string]any{"name": "helper", "full_name": "helper", "line_number": 4}},
		{name: "perl", repoID: "repo-perl-ambiguous", callerPath: "service.pl", calleePath: "helper.pl", call: map[string]any{"name": "helper", "full_name": "helper", "line_number": 4}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, rows := ExtractCodeCallRows(
				bareNameRepoEnvelopes(
					tt.repoID,
					tt.callerPath,
					tt.calleePath,
					"content-entity:caller",
					"content-entity:callee",
					tt.call,
					true,
				),
			)
			if len(rows) != 0 {
				t.Fatalf("len(rows) = %d, want 0", len(rows))
			}
		})
	}
}

func bareNameRepoEnvelopes(
	repoID string,
	callerPath string,
	calleePath string,
	callerEntity string,
	calleeEntity string,
	call map[string]any,
	ambiguous bool,
) []facts.Envelope {
	envelopes := []facts.Envelope{
		{FactKind: "repository", Payload: map[string]any{"repo_id": repoID}},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       repoID,
				"relative_path": callerPath,
				"parsed_file_data": map[string]any{
					"path": callerPath,
					"functions": []any{
						map[string]any{"name": "run", "line_number": 3, "end_line": 6, "uid": callerEntity},
					},
					"function_calls": []any{call},
				},
			},
		},
		{
			FactKind: "file",
			Payload: map[string]any{
				"repo_id":       repoID,
				"relative_path": calleePath,
				"parsed_file_data": map[string]any{
					"path": calleePath,
					"functions": []any{
						map[string]any{"name": call["name"], "line_number": 1, "end_line": 2, "uid": calleeEntity},
					},
				},
			},
		},
	}
	if !ambiguous {
		return envelopes
	}

	return append(envelopes, facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": "alternate_" + calleePath,
			"parsed_file_data": map[string]any{
				"path": "alternate_" + calleePath,
				"functions": []any{
					map[string]any{"name": call["name"], "line_number": 1, "end_line": 2, "uid": "content-entity:alternate-callee"},
				},
			},
		},
	})
}
