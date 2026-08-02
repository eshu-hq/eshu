// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// RequiredGate is one deduplicated GitHub Actions job that must succeed for
// the changed paths. GateIDs records every blocking registry entry represented
// by the workflow/job pair.
type RequiredGate struct {
	Workflow   string
	Job        string
	CheckNames []string
	GateIDs    []string
}

// RequiredGates returns every path-selected blocking CI gate, deduplicated by
// workflow and job in registry order. Unlike local Select, it deliberately
// includes CI-only, ci-heavy, and manual-tier entries: Blocking describes merge
// enforcement, not whether a gate can run in a local preflight.
func (r *Registry) RequiredGates(changed []string) ([]RequiredGate, error) {
	required := make([]RequiredGate, 0)
	indexes := make(map[string]int)
	for _, gate := range r.Gates {
		if !gate.Blocking || !gateMatchesAnyPath(gate, changed) {
			continue
		}
		if gate.CI.Workflow == "" || gate.CI.Job == "" {
			return nil, fmt.Errorf(
				"blocking gate %q matches the changed paths but has no ci.workflow/ci.job reachability",
				gate.ID,
			)
		}
		key := gate.CI.Workflow + "\x00" + gate.CI.Job
		if index, ok := indexes[key]; ok {
			if !slices.Equal(required[index].CheckNames, gate.CI.CheckNames) {
				return nil, fmt.Errorf(
					"blocking gates %q and %q share ci workflow/job %q/%q but declare different check_names",
					required[index].GateIDs[0],
					gate.ID,
					gate.CI.Workflow,
					gate.CI.Job,
				)
			}
			required[index].GateIDs = append(required[index].GateIDs, gate.ID)
			continue
		}
		indexes[key] = len(required)
		required = append(required, RequiredGate{
			Workflow:   gate.CI.Workflow,
			Job:        gate.CI.Job,
			CheckNames: append([]string(nil), gate.CI.CheckNames...),
			GateIDs:    []string{gate.ID},
		})
	}
	return required, nil
}

func gateMatchesAnyPath(gate Gate, changed []string) bool {
	for _, trigger := range gate.Triggers {
		for _, path := range changed {
			if MatchGlob(trigger, path) {
				return true
			}
		}
	}
	return false
}

// ValidateRequiredStatusChecks verifies the repository-owned required-context
// manifest and the structural reachability of every blocking registry gate.
// An absent manifest remains valid for small third-party or hermetic registries;
// once a manifest is declared, exactly one context must aggregate blockers.
func (r *Registry) ValidateRequiredStatusChecks() []error {
	if len(r.RequiredStatusChecks) == 0 {
		return nil
	}

	var errs []error
	seen := make(map[string]struct{}, len(r.RequiredStatusChecks))
	aggregators := 0
	for _, check := range r.RequiredStatusChecks {
		if strings.TrimSpace(check.Context) == "" {
			errs = append(errs, fmt.Errorf("required status context must not be blank"))
		}
		if _, duplicate := seen[check.Context]; duplicate {
			errs = append(errs, fmt.Errorf("required status context %q is duplicated", check.Context))
		}
		seen[check.Context] = struct{}{}
		if strings.TrimSpace(check.Workflow) == "" || strings.TrimSpace(check.Job) == "" {
			errs = append(errs, fmt.Errorf(
				"required status context %q must declare workflow and job",
				check.Context,
			))
		}
		if check.Workflow != "" && filepath.Base(check.Workflow) != check.Workflow {
			errs = append(errs, fmt.Errorf(
				"required status context %q workflow %q must be a filename under .github/workflows",
				check.Context,
				check.Workflow,
			))
		}
		if check.IntegrationID <= 0 {
			errs = append(errs, fmt.Errorf(
				"required status context %q must declare a positive integration_id",
				check.Context,
			))
		}
		if check.AggregatesBlockingGates {
			aggregators++
			if strings.TrimSpace(check.SourceWorkflow) == "" {
				errs = append(errs, fmt.Errorf(
					"required status context %q aggregates blocking gates but has no source_workflow",
					check.Context,
				))
			}
		} else if strings.TrimSpace(check.SourceWorkflow) != "" {
			errs = append(errs, fmt.Errorf(
				"required status context %q does not aggregate blocking gates but declares source_workflow",
				check.Context,
			))
		}
	}
	if aggregators != 1 {
		errs = append(errs, fmt.Errorf(
			"required_status_checks must contain exactly one aggregates_blocking_gates entry; got %d",
			aggregators,
		))
	}
	for _, gate := range r.Gates {
		if gate.Blocking && (gate.CI.Workflow == "" || gate.CI.Job == "") {
			errs = append(errs, fmt.Errorf(
				"blocking gate %q has no ci.workflow/ci.job and cannot reach a required status context",
				gate.ID,
			))
		}
	}
	return errs
}
