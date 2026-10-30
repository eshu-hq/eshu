// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// collectSwiftRuntimeSnapshot removes private Package.swift tools-version
// evidence before file facts persist.
func collectSwiftRuntimeSnapshot(repoPath string, parsedFiles []map[string]any, snapshot *RepositorySnapshot) {
	snapshot.SwiftRuntimeExpected = true
	snapshot.SwiftRuntime = make(map[string]swiftRuntimeSnapshot)
	for _, parsed := range parsedFiles {
		private, present := parsed["swift_runtime_evidence"]
		if !present {
			continue
		}
		delete(parsed, "swift_runtime_evidence")
		evidence, ok := private.(shared.RuntimeEvidence)
		if !ok {
			continue
		}
		relativePath := model.RepositoryRelativePath(repoPath, model.PayloadPath(parsed, "path"))
		snapshot.SwiftRuntime[relativePath] = swiftRuntimeSnapshot{Extraction: declaration.ExtractSwiftEvidence(evidence), Digest: evidence.Digest}
	}
}
