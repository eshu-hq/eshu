// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctruth_test

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestIncidentMediaExactEvidencePreservesProvenance(t *testing.T) {
	t.Parallel()

	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{Kind: "service", ID: "service:checkout-api", Aliases: []string{"checkout-api"}},
	}, doctruth.Options{})
	section := incidentMediaSection("checkout-api recovered after deploy rollback.")
	section.ClaimHints = []doctruth.ClaimHint{{
		ClaimID:     "claim:incident-media:checkout-api",
		ClaimType:   "incident_media_mention",
		ClaimText:   "checkout-api recovered after deploy rollback.",
		SubjectText: "checkout-api",
		SubjectKind: "service",
		SourceMetadata: map[string]string{
			"claim_scope": "document_evidence_only",
		},
	}}

	result, err := extractor.Extract(context.Background(), section)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	mention := onlyPayload[facts.DocumentationEntityMentionPayload](t, result.Envelopes, facts.DocumentationEntityMentionFactKind)
	if got, want := mention.ResolutionStatus, facts.DocumentationMentionResolutionExact; got != want {
		t.Fatalf("mention resolution = %q, want %q", got, want)
	}
	assertIncidentMediaMetadata(t, "mention", mention.SourceMetadata, section)

	claim := onlyPayload[facts.DocumentationClaimCandidatePayload](t, result.Envelopes, facts.DocumentationClaimCandidateFactKind)
	if got, want := claim.Authority, facts.DocumentationClaimAuthorityDocumentEvidence; got != want {
		t.Fatalf("claim authority = %q, want %q", got, want)
	}
	assertIncidentMediaMetadata(t, "claim", claim.SourceMetadata, section)
	if got, want := claim.SourceMetadata["claim_scope"], "document_evidence_only"; got != want {
		t.Fatalf("claim metadata claim_scope = %q, want %q", got, want)
	}
}

