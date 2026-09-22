// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDocumentationDocumentPayloadIsSourceNeutral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload DocumentPayload
	}{
		{
			name: "confluence page",
			payload: DocumentPayload{
				SourceID:          "doc-source:confluence:platform",
				DocumentID:        "doc:confluence:12345",
				ExternalID:        "12345",
				RevisionID:        "17",
				CanonicalURI:      "https://example.atlassian.net/wiki/spaces/PLAT/pages/12345",
				Title:             "Payment Service Deployment",
				ParentDocumentID:  "doc:confluence:10000",
				DocumentType:      "runbook",
				Format:            "storage",
				Language:          "en",
				Labels:            []string{"payments", "deployment"},
				OwnerRefs:         []OwnerRef{{Kind: "group", ID: "team:payments", DisplayName: "Payments"}},
				ACLSummary:        &ACLSummary{Visibility: "restricted", ReaderGroups: []string{"platform"}},
				SourceMetadata:    map[string]string{"space_key": "PLAT"},
				ContentHash:       "sha256:document-content",
				DocumentUpdatedAt: "2026-05-09T12:00:00Z",
			},
		},
		{
			name: "git markdown document",
			payload: DocumentPayload{
				SourceID:          "doc-source:git:platform-docs",
				DocumentID:        "doc:git:platform-docs:docs/payment.md",
				ExternalID:        "docs/payment.md",
				RevisionID:        "7f5a1dd",
				CanonicalURI:      "git://github.com/example/platform-docs/public/payment.md",
				Title:             "Payment Service Deployment",
				ParentDocumentID:  "doc:git:platform-docs:docs",
				DocumentType:      "runbook",
				Format:            "markdown",
				Language:          "en",
				Labels:            []string{"payments", "deployment"},
				OwnerRefs:         []OwnerRef{{Kind: "group", ID: "team:payments", DisplayName: "Payments"}},
				ACLSummary:        &ACLSummary{Visibility: "repository", ReaderGroups: []string{"platform"}},
				SourceMetadata:    map[string]string{"path": "docs/payment.md"},
				ContentHash:       "sha256:document-content",
				DocumentUpdatedAt: "2026-05-09T12:00:00Z",
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.payload.SourceID == "" {
				t.Fatal("SourceID is empty")
			}
			if tt.payload.DocumentID == "" {
				t.Fatal("DocumentID is empty")
			}
			if tt.payload.RevisionID == "" {
				t.Fatal("RevisionID is empty")
			}
			if tt.payload.DocumentType != "runbook" {
				t.Fatalf("DocumentType = %q, want runbook", tt.payload.DocumentType)
			}
			if len(tt.payload.OwnerRefs) != 1 {
				t.Fatalf("OwnerRefs len = %d, want 1", len(tt.payload.OwnerRefs))
			}
		})
	}
}

