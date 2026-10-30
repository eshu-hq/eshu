// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// collectIACRuntimeSnapshot removes parser-only evidence and binds its
// extraction to the exact source bytes accepted by the native parser.
func collectIACRuntimeSnapshot(repoPath string, parsedFiles []map[string]any, snapshot *RepositorySnapshot) {
	snapshot.IACRuntimeExpected = true
	snapshot.IACRuntime = make(map[string]iacRuntimeSnapshot)
	for _, parsed := range parsedFiles {
		private, present := parsed["iac_runtime_evidence"]
		if !present {
			continue
		}
		delete(parsed, "iac_runtime_evidence")
		evidence, ok := private.(shared.RuntimeEvidence)
		if !ok {
			continue
		}
		relativePath := model.RepositoryRelativePath(repoPath, model.PayloadPath(parsed, "path"))
		snapshot.IACRuntime[relativePath] = iacRuntimeSnapshot{Extraction: declaration.ExtractIACEvidence(evidence), Digest: evidence.Digest}
	}
}
