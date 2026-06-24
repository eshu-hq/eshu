// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// contentFactEnvelope builds the durable content fact for one snapshot file:
// body, digest, and the language/artifact/template metadata downstream content
// materialization reads. The stable key is repo+path scoped so re-emission of a
// generation is idempotent. Extracted from git_fact_builder.go to keep that file
// within the repo file-size budget.
func contentFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	fileSnapshot ContentFileSnapshot,
) facts.Envelope {
	payload := map[string]any{
		"content_path":   fileSnapshot.RelativePath,
		"content_body":   fileSnapshot.Body,
		"content_digest": fileSnapshot.Digest,
		"repo_id":        repoID,
	}
	if fileSnapshot.Language != "" {
		payload["language"] = fileSnapshot.Language
	}
	if fileSnapshot.CommitSHA != "" {
		payload["commit_sha"] = fileSnapshot.CommitSHA
	}
	if fileSnapshot.ArtifactType != "" {
		payload["artifact_type"] = fileSnapshot.ArtifactType
	}
	if fileSnapshot.TemplateDialect != "" {
		payload["template_dialect"] = fileSnapshot.TemplateDialect
	}
	if fileSnapshot.IACRelevant != nil {
		payload["iac_relevant"] = strings.ToLower(fmt.Sprintf("%t", *fileSnapshot.IACRelevant))
	}

	return factEnvelope(
		"content",
		scopeID,
		generationID,
		observedAt,
		"content:"+repoID+":"+fileSnapshot.RelativePath,
		payload,
		filepath.Join(repoPath, filepath.FromSlash(fileSnapshot.RelativePath)),
	)
}

// contentEntityFactEnvelope builds the durable content-entity fact for one parsed
// entity (function, class, etc.): its identity, location, language, and any
// extra parser metadata. The stable key is the entity uid so re-emission of a
// generation is idempotent. Extracted from git_fact_builder.go to keep that file
// within the repo file-size budget.
func contentEntityFactEnvelope(
	repoPath string,
	repoID string,
	scopeID string,
	generationID string,
	observedAt time.Time,
	entitySnapshot ContentEntitySnapshot,
) facts.Envelope {
	payload := map[string]any{
		"graph_id":      entitySnapshot.EntityID,
		"graph_kind":    "content_entity",
		"entity_id":     entitySnapshot.EntityID,
		"repo_id":       repoID,
		"relative_path": entitySnapshot.RelativePath,
		"entity_type":   entitySnapshot.EntityType,
		"entity_name":   entitySnapshot.EntityName,
		"start_line":    entitySnapshot.StartLine,
		"end_line":      entitySnapshot.EndLine,
		"language":      entitySnapshot.Language,
		"source_cache":  entitySnapshot.SourceCache,
		"indexed_at":    entitySnapshot.IndexedAt.UTC().Format(time.RFC3339Nano),
	}
	if entitySnapshot.StartByte != nil {
		payload["start_byte"] = *entitySnapshot.StartByte
	}
	if entitySnapshot.EndByte != nil {
		payload["end_byte"] = *entitySnapshot.EndByte
	}
	if entitySnapshot.ArtifactType != "" {
		payload["artifact_type"] = entitySnapshot.ArtifactType
	}
	if entitySnapshot.TemplateDialect != "" {
		payload["template_dialect"] = entitySnapshot.TemplateDialect
	}
	if entitySnapshot.IACRelevant != nil {
		payload["iac_relevant"] = *entitySnapshot.IACRelevant
	}
	if len(entitySnapshot.Metadata) > 0 {
		payload["entity_metadata"] = cloneAnyMap(entitySnapshot.Metadata)
	}

	return factEnvelope(
		"content_entity",
		scopeID,
		generationID,
		observedAt,
		"content_entity:"+entitySnapshot.EntityID,
		payload,
		filepath.Join(repoPath, filepath.FromSlash(entitySnapshot.RelativePath)),
	)
}
