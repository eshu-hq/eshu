package googleworkspace

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

func documentPayload(
	scopeID string,
	file File,
	permission PermissionSummary,
	exportMIME string,
	failure FailureClass,
) facts.DocumentationDocumentPayload {
	metadata := map[string]string{
		"file_kind":    string(file.Kind),
		"file_id_hash": safeFingerprint(file.ID),
	}
	if exportMIME != "" {
		metadata["export_mime"] = exportMIME
	}
	if file.Hidden {
		metadata["hidden"] = "true"
	}
	if failure != "" {
		metadata["failure_class"] = string(failure)
	}
	if permission.IsPartial {
		metadata["acl_partial"] = "true"
	}
	return facts.DocumentationDocumentPayload{
		SourceID:       scopeID,
		DocumentID:     "doc:google_workspace:" + safeFingerprint(file.ID),
		ExternalID:     "gws-file:" + safeFingerprint(file.ID),
		RevisionID:     firstNonEmpty(file.RevisionID, safeFingerprint(file.ID)),
		CanonicalURI:   safeURI(file.WebURL),
		Title:          titleForKind(file.Kind),
		DocumentType:   "workspace_" + string(file.Kind),
		Format:         "google_workspace_export",
		ACLSummary:     aclSummary(permission),
		SourceMetadata: metadata,
		ContentHash:    safeFingerprint(file.ID + ":" + file.RevisionID),
	}
}

func sectionPayload(
	document facts.DocumentationDocumentPayload,
	file File,
	exportMIME string,
	section Section,
	index int,
) facts.DocumentationSectionPayload {
	sectionID := firstNonEmpty(cleanID(section.ID), strconv.Itoa(index+1))
	metadata := map[string]string{
		"file_id_hash": safeFingerprint(file.ID),
		"export_mime":  exportMIME,
	}
	for key, value := range section.Metadata {
		if strings.TrimSpace(key) != "" && strings.TrimSpace(value) != "" {
			metadata[key] = value
		}
	}
	if section.Hidden {
		metadata["hidden"] = "true"
		metadata["metadata_only"] = "true"
	}
	content := strings.TrimSpace(section.Content)
	return facts.DocumentationSectionPayload{
		DocumentID:       document.DocumentID,
		RevisionID:       document.RevisionID,
		SectionID:        "export:" + sectionID,
		SectionAnchor:    "export-" + sectionID,
		HeadingText:      firstNonEmpty(section.Heading, "Workspace export section"),
		OrdinalPath:      []int{index + 1},
		Content:          content,
		ContentFormat:    firstNonEmpty(section.ContentFormat, "text/plain"),
		TextHash:         safeFingerprint(content),
		ExcerptHash:      safeFingerprint(content),
		SourceStartRef:   "export:" + sectionID,
		SourceEndRef:     "export:" + sectionID,
		SourceMetadata:   metadata,
		ContainsWarnings: section.Hidden,
	}
}

func linkPayload(document facts.DocumentationDocumentPayload, link Link, index int) facts.DocumentationLinkPayload {
	linkID := firstNonEmpty(cleanID(link.ID), strconv.Itoa(index+1))
	return facts.DocumentationLinkPayload{
		DocumentID:     document.DocumentID,
		RevisionID:     document.RevisionID,
		SectionID:      "export:" + firstNonEmpty(cleanID(link.SectionID), "body"),
		LinkID:         "link:" + linkID,
		TargetURI:      safeURI(link.TargetURI),
		TargetKind:     "external",
		AnchorTextHash: safeFingerprint(link.Anchor),
		SourceMetadata: map[string]string{"redacted": "true"},
	}
}

func aclSummary(permission PermissionSummary) *facts.DocumentationACLSummary {
	visibility := firstNonEmpty(permission.Visibility, "unknown")
	return &facts.DocumentationACLSummary{
		Visibility:    visibility,
		ReaderGroups:  safePrincipalList(permission.ReaderGroups),
		WriterGroups:  safePrincipalList(permission.WriterGroups),
		ReaderUsers:   safePrincipalList(permission.ReaderUsers),
		WriterUsers:   safePrincipalList(permission.WriterUsers),
		HasInherited:  permission.HasInherited,
		IsPartial:     permission.IsPartial,
		PartialReason: safeReason(permission.PartialReason),
	}
}

func envelope(
	scopeID string,
	generationID string,
	observedAt time.Time,
	kind string,
	key string,
	payload any,
	sourceURI string,
	sourceRecordID string,
) (facts.Envelope, error) {
	payloadMap, err := payloadToMap(payload)
	if err != nil {
		return facts.Envelope{}, err
	}
	return facts.Envelope{
		FactID: facts.StableID("GoogleWorkspaceFact", map[string]any{
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
			SourceURI:      safeURI(sourceURI),
			SourceRecordID: sourceRecordID,
		},
	}, nil
}

func payloadToMap(payload any) (map[string]any, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var out map[string]any
	if err := json.Unmarshal(encoded, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func schemaVersion(kind string) string {
	if kind == facts.DocumentationSectionFactKind {
		return facts.DocumentationSectionFactSchemaVersion
	}
	return facts.DocumentationFactSchemaVersion
}

func titleForKind(kind FileKind) string {
	switch kind {
	case FileKindSpreadsheet:
		return "Google Workspace spreadsheet"
	case FileKindPresentation:
		return "Google Workspace presentation"
	default:
		return "Google Workspace document"
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func cleanStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]bool{}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
