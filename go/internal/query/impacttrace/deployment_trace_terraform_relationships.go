// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package impacttrace

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	artifacts "github.com/eshu-hq/eshu/go/internal/query/repositoryartifacts"
)

// This file hosts the outgoing-terraform-relationship builder behind
// collectProvisioningChainEvidence (Issue #6060, lane B B4). It moved here
// from the query root (content_relationships_terraform.go) with the
// deployment-trace enrichment entry points because the service family
// consumes them through that chain and a handler-family subpackage cannot
// import the query root without an import cycle. The staying content
// relationship path (content_relationships.go) repoints to the exported
// home. Bodies are unchanged modulo package qualifiers.

type contentRelationshipSpec struct {
	relationshipType string
	targetName       string
	reason           string
}

// BuildOutgoingTerraformRelationships builds the outgoing content
// relationships for terraform-shaped entities. Pinned by the staying
// content relationship path (content_relationships.go) and
// collectProvisioningChainEvidence in this package.
func BuildOutgoingTerraformRelationships(entity querycontract.EntityContent) ([]map[string]any, bool, error) {
	switch entity.EntityType {
	case "TerraformModule":
		if source, ok := querycontract.MetadataNonEmptyString(entity.Metadata, "source"); ok {
			if normalized := artifacts.NormalizeConfigArtifactExpression(source, nil); normalized != "" {
				source = normalized
			}
			return []map[string]any{
				{
					"type":        "USES_MODULE",
					"target_name": source,
					"reason":      "terraform_module_source",
				},
			}, true, nil
		}
		return nil, true, nil
	case "TerragruntConfig":
		relationships := make([]map[string]any, 0, 8)
		seen := make(map[string]struct{}, 8)
		add := func(spec contentRelationshipSpec) {
			if spec.relationshipType == "" || spec.targetName == "" || spec.reason == "" {
				return
			}
			key := spec.relationshipType + "|" + spec.targetName + "|" + spec.reason
			if _, ok := seen[key]; ok {
				return
			}
			seen[key] = struct{}{}
			relationships = append(relationships, map[string]any{
				"type":        spec.relationshipType,
				"target_name": spec.targetName,
				"reason":      spec.reason,
			})
		}
		if source, ok := querycontract.MetadataNonEmptyString(entity.Metadata, "terraform_source"); ok {
			if normalized := artifacts.NormalizeConfigArtifactExpression(source, nil); normalized != "" {
				source = normalized
			}
			add(contentRelationshipSpec{
				relationshipType: "USES_MODULE",
				targetName:       source,
				reason:           "terragrunt_terraform_source",
			})
		}
		for _, includePath := range querycontract.MetadataStringSlice(entity.Metadata, "include_paths") {
			includePath = artifacts.NormalizeConfigArtifactExpression(includePath, nil)
			if includePath == "" {
				continue
			}
			add(contentRelationshipSpec{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       includePath,
				reason:           "terragrunt_include_path",
			})
		}
		for _, configPath := range querycontract.MetadataStringSlice(entity.Metadata, "read_config_paths") {
			configPath = artifacts.NormalizeConfigArtifactExpression(configPath, nil)
			if configPath == "" {
				continue
			}
			add(contentRelationshipSpec{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       configPath,
				reason:           "terragrunt_read_config",
			})
		}
		for _, configPath := range querycontract.MetadataStringSlice(entity.Metadata, "find_in_parent_folders_paths") {
			configPath = artifacts.NormalizeConfigArtifactExpression(configPath, nil)
			if configPath == "" {
				continue
			}
			add(contentRelationshipSpec{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       configPath,
				reason:           "terragrunt_find_in_parent_folders",
			})
		}
		for _, configPath := range querycontract.MetadataStringSlice(entity.Metadata, "local_config_asset_paths") {
			configPath = artifacts.NormalizeConfigArtifactExpression(configPath, nil)
			if configPath == "" {
				continue
			}
			add(contentRelationshipSpec{
				relationshipType: "DISCOVERS_CONFIG_IN",
				targetName:       configPath,
				reason:           "local_config_asset",
			})
		}
		return relationships, true, nil
	case "TerragruntDependency":
		if configPath, ok := querycontract.MetadataNonEmptyString(entity.Metadata, "config_path"); ok {
			configPath = artifacts.NormalizeConfigArtifactExpression(configPath, nil)
			if configPath == "" {
				return nil, true, nil
			}
			return []map[string]any{
				{
					"type":        "DISCOVERS_CONFIG_IN",
					"target_name": configPath,
					"reason":      "terragrunt_dependency_config_path",
				},
			}, true, nil
		}
		return nil, true, nil
	default:
		return nil, false, nil
	}
}
