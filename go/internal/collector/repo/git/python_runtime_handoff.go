// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	"github.com/eshu-hq/eshu/go/internal/parser/pythondep"
)

// collectPythonRuntimeSnapshot removes parser-private evidence before the
// parsed file can become a durable file fact.
func collectPythonRuntimeSnapshot(repoPath string, parsedFiles []map[string]any, snapshot *RepositorySnapshot) {
	snapshot.PythonRuntimeExpected = true
	snapshot.PythonRuntime = make(map[string]pythonRuntimeSnapshot)
	for _, parsed := range parsedFiles {
		private, present := parsed["python_runtime_evidence"]
		if !present {
			continue
		}
		delete(parsed, "python_runtime_evidence")
		evidence, ok := private.(pythondep.PythonRuntimeEvidence)
		if !ok {
			continue
		}
		relativePath := model.RepositoryRelativePath(repoPath, model.PayloadPath(parsed, "path"))
		snapshot.PythonRuntime[relativePath] = pythonRuntimeSnapshot{
			Extraction: declaration.ExtractPythonEvidence(evidence), Digest: evidence.Digest,
		}
	}
}
