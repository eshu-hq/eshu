// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocrdoc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/imagepreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	formatImageOCR              = "image_ocr"
	incidentMediaClassOCRRegion = "ocr_region"
	documentTypeImage           = "image"
	defaultMaxSectionChars      = 16 * 1024
)

// Extract preflights an image and emits source-neutral OCR documentation facts.
func Extract(ctx context.Context, req Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	sourceName := firstNonEmpty(req.SourceName, req.SourceURI, req.ExternalID)
	preflight, err := imagepreflight.Preflight(
		ctx,
		sourceName,
		bytes.NewReader(req.Body),
		int64(len(req.Body)),
		req.Options.Preflight,
	)
	sourceHash := hashBytes(req.Body)
	engineSourceName := safeSourceURI(sourceName)
	if engineSourceName == "" {
		engineSourceName = "ocr-source:" + hashText(sourceHash)
	}
	document := buildDocument(req, preflight, sourceHash)
	result := Result{Preflight: preflight, Document: document}
	if err != nil {
		document.SourceMetadata["ocr_status"] = "skipped"
		result.Document = document
		result.Envelopes = append(result.Envelopes, envelope(req, facts.DocumentationDocumentFactKind, facts.DocumentationDocumentStableID(document), document))
		return result, err
	}
	if skipPreflight(preflight) {
		document.SourceMetadata["ocr_status"] = "skipped"
		result.Document = document
		result.Envelopes = append(result.Envelopes, envelope(req, facts.DocumentationDocumentFactKind, facts.DocumentationDocumentStableID(document), document))
		return result, nil
	}
	if req.Engine == nil {
		return Result{}, fmt.Errorf("ocr engine is required after image preflight")
	}
	ocrResult, err := req.Engine.Recognize(ctx, Image{
		SourceName: engineSourceName,
		Format:     preflight.Format,
		Width:      preflight.Width,
		Height:     preflight.Height,
		FrameCount: preflight.FrameCount,
		FrameIndex: 0,
		Body:       append([]byte(nil), req.Body...),
	})
	if err != nil {
		return Result{}, fmt.Errorf("recognize OCR regions: %w", err)
	}
	sections := buildSections(req, document, ocrResult, sourceHash)
	if len(sections) == 0 {
		document.SourceMetadata["ocr_status"] = "no_text"
	} else {
		document.SourceMetadata["ocr_status"] = "completed"
	}
	document.SourceMetadata["ocr_region_count"] = strconv.Itoa(len(sections))
	result.Document = document
	result.Sections = sections
	result.Envelopes = append(result.Envelopes, envelope(req, facts.DocumentationDocumentFactKind, facts.DocumentationDocumentStableID(document), document))
	for _, section := range sections {
		result.Envelopes = append(result.Envelopes, envelope(req, facts.DocumentationSectionFactKind, facts.DocumentationSectionStableID(section), section))
	}
	return result, nil
}

func buildDocument(req Request, preflight imagepreflight.Result, sourceHash string) facts.DocumentationDocumentPayload {
	metadata := map[string]string{
		"format_family":               formatImageOCR,
		"incident_media_source_class": incidentMediaClassOCRRegion,
		"ocr_status":                  "pending",
		"ocr_region_count":            "0",
		"source_hash":                 sourceHash,
	}
	if preflight.Format != "" {
		metadata["image_format"] = preflight.Format
	}
	addPositiveInt(metadata, "image_width", preflight.Width)
	addPositiveInt(metadata, "image_height", preflight.Height)
	addPositiveInt64(metadata, "pixel_count", preflight.PixelCount)
	addPositiveInt64(metadata, "source_bytes", preflight.SourceBytes)
	addPositiveInt(metadata, "frame_count", preflight.FrameCount)
	if preflight.Format == imagepreflight.FormatGIF && preflight.FrameCount > 1 {
		metadata["gif_frame_policy"] = "first_frame"
	}
	addWarnings(metadata, warningClasses(preflight.Warnings)...)
	documentID, documentRedacted := safeIdentity(req.DocumentID, "doc:ocr")
	externalID, externalRedacted := safeIdentity(req.ExternalID, "ocr-source")
	canonicalURI, canonicalRedacted := safeCanonicalURI(req.CanonicalURI)
	identitySeed := firstNonEmpty(req.SourceURI, req.SourceName, req.ExternalID, sourceHash)
	if documentID == "" {
		documentID = "doc:ocr:" + hashText(identitySeed)
		metadata["document_id_generated"] = "true"
	}
	if externalID == "" {
		externalID = "ocr-source:" + hashText(identitySeed)
		metadata["external_id_generated"] = "true"
	}
	revisionID := strings.TrimSpace(req.RevisionID)
	if revisionID == "" {
		revisionID = sourceHash
		metadata["revision_id_generated"] = "true"
	}
	if documentRedacted {
		metadata["document_id_redacted"] = "true"
	}
	if externalRedacted {
		metadata["external_id_redacted"] = "true"
	}
	if canonicalRedacted {
		metadata["canonical_uri_redacted"] = "true"
	}
	return facts.DocumentationDocumentPayload{
		SourceID:       req.SourceID,
		DocumentID:     documentID,
		ExternalID:     externalID,
		RevisionID:     revisionID,
		CanonicalURI:   canonicalURI,
		Title:          req.Title,
		DocumentType:   documentTypeImage,
		Format:         formatImageOCR,
		Language:       "en",
		SourceMetadata: metadata,
		ContentHash:    sourceHash,
	}
}

