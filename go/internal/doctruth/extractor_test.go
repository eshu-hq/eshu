// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package doctruth_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/facts/docs"
)

func TestExtractorResolvesExactEntityMentionFromServiceName(t *testing.T) {
	t.Parallel()

	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{
			Kind:    "service",
			ID:      "service:payment-api",
			Aliases: []string{"payment-api"},
		},
	}, doctruth.Options{})

	result, err := extractor.Extract(context.Background(), baseSectionInput("payment-api owns customer payment authorization."))
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	mention := onlyPayload[docs.EntityMentionPayload](t, result.Envelopes, docs.EntityMentionFactKind)
	if got, want := mention.MentionText, "payment-api"; got != want {
		t.Fatalf("MentionText = %q, want %q", got, want)
	}
	if got, want := mention.ResolutionStatus, docs.MentionResolutionExact; got != want {
		t.Fatalf("ResolutionStatus = %q, want %q", got, want)
	}
	if got, want := len(mention.CandidateRefs), 1; got != want {
		t.Fatalf("CandidateRefs len = %d, want %d", got, want)
	}
	if got, want := mention.CandidateRefs[0].ID, "service:payment-api"; got != want {
		t.Fatalf("CandidateRefs[0].ID = %q, want %q", got, want)
	}
}

func TestExtractorEmitsAmbiguityAndSuppressesClaims(t *testing.T) {
	t.Parallel()

	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{Kind: "service", ID: "service:payments-api", Aliases: []string{"payments"}},
		{Kind: "service", ID: "service:payments-worker", Aliases: []string{"payments"}},
	}, doctruth.Options{})
	section := baseSectionInput("payments uses the shared checkout database.")
	section.ClaimHints = []doctruth.ClaimHint{{
		ClaimID:     "claim:payments:database",
		ClaimType:   "service_dependency",
		ClaimText:   "payments uses the shared checkout database.",
		SubjectText: "payments",
		SubjectKind: "service",
	}}

	result, err := extractor.Extract(context.Background(), section)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	mention := onlyPayload[docs.EntityMentionPayload](t, result.Envelopes, docs.EntityMentionFactKind)
	if got, want := mention.ResolutionStatus, docs.MentionResolutionAmbiguous; got != want {
		t.Fatalf("ResolutionStatus = %q, want %q", got, want)
	}
	if got, want := len(mention.CandidateRefs), 2; got != want {
		t.Fatalf("CandidateRefs len = %d, want %d", got, want)
	}
	if got := countKind(result.Envelopes, docs.ClaimCandidateFactKind); got != 0 {
		t.Fatalf("claim candidates = %d, want 0 for ambiguous subject", got)
	}
	if got, want := result.Report.ClaimsSuppressedAmbiguous, 1; got != want {
		t.Fatalf("ClaimsSuppressedAmbiguous = %d, want %d", got, want)
	}
}

func TestExtractorResolvesEntityMentionFromLinkURI(t *testing.T) {
	t.Parallel()

	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{
			Kind: "repository",
			ID:   "repo:platform-deployments",
			URIs: []string{"https://github.com/example/platform-deployments"},
		},
	}, doctruth.Options{})
	section := baseSectionInput("Deployment manifests live in the platform deployments repo.")
	section.Links = []docs.LinkPayload{{
		DocumentID: section.DocumentID,
		RevisionID: section.RevisionID,
		SectionID:  section.SectionID,
		LinkID:     "link:platform-deployments",
		TargetURI:  "https://github.com/example/platform-deployments",
	}}

	result, err := extractor.Extract(context.Background(), section)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	mention := onlyPayload[docs.EntityMentionPayload](t, result.Envelopes, docs.EntityMentionFactKind)
	if got, want := mention.ResolutionStatus, docs.MentionResolutionExact; got != want {
		t.Fatalf("ResolutionStatus = %q, want %q", got, want)
	}
	if got, want := mention.CandidateRefs[0].ID, "repo:platform-deployments"; got != want {
		t.Fatalf("CandidateRefs[0].ID = %q, want %q", got, want)
	}
}

