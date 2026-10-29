// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// collectDotNetRuntimeSnapshot removes private parser evidence before facts persist.
func collectDotNetRuntimeSnapshot(repoPath string, parsedFiles []map[string]any, snapshot *RepositorySnapshot) {
	snapshot.DotNetRuntimeExpected = true
	snapshot.DotNetRuntime = make(map[string]dotNetRuntimeSnapshot)
	for _, parsed := range parsedFiles {
		private, present := parsed["dotnet_runtime_evidence"]
		if !present {
			continue
		}
		delete(parsed, "dotnet_runtime_evidence")
		evidence, ok := private.(shared.DotNetRuntimeEvidence)
		if !ok {
			continue
		}
		relativePath := model.RepositoryRelativePath(repoPath, model.PayloadPath(parsed, "path"))
		snapshot.DotNetRuntime[relativePath] = dotNetRuntimeSnapshot{Extraction: declaration.ExtractDotNetEvidence(evidence), Digest: evidence.Digest}
	}
}
