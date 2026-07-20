// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"slices"
	"strings"
)

var rubyDeadCodeMetadataRootKinds = []string{
	"ruby.rails_controller_action",
	"ruby.rails_callback_method",
	"ruby.dynamic_dispatch_hook",
	"ruby.method_reference_target",
	"ruby.script_entrypoint",
}

func deadCodeIsRubyRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats, downgraded deadCodeDowngradedRoots) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "ruby" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	entityID := strings.TrimSpace(StringVal(result, "entity_id"))
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
		if rootKind == rubyRailsControllerActionRootKind && downgraded.isDowngraded(entityID, rootKind) {
			continue
		}
		stats.ParserMetadataFrameworkRoots++
		return true
	}
	return false
}
