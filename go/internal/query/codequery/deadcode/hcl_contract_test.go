// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package deadcode

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser"
	"github.com/eshu-hq/eshu/go/internal/query/codemodel"
)

func TestDeadCodeLanguageMaturityCoversParserLanguageContracts(t *testing.T) {
	t.Parallel()

	registry := parser.DefaultRegistry()
	for _, definition := range registry.Definitions() {
		key := definition.ParserKey
		if !deadCodeMaturityParserKeys[key] {
			if _, ok := codemodel.DeadCodeLanguageMaturity[key]; ok {
				t.Fatalf("codemodel.DeadCodeLanguageMaturity[%q] exists, want parser key without dead-code contract excluded", key)
			}
			continue
		}
		if _, ok := codemodel.DeadCodeLanguageMaturity[key]; !ok {
			t.Fatalf("codemodel.DeadCodeLanguageMaturity missing parser key %q", key)
		}
	}
}

func TestDeadCodeCandidateLabelsForHCLAreEmpty(t *testing.T) {
	t.Parallel()

	if got := DeadCodeCandidateLabelsForLanguage("hcl"); len(got) != 0 {
		t.Fatalf("DeadCodeCandidateLabelsForLanguage(hcl) = %#v, want no code candidate labels", got)
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
