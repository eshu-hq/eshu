// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// collectHaskellRuntimeSnapshot removes parser-only evidence and binds its
// extraction to the exact source bytes accepted by the native parser.
func collectHaskellRuntimeSnapshot(repoPath string, parsedFiles []map[string]any, snapshot *RepositorySnapshot) {
	snapshot.HaskellRuntimeExpected = true
	snapshot.HaskellRuntime = make(map[string]haskellRuntimeSnapshot)
	for _, parsed := range parsedFiles {
		private, present := parsed["haskell_runtime_evidence"]
		if !present {
			continue
		}
		delete(parsed, "haskell_runtime_evidence")
		evidence, ok := private.(shared.RuntimeEvidence)
		if !ok {
			continue
		}
		relativePath := model.RepositoryRelativePath(repoPath, model.PayloadPath(parsed, "path"))
		snapshot.HaskellRuntime[relativePath] = haskellRuntimeSnapshot{Extraction: declaration.ExtractHaskellEvidence(evidence), Digest: evidence.Digest}
	}
}
