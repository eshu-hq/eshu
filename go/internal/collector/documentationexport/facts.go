// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package documentationexport

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/exportmanifestpreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const sourceFormat = "documentation_export"

func sourcePayload(decoded manifest, scopeID string, fileCount int) facts.DocumentationSourcePayload {
	scopeHash := safeFingerprint(decoded.SourceSystem + ":" + decoded.SourceScopeID)
	metadata := map[string]string{
		"source_system":     decoded.SourceSystem,
		"source_scope_hash": scopeHash,
		"exported_at":       decoded.ExportedAt,
		"acl_policy":        decoded.ACLPolicy,
		"file_count":        strconv.Itoa(fileCount),
		"runtime_status":    "default_off_parser_only",
	}
	addScopeKindMetadata(metadata, decoded.SourceScopeKind)
	if revisionHash := safeFingerprintIfPresent(decoded.SourceRevision); revisionHash != "" {
		metadata["source_revision"] = revisionHash
	}
	if cursorHash := safeFingerprintIfPresent(decoded.SourceCursor); cursorHash != "" {
		metadata["source_cursor"] = cursorHash
	}
	return facts.DocumentationSourcePayload{
		SourceID:       scopeID,
		SourceSystem:   decoded.SourceSystem,
		ExternalID:     sourceFormat + ":" + scopeHash,
		DisplayName:    decoded.SourceSystem + " offline export",
		SourceType:     "offline_export",
		ACLSummary:     aclSummary(decoded.ACLPolicy),
		SourceMetadata: metadata,
	}
}

func addScopeKindMetadata(metadata map[string]string, scopeKind string) {
	scopeKind = strings.ToLower(strings.TrimSpace(scopeKind))
	if scopeKind == "" {
		return
	}
	switch scopeKind {
	case "repository", "project", "channel", "team", "chat", "thread",
		"file", "file_set", "folder", "drive", "export", "workspace_export",
		"generic":
		metadata["source_scope_kind"] = scopeKind
	default:
		metadata["source_scope_kind_hash"] = safeFingerprint(scopeKind)
	}
}

func documentPayload(scopeID string, decoded manifest, file manifestFile, record exportRecord, warning string, rawRecord string) facts.DocumentationDocumentPayload {
	itemID := firstNonEmpty(file.SourceItemID, record.ID, file.Path)
	itemHash := safeFingerprint(decoded.SourceSystem + ":" + decoded.SourceScopeID + ":" + itemID)
	metadata := map[string]string{
		"source_system":     decoded.SourceSystem,
		"source_scope_hash": safeFingerprint(decoded.SourceSystem + ":" + decoded.SourceScopeID),
		"source_item_hash":  itemHash,
		"export_path_hash":  safeFingerprint(file.Path),
		"export_file_kind":  file.Kind,
		"import_mode":       "offline_export",
		"acl_policy":        decoded.ACLPolicy,
	}
	if file.Deleted || record.Deleted {
		metadata["source_deleted"] = "true"
	}
	if file.Edited || record.Edited {
		metadata["source_edited"] = "true"
	}
	if warning != "" {
		metadata["warning"] = warning
		metadata["metadata_only"] = "true"
	}
	title := firstNonEmpty(record.Title, decoded.SourceSystem+" export record")
	return facts.DocumentationDocumentPayload{
		SourceID:       scopeID,
		DocumentID:     "doc:" + sourceFormat + ":" + itemHash,
		ExternalID:     "export-item:" + itemHash,
		RevisionID:     revisionID(decoded, itemHash),
		CanonicalURI:   "redacted:source:" + safeFingerprint(firstNonEmpty(file.URL, file.Path)),
		Title:          title,
		DocumentType:   "offline_" + decoded.SourceSystem + "_export",
		Format:         sourceFormat,
		ACLSummary:     aclSummary(decoded.ACLPolicy),
		SourceMetadata: metadata,
		ContentHash:    safeFingerprint(rawRecord + warning),
	}
}

func sectionPayload(document facts.DocumentationDocumentPayload, sourceSystem string, section exportSection, index int) facts.DocumentationSectionPayload {
	sectionID := "export:" + strconv.Itoa(index+1)
	metadata := map[string]string{
		"source_system": sourceSystem,
		"source_ref":    safeFingerprint(section.ref),
	}
	if section.deleted {
		metadata["message_deleted"] = "true"
		metadata["metadata_only"] = "true"
	}
	if section.edited {
		metadata["message_edited"] = "true"
	}
	content, truncated := truncateSection(section.content)
	if truncated {
		metadata["section_truncated"] = "true"
	}
	if content == "" && section.deleted {
		metadata["content_redacted"] = "true"
	}
	return facts.DocumentationSectionPayload{
		DocumentID:       document.DocumentID,
		RevisionID:       document.RevisionID,
		SectionID:        sectionID,
		SectionAnchor:    sectionID,
		HeadingText:      firstNonEmpty(section.heading, "Export section"),
		OrdinalPath:      []int{index + 1},
		Content:          content,
		ContentFormat:    "text/plain",
		TextHash:         safeFingerprint(content),
		ExcerptHash:      safeFingerprint(content),
		SourceStartRef:   sectionID,
		SourceEndRef:     sectionID,
		SourceMetadata:   metadata,
		ContainsWarnings: section.deleted || truncated,
	}
}

