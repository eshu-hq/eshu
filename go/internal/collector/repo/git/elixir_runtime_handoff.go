// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// collectElixirRuntimeSnapshot removes private mix.exs parser evidence before
// file facts persist. It keeps the guarded extraction bound to the exact
// source bytes the native Elixir parse read.
func collectElixirRuntimeSnapshot(repoPath string, parsedFiles []map[string]any, snapshot *RepositorySnapshot) {
	snapshot.ElixirRuntimeExpected = true
	snapshot.ElixirRuntime = make(map[string]elixirRuntimeSnapshot)
	for _, parsed := range parsedFiles {
		private, present := parsed["elixir_runtime_evidence"]
		if !present {
			continue
		}
		delete(parsed, "elixir_runtime_evidence")
		evidence, ok := private.(shared.RuntimeEvidence)
		if !ok {
			continue
		}
		relativePath := model.RepositoryRelativePath(repoPath, model.PayloadPath(parsed, "path"))
		snapshot.ElixirRuntime[relativePath] = elixirRuntimeSnapshot{Extraction: declaration.ExtractElixirEvidence(evidence), Digest: evidence.Digest}
	}
}
