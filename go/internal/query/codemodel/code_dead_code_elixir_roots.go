// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ElixirDeadCodeMetadataRootKinds lists the parser metadata root kinds
// for Elixir. It is exported because the staying roots test iterates it
// via the root forward.
var ElixirDeadCodeMetadataRootKinds = []string{
	"elixir.application_start",
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

// DeadCodeIsElixirRoot reports whether the candidate is an Elixir entrypoint root.
func DeadCodeIsElixirRoot(result map[string]any, entity *querycontract.EntityContent, stats *DeadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "elixir" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range ElixirDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