func TestDocumentationDocumentPayloadOmitMissingACLSummary(t *testing.T) {
	t.Parallel()

	payload := DocumentPayload{
		SourceID:   "doc-source:git:platform-docs",
		DocumentID: "doc:git:platform-docs:docs/payment.md",
		ExternalID: "docs/payment.md",
		RevisionID: "7f5a1dd",
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	if strings.Contains(string(encoded), "acl_summary") {
		t.Fatalf("payload JSON = %s, want missing acl_summary when ACL was not collected", encoded)
	}
}

func TestDocumentationSectionStableIDIgnoresMutableHeading(t *testing.T) {
	t.Parallel()

	first := SectionStableID(SectionPayload{
		DocumentID:       "doc:confluence:12345",
		RevisionID:       "17",
		SectionID:        "section:deployment",
		SectionAnchor:    "deployment",
		HeadingText:      "Deployment",
		OrdinalPath:      []int{2, 1},
		TextHash:         "sha256:section-text",
		ExcerptHash:      "sha256:bounded-excerpt",
		ParentSectionID:  "section:overview",
		SourceStartRef:   "block:10",
		SourceEndRef:     "block:12",
		SourceMetadata:   map[string]string{"source": "confluence"},
		ContainsWarnings: false,
	})
	second := SectionStableID(SectionPayload{
		DocumentID:      "doc:confluence:12345",
		RevisionID:      "17",
		SectionID:       "section:deployment",
		SectionAnchor:   "deployment",
		HeadingText:     "How The Payment Service Ships",
		OrdinalPath:     []int{2, 1},
		TextHash:        "sha256:section-text",
		ExcerptHash:     "sha256:bounded-excerpt",
		ParentSectionID: "section:overview",
		SourceStartRef:  "block:10",
		SourceEndRef:    "block:12",
		SourceMetadata:  map[string]string{"source": "confluence"},
	})

	if first == "" {
		t.Fatal("SectionStableID returned empty ID")
	}
	if first != second {
		t.Fatalf("stable ID changed after heading edit: first=%q second=%q", first, second)
	}
}

func TestDocumentationSectionStableIDIgnoresPersistedContent(t *testing.T) {
	t.Parallel()

	first := SectionStableID(SectionPayload{
		DocumentID:    "doc:confluence:12345",
		RevisionID:    "17",
		SectionID:     "body",
		SectionAnchor: "body",
		OrdinalPath:   []int{1},
		TextHash:      "sha256:section-text",
		ExcerptHash:   "sha256:bounded-excerpt",
		Content:       "<p>old body</p>",
		ContentFormat: "storage",
	})
	second := SectionStableID(SectionPayload{
		DocumentID:    "doc:confluence:12345",
		RevisionID:    "17",
		SectionID:     "body",
		SectionAnchor: "body",
		OrdinalPath:   []int{1},
		TextHash:      "sha256:section-text",
		ExcerptHash:   "sha256:bounded-excerpt",
		Content:       "<p>new body with same normalized hashes</p>",
		ContentFormat: "storage",
	})

	if first == "" {
		t.Fatal("SectionStableID returned empty ID")
	}
	if first != second {
		t.Fatalf("stable ID changed after persisted content edit: first=%q second=%q", first, second)
	}
}

func TestDocumentationLinkStableIDUsesDurableIdentity(t *testing.T) {
	t.Parallel()

	first := LinkStableID(LinkPayload{
		DocumentID:     "doc:confluence:12345",
		RevisionID:     "17",
		SectionID:      "section:deployment",
		LinkID:         "link:deployment-chart",
		TargetURI:      "https://github.com/example/platform-deployments/payment.yaml",
		TargetKind:     "git_file",
		AnchorTextHash: "sha256:deployment-link-text",
	})
	second := LinkStableID(LinkPayload{
		DocumentID:     "doc:confluence:12345",
		RevisionID:     "17",
		SectionID:      "section:deployment",
		LinkID:         "link:deployment-chart",
		TargetURI:      "https://github.com/example/platform-deployments/payment.yaml",
		TargetKind:     "source_file",
		AnchorTextHash: "sha256:renamed-link-text",
	})

	if first == "" {
		t.Fatal("LinkStableID returned empty ID")
	}
	if first != second {
		t.Fatalf("stable ID changed after link display metadata edit: first=%q second=%q", first, second)
	}
}

func TestDocumentationEntityMentionPayloadSupportsResolutionStates(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		resolution string
		candidates []EvidenceRef
	}{
		{
			name:       "exact",
			resolution: MentionResolutionExact,
			candidates: []EvidenceRef{{Kind: "service", ID: "service:payment-api", Confidence: "exact"}},
		},
		{
			name:       "ambiguous",
			resolution: MentionResolutionAmbiguous,
			candidates: []EvidenceRef{
				{Kind: "service", ID: "service:payment-api", Confidence: "derived"},
				{Kind: "service", ID: "service:payment-worker", Confidence: "derived"},
			},
		},
		{
			name:       "unmatched",
			resolution: MentionResolutionUnmatched,
			candidates: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload := EntityMentionPayload{
				DocumentID:       "doc:confluence:12345",
				SectionID:        "section:deployment",
				MentionID:        "mention:payment-api",
				MentionText:      "payment-api",
				MentionKind:      "service",
				ResolutionStatus: tt.resolution,
				CandidateRefs:    tt.candidates,
				ExcerptHash:      "sha256:bounded-excerpt",
			}

			if payload.ResolutionStatus != tt.resolution {
				t.Fatalf("ResolutionStatus = %q, want %q", payload.ResolutionStatus, tt.resolution)
			}
			if payload.ResolutionStatus == MentionResolutionExact && len(payload.CandidateRefs) != 1 {
				t.Fatalf("exact mention candidates len = %d, want 1", len(payload.CandidateRefs))
			}
		})
	}
}

