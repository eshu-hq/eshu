// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fallback

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func buildOutgoingArgoCDApplicationRelationships(entity querycontract.EntityContent) []map[string]any {
	relationships := make([]map[string]any, 0, 2)
	sourceRepos := querycontract.MetadataStringSlice(entity.Metadata, "source_repos")
	if len(sourceRepos) == 0 {
		if sourceRepo, ok := querycontract.MetadataNonEmptyString(entity.Metadata, "source_repo"); ok {
			sourceRepos = append(sourceRepos, sourceRepo)
		}
	}
	for _, sourceRepo := range sourceRepos {
		relationships = append(relationships, map[string]any{
			"type":        "DEPLOYS_FROM",
			"target_name": sourceRepo,
			"reason":      "argocd_application_source",
		})
	}
	if destination, ok := querycontract.MetadataNonEmptyString(entity.Metadata, "dest_server"); ok {
		relationships = append(relationships, map[string]any{
			"type":        "RUNS_ON",
			"target_name": destination,
			"reason":      "argocd_destination_server",
		})
	}
	return relationships
}

func buildOutgoingArgoCDApplicationSetRelationships(entity querycontract.EntityContent) []map[string]any {
	relationships := make([]map[string]any, 0, 3)
	for _, repoURL := range querycontract.MetadataStringSlice(entity.Metadata, "generator_source_repos") {
		relationships = append(relationships, map[string]any{
			"type":        "DISCOVERS_CONFIG_IN",
			"target_name": repoURL,
			"reason":      "argocd_applicationset_generator",
		})
	}
	for _, repoURL := range querycontract.MetadataStringSlice(entity.Metadata, "template_source_repos") {
		relationships = append(relationships, map[string]any{
			"type":        "DEPLOYS_FROM",
			"target_name": repoURL,
			"reason":      "argocd_applicationset_template",
		})
	}
	if destination, ok := querycontract.MetadataNonEmptyString(entity.Metadata, "dest_server"); ok {
		relationships = append(relationships, map[string]any{
			"type":        "RUNS_ON",
			"target_name": destination,
			"reason":      "argocd_destination_server",
		})
	}
	return relationships
}