func TestExtractorResolvesEntityMentionFromCodePath(t *testing.T) {
	t.Parallel()

	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{
			Kind:      "workload",
			ID:        "workload:payment-worker",
			CodePaths: []string{"services/payment-worker/main.go"},
		},
	}, doctruth.Options{})
	section := baseSectionInput("The worker entrypoint is services/payment-worker/main.go.")

	result, err := extractor.Extract(context.Background(), section)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	mention := onlyPayload[docs.EntityMentionPayload](t, result.Envelopes, docs.EntityMentionFactKind)
	if got, want := mention.ResolutionStatus, docs.MentionResolutionExact; got != want {
		t.Fatalf("ResolutionStatus = %q, want %q", got, want)
	}
	if got, want := mention.CandidateRefs[0].ID, "workload:payment-worker"; got != want {
		t.Fatalf("CandidateRefs[0].ID = %q, want %q", got, want)
	}
}

func TestExtractorEmitsUnmatchedMentionForDeterministicHint(t *testing.T) {
	t.Parallel()

	extractor := doctruth.NewExtractor(nil, doctruth.Options{})
	section := baseSectionInput("The section metadata names ghost-api.")
	section.MentionHints = []doctruth.MentionHint{{
		Text: "ghost-api",
		Kind: "service",
		From: doctruth.MentionHintStructuredSection,
	}}

	result, err := extractor.Extract(context.Background(), section)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	mention := onlyPayload[docs.EntityMentionPayload](t, result.Envelopes, docs.EntityMentionFactKind)
	if got, want := mention.ResolutionStatus, docs.MentionResolutionUnmatched; got != want {
		t.Fatalf("ResolutionStatus = %q, want %q", got, want)
	}
	if got := len(mention.CandidateRefs); got != 0 {
		t.Fatalf("CandidateRefs len = %d, want 0", got)
	}
	if got := countKind(result.Envelopes, docs.ClaimCandidateFactKind); got != 0 {
		t.Fatalf("claim candidates = %d, want 0 for unmatched subject", got)
	}
}

func TestExtractorClaimCandidateRetainsSectionProvenanceAndExcerptHash(t *testing.T) {
	t.Parallel()

	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{Kind: "service", ID: "service:payment-api", Aliases: []string{"payment-api"}},
	}, doctruth.Options{})
	section := baseSectionInput("payment-api deploys through the payment-prod Helm release.")
	section.ClaimHints = []doctruth.ClaimHint{{
		ClaimID:     "claim:deployment:payment-api",
		ClaimType:   "service_deployment",
		ClaimText:   "payment-api deploys through the payment-prod Helm release.",
		SubjectText: "payment-api",
		SubjectKind: "service",
	}}

	result, err := extractor.Extract(context.Background(), section)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	claim := onlyPayload[docs.ClaimCandidatePayload](t, result.Envelopes, docs.ClaimCandidateFactKind)
	if got, want := claim.DocumentID, section.DocumentID; got != want {
		t.Fatalf("DocumentID = %q, want %q", got, want)
	}
	if got, want := claim.RevisionID, section.RevisionID; got != want {
		t.Fatalf("RevisionID = %q, want %q", got, want)
	}
	if got, want := claim.SectionID, section.SectionID; got != want {
		t.Fatalf("SectionID = %q, want %q", got, want)
	}
	if got, want := claim.ExcerptHash, section.ExcerptHash; got != want {
		t.Fatalf("ExcerptHash = %q, want %q", got, want)
	}
	if got, want := claim.Authority, docs.ClaimAuthorityDocumentEvidence; got != want {
		t.Fatalf("Authority = %q, want %q", got, want)
	}
	if got, want := len(claim.EvidenceRefs), 1; got != want {
		t.Fatalf("EvidenceRefs len = %d, want %d", got, want)
	}
	if got, want := claim.EvidenceRefs[0].ID, section.SectionID; got != want {
		t.Fatalf("EvidenceRefs[0].ID = %q, want %q", got, want)
	}
}

