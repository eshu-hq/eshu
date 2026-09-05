// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

var rustDeadCodeMetadataRootKinds = []string{
	"rust.main_function",
	"rust.test_function",
	"rust.tokio_main",
	"rust.tokio_test",
	"rust.public_api_item",
	"rust.trait_impl_method",
	"rust.benchmark_function",
}

// DeadCodeIsRustRoot reports whether the candidate is a Rust entrypoint root.
func DeadCodeIsRustRoot(result map[string]any, entity *querycontract.EntityContent, stats *DeadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "rust" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range rustDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}

// DeadCodeIsRustCargoAuxiliaryTarget reports whether the candidate is a Rust cargo auxiliary target.
func DeadCodeIsRustCargoAuxiliaryTarget(result map[string]any, entity *querycontract.EntityContent) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "rust" {
		return false
	}
	path := strings.ToLower(deadCodeEntityPath(result, entity))
	return strings.HasPrefix(path, "benches/") ||
		strings.Contains(path, "/benches/") ||
		strings.HasPrefix(path, "examples/") ||
		strings.Contains(path, "/examples/")
}
