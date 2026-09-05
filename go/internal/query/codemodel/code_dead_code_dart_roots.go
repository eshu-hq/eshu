// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

var dartDeadCodeMetadataRootKinds = []string{
	"dart.main_function",
	"dart.constructor",
	"dart.override_method",
	"dart.flutter_widget_build",
	"dart.flutter_create_state",
	"dart.public_library_api",
}

// DeadCodeIsDartRoot reports whether the candidate is a Dart entrypoint root.
func DeadCodeIsDartRoot(result map[string]any, entity *querycontract.EntityContent, stats *DeadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "dart" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range dartDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
