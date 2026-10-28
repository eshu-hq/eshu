// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/content"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/projector/decode"
)

// BuildContentRecord converts a file-observed fact into the content record the
// content store persists. The second result is false when the fact carries no
// content_path, or carries neither a content_body nor a content_digest, so a
// fact that only describes a path is not mistaken for content. A tombstone fact
// produces a record marked Deleted rather than being skipped. The body retains
// its literal whitespace and newlines; digest parsing retains its existing rules.
func BuildContentRecord(fact facts.Envelope) (content.Record, bool) {
	path, ok := decode.PayloadString(fact.Payload, "content_path")
	if !ok {
		return content.Record{}, false
	}
	if !decode.PayloadHasKey(fact.Payload, "content_body") && !decode.PayloadHasKey(fact.Payload, "content_digest") {
		return content.Record{}, false
	}

	body, _ := decode.PayloadRawText(fact.Payload, "content_body")
	digest, _ := decode.PayloadString(fact.Payload, "content_digest")
	var installNames []string
	switch values := fact.Payload["dockerfile_install_names"].(type) {
	case []string:
		installNames = append([]string(nil), values...)
	case []any:
		for _, value := range values {
			if name, ok := value.(string); ok {
				installNames = append(installNames, name)
			}
		}
	}

	return content.Record{
		Path:         path,
		Body:         body,
		Digest:       digest,
		Deleted:      fact.IsTombstone,
		Metadata:     decode.PayloadAttributes(fact.Payload, "content_path", "content_body", "content_digest", "dockerfile_install_names"),
		InstallNames: installNames,
	}, true
}

// BuildContentEntityRecord converts a parsed-entity fact into the entity record
// the content store persists, resolving the entity's path from content_path,
// relative_path, or path in that order. The second result is false when the fact
// carries none of them, because an entity record with no path cannot be
// addressed on read. SourceCache retains its literal whitespace and newlines.
func BuildContentEntityRecord(repoID string, fact facts.Envelope) (content.EntityRecord, bool) {
	relativePath, ok := decode.PayloadString(fact.Payload, "content_path")
	if !ok {
		relativePath, ok = decode.PayloadString(fact.Payload, "relative_path")
	}
	if !ok {
		relativePath, ok = decode.PayloadString(fact.Payload, "path")
	}
	if !ok {
		return content.EntityRecord{}, false
	}

	entityType, ok := decode.PayloadString(fact.Payload, "entity_kind")
	if !ok {
		entityType, ok = decode.PayloadString(fact.Payload, "entity_type")
	}
	if !ok {
		entityType, ok = decode.PayloadString(fact.Payload, "sql_entity_type")
	}
	if !ok {
		entityType = fact.FactKind
	}
	if strings.TrimSpace(entityType) == "" {
		return content.EntityRecord{}, false
	}

	entityName, ok := decode.PayloadString(fact.Payload, "entity_name")
	if !ok {
		entityName, ok = decode.PayloadString(fact.Payload, "name")
	}
	if !ok {
		return content.EntityRecord{}, false
	}

	startLine, ok := decode.PayloadInt(fact.Payload, "start_line")
	if !ok {
		startLine, ok = decode.PayloadInt(fact.Payload, "line_number")
	}
	if !ok || startLine <= 0 {
		startLine = 1
	}

	endLine, ok := decode.PayloadInt(fact.Payload, "end_line")
	if !ok || endLine < startLine {
		endLine = startLine
	}

	startByte := decode.PayloadIntPtr(fact.Payload, "start_byte")
	endByte := decode.PayloadIntPtr(fact.Payload, "end_byte")
	language, _ := decode.PayloadString(fact.Payload, "language")
	if language == "" {
		language, _ = decode.PayloadString(fact.Payload, "lang")
	}
	artifactType, _ := decode.PayloadString(fact.Payload, "artifact_type")
	templateDialect, _ := decode.PayloadString(fact.Payload, "template_dialect")
	iacRelevant := decode.PayloadBoolPtr(fact.Payload, "iac_relevant")
	// metadata is computed once and shared by the entity_id mint fallback
	// below and the record's Metadata field, so both agree on exactly the
	// same view of the payload's dependency-identity keys (section,
	// config_kind, package_manager, lockfile) that
	// content.CanonicalEntityIDWithMetadata gates on. This fallback only
	// fires for facts without a collector-minted entity_id (version skew,
	// replayed old cassettes, non-git producers) — precisely the path where
	// divergent minting between here and the shape.Materialize mint site
	// would silently corrupt identity, so the two MUST stay in lockstep.
	metadata := canonical.EntityMetadataFromPayload(fact.Payload)
	entityID, ok := decode.PayloadString(fact.Payload, "entity_id")
	if !ok {
		entityID = content.CanonicalEntityIDWithMetadata(repoID, relativePath, entityType, entityName, startLine, metadata)
	}
	sourceCache, _ := decode.PayloadRawText(fact.Payload, "source_cache")

	return content.EntityRecord{
		EntityID:        entityID,
		Path:            relativePath,
		EntityType:      entityType,
		EntityName:      entityName,
		StartLine:       startLine,
		EndLine:         endLine,
		StartByte:       startByte,
		EndByte:         endByte,
		Language:        language,
		ArtifactType:    artifactType,
		TemplateDialect: templateDialect,
		IACRelevant:     iacRelevant,
		SourceCache:     sourceCache,
		Metadata:        metadata,
		Deleted:         fact.IsTombstone,
	}, true
}
