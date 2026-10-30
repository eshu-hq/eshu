// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func collectRustRuntimeSnapshot(repoPath string, parsedFiles []map[string]any, snapshot *RepositorySnapshot) {
	snapshot.RustRuntimeExpected = true
	snapshot.RustRuntime = make(map[string]rustRuntimeSnapshot)
	for _, parsed := range parsedFiles {
		private, present := parsed["rust_runtime_evidence"]
		if !present {
			continue
		}
		delete(parsed, "rust_runtime_evidence")
		evidence, ok := private.(shared.RuntimeEvidence)
		if !ok {
			continue
		}
		relativePath := model.RepositoryRelativePath(repoPath, model.PayloadPath(parsed, "path"))
		snapshot.RustRuntime[relativePath] = rustRuntimeSnapshot{Extraction: declaration.ExtractRustEvidence(evidence), Digest: evidence.Digest}
	}
}