func buildSections(
	req Request,
	document facts.DocumentationDocumentPayload,
	ocrResult EngineResult,
	sourceHash string,
) []facts.DocumentationSectionPayload {
	sections := make([]facts.DocumentationSectionPayload, 0, len(ocrResult.Regions))
	for i, region := range ocrResult.Regions {
		text := strings.TrimSpace(region.Text)
		if text == "" {
			continue
		}
		content, warnings, redacted := persistedContent(text, req.Options.MaxSectionChars)
		regionID := firstNonEmpty(cleanID(region.RegionID), strconv.Itoa(i+1))
		metadata := map[string]string{
			"format_family":               formatImageOCR,
			"incident_media_source_class": incidentMediaClassOCRRegion,
			"ocr_engine":                  firstNonEmpty(ocrResult.EngineName, "unknown"),
			"ocr_engine_version":          firstNonEmpty(ocrResult.EngineVersion, "unknown"),
			"confidence_bucket":           confidenceBucket(region.Confidence),
			"source_hash":                 sourceHash,
			"frame_index":                 "0",
			"bounds_x":                    fmt.Sprintf("%.4f", clamp01(region.Bounds.X)),
			"bounds_y":                    fmt.Sprintf("%.4f", clamp01(region.Bounds.Y)),
			"bounds_width":                fmt.Sprintf("%.4f", clamp01(region.Bounds.Width)),
			"bounds_height":               fmt.Sprintf("%.4f", clamp01(region.Bounds.Height)),
		}
		if ocrResult.Language != "" {
			metadata["language"] = ocrResult.Language
		}
		if redacted {
			metadata["redacted"] = "true"
			metadata["redaction_class"] = string(imagepreflight.WarningSensitiveValueRedacted)
		}
		if region.Confidence > 0 && region.Confidence < 0.50 {
			warnings = append(warnings, "ocr_low_confidence")
		}
		addWarnings(metadata, warnings...)
		sections = append(sections, facts.DocumentationSectionPayload{
			DocumentID:       document.DocumentID,
			RevisionID:       document.RevisionID,
			SectionID:        "ocr:" + regionID,
			SectionAnchor:    "ocr-region-" + regionID,
			HeadingText:      "OCR region",
			OrdinalPath:      []int{len(sections) + 1},
			Content:          content,
			ContentFormat:    "text/plain",
			TextHash:         hashText(text),
			ExcerptHash:      hashText(content),
			SourceStartRef:   "frame:0",
			SourceEndRef:     "frame:0",
			SourceMetadata:   metadata,
			ContainsWarnings: len(metadata["warning"]) > 0,
		})
	}
	return sections
}

func skipPreflight(preflight imagepreflight.Result) bool {
	if len(preflight.Warnings) == 0 {
		return false
	}
	for _, warning := range preflight.Warnings {
		if warning.Class != imagepreflight.WarningPartialExtraction {
			return true
		}
	}
	return false
}

