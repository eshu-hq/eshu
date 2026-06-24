// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/parser"
)

func TestTextDiagramDocumentationFormatsAreDocumentationFiles(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	relativePaths := []string{
		"docs/architecture.mmd",
		"docs/architecture.mermaid",
		"docs/service-map.d2",
	}
	files := make([]string, 0, len(relativePaths))
	for _, relativePath := range relativePaths {
		file := filepath.Join(repoPath, filepath.FromSlash(relativePath))
		writeCollectorTestFile(t, file, "flowchart LR\n  docs[Documentation] --> api[API]\n")
		files = append(files, file)
	}

	parserFiles, documentationFiles := partitionNativeSnapshotFiles(files, parser.Registry{})
	if got := len(parserFiles); got != 0 {
		t.Fatalf("parserFiles len = %d, want 0: %#v", got, parserFiles)
	}
	if got, want := len(documentationFiles), len(relativePaths); got != want {
		t.Fatalf("documentationFiles len = %d, want %d: %#v", got, want, documentationFiles)
	}
	metas := documentationFileMetasForPaths(repoPath, files, "commit")
	if got, want := len(metas), len(relativePaths); got != want {
		t.Fatalf("documentationFileMetasForPaths len = %d, want %d: %#v", got, want, metas)
	}
	for _, relativePath := range relativePaths {
		if _, _, ok := gitDocumentationSourceURIAndFormat(relativePath); !ok {
			t.Fatalf("gitDocumentationSourceURIAndFormat(%q) ok = false, want true", relativePath)
		}
	}
}

func TestStreamFactsEmitsTextDiagramDocumentationFactsAfterPreflight(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "architecture.mmd"), `flowchart LR
  repo[Repository Docs] --> api[Documentation API]
  repo --> env[ESHU_SERVICE_URL]
  repo --> runbook[Runbook]
  click runbook "docs/runbook.md" "Runbook docs"
  click repo "../private.md" "Private parent"
  click api "/etc/passwd" "Local absolute"
`)
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "service-map.d2"), `docs: Documentation Facts
api: API Readback
api -> docs: facts route
api.link: docs/service-map.md
`)

	observedAt := time.Date(2026, time.June, 9, 5, 30, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 2,
		DocumentationFileMetas: []ContentFileMeta{
			{RelativePath: "docs/architecture.mmd", Digest: "sha256:mmd", Language: "mermaid", CommitSHA: "abc123"},
			{RelativePath: "docs/service-map.d2", Digest: "sha256:d2", Language: "d2", CommitSHA: "abc123"},
		},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false)
	envelopes := drainFactChannel(collected.Facts)

	documentFacts := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documentFacts), 2; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	formats := map[string]bool{}
	for _, document := range documentFacts {
		formats[payloadString(document.Payload, "format")] = true
		if got, want := payloadString(document.Payload, "document_type"), "diagram"; got != want {
			t.Fatalf("document_type = %q, want %q", got, want)
		}
		if got, want := payloadSourceMetadataValue(document.Payload, "incident_media_source_class"), "diagram_label"; got != want {
			t.Fatalf("document incident_media_source_class = %q, want %q", got, want)
		}
		assertDocumentationFactLinkedRepository(t, document, "repository:r_12345678")
	}
	for _, want := range []string{"mermaid", "d2"} {
		if !formats[want] {
			t.Fatalf("missing diagram document format %q in %#v", want, formats)
		}
	}

	sectionFacts := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sectionFacts), 2; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	assertSectionContentContains(t, sectionFacts, "Repository Docs")
	assertSectionContentContains(t, sectionFacts, "API Readback")
	for _, section := range sectionFacts {
		if got := payloadString(section.Payload, "content_format"); got != "mermaid" && got != "d2" {
			t.Fatalf("section content_format = %q, want mermaid or d2", got)
		}
		if got, want := payloadSourceMetadataValue(section.Payload, "format_family"), "diagram"; got != want {
			t.Fatalf("section format_family = %q, want %q", got, want)
		}
		if got, want := payloadSourceMetadataValue(section.Payload, "incident_media_source_class"), "diagram_label"; got != want {
			t.Fatalf("section incident_media_source_class = %q, want %q", got, want)
		}
		assertDocumentationFactLinkedRepository(t, section, "repository:r_12345678")
	}

	linkFacts := factsByKind(envelopes, facts.DocumentationLinkFactKind)
	if got, want := len(linkFacts), 2; got != want {
		t.Fatalf("documentation_link count = %d, want %d: %#v", got, want, linkFacts)
	}
	assertLinkTargetPresent(t, linkFacts, "docs/runbook.md")
	assertLinkTargetPresent(t, linkFacts, "docs/service-map.md")
	assertLinkTargetAbsent(t, linkFacts, "../private.md")
	assertLinkTargetAbsent(t, linkFacts, "/etc/passwd")
	if got := len(factsByKind(envelopes, facts.DocumentationEntityMentionFactKind)); got != 0 {
		t.Fatalf("documentation_entity_mention count = %d, want 0 for text diagrams", got)
	}
	if got := len(factsByKind(envelopes, facts.DocumentationClaimCandidateFactKind)); got != 0 {
		t.Fatalf("documentation_claim_candidate count = %d, want 0 for text diagrams", got)
	}
}