func TestIncidentMediaEdgeFixturesStayEvidenceOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		entities         []doctruth.Entity
		section          doctruth.SectionInput
		wantMentions     int
		wantStatus       string
		wantRefs         int
		wantClaims       int
		wantACLState     string
		wantMetadataKey  string
		wantMetadataVal  string
		forbidPayloadSub string
	}{
		{
			name: "ambiguous transcript keeps candidates without claim",
			entities: []doctruth.Entity{
				{Kind: "service", ID: "service:checkout-api", Aliases: []string{"checkout"}},
				{Kind: "service", ID: "service:checkout-worker", Aliases: []string{"checkout"}},
			},
			section: func() doctruth.SectionInput {
				section := incidentMediaSection("checkout recovered after the rollback.")
				section.ClaimHints = []doctruth.ClaimHint{{
					ClaimID:     "claim:incident-media:ambiguous",
					ClaimType:   "incident_media_mention",
					ClaimText:   "checkout recovered after the rollback.",
					SubjectText: "checkout",
					SubjectKind: "service",
				}}
				section.SourceMetadata["media_correlation_state"] = "ambiguous"
				return section
			}(),
			wantMentions:    1,
			wantStatus:      facts.DocumentationMentionResolutionAmbiguous,
			wantRefs:        2,
			wantClaims:      0,
			wantMetadataKey: "media_correlation_state",
			wantMetadataVal: "ambiguous",
		},
		{
			name:         "unmatched diagram label stays provenance only",
			entities:     nil,
			section:      incidentHintOnlySection("diagram_label", "orphan-dashboard", "service", "unmatched"),
			wantMentions: 1,
			wantStatus:   facts.DocumentationMentionResolutionUnmatched,
			wantRefs:     0,
			wantClaims:   0,
		},
		{
			name: "diagram label can resolve existing repository truth",
			entities: []doctruth.Entity{
				{Kind: "repository", ID: "repo:checkout", Aliases: []string{"checkout-repo"}},
			},
			section:      incidentHintOnlySection("diagram_label", "checkout-repo", "repository", "exact"),
			wantMentions: 1,
			wantStatus:   facts.DocumentationMentionResolutionExact,
			wantRefs:     1,
			wantClaims:   0,
		},
		{
			name: "diagram label can resolve existing workload truth",
			entities: []doctruth.Entity{
				{Kind: "workload", ID: "workload:checkout-api", Aliases: []string{"checkout workload"}},
			},
			section:      incidentHintOnlySection("diagram_label", "checkout workload", "workload", "exact"),
			wantMentions: 1,
			wantStatus:   facts.DocumentationMentionResolutionExact,
			wantRefs:     1,
			wantClaims:   0,
		},
		{
			name: "diagram label can resolve existing cloud resource truth",
			entities: []doctruth.Entity{
				{Kind: "cloud_resource", ID: "cloud:lb-checkout", Aliases: []string{"checkout-lb"}},
			},
			section:      incidentHintOnlySection("diagram_label", "checkout-lb", "cloud_resource", "exact"),
			wantMentions: 1,
			wantStatus:   facts.DocumentationMentionResolutionExact,
			wantRefs:     1,
			wantClaims:   0,
		},
		{
			name: "transcript chunk can resolve existing deployment truth",
			entities: []doctruth.Entity{
				{Kind: "deployment", ID: "deployment:checkout-prod", Aliases: []string{"checkout prod deploy"}},
			},
			section:      incidentHintOnlySection("transcript_chunk", "checkout prod deploy", "deployment", "exact"),
			wantMentions: 1,
			wantStatus:   facts.DocumentationMentionResolutionExact,
			wantRefs:     1,
			wantClaims:   0,
		},
		{
			name: "stale transcript preserves stale ACL posture",
			entities: []doctruth.Entity{
				{Kind: "incident", ID: "incident:review-2026-05", Aliases: []string{"incident review"}},
			},
			section: func() doctruth.SectionInput {
				section := incidentHintOnlySection("transcript_chunk", "incident review", "incident", "stale")
				section.SourceACLState = facts.SourceACLStateStale
				section.SourceMetadata["media_freshness_state"] = "stale"
				return section
			}(),
			wantMentions:    1,
			wantStatus:      facts.DocumentationMentionResolutionExact,
			wantRefs:        1,
			wantClaims:      0,
			wantACLState:    facts.SourceACLStateStale,
			wantMetadataKey: "media_freshness_state",
			wantMetadataVal: "stale",
		},
		{
			name:             "redacted screenshot omits raw sensitive value",
			entities:         nil,
			section:          incidentHintOnlySection("ocr_region", "[redacted-value]", "cloud_resource", "redacted"),
			wantMentions:     1,
			wantStatus:       facts.DocumentationMentionResolutionUnmatched,
			wantRefs:         0,
			wantClaims:       0,
			wantMetadataKey:  "redaction_state",
			wantMetadataVal:  "applied",
			forbidPayloadSub: "raw-sensitive-value",
		},
		{
			name:         "unsupported media without deterministic hint fabricates nothing",
			entities:     []doctruth.Entity{{Kind: "service", ID: "service:checkout-api", Aliases: []string{"checkout-api"}}},
			section:      unsupportedIncidentMediaSection(),
			wantMentions: 0,
			wantClaims:   0,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := doctruth.NewExtractor(tt.entities, doctruth.Options{}).Extract(context.Background(), tt.section)
			if err != nil {
				t.Fatalf("Extract() error = %v, want nil", err)
			}
			if got := countKind(result.Envelopes, facts.DocumentationEntityMentionFactKind); got != tt.wantMentions {
				t.Fatalf("mention envelopes = %d, want %d", got, tt.wantMentions)
			}
			if got := countKind(result.Envelopes, facts.DocumentationClaimCandidateFactKind); got != tt.wantClaims {
				t.Fatalf("claim envelopes = %d, want %d", got, tt.wantClaims)
			}
			if tt.wantMentions == 0 {
				return
			}

			mention := onlyPayload[facts.DocumentationEntityMentionPayload](t, result.Envelopes, facts.DocumentationEntityMentionFactKind)
			if got := mention.ResolutionStatus; got != tt.wantStatus {
				t.Fatalf("mention resolution = %q, want %q", got, tt.wantStatus)
			}
			if got := len(mention.CandidateRefs); got != tt.wantRefs {
				t.Fatalf("mention candidate refs = %d, want %d", got, tt.wantRefs)
			}
			if tt.wantACLState != "" {
				if mention.ACLSummary == nil {
					t.Fatalf("mention ACLSummary = nil, want source_acl_state %q", tt.wantACLState)
				}
				if got := mention.ACLSummary.SourceACLState; got != tt.wantACLState {
					t.Fatalf("mention source_acl_state = %q, want %q", got, tt.wantACLState)
				}
			}
			if tt.wantMetadataKey != "" {
				if got := mention.SourceMetadata[tt.wantMetadataKey]; got != tt.wantMetadataVal {
					t.Fatalf("mention metadata %s = %q, want %q", tt.wantMetadataKey, got, tt.wantMetadataVal)
				}
			}
			if tt.forbidPayloadSub != "" && strings.Contains(payloadString(t, mention), tt.forbidPayloadSub) {
				t.Fatalf("mention payload contains forbidden sensitive marker %q", tt.forbidPayloadSub)
			}
		})
	}
}

