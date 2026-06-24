// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package envregistry

import (
	"fmt"
	"sort"
	"strings"
)

// RenderMarkdown renders the registry as the operator-facing reference document.
// The output is deterministic (entries sorted by subsystem then name) so it can
// be committed and checked for drift by a test.
func (r *Registry) RenderMarkdown() string {
	var b strings.Builder
	b.WriteString("# Environment Variable Reference\n\n")
	b.WriteString("<!-- Generated from go/internal/envregistry. Do not edit by hand; ")
	b.WriteString("regenerate with `ESHU_UPDATE_ENV_DOC=1 go test ./internal/envregistry ")
	b.WriteString("-run TestEnvRegistryReferenceDocUpToDate`. -->\n\n")
	b.WriteString("This reference is generated from the code-owned registry in ")
	b.WriteString("`go/internal/envregistry`. It covers the core platform subsystems. ")
	b.WriteString("Run `eshu config validate` to check the current environment against it.\n\n")

	bySubsystem := map[string][]Entry{}
	for _, e := range r.Entries() {
		bySubsystem[e.Subsystem] = append(bySubsystem[e.Subsystem], e)
	}
	subsystems := make([]string, 0, len(bySubsystem))
	for s := range bySubsystem {
		subsystems = append(subsystems, s)
	}
	sort.Strings(subsystems)

	for _, subsystem := range subsystems {
		fmt.Fprintf(&b, "## %s\n\n", subsystem)
		b.WriteString("| Variable | Type | Default | Notes |\n")
		b.WriteString("| --- | --- | --- | --- |\n")
		for _, e := range bySubsystem[subsystem] {
			fmt.Fprintf(&b, "| `%s` | %s | %s | %s |\n",
				e.Name, e.Type, defaultCell(e), notesCell(e))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func defaultCell(e Entry) string {
	if e.Default == "" {
		return "—"
	}
	return "`" + e.Default + "`"
}

func notesCell(e Entry) string {
	parts := []string{e.Description}
	if e.Type == VarEnum && len(e.Allowed) > 0 {
		parts = append(parts, "Allowed: "+strings.Join(backtickEach(e.Allowed), ", ")+".")
	}
	if len(e.Aliases) > 0 {
		parts = append(parts, "Aliases: "+strings.Join(backtickEach(e.Aliases), ", ")+".")
	}
	if e.Deprecated {
		replacement := e.ReplacedBy
		if replacement == "" {
			replacement = "(see description)"
		} else {
			replacement = "`" + replacement + "`"
		}
		parts = append(parts, "Deprecated; use "+replacement+".")
	}
	return strings.Join(parts, " ")
}

func backtickEach(values []string) []string {
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = "`" + v + "`"
	}
	return out
}