func TestTextDiagramDocumentationUnsafePreflightSuppressesContent(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "include.mmd"), `flowchart LR
  source[Source] --> target[Target]
  %% !include https://example.invalid/private.mmd
`)
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "secret.d2"), "api_key = \"redacted\"\n")
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "huge.mmd"), strings.Repeat("A-->B\n", 10005))
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "broken.mmd"), "this is not a diagram\n")
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "truncated.mmd"),
		"flowchart LR\n  docs[Documentation] --> api[API]\n"+strings.Repeat("A", documentationMaxBodyBytes+1))

	observedAt := time.Date(2026, time.June, 9, 5, 45, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 5,
		DocumentationFileMetas: []ContentFileMeta{
			{RelativePath: "docs/include.mmd", Digest: "sha256:include", Language: "mermaid"},
			{RelativePath: "docs/secret.d2", Digest: "sha256:secret", Language: "d2"},
			{RelativePath: "docs/huge.mmd", Digest: "sha256:huge", Language: "mermaid"},
			{RelativePath: "docs/broken.mmd", Digest: "sha256:broken", Language: "mermaid"},
			{RelativePath: "docs/truncated.mmd", Digest: "sha256:truncated", Language: "mermaid"},
		},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false)
	envelopes := drainFactChannel(collected.Facts)

	documentFacts := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documentFacts), 5; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	wantWarnings := map[string]string{
		"docs/include.mmd":   "unsupported_remote_include",
		"docs/secret.d2":     "sensitive_value_redacted",
		"docs/huge.mmd":      "resource_limit_exceeded",
		"docs/broken.mmd":    "malformed_media",
		"docs/truncated.mmd": "resource_limit_exceeded",
	}
	for _, document := range documentFacts {
		path := payloadSourceMetadataValue(document.Payload, "path")
		warning := payloadSourceMetadataValue(document.Payload, "warning")
		if !strings.Contains(warning, wantWarnings[path]) {
			t.Fatalf("document %q warning = %q, want %q", path, warning, wantWarnings[path])
		}
		if strings.Contains(warning, "example.invalid") || strings.Contains(warning, "api_key") {
			t.Fatalf("document %q warning leaks sensitive value: %q", path, warning)
		}
	}
	if got := len(factsByKind(envelopes, facts.DocumentationSectionFactKind)); got != 0 {
		t.Fatalf("documentation_section count = %d, want 0 for unsafe diagrams", got)
	}
	if got := len(factsByKind(envelopes, facts.DocumentationLinkFactKind)); got != 0 {
		t.Fatalf("documentation_link count = %d, want 0 for unsafe diagrams", got)
	}
}

func TestTextDiagramDocumentationCanceledPreflightSuppressesContent(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	repo := testCollectorRepositoryMetadata(repoPath)
	body := []byte("flowchart LR\n  docs[Documentation] --> api[API]\n")
	observedAt := time.Date(2026, time.June, 9, 6, 0, 0, 0, time.UTC)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	envelopes, _ := gitDocumentationEnvelopesForContentFile(
		ctx,
		repoPath,
		repo,
		"scope:r",
		"generation:r",
		observedAt,
		"docs/canceled.mmd",
		"sha256:canceled",
		"abc123",
		body,
		false,
	)

	documentFacts := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documentFacts), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	if warning := payloadSourceMetadataValue(documentFacts[0].Payload, "warning"); !strings.Contains(warning, "timeout") {
		t.Fatalf("warning = %q, want timeout", warning)
	}
	if got := len(factsByKind(envelopes, facts.DocumentationSectionFactKind)); got != 0 {
		t.Fatalf("documentation_section count = %d, want 0 for canceled preflight", got)
	}
	if got := len(factsByKind(envelopes, facts.DocumentationLinkFactKind)); got != 0 {
		t.Fatalf("documentation_link count = %d, want 0 for canceled preflight", got)
	}
}

func assertSectionContentContains(t *testing.T, sections []facts.Envelope, text string) {
	t.Helper()
	for _, section := range sections {
		if strings.Contains(payloadString(section.Payload, "content"), text) {
			return
		}
	}
	t.Fatalf("missing section content %q in %#v", text, sections)
}

func assertLinkTargetAbsent(t *testing.T, links []facts.Envelope, target string) {
	t.Helper()
	for _, link := range links {
		if payloadString(link.Payload, "target_uri") == target {
			t.Fatalf("unexpected link target %q in %#v", target, links)
		}
	}
}