func TestExtractorPopulatesExactObjectMentionIDs(t *testing.T) {
	t.Parallel()

	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{Kind: "service", ID: "service:payment-api", Aliases: []string{"payment-api"}},
		{Kind: "workload", ID: "workload:payment-prod", Aliases: []string{"payment-prod"}},
	}, doctruth.Options{})
	section := baseSectionInput("payment-api deploys through the payment-prod Helm release.")
	section.ClaimHints = []doctruth.ClaimHint{{
		ClaimID:     "claim:deployment:payment-api",
		ClaimType:   "service_deployment",
		ClaimText:   "payment-api deploys through the payment-prod Helm release.",
		SubjectText: "payment-api",
		SubjectKind: "service",
		ObjectMentions: []doctruth.MentionHint{{
			Text: "payment-prod",
			Kind: "workload",
		}},
	}}

	result, err := extractor.Extract(context.Background(), section)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	claim := onlyPayload[docs.ClaimCandidatePayload](t, result.Envelopes, docs.ClaimCandidateFactKind)
	if got, want := len(claim.ObjectMentionIDs), 1; got != want {
		t.Fatalf("ObjectMentionIDs len = %d, want %d", got, want)
	}
}

func TestExtractorSuppressesClaimWhenObjectMentionIsAmbiguous(t *testing.T) {
	t.Parallel()

	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{Kind: "service", ID: "service:payment-api", Aliases: []string{"payment-api"}},
		{Kind: "workload", ID: "workload:payment-prod-blue", Aliases: []string{"payment-prod"}},
		{Kind: "workload", ID: "workload:payment-prod-green", Aliases: []string{"payment-prod"}},
	}, doctruth.Options{})
	section := baseSectionInput("payment-api deploys through the payment-prod Helm release.")
	section.ClaimHints = []doctruth.ClaimHint{{
		ClaimID:     "claim:deployment:payment-api",
		ClaimType:   "service_deployment",
		ClaimText:   "payment-api deploys through the payment-prod Helm release.",
		SubjectText: "payment-api",
		SubjectKind: "service",
		ObjectMentions: []doctruth.MentionHint{{
			Text: "payment-prod",
			Kind: "workload",
		}},
	}}

	result, err := extractor.Extract(context.Background(), section)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	if got := countKind(result.Envelopes, docs.ClaimCandidateFactKind); got != 0 {
		t.Fatalf("claim candidates = %d, want 0 for ambiguous object mention", got)
	}
	if got, want := result.Report.ClaimsSuppressedAmbiguous, 1; got != want {
		t.Fatalf("ClaimsSuppressedAmbiguous = %d, want %d", got, want)
	}
}

func TestExtractorRequiresRevisionIDForProvenance(t *testing.T) {
	t.Parallel()

	extractor := doctruth.NewExtractor(nil, doctruth.Options{})
	section := baseSectionInput("payment-api")
	section.RevisionID = ""

	if _, err := extractor.Extract(context.Background(), section); err == nil {
		t.Fatal("Extract() error = nil, want missing revision error")
	}
}

func TestExtractorPreservesCaseSensitiveURIPath(t *testing.T) {
	t.Parallel()

	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{
			Kind: "repository",
			ID:   "repo:case-sensitive",
			URIs: []string{"https://github.com/example/CaseSensitive"},
		},
	}, doctruth.Options{})
	section := baseSectionInput("See linked repository.")
	section.Links = []docs.LinkPayload{{
		DocumentID: section.DocumentID,
		RevisionID: section.RevisionID,
		SectionID:  section.SectionID,
		LinkID:     "link:wrong-case",
		TargetURI:  "https://github.com/example/casesensitive",
	}}

	result, err := extractor.Extract(context.Background(), section)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	if got := countKind(result.Envelopes, docs.EntityMentionFactKind); got != 0 {
		t.Fatalf("entity mentions = %d, want 0 for case-distinct URI path", got)
	}
}

func TestExtractorPreservesTrailingSlashURIPath(t *testing.T) {
	t.Parallel()

	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{
			Kind: "repository",
			ID:   "repo:with-slash",
			URIs: []string{"https://github.com/example/repo/"},
		},
	}, doctruth.Options{})
	section := baseSectionInput("See linked repository.")
	section.Links = []docs.LinkPayload{{
		DocumentID: section.DocumentID,
		RevisionID: section.RevisionID,
		SectionID:  section.SectionID,
		LinkID:     "link:no-slash",
		TargetURI:  "https://github.com/example/repo",
	}}

	result, err := extractor.Extract(context.Background(), section)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	if got := countKind(result.Envelopes, docs.EntityMentionFactKind); got != 0 {
		t.Fatalf("entity mentions = %d, want 0 for path trailing-slash mismatch", got)
	}
}

