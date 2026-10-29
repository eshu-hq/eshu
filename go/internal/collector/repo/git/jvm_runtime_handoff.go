// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// collectJVMRuntimeSnapshot removes parser-only version evidence before file
// payloads reach durable facts and binds each extraction to the source digest.
func collectJVMRuntimeSnapshot(repoPath string, parsedFiles []map[string]any, snapshot *RepositorySnapshot) {
	snapshot.JVMRuntimeExpected = true
	snapshot.JVMRuntime = make(map[string]jvmRuntimeSnapshot)
	for _, parsed := range parsedFiles {
		private, present := parsed["jvm_runtime_evidence"]
		if !present {
			continue
		}
		delete(parsed, "jvm_runtime_evidence")
		evidence, ok := private.(shared.RuntimeEvidence)
		if !ok {
			continue
		}
		relativePath := model.RepositoryRelativePath(repoPath, model.PayloadPath(parsed, "path"))
		snapshot.JVMRuntime[relativePath] = jvmRuntimeSnapshot{Extraction: declaration.ExtractJVMEvidence(evidence), Digest: evidence.Digest}
	}
}
