// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// collectDartRuntimeSnapshot removes private parser evidence before facts persist.
func collectDartRuntimeSnapshot(repoPath string, parsedFiles []map[string]any, snapshot *RepositorySnapshot) {
	snapshot.DartRuntimeExpected = true
	snapshot.DartRuntime = make(map[string]dartRuntimeSnapshot)
	for _, parsed := range parsedFiles {
		private, present := parsed["dart_runtime_evidence"]
		if !present {
			continue
		}
		delete(parsed, "dart_runtime_evidence")
		evidence, ok := private.(shared.RuntimeEvidence)
		if !ok {
			continue
		}
		relativePath := model.RepositoryRelativePath(repoPath, model.PayloadPath(parsed, "path"))
		snapshot.DartRuntime[relativePath] = dartRuntimeSnapshot{Extraction: declaration.ExtractDartEvidence(evidence), Digest: evidence.Digest}
	}
}
