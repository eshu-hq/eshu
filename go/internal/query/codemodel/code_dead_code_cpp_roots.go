// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

var cppDeadCodeMetadataRootKinds = []string{
	"cpp.main_function",
	"cpp.public_header_api",
	"cpp.virtual_method",
	"cpp.override_method",
	"cpp.callback_argument_target",
	"cpp.function_pointer_target",
	"cpp.node_addon_entrypoint",
}

// DeadCodeIsCPPRoot reports whether the candidate is a C++ entrypoint root.
func DeadCodeIsCPPRoot(result map[string]any, entity *querycontract.EntityContent, stats *DeadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "cpp" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range cppDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
