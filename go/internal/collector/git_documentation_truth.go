// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

const gitDocumentationRepositoryEntityKind = "repository"

func gitDocumentationTruthEnvelopes(
	repo repositoryidentity.Metadata,
	scopeID string,
	generationID string,
	observedAt time.Time,
	document facts.DocumentationDocumentPayload,
	sections []facts.DocumentationSectionPayload,
	links []facts.DocumentationLinkPayload,
) []facts.Envelope {
	extractor := doctruth.NewExtractor([]doctruth.Entity{gitDocumentationRepositoryEntity(repo)}, doctruth.Options{})
	subjectText := gitDocumentationTruthSubject(repo)
	out := []facts.Envelope{}
	for _, section := range sections {
		claimHints := doctruth.MarkdownClaimHints(subjectText, gitDocumentationRepositoryEntityKind, section.Content)
		mentionHints := []doctruth.MentionHint{}
		if len(claimHints) > 0 {
			mentionHints = append(mentionHints, doctruth.MentionHint{
				Text: subjectText,
				Kind: gitDocumentationRepositoryEntityKind,
				From: doctruth.MentionHintStructuredSection,
			})
		}
		result, err := extractor.Extract(context.Background(), doctruth.SectionInput{
			ScopeID:        scopeID,
			GenerationID:   generationID,
			SourceSystem:   "git",
			DocumentID:     section.DocumentID,
			RevisionID:     section.RevisionID,
			SectionID:      section.SectionID,
			CanonicalURI:   document.CanonicalURI,
			ExcerptHash:    section.ExcerptHash,
			SourceStartRef: section.SourceStartRef,
			SourceEndRef:   section.SourceEndRef,
			Text:           section.Content,
			Links:          gitDocumentationLinksForSection(links, section.SectionID),
			MentionHints:   mentionHints,
			ClaimHints:     claimHints,
			ObservedAt:     observedAt,
			SourceACLState: facts.BoundedSourceACLState(document.ACLSummary),
		})
		if err != nil {
			continue
		}
		for i := range result.Envelopes {
			gitDocumentationAttachLinkedRepository(&result.Envelopes[i], repo.ID)
		}
		out = append(out, result.Envelopes...)
	}
	return out
}

func gitDocumentationRepositoryEntity(repo repositoryidentity.Metadata) doctruth.Entity {
	return doctruth.Entity{
		Kind:        gitDocumentationRepositoryEntityKind,
		ID:          repo.ID,
		DisplayName: firstNonEmptyString(repo.Name, repo.RepoSlug, repo.ID),
		Aliases: []string{
			repo.ID,
			strings.TrimPrefix(repo.ID, "repository:"),
			repo.Name,
			repo.RepoSlug,
		},
		URIs: []string{repo.RemoteURL},
	}
}

func gitDocumentationTruthSubject(repo repositoryidentity.Metadata) string {
	return firstNonEmptyString(repo.Name, repo.RepoSlug, repo.ID)
}

func gitDocumentationLinksForSection(
	links []facts.DocumentationLinkPayload,
	sectionID string,
) []facts.DocumentationLinkPayload {
	out := make([]facts.DocumentationLinkPayload, 0, len(links))
	for _, link := range links {
		if link.SectionID == sectionID {
			out = append(out, link)
		}
	}
	return out
}

func gitDocumentationAttachLinkedRepository(envelope *facts.Envelope, repoID string) {
	if envelope.Payload == nil {
		envelope.Payload = map[string]any{}
	}
	envelope.Payload["linked_entities"] = []map[string]string{{
		"entity_type": gitDocumentationRepositoryEntityKind,
		"entity_id":   repoID,
	}}
}