func TestDocumentationClaimCandidateIsNonAuthoritativeEvidence(t *testing.T) {
	t.Parallel()

	payload := ClaimCandidatePayload{
		DocumentID:       "doc:confluence:12345",
		SectionID:        "section:deployment",
		ClaimID:          "claim:deployment:payment-api",
		ClaimType:        "service_deployment",
		ClaimText:        "payment-api deploys through the payment-prod Helm release.",
		ClaimHash:        "sha256:claim-text",
		ExcerptHash:      "sha256:bounded-excerpt",
		SubjectMentionID: "mention:payment-api",
		ObjectMentionIDs: []string{"mention:payment-prod"},
		EvidenceRefs: []EvidenceRef{
			{Kind: "document_section", ID: "section:deployment", Confidence: "observed"},
		},
		Authority: ClaimAuthorityDocumentEvidence,
	}

	if payload.Authority != ClaimAuthorityDocumentEvidence {
		t.Fatalf("Authority = %q, want %q", payload.Authority, ClaimAuthorityDocumentEvidence)
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	if strings.Contains(string(encoded), "source_confidence") {
		t.Fatalf("payload JSON = %s, want source_confidence to be envelope-only", encoded)
	}
	if payload.Authority == "operational_truth" {
		t.Fatal("documentation claim candidate must not be operational truth")
	}
	if !strings.Contains(string(encoded), `"excerpt_hash":"sha256:bounded-excerpt"`) {
		t.Fatalf("payload JSON = %s, want excerpt_hash for bounded source evidence", encoded)
	}
}

func TestDocumentationClaimCandidateStableIDIncludesExcerptHash(t *testing.T) {
	t.Parallel()

	first := ClaimCandidateStableID(ClaimCandidatePayload{
		DocumentID:  "doc:confluence:12345",
		RevisionID:  "17",
		SectionID:   "section:deployment",
		ClaimID:     "claim:deployment:payment-api",
		ClaimText:   "payment-api deploys through the payment-prod Helm release.",
		ClaimHash:   "sha256:claim-text",
		ExcerptHash: "sha256:bounded-excerpt-a",
	})
	second := ClaimCandidateStableID(ClaimCandidatePayload{
		DocumentID:  "doc:confluence:12345",
		RevisionID:  "17",
		SectionID:   "section:deployment",
		ClaimID:     "claim:deployment:payment-api",
		ClaimText:   "payment-api deploys through the payment-prod Helm release.",
		ClaimHash:   "sha256:claim-text",
		ExcerptHash: "sha256:bounded-excerpt-b",
	})

	if first == "" {
		t.Fatal("ClaimCandidateStableID returned empty ID")
	}
	if first == second {
		t.Fatalf("stable ID did not change across excerpt bounds: %q", first)
	}
}

func TestDocumentationFindingAndPacketStableIDsUseVersions(t *testing.T) {
	t.Parallel()

	firstFinding := FindingStableID("finding:service-deployment:1", "2026-05-09T19:00:00Z")
	secondFinding := FindingStableID("finding:service-deployment:1", "2026-05-09T20:00:00Z")
	if firstFinding == "" {
		t.Fatal("FindingStableID returned empty ID")
	}
	if firstFinding == secondFinding {
		t.Fatalf("finding stable ID did not change across versions: %q", firstFinding)
	}

	firstPacket := EvidencePacketStableID("doc-packet:service-deployment:1", "1")
	secondPacket := EvidencePacketStableID("doc-packet:service-deployment:1", "2")
	if firstPacket == "" {
		t.Fatal("EvidencePacketStableID returned empty ID")
	}
	if firstPacket == secondPacket {
		t.Fatalf("packet stable ID did not change across versions: %q", firstPacket)
	}
}

func TestDocumentationStableIDsUseDurableIdentity(t *testing.T) {
	t.Parallel()

	first := DocumentStableID(DocumentPayload{
		SourceID:   "doc-source:git:platform-docs",
		DocumentID: "doc:git:platform-docs:docs/payment.md",
		ExternalID: "docs/payment.md",
		RevisionID: "7f5a1dd",
		Title:      "Payment Service Deployment",
	})
	second := DocumentStableID(DocumentPayload{
		SourceID:   "doc-source:git:platform-docs",
		DocumentID: "doc:git:platform-docs:docs/payment.md",
		ExternalID: "docs/payment.md",
		RevisionID: "7f5a1dd",
		Title:      "Payments Runbook",
	})

	if first == "" {
		t.Fatal("DocumentStableID returned empty ID")
	}
	if first != second {
		t.Fatalf("stable ID changed after title edit: first=%q second=%q", first, second)
	}
}
