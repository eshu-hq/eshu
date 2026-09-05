// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

var haskellDeadCodeMetadataRootKinds = []string{
	"haskell.main_function",
	"haskell.module_export",
	"haskell.exported_type",
	"haskell.typeclass_method",
	"haskell.instance_method",
}

// DeadCodeIsHaskellRoot reports whether the candidate is a Haskell entrypoint root.
func DeadCodeIsHaskellRoot(result map[string]any, entity *querycontract.EntityContent, stats *DeadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "haskell" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range haskellDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
