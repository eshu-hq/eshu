// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"fmt"
	"sort"
	"strings"
)

// cloudRuntimeDriftFindingViews projects reducer rows into the bounded wire shape.
// Each view applies the shared safety gate so an unsafe finding is reported as
// rejected with a refused action rather than dropped, and the provider-neutral
// source state is resolved through the same taxonomy the AWS surface uses.
func cloudRuntimeDriftFindingViews(rows []MultiCloudRuntimeDriftFindingRow) []CloudRuntimeDriftFindingView {
	views := make([]CloudRuntimeDriftFindingView, 0, len(rows))
	for _, row := range rows {
		status := strings.TrimSpace(row.ManagementStatus)
		gate := iacManagementSafetyGate(status, row.WarningFlags, nil)
		views = append(views, CloudRuntimeDriftFindingView{
			FactID:                       row.FactID,
			Provider:                     strings.TrimSpace(row.Provider),
			ScopeID:                      row.ScopeID,
			GenerationID:                 row.GenerationID,
			SourceSystem:                 row.SourceSystem,
			CloudResourceUID:             row.CloudResourceUID,
			FindingKind:                  strings.TrimSpace(row.FindingKind),
			ManagementStatus:             status,
			Confidence:                   row.Confidence,
			SourceState:                  string(ResolveReplatformingSourceState(status, gate.ReviewRequired)),
			MatchedTerraformStateAddress: row.MatchedTerraformStateAddress,
			MissingEvidence:              row.MissingEvidence,
			RecommendedAction:            row.RecommendedAction,
			DriftedAttributes:            row.DriftedAttributes,
			MatchedTerraformConfigFile:   row.MatchedTerraformConfigFile,
			MatchedTerraformModulePath:   row.MatchedTerraformModulePath,
			MatchedOtherIaCSource:        row.MatchedOtherIaCSource,
			ServiceCandidates:            row.ServiceCandidates,
			EnvironmentCandidates:        row.EnvironmentCandidates,
			DependencyPaths:              row.DependencyPaths,
			SafetyGate:                   gate,
		})
	}
	return views
}

// cloudRuntimeDriftSourceStateGroup counts findings sharing one provider-neutral
// source state for a quick rollup, with canonical uids attached for drilldown.
type cloudRuntimeDriftSourceStateGroup struct {
	SourceState       string   `json:"source_state"`
	Count             int      `json:"count"`
	CloudResourceUIDs []string `json:"cloud_resource_uids,omitempty"`
}

// cloudRuntimeDriftSourceStateGroups rolls views up by provider-neutral source
// state in canonical order so callers can summarize refusal posture cheaply.
func cloudRuntimeDriftSourceStateGroups(views []CloudRuntimeDriftFindingView) []cloudRuntimeDriftSourceStateGroup {
	byState := map[string]*cloudRuntimeDriftSourceStateGroup{}
	var states []string
	for _, view := range views {
		group := byState[view.SourceState]
		if group == nil {
			group = &cloudRuntimeDriftSourceStateGroup{SourceState: view.SourceState}
			byState[view.SourceState] = group
			states = append(states, view.SourceState)
		}
		group.Count++
		group.CloudResourceUIDs = append(group.CloudResourceUIDs, view.CloudResourceUID)
	}
	sort.Strings(states)
	out := make([]cloudRuntimeDriftSourceStateGroup, 0, len(states))
	for _, state := range states {
		group := byState[state]
		sort.Strings(group.CloudResourceUIDs)
		out = append(out, *group)
	}
	return out
}

func cloudRuntimeDriftStory(
	filter MultiCloudRuntimeDriftFilter,
	views []CloudRuntimeDriftFindingView,
	total int,
) string {
	scope := filter.ScopeID
	if filter.Provider != "" {
		scope = filter.Provider + " resources in " + scope
	}
	return fmt.Sprintf(
		"%d active multi-cloud runtime drift findings matched %s; %d returned in this page.",
		total,
		scope,
		len(views),
	)
}

func cloudRuntimeDriftTruncated(offset, pageLen, total int) bool {
	return offset+pageLen < total
}

func cloudRuntimeDriftNextOffset(offset, pageLen, total int) int {
	if offset+pageLen < total {
		return offset + pageLen
	}
	return 0
}
