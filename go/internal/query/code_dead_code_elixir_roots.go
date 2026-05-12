package query

import (
	"slices"
	"strings"
)

var elixirDeadCodeMetadataRootKinds = []string{
	"elixir.application_start",
	"elixir.main_function",
	"elixir.public_macro",
	"elixir.public_guard",
	"elixir.behaviour_callback",
	"elixir.genserver_callback",
	"elixir.supervisor_callback",
	"elixir.mix_task_run",
	"elixir.protocol_function",
	"elixir.protocol_implementation_function",
	"elixir.phoenix_controller_action",
	"elixir.phoenix_liveview_callback",
}

func deadCodeIsElixirRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "elixir" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range elixirDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
