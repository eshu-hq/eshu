// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

// assistantPlatform identifies a supported assistant whose project-scoped
// instruction file Eshu can manage.
type assistantPlatform struct {
	// id is the stable lowercase selector used on the command line.
	id string
	// label is the human-facing platform name.
	label string
	// relPath is the project-relative instruction file Eshu writes into.
	relPath string
	// commit reports whether the target file is normally committed to the repo,
	// which controls whether install prints a `git add` hint for it.
	commit bool
}

// supportedPlatforms lists every assistant Eshu can install guidance for. The
// order is stable so status and install output is deterministic.
//
// File conventions (current as of this writing):
//   - Claude Code reads project instructions from CLAUDE.md at the repo root.
//   - Codex and other AGENTS.md-aware harnesses read AGENTS.md at the repo root.
//   - Cursor reads project rules from .cursor/rules/*.mdc files.
//
// CLAUDE.md and AGENTS.md are commit-worthy. The Cursor rule file is also
// commit-worthy so teammates share the same guidance.
func supportedPlatforms() []assistantPlatform {
	return []assistantPlatform{
		{id: "claude", label: "Claude Code", relPath: "CLAUDE.md", commit: true},
		{id: "codex", label: "Codex / AGENTS.md", relPath: "AGENTS.md", commit: true},
		{id: "cursor", label: "Cursor", relPath: ".cursor/rules/eshu.mdc", commit: true},
	}
}

// lookupPlatform returns the platform with the given id and whether it is
// supported. Unsupported ids let callers surface a clear error.
func lookupPlatform(id string) (assistantPlatform, bool) {
	for _, p := range supportedPlatforms() {
		if p.id == id {
			return p, true
		}
	}
	return assistantPlatform{}, false
}

// guidanceBody returns the managed-block body for a platform. The body is shared
// across platforms today; the parameter exists so platform-specific framing
// (for example Cursor MDC front matter) can be added without changing callers.
//
// The body deliberately tells assistants to prefer bounded Eshu MCP/API tools
// before broad raw-file search for graph-backed questions, gives concrete first
// prompts, and warns about Eshu truth labels, freshness, and missing evidence.
func guidanceBody(p assistantPlatform) string {
	if p.id == "cursor" {
		// Cursor MDC rules carry YAML front matter that controls when the rule
		// applies. `alwaysApply: true` makes the guidance available in every
		// request without a glob trigger.
		return cursorFrontMatter + "\n" + sharedGuidanceBody
	}
	return sharedGuidanceBody
}

// cursorFrontMatter is the YAML header prepended to the Cursor MDC rule so the
// rule is always in scope.
const cursorFrontMatter = `---
description: Use Eshu's code-to-cloud context graph before raw-file search.
alwaysApply: true
---`

// sharedGuidanceBody is the platform-neutral guidance text. Keep it bounded:
// assistants pay for these tokens on every request, so it states the rules
// crisply and links to the canonical docs instead of restating them.
const sharedGuidanceBody = `## Eshu: code-to-cloud context graph

This project is indexed by Eshu. Eshu answers graph-backed questions about code,
deployments, infrastructure, and documentation through bounded MCP/API tools.

### Prefer Eshu tools before raw-file search

For any graph-backed question (who calls X, what deploys Y, what breaks if I
change Z, how a service is wired, where a symbol lives), call the bounded Eshu
tool FIRST. Only fall back to broad raw-file search or full-repo grep when an
Eshu tool cannot answer.

- Explain a repo or service: ` + "`get_repo_story`" + `, ` + "`get_service_story`" + `, ` + "`investigate_service`" + `.
- Deployment and infra: ` + "`trace_deployment_chain`" + `, ` + "`investigate_resource`" + `.
- Change risk: ` + "`investigate_change_surface`" + `.
- Exact code: ` + "`find_symbol`" + `, ` + "`find_code`" + `, ` + "`get_code_relationship_story`" + `.
- Use raw Cypher only for diagnostics after named tools cannot answer.

### Keep calls bounded

Pass the narrowest known ` + "`repo_id`" + `, service, environment, resource, file, or
symbol. Use ` + "`limit`" + `/` + "`offset`" + `/cursors for lists and check ` + "`truncated`" + `,
` + "`next_offset`" + `, or ` + "`next_cursor`" + ` before claiming a complete result.

### First prompts to try

- "Build the service story for <service> and cite source, manifest, and runtime evidence."
- "Who calls <function> across indexed repos?"
- "Trace the deployment chain for <service> in <environment>."
- "What breaks if I change <file-or-service>? Show direct impact first."

### Respect Eshu truth labels

Every Eshu result carries a truth label. Honor it before you state a conclusion:

- Read ` + "`truth.level`" + ` (exact, derived, fallback). Do not present derived or
  fallback results as exact.
- Read ` + "`truth.freshness.state`" + ` (fresh, stale, building, unavailable). Flag
  stale or building evidence instead of treating it as current.
- When evidence is missing, say so. Do not invent edges, owners, or deployments
  that the graph did not return.

Docs: MCP Guide, Starter Prompts, and Truth Label Protocol in the Eshu docs.`
