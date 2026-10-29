// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package git

import (
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/model"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/runtime/declaration"
	rubyparser "github.com/eshu-hq/eshu/go/internal/parser/ruby"
)

// collectRubyRuntimeSnapshot removes private parser evidence before file facts persist.
func collectRubyRuntimeSnapshot(repoPath string, parsedFiles []map[string]any, snapshot *RepositorySnapshot) {
	snapshot.RubyRuntimeExpected = true
	snapshot.RubyRuntime = make(map[string]rubyRuntimeSnapshot)
	for _, parsed := range parsedFiles {
		private, present := parsed["ruby_runtime_evidence"]
		if !present {
			continue
		}
		delete(parsed, "ruby_runtime_evidence")
		evidence, ok := private.(rubyparser.RubyRuntimeEvidence)
		if !ok {
			continue
		}
		relativePath := model.RepositoryRelativePath(repoPath, model.PayloadPath(parsed, "path"))
		snapshot.RubyRuntime[relativePath] = rubyRuntimeSnapshot{Extraction: declaration.ExtractRubyEvidence(evidence), Digest: evidence.Digest}
	}
}
