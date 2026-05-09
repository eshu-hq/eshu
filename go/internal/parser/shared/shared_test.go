package shared

import "testing"

func TestBasePayloadCarriesCommonParserBuckets(t *testing.T) {
	t.Parallel()

	got := BasePayload("/repo/main.go", "go", true)

	if got["path"] != "/repo/main.go" || got["lang"] != "go" || got["is_dependency"] != true {
		t.Fatalf("BasePayload identity fields = %#v", got)
	}
	for _, key := range []string{"functions", "classes", "variables", "imports", "function_calls"} {
		if _, ok := got[key].([]map[string]any); !ok {
			t.Fatalf("BasePayload[%q] = %T, want []map[string]any", key, got[key])
		}
	}
}

func TestAppendBucketAndSortNamedMapsAreDeterministic(t *testing.T) {
	t.Parallel()

	payload := map[string]any{}
	AppendBucket(payload, "functions", map[string]any{"name": "zeta", "line_number": 20})
	AppendBucket(payload, "functions", map[string]any{"name": "omega", "line_number": 10})
	AppendBucket(payload, "functions", map[string]any{"name": "alpha", "line_number": 10})

	SortNamedBucket(payload, "functions")
	got := payload["functions"].([]map[string]any)
	if got[0]["name"] != "alpha" || got[1]["name"] != "omega" || got[2]["name"] != "zeta" {
		t.Fatalf("sorted functions = %#v, want line then name order", got)
	}
}

func TestOptionsNormalizedVariableScope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   Options
		want string
	}{
		{name: "empty defaults to module", in: Options{}, want: "module"},
		{name: "all survives", in: Options{VariableScope: " ALL "}, want: "all"},
		{name: "unknown becomes module", in: Options{VariableScope: "local"}, want: "module"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := tt.in.NormalizedVariableScope(); got != tt.want {
				t.Fatalf("NormalizedVariableScope() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCollectBucketNamesCleansNonEmptyNames(t *testing.T) {
	t.Parallel()

	payload := map[string]any{
		"functions": []map[string]any{
			{"name": "./cmd/api"},
			{"name": " "},
		},
		"classes": []map[string]any{
			{"name": "internal//parser"},
		},
	}

	got := CollectBucketNames(payload, "functions", "classes")
	if len(got) != 2 || got[0] != "cmd/api" || got[1] != "internal/parser" {
		t.Fatalf("CollectBucketNames() = %#v, want cleaned non-empty names", got)
	}
}

func TestSmallUtilityHelpers(t *testing.T) {
	t.Parallel()

	if got := IntValue(float64(42)); got != 42 {
		t.Fatalf("IntValue(float64) = %d, want 42", got)
	}
	if got := LastPathSegment("a/b/c", "/"); got != "c" {
		t.Fatalf("LastPathSegment() = %q, want c", got)
	}
	if got := DedupeNonEmptyStrings([]string{"b", "a", "", "a"}); len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("DedupeNonEmptyStrings() = %#v, want sorted unique non-empty values", got)
	}
}
