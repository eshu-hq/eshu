// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// PHPDeadCodeMetadataRootKinds lists the parser metadata root kinds for
// PHP. It is exported because the staying roots test iterates it via the
// root forward.
var PHPDeadCodeMetadataRootKinds = []string{
	"php.script_entrypoint",
	"php.constructor",
	"php.magic_method",
	"php.interface_method",
	"php.interface_implementation_method",
	"php.trait_method",
	"php.framework_controller_action",
	"php.route_handler",
	"php.symfony_route_attribute",
	"php.wordpress_hook_callback",
}

// DeadCodeIsPHPRoot reports whether the candidate is a PHP entrypoint root.
func DeadCodeIsPHPRoot(result map[string]any, entity *querycontract.EntityContent, stats *DeadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "php" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range PHPDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
