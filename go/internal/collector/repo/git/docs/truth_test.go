// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docs

import (
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	factsdocs "github.com/eshu-hq/eshu/go/internal/facts/docs"
	"github.com/eshu-hq/eshu/go/internal/repositoryidentity"
)

func TestGitDocumentationLinksForSection(t *testing.T) {
	t.Parallel()

	links := []factsdocs.LinkPayload{
		{SectionID: "section:a"},
		{SectionID: "section:b"},
		{SectionID: "section:a"},
	}
	got := gitDocumentationLinksForSection(links, "section:a")
	if len(got) != 2 {
		t.Fatalf("links = %v, want the two section:a entries", got)
	}
	if len(gitDocumentationLinksForSection(nil, "section:a")) != 0 {
		t.Fatal("nil links yield non-empty result")
	}
}

func TestGitDocumentationAttachLinkedRepository(t *testing.T) {
	t.Parallel()

	envelope := facts.Envelope{}
	gitDocumentationAttachLinkedRepository(&envelope, "repository:x")
	linked, ok := envelope.Payload["linked_entities"].([]map[string]string)
	if !ok || len(linked) != 1 || linked[0]["entity_type"] != "repository" || linked[0]["entity_id"] != "repository:x" {
		t.Fatalf("payload = %v, want repository link", envelope.Payload)
	}

	envelope = facts.Envelope{Payload: map[string]any{"keep": "me"}}
	gitDocumentationAttachLinkedRepository(&envelope, "repository:y")
	if envelope.Payload["keep"] != "me" {
		t.Fatalf("payload = %v, want existing keys preserved", envelope.Payload)
	}
}

func TestGitDocumentationRepositoryEntity(t *testing.T) {
	t.Parallel()

	entity := gitDocumentationRepositoryEntity(repositoryidentity.Metadata{
		ID:        "repository:x",
		Name:      "X",
		RepoSlug:  "o/x",
		RemoteURL: "https://github.com/o/x",
	})
	if entity.Kind != "repository" || entity.DisplayName != "X" {
		t.Fatalf("entity = %+v, want repository/X", entity)
	}
	if len(entity.URIs) != 1 || entity.URIs[0] != "https://github.com/o/x" {
		t.Fatalf("uris = %v", entity.URIs)
	}

	nameless := repositoryidentity.Metadata{ID: "repository:x", RepoSlug: "o/x"}
	if got := gitDocumentationRepositoryEntity(nameless).DisplayName; got != "o/x" {
		t.Fatalf("display name = %q, want slug fallback", got)
	}
	if got := gitDocumentationTruthSubject(repositoryidentity.Metadata{ID: "repository:x"}); got != "repository:x" {
		t.Fatalf("subject = %q, want id fallback", got)
	}
}

func TestGitDocumentationTruthEnvelopes(t *testing.T) {
	t.Parallel()

	repo := testDocRepo()
	observedAt := time.Now()

	t.Run("no sections means no envelopes", func(t *testing.T) {
		t.Parallel()

		document := factsdocs.DocumentPayload{DocumentID: "doc:1", CanonicalURI: "git://test/notes.md"}
		if got := gitDocumentationTruthEnvelopes(repo, "scope", "gen", observedAt, document, nil, nil); len(got) != 0 {
			t.Fatalf("envelopes = %d, want 0", len(got))
		}
	})

	t.Run("every envelope links the repository", func(t *testing.T) {
		t.Parallel()

		document := factsdocs.DocumentPayload{DocumentID: "doc:1", CanonicalURI: "git://test/notes.md"}
		sections := []factsdocs.SectionPayload{{
			DocumentID: "doc:1",
			RevisionID: "c",
			SectionID:  "section:body",
			Content:    "The Test repository ships a collector that parses markdown.",
		}}
		envelopes := gitDocumentationTruthEnvelopes(repo, "scope", "gen", observedAt, document, sections, nil)
		for _, envelope := range envelopes {
			linked, ok := envelope.Payload["linked_entities"].([]map[string]string)
			if !ok || len(linked) == 0 || linked[0]["entity_id"] != repo.ID {
				t.Fatalf("envelope payload = %v, want repository link", envelope.Payload)
			}
		}
	})
}
