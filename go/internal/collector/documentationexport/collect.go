// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package documentationexport

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/collector/exportmanifestpreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const maxSectionBytes = 16 * 1024

// Collect parses explicit offline export files into source-neutral documentation facts.
func Collect(ctx context.Context, req Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	preflight, err := exportmanifestpreflight.Preflight(ctx, req.ManifestName, bytes.NewReader(req.Manifest), exportmanifestpreflight.Options{})
	result := Result{Preflight: preflight}
	if err != nil {
		return result, err
	}
	if !preflight.Safe {
		return result, nil
	}
	var decoded manifest
	if err := json.Unmarshal(req.Manifest, &decoded); err != nil {
		return result, nil
	}
	scopeID := firstNonEmpty(req.ScopeID, "doc-source:"+sourceFormat+":"+safeFingerprint(decoded.SourceSystem+":"+decoded.SourceScopeID))
	observedAt := req.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	generationID := firstNonEmpty(req.GenerationID, "export-generation:"+safeFingerprint(scopeID+":"+observedAt.Format(time.RFC3339Nano)))

	sourcePayload := sourcePayload(decoded, scopeID, len(decoded.Files))
	sourceEnvelope, err := envelope(
		scopeID,
		generationID,
		observedAt,
		facts.DocumentationSourceFactKind,
		facts.DocumentationSourceStableID(sourcePayload),
		sourcePayload,
		decoded.SourceSystem,
		"export-manifest:"+safeFingerprint(req.ManifestName),
		sourcePayload.ExternalID,
	)
	if err != nil {
		return result, err
	}
	result.Envelopes = append(result.Envelopes, sourceEnvelope)

	for _, file := range decoded.Files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		envelopes, err := collectFile(decoded, file, req.Files[file.Path], scopeID, generationID, observedAt)
		if err != nil {
			return result, err
		}
		result.Envelopes = append(result.Envelopes, envelopes...)
	}
	return result, nil
}

func collectFile(decoded manifest, file manifestFile, body []byte, scopeID string, generationID string, observedAt time.Time) ([]facts.Envelope, error) {
	var record exportRecord
	warning := ""
	if len(body) == 0 {
		warning = "export_file_missing"
	} else if err := json.Unmarshal(body, &record); err != nil {
		warning = "malformed_json"
	}
	if warning == "" && unsupportedRecord(record) {
		warning = "unsupported_export_shape"
	}
	document := documentPayload(scopeID, decoded, file, record, warning, string(body))
	documentEnvelope, err := envelope(
		scopeID,
		generationID,
		observedAt,
		facts.DocumentationDocumentFactKind,
		facts.DocumentationDocumentStableID(document),
		document,
		decoded.SourceSystem,
		"export-file:"+safeFingerprint(file.Path),
		document.ExternalID,
	)
	if err != nil {
		return nil, err
	}
	if warning != "" {
		return []facts.Envelope{documentEnvelope}, nil
	}
	out := []facts.Envelope{documentEnvelope}
	sections := recordSections(record)
	for i, section := range sections {
		payload := sectionPayload(document, decoded.SourceSystem, section, i)
		sectionEnvelope, err := envelope(
			scopeID,
			generationID,
			observedAt,
			facts.DocumentationSectionFactKind,
			facts.DocumentationSectionStableID(payload),
			payload,
			decoded.SourceSystem,
			"export-file:"+safeFingerprint(file.Path),
			document.ExternalID,
		)
		if err != nil {
			return nil, err
		}
		out = append(out, sectionEnvelope)
	}
	for i, link := range record.Links {
		payload := linkPayload(document, link, i)
		if payload.TargetURI == "" {
			continue
		}
		linkEnvelope, err := envelope(
			scopeID,
			generationID,
			observedAt,
			facts.DocumentationLinkFactKind,
			facts.DocumentationLinkStableID(payload),
			payload,
			decoded.SourceSystem,
			"export-file:"+safeFingerprint(file.Path),
			document.ExternalID,
		)
		if err != nil {
			return nil, err
		}
		out = append(out, linkEnvelope)
	}
	return out, nil
}

type exportSection struct {
	heading string
	content string
	ref     string
	deleted bool
	edited  bool
}

func unsupportedRecord(record exportRecord) bool {
	return firstNonEmpty(record.Body, record.Text, record.Content) == "" &&
		len(record.Comments)+len(record.Timeline)+len(record.Changelog)+len(record.Messages)+len(record.Links) == 0
}

func recordSections(record exportRecord) []exportSection {
	sections := []exportSection{}
	if content := firstNonEmpty(record.Body, record.Text, record.Content); content != "" || record.Deleted {
		sections = append(sections, exportSection{
			heading: firstNonEmpty(record.Title, "Export body"),
			content: contentUnlessDeleted(content, record.Deleted),
			ref:     firstNonEmpty(record.ID, "body"),
			deleted: record.Deleted,
			edited:  record.Edited,
		})
	}
	sections = append(sections, recordSectionGroup("comment", record.Comments)...)
	sections = append(sections, recordSectionGroup("timeline", record.Timeline)...)
	sections = append(sections, recordSectionGroup("changelog", record.Changelog)...)
	sections = append(sections, recordSectionGroup("message", record.Messages)...)
	return sections
}

func recordSectionGroup(kind string, records []recordSection) []exportSection {
	out := make([]exportSection, 0, len(records))
	for i, record := range records {
		content := firstNonEmpty(record.Body, record.Text, record.Content)
		out = append(out, exportSection{
			heading: firstNonEmpty(record.Heading, record.Title, defaultSectionHeading(kind)),
			content: contentUnlessDeleted(content, record.Deleted),
			ref:     firstNonEmpty(record.ID, fmt.Sprintf("%s:%d", kind, i+1)),
			deleted: record.Deleted,
			edited:  record.Edited,
		})
	}
	return out
}

func defaultSectionHeading(kind string) string {
	switch kind {
	case "comment":
		return "Comment"
	case "timeline":
		return "Timeline"
	case "changelog":
		return "Changelog"
	case "message":
		return "Message"
	default:
		return "Export section"
	}
}

func contentUnlessDeleted(content string, deleted bool) string {
	if deleted {
		return ""
	}
	return strings.TrimSpace(content)
}

func truncateSection(content string) (string, bool) {
	if len(content) <= maxSectionBytes {
		return content, false
	}
	end := maxSectionBytes
	for end > 0 && !utf8.ValidString(content[:end]) {
		end--
	}
	return content[:end], true
}