func persistedContent(text string, maxChars int) (string, []string, bool) {
	warnings := []string{}
	if containsSensitiveMarker(text) {
		return "[redacted]", []string{string(imagepreflight.WarningSensitiveValueRedacted)}, true
	}
	if maxChars <= 0 {
		maxChars = defaultMaxSectionChars
	}
	runes := []rune(text)
	if len(runes) <= maxChars {
		return text, warnings, false
	}
	return strings.TrimSpace(string(runes[:maxChars])), []string{"partial_extraction"}, false
}

func envelope(req Request, kind string, key string, payload any) facts.Envelope {
	payloadMap, err := documentationPayloadMap(payload)
	if err != nil {
		payloadMap = map[string]any{"payload_error": err.Error()}
	}
	observedAt := req.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	sourceSystem := firstNonEmpty(req.SourceSystem, "documentation_ocr")
	sourceRecordID, _ := safeIdentity(firstNonEmpty(req.SourceRecordID, req.ExternalID, key), "ocr-record")
	sourceURI := safeSourceURI(req.SourceURI)
	if strings.HasPrefix(sourceURI, "redacted:") {
		sourceRecordID = sourceURI
	}
	return facts.Envelope{
		FactID: facts.StableID("DocumentationOCRFact", map[string]any{
			"fact_kind":     kind,
			"stable_key":    key,
			"scope_id":      req.ScopeID,
			"generation_id": req.GenerationID,
		}),
		ScopeID:          req.ScopeID,
		GenerationID:     req.GenerationID,
		FactKind:         kind,
		StableFactKey:    key,
		SchemaVersion:    schemaVersion(kind),
		CollectorKind:    string(scope.CollectorDocumentation),
		SourceConfidence: facts.SourceConfidenceObserved,
		ObservedAt:       observedAt,
		Payload:          payloadMap,
		SourceRef: facts.Ref{
			SourceSystem:   sourceSystem,
			ScopeID:        req.ScopeID,
			GenerationID:   req.GenerationID,
			FactKey:        key,
			SourceURI:      sourceURI,
			SourceRecordID: sourceRecordID,
		},
	}
}

func documentationPayloadMap(payload any) (map[string]any, error) {
	switch value := payload.(type) {
	case facts.DocumentationDocumentPayload:
		return facts.EncodeDocumentationDocument(value)
	case facts.DocumentationSectionPayload:
		return facts.EncodeDocumentationSection(value)
	default:
		return nil, fmt.Errorf("unsupported OCR documentation payload type %T", payload)
	}
}

func schemaVersion(kind string) string {
	if kind == facts.DocumentationSectionFactKind {
		return facts.DocumentationSectionFactSchemaVersion
	}
	return facts.DocumentationFactSchemaVersion
}

func warningClasses(warnings []imagepreflight.Warning) []string {
	out := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if warning.Count > 0 {
			out = append(out, string(warning.Class))
		}
	}
	return out
}

func addWarnings(metadata map[string]string, warnings ...string) {
	if len(warnings) == 0 {
		return
	}
	seen := map[string]bool{}
	for _, warning := range strings.Split(metadata["warning"], ",") {
		warning = strings.TrimSpace(warning)
		if warning != "" {
			seen[warning] = true
		}
	}
	for _, warning := range warnings {
		warning = strings.TrimSpace(warning)
		if warning != "" {
			seen[warning] = true
		}
	}
	ordered := make([]string, 0, len(seen))
	for warning := range seen {
		ordered = append(ordered, warning)
	}
	sort.Strings(ordered)
	metadata["warning"] = strings.Join(ordered, ",")
}

func addPositiveInt(metadata map[string]string, key string, value int) {
	if value > 0 {
		metadata[key] = strconv.Itoa(value)
	}
}

func addPositiveInt64(metadata map[string]string, key string, value int64) {
	if value > 0 {
		metadata[key] = strconv.FormatInt(value, 10)
	}
}

func confidenceBucket(confidence float64) string {
	switch {
	case confidence >= 0.85:
		return "high"
	case confidence >= 0.60:
		return "medium"
	case confidence > 0:
		return "low"
	default:
		return "unknown"
	}
}

func cleanID(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	var builder strings.Builder
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		}
	}
	return builder.String()
}

func clamp01(value float64) float64 {
	if value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}
	return value
}

func hashBytes(body []byte) string {
	sum := sha256.Sum256(body)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func hashText(text string) string {
	return hashBytes([]byte(text))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
