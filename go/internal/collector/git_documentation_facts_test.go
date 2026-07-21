// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestStreamFactsEmitsGitMarkdownDocumentationFacts(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	body := `---
owner: platform
---
# Payment Service

The payment service is deployed from [deployment docs](docs/deploy.md).

` + "```bash\nkubectl get deploy\n```\n" + `
## Rollback

Run the rollback checklist.
`
	writeCollectorTestFile(t, filepath.Join(repoPath, "README.md"), body)

	observedAt := time.Date(2026, time.June, 8, 23, 30, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: "README.md",
			Digest:       "sha256:readme",
			Language:     "markdown",
			CommitSHA:    "abc123",
		}},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	sourceFacts := factsByKind(envelopes, facts.DocumentationSourceFactKind)
	if got, want := len(sourceFacts), 1; got != want {
		t.Fatalf("documentation_source count = %d, want %d", got, want)
	}
	if got, want := sourceFacts[0].Payload["source_system"], "git"; got != want {
		t.Fatalf("source_system = %#v, want %#v", got, want)
	}
	if got, want := sourceFacts[0].Payload["source_type"], "repository_documentation"; got != want {
		t.Fatalf("source_type = %#v, want %#v", got, want)
	}

	documentFacts := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documentFacts), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	document := documentFacts[0]
	if got, want := document.Payload["document_id"], "doc:git:repository:r_12345678:README.md"; got != want {
		t.Fatalf("document_id = %#v, want %#v", got, want)
	}
	if got, want := document.Payload["title"], "Payment Service"; got != want {
		t.Fatalf("title = %#v, want %#v", got, want)
	}
	if got, want := document.Payload["format"], "markdown"; got != want {
		t.Fatalf("format = %#v, want %#v", got, want)
	}
	assertDocumentationFactLinkedRepository(t, document, "repository:r_12345678")
	if got, want := document.SourceRef.SourceURI, filepath.Join(repoPath, "README.md"); got != want {
		t.Fatalf("document SourceURI = %q, want %q", got, want)
	}

	sectionFacts := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sectionFacts), 2; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	if got, want := sectionFacts[0].Payload["heading_text"], "Payment Service"; got != want {
		t.Fatalf("first section heading = %#v, want %#v", got, want)
	}
	if got, want := sectionFacts[0].Payload["section_anchor"], "payment-service"; got != want {
		t.Fatalf("first section anchor = %#v, want %#v", got, want)
	}
	assertDocumentationFactLinkedRepository(t, sectionFacts[0], "repository:r_12345678")
	if content := sectionFacts[0].Payload["content"].(string); strings.Contains(content, "kubectl get deploy") {
		t.Fatalf("first section content includes fenced code block: %q", content)
	}
	if got, want := sectionFacts[1].Payload["parent_section_id"], sectionFacts[0].Payload["section_id"]; got != want {
		t.Fatalf("rollback parent_section_id = %#v, want %#v", got, want)
	}

	linkFacts := factsByKind(envelopes, facts.DocumentationLinkFactKind)
	if got, want := len(linkFacts), 1; got != want {
		t.Fatalf("documentation_link count = %d, want %d", got, want)
	}
	if got, want := linkFacts[0].Payload["target_uri"], "docs/deploy.md"; got != want {
		t.Fatalf("target_uri = %#v, want %#v", got, want)
	}
	assertDocumentationFactLinkedRepository(t, linkFacts[0], "repository:r_12345678")
}

func TestStreamFactsEmitsDocumentationTruthMentionsAndClaimCandidates(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	body := "# Runtime\n\nRun `eshu docs verify docs/public` before publishing.\n" +
		"Set ESHU_SERVICE_URL for remote reads.\n" +
		"Use GET /api/v0/documentation/facts for collected facts.\n"
	writeCollectorTestFile(t, filepath.Join(repoPath, "runbooks", "docs.md"), body)

	observedAt := time.Date(2026, time.June, 9, 0, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: "runbooks/docs.md",
			Digest:       "sha256:docs",
			Language:     "markdown",
			CommitSHA:    "def456",
		}},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	mentionFacts := factsByKind(envelopes, facts.DocumentationEntityMentionFactKind)
	if got, want := len(mentionFacts), 1; got != want {
		t.Fatalf("documentation_entity_mention count = %d, want %d", got, want)
	}
	if got, want := mentionFacts[0].SourceConfidence, facts.SourceConfidenceDerived; got != want {
		t.Fatalf("mention SourceConfidence = %q, want %q", got, want)
	}
	assertDocumentationFactLinkedRepository(t, mentionFacts[0], "repository:r_12345678")

	claimFacts := factsByKind(envelopes, facts.DocumentationClaimCandidateFactKind)
	if got, want := len(claimFacts), 3; got != want {
		t.Fatalf("documentation_claim_candidate count = %d, want %d", got, want)
	}
	claimTypes := map[string]bool{}
	for _, claim := range claimFacts {
		claimTypes[payloadString(claim.Payload, "claim_type")] = true
		if got, want := payloadString(claim.Payload, "authority"), facts.DocumentationClaimAuthorityDocumentEvidence; got != want {
			t.Fatalf("claim authority = %q, want %q", got, want)
		}
		assertDocumentationFactLinkedRepository(t, claim, "repository:r_12345678")
	}
	for _, want := range []string{"cli_command", "environment_variable", "http_endpoint"} {
		if !claimTypes[want] {
			t.Fatalf("missing claim type %q in %#v", want, claimTypes)
		}
	}
}

func TestStreamFactsEmitsDocumentationDocumentsForMarkdownFamily(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "adr-0001.markdown"), "# ADR 0001\n\nUse facts first.\n")
	writeCollectorTestFile(t, filepath.Join(repoPath, "runbooks", "deploy.mdx"), "Deploy without heading.\n")
	writeCollectorTestFile(t, filepath.Join(repoPath, "notes", "empty.md"), "")

	observedAt := time.Date(2026, time.June, 8, 23, 45, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 3,
		ContentFileMetas: []ContentFileMeta{
			{RelativePath: "docs/adr-0001.markdown", Digest: "sha256:adr", Language: "markdown"},
			{RelativePath: "runbooks/deploy.mdx", Digest: "sha256:mdx", Language: "markdown"},
			{RelativePath: "notes/empty.md", Digest: "sha256:empty", Language: "markdown"},
		},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	documentFacts := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documentFacts), 3; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	for _, envelope := range documentFacts {
		if strings.Contains(payloadString(envelope.Payload, "canonical_uri"), "/blob/sha256:") {
			t.Fatalf("canonical_uri used digest as GitHub blob revision: %#v", envelope.Payload["canonical_uri"])
		}
	}
	sectionFacts := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sectionFacts), 2; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	for _, envelope := range append(documentFacts, sectionFacts...) {
		if got, want := envelope.CollectorKind, "git"; got != want {
			t.Fatalf("%s CollectorKind = %q, want %q", envelope.FactKind, got, want)
		}
		if got, want := envelope.SourceConfidence, facts.SourceConfidenceObserved; got != want {
			t.Fatalf("%s SourceConfidence = %q, want %q", envelope.FactKind, got, want)
		}
	}
}

func TestStreamFactsDisambiguatesDuplicateMarkdownHeadingAnchors(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "runbook.md"), "# Operate\n\n## Step\n\nOne.\n\n## Step\n\nTwo.\n")

	observedAt := time.Date(2026, time.June, 9, 0, 15, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 1,
		ContentFileMetas: []ContentFileMeta{{
			RelativePath: "docs/runbook.md",
			Digest:       "sha256:runbook",
			Language:     "markdown",
		}},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false, "")
	sections := factsByKind(drainFactChannel(collected.Facts), facts.DocumentationSectionFactKind)
	if got, want := len(sections), 3; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	anchors := []string{
		payloadString(sections[0].Payload, "section_anchor"),
		payloadString(sections[1].Payload, "section_anchor"),
		payloadString(sections[2].Payload, "section_anchor"),
	}
	if got, want := strings.Join(anchors, ","), "operate,step,step-2"; got != want {
		t.Fatalf("section anchors = %q, want %q", got, want)
	}
	if got, want := payloadString(sections[2].Payload, "parent_section_id"), payloadString(sections[0].Payload, "section_id"); got != want {
		t.Fatalf("duplicate heading parent_section_id = %q, want %q", got, want)
	}
}

func assertDocumentationFactLinkedRepository(t *testing.T, envelope facts.Envelope, repoID string) {
	t.Helper()

	switch linked := envelope.Payload["linked_entities"].(type) {
	case []any:
		for _, raw := range linked {
			item, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if item["entity_type"] == "repository" && item["entity_id"] == repoID {
				return
			}
		}
	case []map[string]string:
		for _, item := range linked {
			if item["entity_type"] == "repository" && item["entity_id"] == repoID {
				return
			}
		}
	default:
		t.Fatalf("%s linked_entities = %#v, want array", envelope.FactKind, envelope.Payload["linked_entities"])
	}
	t.Fatalf("%s linked_entities = %#v, want repository %q", envelope.FactKind, envelope.Payload["linked_entities"], repoID)
}
