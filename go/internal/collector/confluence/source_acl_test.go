// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package confluence

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// TestSourceEmitsPartialSourceACLStateOnContentFacts asserts that Confluence
// content/evidence facts carry the bounded source_acl_state. Confluence reads
// are credential-viewable but per-source and per-page restrictions are not
// collected, so the read is incomplete and stays partial (fail closed; never
// upgraded to allowed).
func TestSourceEmitsPartialSourceACLStateOnContentFacts(t *testing.T) {
	t.Parallel()

	observedAt := time.Date(2026, time.May, 9, 12, 0, 0, 0, time.UTC)
	client := &fakeClient{
		space: Space{
			ID:    "100",
			Key:   "PLAT",
			Name:  "Platform",
			Links: Links{Base: "https://example.atlassian.net/wiki"},
		},
		spacePages: []Page{
			confluencePage("123", "Payment Service Deployment", 17, `<p>Body.</p>`),
		},
	}
	source := Source{
		Client: client,
		Config: SourceConfig{
			BaseURL:  "https://example.atlassian.net/wiki",
			SpaceID:  "100",
			SpaceKey: "PLAT",
			Now:      func() time.Time { return observedAt },
		},
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	envelopes := drainFacts(t, collected.Facts)

	sourceFact := factsByKind(envelopes, facts.DocumentationSourceFactKind)[0]
	sourceACL := payloadMap(sourceFact.Payload, "acl_summary")
	if got, want := payloadString(sourceACL, "source_acl_state"), facts.SourceACLStatePartial; got != want {
		t.Fatalf("source fact source_acl_state = %q, want %q", got, want)
	}

	documentFact := factsByKind(envelopes, facts.DocumentationDocumentFactKind)[0]
	documentACL := payloadMap(documentFact.Payload, "acl_summary")
	if got, want := payloadString(documentACL, "source_acl_state"), facts.SourceACLStatePartial; got != want {
		t.Fatalf("document fact source_acl_state = %q, want %q", got, want)
	}
}

// TestSourcePropagatesPartialSourceACLStateOntoTruthEvidence proves the bounded
// source_acl_state observed on the Confluence document is propagated verbatim
// onto the derived mention and claim evidence facts (#2178). This is the
// collector end of the #1901 evidence path: the merged reducer projection and
// query surfacing read payload.acl_summary.source_acl_state from these facts, so
// propagating the document posture here carries it end-to-end with no further
// change. The posture stays partial (fail closed; never upgraded to allowed).
func TestSourcePropagatesPartialSourceACLStateOntoTruthEvidence(t *testing.T) {
	t.Parallel()

	client := &fakeClient{
		space: Space{ID: "100", Key: "PLAT", Name: "Platform"},
		spacePages: []Page{
			confluencePage("123", "Payment Service Deployment", 17, `<p>payment-api deploys from <a href="https://github.com/example/platform-deployments">deployments</a>.</p>`),
		},
	}
	source := Source{
		Client: client,
		Config: SourceConfig{
			BaseURL: "https://example.atlassian.net/wiki",
			SpaceID: "100",
			Now:     fixedNow,
		},
		TruthExtractor: doctruth.NewExtractor([]doctruth.Entity{
			{Kind: "service", ID: "service:payment-api", Aliases: []string{"payment-api"}},
		}, doctruth.Options{}),
		TruthClaimHints: func(_ Page, _ facts.DocumentationSectionPayload) []doctruth.ClaimHint {
			return []doctruth.ClaimHint{{
				ClaimID:     "claim:payment-api:deployment",
				ClaimType:   "service_deployment",
				ClaimText:   "payment-api deploys from deployments.",
				SubjectText: "payment-api",
				SubjectKind: "service",
			}}
		},
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v, want nil", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	envelopes := drainFacts(t, collected.Facts)

	for _, mention := range factsByKind(envelopes, facts.DocumentationEntityMentionFactKind) {
		mentionACL := payloadMap(mention.Payload, "acl_summary")
		if got, want := payloadString(mentionACL, "source_acl_state"), facts.SourceACLStatePartial; got != want {
			t.Fatalf("mention fact source_acl_state = %q, want %q (verbatim from document)", got, want)
		}
	}

	claims := factsByKind(envelopes, facts.DocumentationClaimCandidateFactKind)
	if len(claims) == 0 {
		t.Fatal("expected at least one claim candidate fact")
	}
	for _, claim := range claims {
		claimACL := payloadMap(claim.Payload, "acl_summary")
		if got, want := payloadString(claimACL, "source_acl_state"), facts.SourceACLStatePartial; got != want {
			t.Fatalf("claim fact source_acl_state = %q, want %q (verbatim from document)", got, want)
		}
	}
}
