// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "strings"

type fluxTargetAttributionTally struct {
	Ambiguous   int
	Linked      int
	Missing     int
	Saturated   int
	Unsupported int
}

func (t fluxTargetAttributionTally) asMap() map[string]any {
	return map[string]any{
		"ambiguous":   t.Ambiguous,
		"linked":      t.Linked,
		"missing":     t.Missing,
		"saturated":   t.Saturated,
		"unsupported": t.Unsupported,
	}
}

func bindFluxControllersToCrossRepoTargets(
	controllers []map[string]any,
	deploymentSources []map[string]any,
) fluxTargetAttributionTally {
	tally := fluxTargetAttributionTally{}
	for _, controller := range controllers {
		if !isFluxController(controller) {
			continue
		}
		if StringVal(controller, "source_ref_kind") != "GitRepository" {
			tally.Unsupported++
			continue
		}
		if strings.TrimSpace(StringVal(controller, "source_ref_name")) == "" {
			tally.Missing++
			continue
		}
		if fluxTargetBindingsSaturated(deploymentSources) {
			tally.Saturated++
			continue
		}

		targets := matchingFluxControllerTargetRepoIDs(controller, deploymentSources)
		switch len(targets) {
		case 0:
			tally.Missing++
		case 1:
			controller["flux_target_repo_id"] = targets[0]
			tally.Linked++
		default:
			tally.Ambiguous++
		}
	}
	return tally
}

func isFluxController(controller map[string]any) bool {
	switch StringVal(controller, "controller_kind") {
	case "flux_kustomization", "flux_helm_release":
		return true
	default:
		return false
	}
}

func fluxTargetBindingsSaturated(deploymentSources []map[string]any) bool {
	for _, source := range deploymentSources {
		if BoolVal(source, "flux_target_bindings_saturated") {
			return true
		}
	}
	return false
}

func matchingFluxControllerTargetRepoIDs(controller map[string]any, deploymentSources []map[string]any) []string {
	controllerRepoID := StringVal(controller, "repo_id")
	sourceRefName := StringVal(controller, "source_ref_name")
	targets := make(map[string]struct{})
	for _, source := range deploymentSources {
		if StringVal(source, "relationship_type") != "DEPLOYS_FROM" ||
			StringVal(source, "source_id") != controllerRepoID {
			continue
		}
		for _, name := range StringSliceVal(source, "flux_git_repository_names") {
			if name != sourceRefName {
				continue
			}
			targetRepoID := StringVal(source, "target_id")
			if targetRepoID != "" && targetRepoID != controllerRepoID {
				targets[targetRepoID] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(targets))
	for targetRepoID := range targets {
		result = append(result, targetRepoID)
	}
	return result
}
