// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// FluxTargetAttributionTally counts cross-repo Flux target attribution
// outcomes. Exported because the root live-backend proof test pins it; the
// home stays this package. See #6060.
type FluxTargetAttributionTally struct {
	Ambiguous   int
	Linked      int
	Missing     int
	Saturated   int
	Unsupported int
}

func (t FluxTargetAttributionTally) AsMap() map[string]any {
	return map[string]any{
		"ambiguous":   t.Ambiguous,
		"linked":      t.Linked,
		"missing":     t.Missing,
		"saturated":   t.Saturated,
		"unsupported": t.Unsupported,
	}
}

func BindFluxControllersToCrossRepoTargets(
	controllers []map[string]any,
	deploymentSources []map[string]any,
) FluxTargetAttributionTally {
	tally := FluxTargetAttributionTally{}
	for _, controller := range controllers {
		if !isFluxController(controller) {
			continue
		}
		if querycontract.StringVal(controller, "source_ref_kind") != "GitRepository" {
			tally.Unsupported++
			continue
		}
		if strings.TrimSpace(querycontract.StringVal(controller, "source_ref_name")) == "" || effectiveFluxSourceRefNamespace(controller) == "" {
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

func effectiveFluxSourceRefNamespace(controller map[string]any) string {
	if namespace := strings.TrimSpace(querycontract.StringVal(controller, "source_ref_namespace")); namespace != "" {
		return namespace
	}
	return strings.TrimSpace(querycontract.StringVal(controller, "namespace"))
}

func isFluxController(controller map[string]any) bool {
	switch querycontract.StringVal(controller, "controller_kind") {
	case "flux_kustomization", "flux_helm_release":
		return true
	default:
		return false
	}
}

func fluxTargetBindingsSaturated(deploymentSources []map[string]any) bool {
	for _, source := range deploymentSources {
		if querycontract.BoolVal(source, "flux_target_bindings_saturated") {
			return true
		}
	}
	return false
}

func matchingFluxControllerTargetRepoIDs(controller map[string]any, deploymentSources []map[string]any) []string {
	controllerRepoID := querycontract.StringVal(controller, "repo_id")
	sourceRefName := querycontract.StringVal(controller, "source_ref_name")
	sourceRefNamespace := effectiveFluxSourceRefNamespace(controller)
	targets := make(map[string]struct{})
	for _, source := range deploymentSources {
		if querycontract.StringVal(source, "relationship_type") != "DEPLOYS_FROM" ||
			querycontract.StringVal(source, "source_id") != controllerRepoID {
			continue
		}
		bindings, _ := source["flux_git_repository_bindings"].([]map[string]any)
		for _, binding := range bindings {
			if querycontract.StringVal(binding, "name") != sourceRefName || querycontract.StringVal(binding, "namespace") != sourceRefNamespace {
				continue
			}
			targetRepoID := querycontract.StringVal(source, "target_id")
			if targetRepoID != "" && targetRepoID != controllerRepoID {
				targets[targetRepoID] = struct{}{}
			}
		}
	}
	result := make([]string, 0, len(targets))
	for targetRepoID := range targets {
		result = append(result, targetRepoID)
	}
	sort.Strings(result)
	return result
}