func TestExtractorPreservesTrailingSlashQueryValue(t *testing.T) {
	t.Parallel()

	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{
			Kind: "repository",
			ID:   "repo:query-slash",
			URIs: []string{"https://github.com/example/repo?path=docs/"},
		},
	}, doctruth.Options{})
	section := baseSectionInput("See linked repository.")
	section.Links = []docs.LinkPayload{{
		DocumentID: section.DocumentID,
		RevisionID: section.RevisionID,
		SectionID:  section.SectionID,
		LinkID:     "link:query-no-slash",
		TargetURI:  "https://github.com/example/repo?path=docs",
	}}

	result, err := extractor.Extract(context.Background(), section)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	if got := countKind(result.Envelopes, docs.EntityMentionFactKind); got != 0 {
		t.Fatalf("entity mentions = %d, want 0 for query trailing-slash mismatch", got)
	}
}

func TestExtractorProtectsReservedProvenanceMetadata(t *testing.T) {
	t.Parallel()

	extractor := doctruth.NewExtractor([]doctruth.Entity{
		{Kind: "service", ID: "service:payment-api", Aliases: []string{"payment-api"}},
	}, doctruth.Options{})
	section := baseSectionInput("payment-api owns customer payment authorization.")
	section.ClaimHints = []doctruth.ClaimHint{{
		ClaimID:     "claim:payment-api",
		ClaimType:   "service_ownership",
		ClaimText:   "payment-api owns customer payment authorization.",
		SubjectText: "payment-api",
		SubjectKind: "service",
		SourceMetadata: map[string]string{
			"source_start_ref": "tampered",
			"source_end_ref":   "tampered",
		},
	}}

	result, err := extractor.Extract(context.Background(), section)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}

	claim := onlyPayload[docs.ClaimCandidatePayload](t, result.Envelopes, docs.ClaimCandidateFactKind)
	if got, want := claim.SourceMetadata["source_start_ref"], section.SourceStartRef; got != want {
		t.Fatalf("source_start_ref = %q, want %q", got, want)
	}
	if got, want := claim.SourceMetadata["source_end_ref"], section.SourceEndRef; got != want {
		t.Fatalf("source_end_ref = %q, want %q", got, want)
	}
	if got, want := claim.SourceMetadata["hint.source_start_ref"], "tampered"; got != want {
		t.Fatalf("hint.source_start_ref = %q, want %q", got, want)
	}
}

func baseSectionInput(text string) doctruth.SectionInput {
	return doctruth.SectionInput{
		ScopeID:        "doc-source:confluence:platform",
		GenerationID:   "generation:2026-05-09",
		SourceSystem:   "confluence",
		DocumentID:     "doc:confluence:12345",
		RevisionID:     "17",
		SectionID:      "section:deployment",
		CanonicalURI:   "https://example.atlassian.net/wiki/spaces/PLAT/pages/12345",
		ExcerptHash:    "sha256:bounded-excerpt",
		SourceStartRef: "block:10",
		SourceEndRef:   "block:12",
		Text:           text,
		ObservedAt:     time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC),
	}
}

func countKind(envelopes []facts.Envelope, kind string) int {
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			count++
		}
	}
	return count
}

func onlyPayload[T any](t *testing.T, envelopes []facts.Envelope, kind string) T {
	t.Helper()

	var matches []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			matches = append(matches, envelope)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("%s envelopes len = %d, want 1", kind, len(matches))
	}
	encoded, err := json.Marshal(matches[0].Payload)
	if err != nil {
		t.Fatalf("json.Marshal(%s payload) error = %v, want nil", kind, err)
	}
	var out T
	if err := json.Unmarshal(encoded, &out); err != nil {
		t.Fatalf("json.Unmarshal(%s payload) error = %v, want nil", kind, err)
	}
	return out
}
