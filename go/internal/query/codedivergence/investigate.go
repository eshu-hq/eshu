// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codedivergence

import "fmt"

// MaxInvestigateSteps bounds the next_steps list: two calls per member
// (call chain + file range), so twelve members fill the budget and larger
// groups truncate with the flag set rather than flooding the client.
const MaxInvestigateSteps = 25

// NextStep is one bounded follow-up call with arguments filled in. Tool
// names the real MCP tools find_function_call_chain and get_file_lines; an
// MCP client executes them verbatim.
type NextStep struct {
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args"`
	Purpose string         `json:"purpose"`
}

// InvestigateSteps builds the follow-up calls for a finding's members: the
// call chain (callers and callees) and the file range (the diff input for
// any member pair) per member, repo-scoped and entity-addressed. Steps stop
// at MaxInvestigateSteps; truncated reports the cutoff.
func InvestigateSteps(repoID string, finding Finding) (steps []NextStep, truncated bool) {
	steps = make([]NextStep, 0, MaxInvestigateSteps)
	for _, member := range finding.Members {
		// Pairs stay whole: a member contributes both calls or neither,
		// so a truncated list never strands a call chain without its
		// file range.
		if len(steps)+2 > MaxInvestigateSteps {
			return steps, true
		}
		steps = append(steps,
			NextStep{
				Tool: "find_function_call_chain",
				Args: map[string]any{
					"start_entity_id": member.EntityID,
					"repo_id":         repoID,
				},
				Purpose: fmt.Sprintf("callers and callees of %s (%s)", member.EntityName, member.EntityID),
			},
			NextStep{
				Tool: "get_file_lines",
				Args: map[string]any{
					"repo_id":       repoID,
					"relative_path": member.RelativePath,
					"start_line":    member.StartLine,
					"end_line":      member.EndLine,
				},
				Purpose: fmt.Sprintf("read %s body for diffing (%s:%d-%d)", member.EntityName, member.RelativePath, member.StartLine, member.EndLine),
			},
		)
	}
	return steps, false
}
