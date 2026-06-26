// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// makeCodeImportFileEnvelopeEntries builds a file-kind envelope whose
// parsed_file_data.imports[] carry the given raw entry maps verbatim. Unlike
// makeCodeImportFileEnvelope (which only sets "source"), this lets a test model
// the real per-language parser shape: Go emits the import path under "name" with
// no "source"; JS/TS/Python emit "name"=imported symbol and "source"=module.
func makeCodeImportFileEnvelopeEntries(repoID, relPath, language string, entries []map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: factKindFile,
		Payload: map[string]any{
			"repo_id":       repoID,
			"relative_path": relPath,
			"language":      language,
			"parsed_file_data": map[string]any{
				"imports": entries,
			},
		},
	}
}

// TestCodeImportResolvesGoNameOnlyImport guards #3870: the Go parser emits an
// import as {"name": "<module path>", "lang": "go"} with no "source" field, so a
// resolver that reads only "source"/"resolved_source" never resolves Go imports.
// The import path is under "name" and must resolve to the owning repo.
func TestCodeImportResolvesGoNameOnlyImport(t *testing.T) {
	t.Parallel()

	owners := codeImportTestOwners() // {"gomod","github.com/gin-gonic/gin"} -> repo-gin
	envelopes := []facts.Envelope{
		makeCodeImportFileEnvelopeEntries("consumer-repo-a", "main.go", "go", []map[string]any{
			{"name": "github.com/gin-gonic/gin", "lang": "go"},
		}),
	}

	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-go-name",
		GenerationID:  "gen-go-name",
		SourceRunID:   "code_import_repo_dependency:scope-go-name",
		CreatedAt:     time.Now(),
		FileEnvelopes: envelopes,
		Owners:        owners,
	}
	intents := BuildCodeImportRepoDependencyIntents(input)
	if len(intents) != 1 {
		t.Fatalf("len(intents) = %d, want 1 (go import path under \"name\" must resolve)", len(intents))
	}
	if got := intents[0].Payload["target_repo_id"]; got != "repo-gin" {
		t.Fatalf("target_repo_id = %v, want %q", got, "repo-gin")
	}
}

// TestCodeImportPrefersModuleSourceOverSymbolName guards the regression risk in
// the #3870 fix: for JS/TS/Python, "name" is the imported SYMBOL and "source" is
// the module. The resolver must key on the module ("source"), never the symbol —
// reading "name" first would drop the real edge and could fabricate a false one
// if a symbol name collides with a package name.
func TestCodeImportPrefersModuleSourceOverSymbolName(t *testing.T) {
	t.Parallel()

	owners := codeImportTestOwners() // {"npm","express"} -> repo-express; no owner named "Router"
	envelopes := []facts.Envelope{
		// import { Router } from "express"  ->  name="Router" (symbol), source="express" (module)
		makeCodeImportFileEnvelopeEntries("consumer-repo-b", "src/app.js", "javascript", []map[string]any{
			{"name": "Router", "source": "express"},
		}),
	}

	input := CodeImportRepoDependencyInput{
		ScopeID:       "scope-js-symbol",
		GenerationID:  "gen-js-symbol",
		SourceRunID:   "code_import_repo_dependency:scope-js-symbol",
		CreatedAt:     time.Now(),
		FileEnvelopes: envelopes,
		Owners:        owners,
	}
	intents := BuildCodeImportRepoDependencyIntents(input)
	if len(intents) != 1 {
		t.Fatalf("len(intents) = %d, want 1 (module \"source\" must resolve, not symbol \"name\")", len(intents))
	}
	if got := intents[0].Payload["target_repo_id"]; got != "repo-express" {
		t.Fatalf("target_repo_id = %v, want %q (resolved from module, not symbol)", got, "repo-express")
	}
}
