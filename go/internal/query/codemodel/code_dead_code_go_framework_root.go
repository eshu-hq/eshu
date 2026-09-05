// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

// deadCodeIsGoFrameworkRoot reports whether a Go entity is a framework root
// (CLI command, HTTP handler, or framework callback) that should be excluded
// from dead-code results. It records whether the decision came from parser
// metadata or the source fallback so the analysis can report its provenance.
func DeadCodeIsGoFrameworkRoot(result map[string]any, policy DeadCodeGoPolicyContext, stats *DeadCodePolicyStats) bool {
	if policy.Language != "go" {
		return false
	}
	if len(policy.RootKinds) > 0 {
		if deadCodeIsGoCLICommandRoot(result, policy) ||
			deadCodeIsGoHTTPHandlerRoot(result, policy) ||
			deadCodeIsGoFrameworkCallbackRoot(result, policy) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
		return false
	}

	if deadCodeIsGoCLICommandRoot(result, policy) ||
		deadCodeIsGoHTTPHandlerRoot(result, policy) ||
		deadCodeIsGoFrameworkCallbackRoot(result, policy) {
		stats.SourceFallbackFrameworkRoots++
		return true
	}
	return false
}
