// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"slices"
)

// QueueSelection is the decision for one merge-group workflow or job. Jobs
// contains registry ci.job identities, including matrix parents; Gates lists
// contributing registry IDs in first-seen order across the registries.
type QueueSelection struct {
	Selected  bool     `json:"selected"`
	Jobs      []string `json:"jobs"`
	Gates     []string `json:"gates"`
	Truncated bool     `json:"truncated"`
}

// SelectQueueWorkflow matches changed paths against blocking and advisory CI
// rows in one workflow. The head and trusted default-branch registries are
// unioned so a pending registry edit cannot remove a gate from the publisher's
// expected set. A truncated compare or policy-file edit selects every row.
func SelectQueueWorkflow(registries []*Registry, paths []string, truncated bool, workflow, job string) (QueueSelection, error) {
	result := QueueSelection{Jobs: []string{}, Gates: []string{}, Truncated: truncated}
	if len(registries) == 0 || workflow == "" {
		return result, fmt.Errorf("a registry and workflow are required")
	}
	all := truncated || slices.Contains(paths, ".github/workflows/"+workflow) || slices.Contains(paths, "specs/ci-gates.v1.yaml")
	knownWorkflow, knownJob := false, job == ""
	seenJobs := make(map[string]struct{})
	seenGates := make(map[string]struct{})
	for _, reg := range registries {
		if reg == nil {
			return result, fmt.Errorf("nil gate registry")
		}
		for _, gate := range reg.Gates {
			if gate.CI.Workflow != workflow || gate.CI.Job == "" {
				continue
			}
			knownWorkflow = true
			if queueJobMatches(gate.CI, job) {
				knownJob = true
			}
		}
		var (
			required []RequiredGate
			err      error
		)
		if all {
			required, err = reg.AllBlockingGates()
		} else {
			required, err = reg.RequiredGates(paths)
		}
		if err != nil {
			return result, fmt.Errorf("select blocking gates: %w", err)
		}
		selectedBlocking := make(map[string]struct{})
		for _, gate := range required {
			for _, id := range gate.GateIDs {
				selectedBlocking[id] = struct{}{}
			}
		}
		for _, gate := range reg.Gates {
			if gate.CI.Workflow != workflow || gate.CI.Job == "" || !queueJobMatches(gate.CI, job) {
				continue
			}
			_, required := selectedBlocking[gate.ID]
			if required || (!gate.Blocking && (all || gateMatchesAnyPath(gate, paths))) {
				queueAddSelection(&result, gate.CI.Job, gate.ID, seenJobs, seenGates)
			}
		}
	}
	if !knownWorkflow {
		return result, fmt.Errorf("workflow %q has no CI gate owner in either registry", workflow)
	}
	if !knownJob {
		return result, fmt.Errorf("job %q has no CI gate owner in workflow %q", job, workflow)
	}
	result.Selected = len(result.Gates) > 0
	return result, nil
}

func queueJobMatches(ci CI, job string) bool {
	return job == "" || ci.Job == job || slices.Contains(ci.CheckNames, job)
}

func queueAddSelection(result *QueueSelection, job, id string, seenJobs, seenGates map[string]struct{}) {
	if _, exists := seenJobs[job]; !exists {
		seenJobs[job] = struct{}{}
		result.Jobs = append(result.Jobs, job)
	}
	if _, exists := seenGates[id]; !exists {
		seenGates[id] = struct{}{}
		result.Gates = append(result.Gates, id)
	}
}
