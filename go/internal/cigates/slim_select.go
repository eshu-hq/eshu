// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates

import (
	"fmt"
	"slices"
	"strings"
)

// Merge-group slim lane (#7859 P3). On the merge_group event the selection
// layer serves an owner-fixed ordered set of blocking gates declared by
// merge_group_slim_gates in the registry, ignoring changed paths entirely.
// Pull-request selection (SelectQueueWorkflow, RequiredGates) never reads the
// slim set, so the lane cannot alter pull-request behavior.

// parseMergeGroupSlim validates the registry's merge_group slim set: every
// entry names a distinct existing blocking gate. A slim gate must block: the
// lane awaits every slim entry as a required gate, and a non-blocking entry
// would silently weaken the queue. seenIDs holds every gate ID Load accepted.
func parseMergeGroupSlim(path string, ids []string, seenIDs map[string]struct{}, reg *Registry) error {
	byID := make(map[string]Gate, len(reg.Gates))
	for _, gate := range reg.Gates {
		byID[gate.ID] = gate
	}
	seen := make(map[string]struct{}, len(ids))
	slim := make([]string, 0, len(ids))
	for i, raw := range ids {
		id := strings.TrimSpace(raw)
		if id == "" {
			return fmt.Errorf("ci-gates registry %s: merge_group_slim_gates[%d] has blank id", path, i)
		}
		if _, dup := seen[id]; dup {
			return fmt.Errorf("ci-gates registry %s: merge_group_slim_gates entry %q is duplicated", path, id)
		}
		seen[id] = struct{}{}
		if _, ok := seenIDs[id]; !ok {
			return fmt.Errorf("ci-gates registry %s: merge_group_slim_gates entry %q names no gate", path, id)
		}
		if gate := byID[id]; !gate.Blocking {
			return fmt.Errorf("ci-gates registry %s: merge_group_slim_gates entry %q is not blocking", path, id)
		}
		slim = append(slim, id)
	}
	reg.mergeGroupSlim = slim
	return nil
}

// SlimGates returns the registry's merge_group slim set as required gates,
// deduplicated by workflow and job in slim-set order. Like RequiredGates it
// groups gate IDs that share one workflow job, and gates that share a job
// must declare identical check_names: await waits on the job's check rows
// once, so divergent names would leave one gate's rows unwatched.
func (r *Registry) SlimGates() ([]RequiredGate, error) {
	byID := make(map[string]Gate, len(r.Gates))
	for _, gate := range r.Gates {
		byID[gate.ID] = gate
	}
	required := make([]RequiredGate, 0, len(r.mergeGroupSlim))
	indexes := make(map[string]int)
	for _, id := range r.mergeGroupSlim {
		gate, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("merge_group slim gate %q names no gate", id)
		}
		if !gate.Blocking {
			return nil, fmt.Errorf("merge_group slim gate %q is not blocking", id)
		}
		if gate.CI.Workflow == "" || gate.CI.Job == "" {
			return nil, fmt.Errorf(
				"merge_group slim gate %q has no ci.workflow/ci.job reachability",
				id,
			)
		}
		key := gate.CI.Workflow + "\x00" + gate.CI.Job
		if index, ok := indexes[key]; ok {
			if !slices.Equal(required[index].CheckNames, gate.CI.CheckNames) {
				return nil, fmt.Errorf(
					"merge_group slim gates %q and %q share ci workflow/job %q/%q but declare different check_names",
					required[index].GateIDs[0],
					id,
					gate.CI.Workflow,
					gate.CI.Job,
				)
			}
			required[index].GateIDs = append(required[index].GateIDs, id)
			continue
		}
		indexes[key] = len(required)
		required = append(required, RequiredGate{
			Workflow:   gate.CI.Workflow,
			Job:        gate.CI.Job,
			CheckNames: append([]string(nil), gate.CI.CheckNames...),
			GateIDs:    []string{id},
		})
	}
	return required, nil
}

// SelectSlimWorkflow matches one workflow against the union of the head and
// trusted default-branch registries' merge_group slim sets. Changed paths
// play no role: the fixed set is the selection. The registries are unioned
// so a pending registry edit cannot drop a gate the trusted default-branch
// publisher still requires, mirroring SelectQueueWorkflow's union.
func SelectSlimWorkflow(registries []*Registry, workflow, job string) (QueueSelection, error) {
	result := QueueSelection{Jobs: []string{}, Gates: []string{}}
	if len(registries) == 0 || workflow == "" {
		return result, fmt.Errorf("a registry and workflow are required")
	}
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
		slim, err := reg.SlimGates()
		if err != nil {
			return result, fmt.Errorf("select merge_group slim gates: %w", err)
		}
		slimIDs := make(map[string]struct{})
		for _, required := range slim {
			for _, id := range required.GateIDs {
				slimIDs[id] = struct{}{}
			}
		}
		byID := make(map[string]Gate, len(reg.Gates))
		for _, gate := range reg.Gates {
			byID[gate.ID] = gate
		}
		for _, id := range reg.MergeGroupSlim() {
			if _, ok := slimIDs[id]; !ok {
				continue
			}
			gate := byID[id]
			if gate.CI.Workflow != workflow || gate.CI.Job == "" || !queueJobMatches(gate.CI, job) {
				continue
			}
			queueAddSelection(&result, gate.CI.Job, gate.ID, seenJobs, seenGates)
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
