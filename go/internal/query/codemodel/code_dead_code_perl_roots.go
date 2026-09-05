// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

var perlDeadCodeMetadataRootKinds = []string{
	"perl.script_entrypoint",
	"perl.package_namespace",
	"perl.exported_subroutine",
	"perl.constructor",
	"perl.special_block",
	"perl.autoload_subroutine",
	"perl.destroy_subroutine",
}

// DeadCodeIsPerlRoot reports whether the candidate is a Perl entrypoint root.
func DeadCodeIsPerlRoot(result map[string]any, entity *querycontract.EntityContent, stats *DeadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "perl" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range perlDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
