package facts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDocumentationDocumentPayloadIsSourceNeutral(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		payload DocumentationDocumentPayload
	}{
		{
			name: "confluence page",
			payload: DocumentationDocumentPayload{
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
				OwnerRefs:         []DocumentationOwnerRef{{Kind: "group", ID: "team:payments", DisplayName: "Payments"}},
				ACLSummary:        &DocumentationACLSummary{Visibility: "restricted", ReaderGroups: []string{"platform"}},
				SourceMetadata:    map[string]string{"space_key": "PLAT"},
				ContentHash:       "sha256:document-content",
				DocumentUpdatedAt: "2026-05-09T12:00:00Z",
			},
		},
		{
			name: "git markdown document",
			payload: DocumentationDocumentPayload{
				SourceID:          "doc-source:git:platform-docs",
				DocumentID:        "doc:git:platform-docs:docs/payment.md",
				ExternalID:        "docs/payment.md",
				RevisionID:        "7f5a1dd",
				CanonicalURI:      "git://github.com/example/platform-docs/docs/payment.md",
				Title:             "Payment Service Deployment",
				ParentDocumentID:  "doc:git:platform-docs:docs",
				DocumentType:      "runbook",
				Format:            "markdown",
				Language:          "en",
				Labels:            []string{"payments", "deployment"},
				OwnerRefs:         []DocumentationOwnerRef{{Kind: "group", ID: "team:payments", DisplayName: "Payments"}},
				ACLSummary:        &DocumentationACLSummary{Visibility: "repository", ReaderGroups: []string{"platform"}},
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

	payload := DocumentationDocumentPayload{
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

	first := DocumentationSectionStableID(DocumentationSectionPayload{
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
	second := DocumentationSectionStableID(DocumentationSectionPayload{
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
		t.Fatal("DocumentationSectionStableID returned empty ID")
	}
	if first != second {
		t.Fatalf("stable ID changed after heading edit: first=%q second=%q", first, second)
	}
}

func TestDocumentationLinkStableIDUsesDurableIdentity(t *testing.T) {
	t.Parallel()

	first := DocumentationLinkStableID(DocumentationLinkPayload{
		DocumentID:     "doc:confluence:12345",
		RevisionID:     "17",
		SectionID:      "section:deployment",
		LinkID:         "link:deployment-chart",
		TargetURI:      "https://github.com/example/platform-deployments/payment.yaml",
		TargetKind:     "git_file",
		AnchorTextHash: "sha256:deployment-link-text",
	})
	second := DocumentationLinkStableID(DocumentationLinkPayload{
		DocumentID:     "doc:confluence:12345",
		RevisionID:     "17",
		SectionID:      "section:deployment",
		LinkID:         "link:deployment-chart",
		TargetURI:      "https://github.com/example/platform-deployments/payment.yaml",
		TargetKind:     "source_file",
		AnchorTextHash: "sha256:renamed-link-text",
	})

	if first == "" {
		t.Fatal("DocumentationLinkStableID returned empty ID")
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
		candidates []DocumentationEvidenceRef
	}{
		{
			name:       "exact",
			resolution: DocumentationMentionResolutionExact,
			candidates: []DocumentationEvidenceRef{{Kind: "service", ID: "service:payment-api", Confidence: "exact"}},
		},
		{
			name:       "ambiguous",
			resolution: DocumentationMentionResolutionAmbiguous,
			candidates: []DocumentationEvidenceRef{
				{Kind: "service", ID: "service:payment-api", Confidence: "derived"},
				{Kind: "service", ID: "service:payment-worker", Confidence: "derived"},
			},
		},
		{
			name:       "unmatched",
			resolution: DocumentationMentionResolutionUnmatched,
			candidates: nil,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload := DocumentationEntityMentionPayload{
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
			if payload.ResolutionStatus == DocumentationMentionResolutionExact && len(payload.CandidateRefs) != 1 {
				t.Fatalf("exact mention candidates len = %d, want 1", len(payload.CandidateRefs))
			}
		})
	}
}

func TestDocumentationClaimCandidateIsNonAuthoritativeEvidence(t *testing.T) {
	t.Parallel()

	payload := DocumentationClaimCandidatePayload{
		DocumentID:       "doc:confluence:12345",
		SectionID:        "section:deployment",
		ClaimID:          "claim:deployment:payment-api",
		ClaimType:        "service_deployment",
		ClaimText:        "payment-api deploys through the payment-prod Helm release.",
		ClaimHash:        "sha256:claim-text",
		SubjectMentionID: "mention:payment-api",
		ObjectMentionIDs: []string{"mention:payment-prod"},
		EvidenceRefs: []DocumentationEvidenceRef{
			{Kind: "document_section", ID: "section:deployment", Confidence: "observed"},
		},
		Authority: DocumentationClaimAuthorityDocumentEvidence,
	}

	if payload.Authority != DocumentationClaimAuthorityDocumentEvidence {
		t.Fatalf("Authority = %q, want %q", payload.Authority, DocumentationClaimAuthorityDocumentEvidence)
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
}

func TestDocumentationStableIDsUseDurableIdentity(t *testing.T) {
	t.Parallel()

	first := DocumentationDocumentStableID(DocumentationDocumentPayload{
		SourceID:   "doc-source:git:platform-docs",
		DocumentID: "doc:git:platform-docs:docs/payment.md",
		ExternalID: "docs/payment.md",
		RevisionID: "7f5a1dd",
		Title:      "Payment Service Deployment",
	})
	second := DocumentationDocumentStableID(DocumentationDocumentPayload{
		SourceID:   "doc-source:git:platform-docs",
		DocumentID: "doc:git:platform-docs:docs/payment.md",
		ExternalID: "docs/payment.md",
		RevisionID: "7f5a1dd",
		Title:      "Payments Runbook",
	})

	if first == "" {
		t.Fatal("DocumentationDocumentStableID returned empty ID")
	}
	if first != second {
		t.Fatalf("stable ID changed after title edit: first=%q second=%q", first, second)
	}
}
