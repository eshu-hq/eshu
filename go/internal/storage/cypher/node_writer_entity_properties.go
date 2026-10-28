// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

func canonicalEntityProperties(
	entity canonical.EntityRow,
	scopeID string,
	generationID string,
) map[string]any {
	properties := map[string]any{
		"id":              entity.EntityID,
		"name":            entity.EntityName,
		"path":            entity.FilePath,
		"relative_path":   entity.RelativePath,
		"line_number":     entity.StartLine,
		"start_line":      entity.StartLine,
		"end_line":        entity.EndLine,
		"repo_id":         entity.RepoID,
		"language":        entity.Language,
		"lang":            entity.Language,
		"scope_id":        scopeID,
		"generation_id":   generationID,
		"evidence_source": "projector/canonical",
	}
	row := map[string]any{
		"entity_metadata": entity.Metadata,
		"language":        entity.Language,
		"label":           entity.Label,
	}
	if metadata := node.EntityMetadataProperties(row); len(metadata) > 0 {
		for key, value := range metadata {
			properties[key] = value
		}
	}
	if entity.Label == "DockerfileInstall" {
		// SET n += row.props removes stale optional values when their new value
		// is nil. Without the explicit key, a later unpinned declaration keeps
		// the previous generation's version on the same uid.
		if _, ok := properties["version"]; !ok {
			properties["version"] = nil
		}
	}
	if metadata := node.TypeScriptClassFamilyMetadata(row); len(metadata) > 0 {
		for key, value := range metadata {
			properties[key] = value
		}
	}
	if entity.CyclomaticComplexity > 0 {
		properties["cyclomatic_complexity"] = entity.CyclomaticComplexity
	}

	return properties
}