func incidentMediaSection(text string) doctruth.SectionInput {
	section := baseSectionInput(text)
	section.SourceSystem = "incident_media_fixture"
	section.DocumentID = "doc:incident-media:postmortem-review"
	section.RevisionID = "rev:media-fixture:1"
	section.SectionID = "section:postmortem:timeline"
	section.CanonicalURI = "eshu-fixture://incident-media/postmortem-review#timeline"
	section.SourceStartRef = "transcript:00:01:10.000"
	section.SourceEndRef = "transcript:00:01:24.000"
	section.SourceMetadata = map[string]string{
		"format_family":               "media_transcript",
		"incident_media_source_class": "transcript_chunk",
		"media_correlation_state":     "exact",
		"media_freshness_state":       "current",
		"redaction_policy_version":    "fixture-v1",
		"redaction_state":             "none",
		"section_anchor":              "postmortem-timeline",
	}
	return section
}

func incidentHintOnlySection(sourceClass string, text string, kind string, state string) doctruth.SectionInput {
	section := incidentMediaSection("")
	section.MentionHints = []doctruth.MentionHint{{
		Text: text,
		Kind: kind,
		From: sourceClass,
	}}
	section.SourceMetadata["incident_media_source_class"] = sourceClass
	section.SourceMetadata["media_correlation_state"] = state
	if state == "redacted" {
		section.SourceMetadata["format_family"] = "screenshot"
		section.SourceMetadata["redaction_state"] = "applied"
		section.SourceMetadata["redacted_region_count"] = "1"
		section.SourceStartRef = "ocr:region:1"
		section.SourceEndRef = "ocr:region:1"
	}
	return section
}

func unsupportedIncidentMediaSection() doctruth.SectionInput {
	section := incidentMediaSection("")
	section.SourceMetadata["incident_media_source_class"] = "unsupported_media"
	section.SourceMetadata["media_correlation_state"] = "unsupported"
	section.SourceMetadata["provider_state"] = "unavailable"
	return section
}

func assertIncidentMediaMetadata(t *testing.T, label string, metadata map[string]string, section doctruth.SectionInput) {
	t.Helper()

	for _, key := range []string{
		"format_family",
		"incident_media_source_class",
		"media_correlation_state",
		"media_freshness_state",
		"redaction_policy_version",
		"redaction_state",
		"section_anchor",
	} {
		if got, want := metadata[key], section.SourceMetadata[key]; got != want {
			t.Fatalf("%s metadata %s = %q, want %q", label, key, got, want)
		}
	}
	if got, want := metadata["source_start_ref"], section.SourceStartRef; got != want {
		t.Fatalf("%s metadata source_start_ref = %q, want %q", label, got, want)
	}
	if got, want := metadata["source_end_ref"], section.SourceEndRef; got != want {
		t.Fatalf("%s metadata source_end_ref = %q, want %q", label, got, want)
	}
}

func payloadString(t *testing.T, value any) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal(payload) error = %v, want nil", err)
	}
	return string(encoded)
}
