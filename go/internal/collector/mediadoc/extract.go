// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mediadoc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/mediapreflight"
	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

const (
	formatMediaTranscript    = "media_transcript"
	incidentSourceTranscript = "transcript_chunk"
	documentTypeMedia        = "media"
	defaultMaxSectionChars   = 16 * 1024
)

// Extract preflights local media and emits source-neutral transcript facts.
func Extract(ctx context.Context, req Request) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	sourceName := firstNonEmpty(req.SourceName, req.SourceURI, req.ExternalID)
	preflight, err := mediapreflight.Preflight(
		ctx,
		sourceName,
		bytes.NewReader(req.Body),
		int64(len(req.Body)),
		req.Options.Preflight,
	)
	sourceHash := hashBytes(req.Body)
	engineSourceName := safeSourceURI(sourceName)
	if engineSourceName == "" {
		engineSourceName = "media-source:" + hashText(sourceHash)
	}
	document := buildDocument(req, preflight, sourceHash)
	result := Result{Preflight: preflight, Document: document}
	if err != nil {
		document.SourceMetadata["transcript_status"] = "skipped"
		result.Document = document
		result.Envelopes = append(result.Envelopes, envelope(req, facts.DocumentationDocumentFactKind, facts.DocumentationDocumentStableID(document), document))
		return result, err
	}
	if skipPreflight(preflight) {
		document.SourceMetadata["transcript_status"] = skippedStatus(preflight)
		result.Document = document
		result.Envelopes = append(result.Envelopes, envelope(req, facts.DocumentationDocumentFactKind, facts.DocumentationDocumentStableID(document), document))
		return result, nil
	}
	if req.Engine == nil {
		return Result{}, fmt.Errorf("transcript engine is required after media preflight")
	}
	transcript, err := req.Engine.Transcribe(ctx, Media{
		SourceName:       engineSourceName,
		Format:           preflight.Format,
		DurationMillis:   preflight.DurationMillis,
		AudioStreamCount: preflight.AudioStreamCount,
		Body:             append([]byte(nil), req.Body...),
	})
	if err != nil {
		return Result{}, fmt.Errorf("transcribe media segments: %w", err)
	}
	transcriptSections := buildSections(req, document, transcript, sourceHash)
	sections := sectionPayloads(transcriptSections)
	if len(sections) == 0 {
		document.SourceMetadata["transcript_status"] = "no_text"
	} else {
		document.SourceMetadata["transcript_status"] = "completed"
	}
	document.SourceMetadata["transcript_segment_count"] = strconv.Itoa(len(sections))
	result.Document = document
	result.Sections = sections
	result.Envelopes = append(result.Envelopes, envelope(req, facts.DocumentationDocumentFactKind, facts.DocumentationDocumentStableID(document), document))
	for _, section := range sections {
		result.Envelopes = append(result.Envelopes, envelope(req, facts.DocumentationSectionFactKind, facts.DocumentationSectionStableID(section), section))
	}
	mentions, err := mentionEnvelopes(ctx, req, document, transcriptSections)
	if err != nil {
		return Result{}, err
	}
	result.Envelopes = append(result.Envelopes, mentions...)
	return result, nil
}

type transcriptSection struct {
	payload      facts.DocumentationSectionPayload
	mentionHints []doctruth.MentionHint
	redacted     bool
}

