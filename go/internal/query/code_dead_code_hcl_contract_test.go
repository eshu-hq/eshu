package query

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestDeadCodeLanguageMaturityCoversParserLanguageContracts(t *testing.T) {
	t.Parallel()

	registry := parser.DefaultRegistry()
	for _, definition := range registry.Definitions() {
		key := definition.ParserKey
		if !deadCodeMaturityParserKeys[key] {
			if _, ok := deadCodeLanguageMaturity[key]; ok {
				t.Fatalf("deadCodeLanguageMaturity[%q] exists, want parser key without dead-code contract excluded", key)
			}
			continue
		}
		if _, ok := deadCodeLanguageMaturity[key]; !ok {
			t.Fatalf("deadCodeLanguageMaturity missing parser key %q", key)
		}
	}
}

func TestDeadCodeCandidateLabelsForHCLAreEmpty(t *testing.T) {
	t.Parallel()

	if got := deadCodeCandidateLabelsForLanguage("hcl"); len(got) != 0 {
		t.Fatalf("deadCodeCandidateLabelsForLanguage(hcl) = %#v, want no code candidate labels", got)
	}
}

var deadCodeMaturityParserKeys = map[string]bool{
	"c":          true,
	"c_sharp":    true,
	"cpp":        true,
	"dart":       true,
	"elixir":     true,
	"go":         true,
	"groovy":     true,
	"hcl":        true,
	"haskell":    true,
	"java":       true,
	"javascript": true,
	"kotlin":     true,
	"perl":       true,
	"php":        true,
	"python":     true,
	"ruby":       true,
	"rust":       true,
	"scala":      true,
	"sql":        true,
	"swift":      true,
	"tsx":        true,
	"typescript": true,
}

func assertQueryTestStringSliceEqual(t *testing.T, got any, want []string) {
	t.Helper()

	gotSlice, ok := got.([]any)
	if !ok {
		t.Fatalf("string slice type = %T, want []any", got)
	}
	if len(gotSlice) != len(want) {
		t.Fatalf("string slice = %#v, want %#v", gotSlice, want)
	}
	for i, wantValue := range want {
		if gotValue, ok := gotSlice[i].(string); !ok || gotValue != wantValue {
			t.Fatalf("string slice = %#v, want %#v", gotSlice, want)
		}
	}
}