func linkPayload(document facts.DocumentationDocumentPayload, link recordLink, index int) facts.DocumentationLinkPayload {
	target := firstNonEmpty(link.TargetURI, link.Target, link.URL)
	targetURI, metadata := safeTargetURI(target)
	if metadata == nil {
		metadata = map[string]string{}
	}
	metadata["source_ref"] = safeFingerprint(firstNonEmpty(link.ID, target))
	return facts.DocumentationLinkPayload{
		DocumentID:     document.DocumentID,
		RevisionID:     document.RevisionID,
		SectionID:      linkSectionID(link.SectionID),
		LinkID:         "link:" + strconv.Itoa(index+1),
		TargetURI:      targetURI,
		TargetKind:     "external",
		AnchorTextHash: safeFingerprint(link.Anchor),
		SourceMetadata: metadata,
	}
}

func linkSectionID(sectionID string) string {
	sectionID = strings.TrimSpace(sectionID)
	if strings.HasPrefix(sectionID, "export:") {
		return sectionID
	}
	return "export:1"
}

func aclSummary(policy string) *facts.DocumentationACLSummary {
	summary := &facts.DocumentationACLSummary{Visibility: "unknown"}
	switch policy {
	case exportmanifestpreflight.ACLPolicyEvaluated:
		// The source ACL was evaluated before import: assert allowed. This is
		// the only documentation producer that observes a complete ACL
		// evaluation, so it is the only one that may report allowed.
		summary.PartialReason = ""
		summary.SourceACLState = facts.SourceACLStateAllowed
	case exportmanifestpreflight.ACLPolicyPartial:
		summary.IsPartial = true
		summary.SourceACLState = facts.SourceACLStatePartial
		summary.PartialReason = "acl_partial"
	default:
		// ACL evidence is unavailable: no access-posture signal was observed,
		// so the bounded state is omitted (absence means "no ACL claim").
		// Deciding a conservative default here is a disclosure policy call
		// reserved for security review and the reducer/query children.
		summary.IsPartial = true
		summary.PartialReason = "acl_unavailable"
	}
	return summary
}

func envelope(scopeID string, generationID string, observedAt time.Time, kind string, key string, payload any, sourceSystem string, sourceURI string, sourceRecordID string) (facts.Envelope, error) {
	payloadMap, err := documentationPayloadMap(payload)
	if err != nil {
		return facts.Envelope{}, err
	}
	return facts.Envelope{
		FactID: facts.StableID("DocumentationExportFact", map[string]any{
			"fact_kind":     kind,
			"stable_key":    key,
			"scope_id":      scopeID,
			"generation_id": generationID,
		}),
		ScopeID:          scopeID,
		GenerationID:     generationID,
		FactKind:         kind,
		StableFactKey:    key,
		SchemaVersion:    schemaVersion(kind),
		CollectorKind:    string(scope.CollectorDocumentation),
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       observedAt.UTC(),
		Payload:          payloadMap,
		SourceRef: facts.Ref{
			SourceSystem:   sourceSystem,
			ScopeID:        scopeID,
			GenerationID:   generationID,
			FactKey:        key,
			SourceURI:      sourceURI,
			SourceRecordID: sourceRecordID,
		},
	}, nil
}

func documentationPayloadMap(payload any) (map[string]any, error) {
	switch value := payload.(type) {
	case facts.DocumentationSourcePayload:
		return facts.EncodeDocumentationSource(value)
	case facts.DocumentationDocumentPayload:
		return facts.EncodeDocumentationDocument(value)
	case facts.DocumentationSectionPayload:
		return facts.EncodeDocumentationSection(value)
	case facts.DocumentationLinkPayload:
		return facts.EncodeDocumentationLink(value)
	default:
		return nil, fmt.Errorf("unsupported documentation export payload type %T", payload)
	}
}

func schemaVersion(kind string) string {
	if kind == facts.DocumentationSectionFactKind {
		return facts.DocumentationSectionFactSchemaVersion
	}
	return facts.DocumentationFactSchemaVersion
}

func revisionID(decoded manifest, fallback string) string {
	return firstNonEmpty(
		safeFingerprintIfPresent(decoded.SourceRevision),
		safeFingerprintIfPresent(decoded.SourceCursor),
		safeFingerprintIfPresent(decoded.ExportedAt),
		fallback,
	)
}
