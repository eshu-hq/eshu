// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mediadoc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractEmitsProvenanceOnlyTranscriptMentions(t *testing.T) {
	t.Parallel()

	body := encodeTestWAV(t, 3000)
	engine := &fakeTranscriptEngine{result: EngineResult{Segments: []Segment{
		{
			SegmentID:   "exact",
			Text:        "gateway-api owns the restore step.",
			StartMillis: 0,
			EndMillis:   800,
			Confidence:  0.94,
		},
		{
			SegmentID:   "ambiguous",
			Text:        "shared-worker handles retry drains.",
			StartMillis: 900,
			EndMillis:   1700,
			Confidence:  0.73,
		},
		{
			SegmentID:   "unmatched",
			Text:        "unknown-service needs follow up.",
			StartMillis: 1800,
			EndMillis:   2600,
			Confidence:  0.68,
			MentionHints: []doctruth.MentionHint{{
				Text: "unknown-service",
				Kind: "service",
				From: doctruth.MentionHintStructuredSection,
			}},
		},
	}}}
	req := testRequest("docs/incident.wav", body, engine)
	req.Entities = []doctruth.Entity{
		{Kind: "service", ID: "service:gateway-api", Aliases: []string{"gateway-api"}},
		{Kind: "service", ID: "service:shared-worker-a", Aliases: []string{"shared-worker"}},
		{Kind: "service", ID: "service:shared-worker-b", Aliases: []string{"shared-worker"}},
	}

	result, err := Extract(context.Background(), req)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	mentions := payloadsByKind(result.Envelopes, facts.DocumentationEntityMentionFactKind)
	if got, want := len(mentions), 3; got != want {
		t.Fatalf("documentation_entity_mention count = %d, want %d", got, want)
	}
	statuses := map[string]string{}
	for _, mention := range mentions {
		text := mention["mention_text"].(string)
		statuses[text] = mention["resolution_status"].(string)
		metadata := stringMapValue(t, mention, "source_metadata")
		for key, want := range map[string]string{
			"format_family":               "media_transcript",
			"incident_media_source_class": "transcript_chunk",
		} {
			if got := metadata[key]; got != want {
				t.Fatalf("mention %q source_metadata[%q] = %q, want %q", text, key, got, want)
			}
		}
		if metadata["source_start_ref"] == "" || metadata["source_end_ref"] == "" {
			t.Fatalf("mention %q missing timestamp provenance: %#v", text, metadata)
		}
	}
	for text, want := range map[string]string{
		"gateway-api":     facts.DocumentationMentionResolutionExact,
		"shared-worker":   facts.DocumentationMentionResolutionAmbiguous,
		"unknown-service": facts.DocumentationMentionResolutionUnmatched,
	} {
		if got := statuses[text]; got != want {
			t.Fatalf("mention %q resolution_status = %q, want %q", text, got, want)
		}
	}
	if got := countKind(result.Envelopes, facts.DocumentationClaimCandidateFactKind); got != 0 {
		t.Fatalf("documentation_claim_candidate count = %d, want 0 for transcript mentions", got)
	}
}

func TestExtractDoesNotEmitMentionsFromRedactedTranscriptSections(t *testing.T) {
	t.Parallel()

	body := encodeTestWAV(t, 1000)
	engine := &fakeTranscriptEngine{result: EngineResult{Segments: []Segment{
		{
			SegmentID:   "sensitive",
			Text:        "credential_marker gateway-api was spoken aloud",
			StartMillis: 0,
			EndMillis:   900,
			Confidence:  0.98,
			MentionHints: []doctruth.MentionHint{{
				Text: "gateway-api",
				Kind: "service",
				From: doctruth.MentionHintStructuredSection,
			}},
		},
		{
			SegmentID:   "sensitive-hint",
			Text:        "A follow-up mention was detected.",
			StartMillis: 901,
			EndMillis:   1000,
			Confidence:  0.98,
			MentionHints: []doctruth.MentionHint{{
				Text: "token_marker-service",
				Kind: "service",
				From: doctruth.MentionHintStructuredSection,
			}},
		},
	}}}
	req := testRequest("docs/secret-looking.wav", body, engine)
	req.Entities = []doctruth.Entity{
		{Kind: "service", ID: "service:gateway-api", Aliases: []string{"gateway-api"}},
	}

	result, err := Extract(context.Background(), req)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	if got := countKind(result.Envelopes, facts.DocumentationEntityMentionFactKind); got != 0 {
		t.Fatalf("documentation_entity_mention count = %d, want 0 for redacted transcript section", got)
	}
	encoded, err := json.Marshal(result.Envelopes)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	for _, disallowed := range []string{"credential_marker", "gateway-api", "token_marker-service"} {
		if strings.Contains(string(encoded), disallowed) {
			t.Fatalf("redacted transcript envelopes leaked %q: %s", disallowed, encoded)
		}
	}
}
