// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "fmt"

// validatePRPlan checks the JSON contract before it can be consumed as a
// successful schedule. An unsupported schema or absent result is an error.
func validatePRPlan(plan prPlan) error {
	if plan.SchemaVersion != 1 {
		return fmt.Errorf("unsupported PR plan schema %d", plan.SchemaVersion)
	}
	if !commitSHAPattern.MatchString(plan.HeadSHA) {
		return fmt.Errorf("PR plan has no exact head SHA")
	}
	if plan.Jobs == nil || plan.Workflows == nil || plan.Gates == nil {
		return fmt.Errorf("PR plan has missing jobs or workflows output")
	}
	if plan.Reason == "" {
		return fmt.Errorf("PR plan has no reason")
	}
	switch plan.Mode {
	case "reuse":
		if !commitSHAPattern.MatchString(plan.SourceSHA) || !commitSHAPattern.MatchString(plan.Tree) || len(plan.Workflows) != 0 || len(plan.Jobs) != 0 || len(plan.Gates) != 0 {
			return fmt.Errorf("reuse plan lacks source proof or schedules leaf jobs")
		}
	case "smoke":
		if !commitSHAPattern.MatchString(plan.SourceSHA) || !commitSHAPattern.MatchString(plan.Tree) {
			return fmt.Errorf("smoke plan lacks source proof")
		}
		fallthrough
	case "full":
		if plan.Mode == "full" && (plan.SourceSHA != "" || plan.Tree != "") {
			return fmt.Errorf("full plan cannot claim transferred provenance")
		}
		if len(plan.Workflows) == 0 || len(plan.Jobs) == 0 {
			return fmt.Errorf("%s plan selects no workflow", plan.Mode)
		}
	default:
		return fmt.Errorf("unsupported PR plan mode %q", plan.Mode)
	}
	seen := make(map[string]bool)
	for _, workflow := range plan.Workflows {
		if seen[workflow] || len(plan.Jobs[workflow]) == 0 {
			return fmt.Errorf("PR plan workflow %q is duplicate or has no jobs", workflow)
		}
		seen[workflow] = true
	}
	if len(seen) != len(plan.Jobs) {
		return fmt.Errorf("PR plan jobs and workflows disagree")
	}
	return nil
}