func buildDocument(req Request, preflight mediapreflight.Result, sourceHash string) facts.DocumentationDocumentPayload {
	metadata := map[string]string{
		"format_family":               formatMediaTranscript,
		"incident_media_source_class": incidentSourceTranscript,
		"transcript_status":           "pending",
		"transcript_segment_count":    "0",
		"source_hash":                 sourceHash,
	}
	if preflight.Format != "" {
		metadata["media_format"] = preflight.Format
	}
	addPositiveInt64(metadata, "source_bytes", preflight.SourceBytes)
	addPositiveInt64(metadata, "duration_millis", preflight.DurationMillis)
	addPositiveInt(metadata, "audio_stream_count", preflight.AudioStreamCount)
	addPositiveInt(metadata, "external_reference_count", preflight.ExternalReferenceCount)
	addPositiveInt(metadata, "sensitive_value_count", preflight.SensitiveValueCount)
	addPositiveInt(metadata, "metadata_redaction_count", preflight.MetadataRedactionCount)
	addWarnings(metadata, warningClasses(preflight.Warnings)...)
	documentID, documentRedacted := safeIdentity(req.DocumentID, "doc:media")
	externalID, externalRedacted := safeIdentity(req.ExternalID, "media-source")
	canonicalURI, canonicalRedacted := safeCanonicalURI(req.CanonicalURI)
	identitySeed := firstNonEmpty(req.SourceURI, req.SourceName, req.ExternalID, sourceHash)
	if documentID == "" {
		documentID = "doc:media:" + hashText(identitySeed)
		metadata["document_id_generated"] = "true"
	}
	if externalID == "" {
		externalID = "media-source:" + hashText(identitySeed)
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
		DocumentType:   documentTypeMedia,
		Format:         formatMediaTranscript,
		Language:       "en",
		SourceMetadata: metadata,
		ContentHash:    sourceHash,
	}
}

func buildSections(
	req Request,
	document facts.DocumentationDocumentPayload,
	transcript EngineResult,
	sourceHash string,
) []transcriptSection {
	sections := make([]transcriptSection, 0, len(transcript.Segments))
	for i, segment := range transcript.Segments {
		text := strings.TrimSpace(segment.Text)
		if text == "" || segment.EndMillis < segment.StartMillis || segment.StartMillis < 0 {
			continue
		}
		content, warnings, redacted := persistedContent(text, req.Options.MaxSectionChars)
		segmentID := firstNonEmpty(cleanID(segment.SegmentID), strconv.Itoa(i+1))
		metadata := map[string]string{
			"format_family":               formatMediaTranscript,
			"incident_media_source_class": incidentSourceTranscript,
			"transcript_engine":           firstNonEmpty(transcript.EngineName, "unknown"),
			"transcript_engine_version":   firstNonEmpty(transcript.EngineVersion, "unknown"),
			"confidence_bucket":           confidenceBucket(segment.Confidence),
			"source_hash":                 sourceHash,
			"start_millis":                strconv.FormatInt(segment.StartMillis, 10),
			"end_millis":                  strconv.FormatInt(segment.EndMillis, 10),
			"segment_duration_millis":     strconv.FormatInt(segment.EndMillis-segment.StartMillis, 10),
		}
		if transcript.Language != "" {
			metadata["language"] = transcript.Language
		}
		if strings.TrimSpace(segment.SpeakerLabel) != "" {
			metadata["speaker_label_present"] = "true"
			metadata["speaker_label_hash"] = hashText(segment.SpeakerLabel)
		}
		if redacted {
			metadata["redacted"] = "true"
			metadata["redaction_class"] = string(mediapreflight.WarningSensitiveValueRedacted)
		}
		if segment.Confidence > 0 && segment.Confidence < 0.50 {
			warnings = append(warnings, "transcript_low_confidence")
		}
		addWarnings(metadata, warnings...)
		sections = append(sections, transcriptSection{
			payload: facts.DocumentationSectionPayload{
				DocumentID:       document.DocumentID,
				RevisionID:       document.RevisionID,
				SectionID:        "transcript:" + segmentID,
				SectionAnchor:    "transcript-" + segmentID,
				HeadingText:      "Transcript segment",
				OrdinalPath:      []int{len(sections) + 1},
				Content:          content,
				ContentFormat:    "text/plain",
				TextHash:         hashText(text),
				ExcerptHash:      hashText(content),
				SourceStartRef:   "time:" + formatMillis(segment.StartMillis),
				SourceEndRef:     "time:" + formatMillis(segment.EndMillis),
				SourceMetadata:   metadata,
				ContainsWarnings: len(metadata["warning"]) > 0,
			},
			mentionHints: safeMentionHints(segment.MentionHints),
			redacted:     redacted,
		})
	}
	return sections
}

