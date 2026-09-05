// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// RubyRailsControllerActionRootKind is the only guess-based dead-code root
// kind the #5376 reducer verdict can downgrade today. It split here from
// root code_dead_code_verdicts.go (#6060 lane A L1) with the ruby root
// reader that matches on it. It is exported because staying root tests
// build downgraded fixtures with it via the root forward.
const RubyRailsControllerActionRootKind = "ruby.rails_controller_action"

var rubyDeadCodeMetadataRootKinds = []string{
	"ruby.rails_controller_action",
	"ruby.rails_callback_method",
	"ruby.dynamic_dispatch_hook",
	"ruby.method_reference_target",
	"ruby.script_entrypoint",
}

// DeadCodeIsRubyRoot reports whether the candidate is a Ruby entrypoint root.
func DeadCodeIsRubyRoot(result map[string]any, entity *querycontract.EntityContent, stats *DeadCodePolicyStats, downgraded DeadCodeDowngradedRoots) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "ruby" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	entityID := strings.TrimSpace(querycontract.StringVal(result, "entity_id"))
	for _, rootKind := range rubyDeadCodeMetadataRootKinds {
		if !slices.Contains(rootKinds, rootKind) {
			continue
		}
		// #5376: the reducer's repo-wide verdict can downgrade the guess-based
		// ruby.rails_controller_action root when the controller's real base
		// resolves onward to a non-controller class. When downgraded for THIS
		// entity, that kind no longer keeps the action a root; any OTHER ruby
		// root kind on the same entity still keeps it. Absence of a downgraded
		// row keeps the root, exactly as before #5376.
		if rootKind == RubyRailsControllerActionRootKind && downgraded.IsDowngraded(entityID, rootKind) {
			continue
		}
		stats.ParserMetadataFrameworkRoots++
		return true
	}
	return false
}