func safeMentionHints(hints []doctruth.MentionHint) []doctruth.MentionHint {
	out := make([]doctruth.MentionHint, 0, len(hints))
	for _, hint := range hints {
		if containsSensitiveMarker(hint.Text) || isUnsafeSourcePath(hint.Text) {
			continue
		}
		out = append(out, hint)
	}
	return out
}

func sectionPayloads(sections []transcriptSection) []facts.DocumentationSectionPayload {
	out := make([]facts.DocumentationSectionPayload, 0, len(sections))
	for _, section := range sections {
		out = append(out, section.payload)
	}
	return out
}

func mentionEnvelopes(
	ctx context.Context,
	req Request,
	document facts.DocumentationDocumentPayload,
	sections []transcriptSection,
) ([]facts.Envelope, error) {
	if len(req.Entities) == 0 && !hasMentionHints(sections) {
		return nil, nil
	}
	extractor := doctruth.NewExtractor(req.Entities, doctruth.Options{})
	var out []facts.Envelope
	for _, section := range sections {
		if section.redacted {
			continue
		}
		result, err := extractor.Extract(ctx, doctruth.SectionInput{
			ScopeID:        req.ScopeID,
			GenerationID:   req.GenerationID,
			SourceSystem:   firstNonEmpty(req.SourceSystem, "documentation_media"),
			DocumentID:     document.DocumentID,
			RevisionID:     document.RevisionID,
			SectionID:      section.payload.SectionID,
			CanonicalURI:   document.CanonicalURI,
			ExcerptHash:    section.payload.ExcerptHash,
			SourceStartRef: section.payload.SourceStartRef,
			SourceEndRef:   section.payload.SourceEndRef,
			Text:           section.payload.Content,
			MentionHints:   section.mentionHints,
			ObservedAt:     req.ObservedAt,
			SourceMetadata: section.payload.SourceMetadata,
		})
		if err != nil {
			return nil, fmt.Errorf("extract transcript mentions for %s: %w", section.payload.SectionID, err)
		}
		for _, envelope := range result.Envelopes {
			if envelope.FactKind == facts.DocumentationEntityMentionFactKind {
				out = append(out, envelope)
			}
		}
	}
	return out, nil
}

func hasMentionHints(sections []transcriptSection) bool {
	for _, section := range sections {
		if len(section.mentionHints) > 0 {
			return true
		}
	}
	return false
}

func skipPreflight(preflight mediapreflight.Result) bool {
	return len(preflight.Warnings) > 0
}

func skippedStatus(preflight mediapreflight.Result) string {
	for _, warning := range preflight.Warnings {
		if warning.Class == mediapreflight.WarningTranscriptNoSpeech {
			return "no_speech"
		}
	}
	return "skipped"
}

func persistedContent(text string, maxChars int) (string, []string, bool) {
	warnings := []string{}
	if containsSensitiveMarker(text) {
		return "[redacted]", []string{string(mediapreflight.WarningSensitiveValueRedacted)}, true
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
	payloadMap, err := payloadToMap(payload)
	if err != nil {
		payloadMap = map[string]any{"payload_error": err.Error()}
	}
	observedAt := req.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	sourceSystem := firstNonEmpty(req.SourceSystem, "documentation_media")
	sourceRecordID, _ := safeIdentity(firstNonEmpty(req.SourceRecordID, req.ExternalID, key), "media-record")
	sourceURI := safeSourceURI(req.SourceURI)
	if strings.HasPrefix(sourceURI, "redacted:") {
		sourceRecordID = sourceURI
	}
	return facts.Envelope{
		FactID: facts.StableID("DocumentationMediaFact", map[string]any{
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

func warningClasses(warnings []mediapreflight.Warning) []string {
	out := make([]string, 0, len(warnings))
	for _, warning := range warnings {
		if warning.Count > 0 {
			out = append(out, string(warning.Class))
		}
	}
	return out
}
