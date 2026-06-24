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

func TestStreamFactsEmitsLightweightTextDocumentationFormats(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "README.txt"), "Payment Service\n\nRun `eshu docs verify docs/public` before release.\n")
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "guide.rst"), "Guide\n=====\n\n.. include:: omitted.rst\n\nUsage\n-----\n\nSee https://docs.example.test/guide.\n")
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "manual.adoc"), "= Manual\n\nUse link:https://docs.example.test/manual[manual docs].\n")
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "tutorial.asciidoc"), "== Tutorial\n\nFollow the tutorial steps.\n")
	writeCollectorTestFile(t, filepath.Join(repoPath, "notebooks", "analysis.qmd"), "---\ntitle: Analysis\n---\n# Analysis\n\nSee [runbook](../README.txt).\n")

	observedAt := time.Date(2026, time.June, 9, 1, 0, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 5,
		DocumentationFileMetas: []ContentFileMeta{
			{RelativePath: "README.txt", Digest: "sha256:readme", Language: "text"},
			{RelativePath: "docs/guide.rst", Digest: "sha256:rst", Language: "restructuredtext"},
			{RelativePath: "docs/manual.adoc", Digest: "sha256:adoc", Language: "asciidoc"},
			{RelativePath: "docs/tutorial.asciidoc", Digest: "sha256:asciidoc", Language: "asciidoc"},
			{RelativePath: "notebooks/analysis.qmd", Digest: "sha256:qmd", Language: "quarto"},
		},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false)
	envelopes := drainFactChannel(collected.Facts)

	sourceFacts := factsByKind(envelopes, facts.DocumentationSourceFactKind)
	if got, want := len(sourceFacts), 1; got != want {
		t.Fatalf("documentation_source count = %d, want %d", got, want)
	}
	if got, want := payloadString(sourceFacts[0].Payload, "source_type"), "repository_documentation"; got != want {
		t.Fatalf("source_type = %q, want %q", got, want)
	}

	documentFacts := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documentFacts), 5; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	formats := map[string]bool{}
	for _, document := range documentFacts {
		formats[payloadString(document.Payload, "format")] = true
		assertDocumentationFactLinkedRepository(t, document, "repository:r_12345678")
	}
	for _, want := range []string{"text", "restructuredtext", "asciidoc", "quarto"} {
		if !formats[want] {
			t.Fatalf("missing document format %q in %#v", want, formats)
		}
	}

	sectionFacts := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	assertSectionHeadingPresent(t, sectionFacts, "Payment Service")
	assertSectionHeadingPresent(t, sectionFacts, "Guide")
	assertSectionHeadingPresent(t, sectionFacts, "Usage")
	assertSectionHeadingPresent(t, sectionFacts, "Manual")
	assertSectionHeadingPresent(t, sectionFacts, "Tutorial")
	assertSectionHeadingPresent(t, sectionFacts, "Analysis")
	if !hasWarningSection(sectionFacts, "unsupported_directive") {
		t.Fatalf("expected unsupported_directive warning in section metadata: %#v", sectionFacts)
	}

	linkFacts := factsByKind(envelopes, facts.DocumentationLinkFactKind)
	assertLinkTargetPresent(t, linkFacts, "https://docs.example.test/guide")
	assertLinkTargetPresent(t, linkFacts, "https://docs.example.test/manual")
	assertLinkTargetPresent(t, linkFacts, "../README.txt")

	claimFacts := factsByKind(envelopes, facts.DocumentationClaimCandidateFactKind)
	if got, want := len(claimFacts), 1; got != want {
		t.Fatalf("documentation_claim_candidate count = %d, want %d", got, want)
	}
	if got, want := payloadString(claimFacts[0].Payload, "claim_type"), "cli_command"; got != want {
		t.Fatalf("claim_type = %q, want %q", got, want)
	}
}

func TestStreamFactsEmitsHTMLDocumentationFacts(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "site", "index.html"), `<!doctype html>
<html>
  <head><style>.hidden { display: none; }</style><script>window.internal = "skip";</script></head>
  <body>
    <h1 id="intro">Reference</h1>
    <p>Visible intro with <a href="#usage">usage link</a>.</p>
    <h2 id="usage">Usage</h2>
    <table><tr><th>Metric</th><th>Target</th></tr><tr><td>SLO</td><td>99.9%</td></tr></table>
    <p>External <a href="https://docs.example.test/api">API docs</a>.</p>
  </body>
</html>`)
	writeCollectorTestFile(t, filepath.Join(repoPath, "site", "empty.htm"), "<html><body></body></html>")
	writeCollectorTestFile(t, filepath.Join(repoPath, "site", "generated", "broken.html"), "<html><body><h1>Generated Reference</h1><p Broken")

	observedAt := time.Date(2026, time.June, 9, 1, 30, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 3,
		DocumentationFileMetas: []ContentFileMeta{
			{RelativePath: "site/index.html", Digest: "sha256:index", Language: "html"},
			{RelativePath: "site/empty.htm", Digest: "sha256:empty", Language: "html"},
			{RelativePath: "site/generated/broken.html", Digest: "sha256:broken", Language: "html"},
		},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false)
	envelopes := drainFactChannel(collected.Facts)

	documentFacts := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documentFacts), 3; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	for _, document := range documentFacts {
		if got, want := payloadString(document.Payload, "format"), "html"; got != want {
			t.Fatalf("document format = %q, want %q", got, want)
		}
	}

	sectionFacts := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	reference := sectionByHeading(sectionFacts, "Reference")
	if reference == nil {
		t.Fatalf("missing Reference section in %#v", sectionFacts)
	}
	if content := payloadString(reference.Payload, "content"); strings.Contains(content, "window.internal") || strings.Contains(content, "display: none") {
		t.Fatalf("HTML section content includes script/style text: %q", content)
	}
	usage := sectionByHeading(sectionFacts, "Usage")
	if usage == nil {
		t.Fatalf("missing Usage section in %#v", sectionFacts)
	}
	if content := payloadString(usage.Payload, "content"); !strings.Contains(content, "SLO") || !strings.Contains(content, "99.9%") {
		t.Fatalf("Usage section content missing table text: %q", content)
	}
	if !hasWarningSection(sectionFacts, "malformed_html") {
		t.Fatalf("expected malformed_html warning in section metadata: %#v", sectionFacts)
	}

	linkFacts := factsByKind(envelopes, facts.DocumentationLinkFactKind)
	assertLinkTargetPresent(t, linkFacts, "#usage")
	assertLinkTargetPresent(t, linkFacts, "https://docs.example.test/api")
}

func TestNativeRepositorySnapshotterIncludesDocumentationMetasWithoutParsingDocs(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "app.py"), "def handler():\n    return 1\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "requirements.txt"), "requests==2.31.0\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "docs", "guide.rst"), "Guide\n=====\n\nUse the guide.\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "site", "index.html"), "<html><body><h1>Reference</h1></body></html>")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	got, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if got.FileCount != 4 {
		t.Fatalf("FileCount = %d, want 4", got.FileCount)
	}
	if gotParsedFilePathCount(got.FileData, "guide.rst") != 0 || gotParsedFilePathCount(got.FileData, "index.html") != 0 {
		t.Fatalf("documentation files were parsed as source files: %#v", got.FileData)
	}
	if gotParsedFilePathCount(got.FileData, "requirements.txt") != 1 {
		t.Fatalf("requirements.txt was not preserved as parser-supported source data: %#v", got.FileData)
	}
	if got, want := len(got.DocumentationFileMetas), 2; got != want {
		t.Fatalf("len(DocumentationFileMetas) = %d, want %d", got, want)
	}

	collected := buildStreamingGeneration(
		got.RepoPath,
		testCollectorRepositoryMetadata(got.RepoPath),
		"run-1",
		time.Date(2026, time.June, 9, 2, 0, 0, 0, time.UTC),
		got,
		false,
	)
	documentFacts := factsByKind(drainFactChannel(collected.Facts), facts.DocumentationDocumentFactKind)
	if got, want := len(documentFacts), 2; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
}

func TestNativeRepositorySnapshotterEmitsDocumentationOnlyRepository(t *testing.T) {
	t.Parallel()

	repoRoot := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoRoot, "README.txt"), "Docs Only\n\nUse these docs.\n")
	writeCollectorTestFile(t, filepath.Join(repoRoot, "site", "index.html"), "<html><body><h1>Reference</h1></body></html>")

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	got, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoRoot})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}

	if got.FileCount != 2 {
		t.Fatalf("FileCount = %d, want 2", got.FileCount)
	}
	if len(got.FileData) != 0 {
		t.Fatalf("len(FileData) = %d, want 0", len(got.FileData))
	}
	if got, want := len(got.DocumentationFileMetas), 2; got != want {
		t.Fatalf("len(DocumentationFileMetas) = %d, want %d", got, want)
	}

	collected := buildStreamingGeneration(
		got.RepoPath,
		testCollectorRepositoryMetadata(got.RepoPath),
		"run-1",
		time.Date(2026, time.June, 9, 2, 15, 0, 0, time.UTC),
		got,
		false,
	)
	documentFacts := factsByKind(drainFactChannel(collected.Facts), facts.DocumentationDocumentFactKind)
	if got, want := len(documentFacts), 2; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
}

func TestReadDocumentationBodyBoundsLargeDocumentationFile(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(
		t,
		filepath.Join(repoPath, "docs", "large.html"),
		strings.Repeat("x", documentationMaxBodyBytes+1024),
	)

	body, ok := readDocumentationBody(repoPath, "docs/large.html", nil)
	if !ok {
		t.Fatal("readDocumentationBody() ok = false, want true")
	}
	if got, want := len(body), documentationMaxBodyBytes+1; got != want {
		t.Fatalf("len(body) = %d, want %d", got, want)
	}
}

func TestGitDocumentationSourceURIAndFormatRejectsParserOnlyFiles(t *testing.T) {
	t.Parallel()

	if _, _, ok := gitDocumentationSourceURIAndFormat("app.py"); ok {
		t.Fatal("gitDocumentationSourceURIAndFormat(app.py) ok = true, want false")
	}
	sourceURI, format, ok := gitDocumentationSourceURIAndFormat("docs/guide.rst")
	if !ok {
		t.Fatal("gitDocumentationSourceURIAndFormat(docs/guide.rst) ok = false, want true")
	}
	if sourceURI != "docs/guide.rst" {
		t.Fatalf("sourceURI = %q, want %q", sourceURI, "docs/guide.rst")
	}
	if format.format != "restructuredtext" {
		t.Fatalf("format = %q, want %q", format.format, "restructuredtext")
	}
}

func assertSectionHeadingPresent(t *testing.T, sections []facts.Envelope, heading string) {
	t.Helper()
	if sectionByHeading(sections, heading) == nil {
		t.Fatalf("missing section heading %q in %#v", heading, sections)
	}
}

func sectionByHeading(sections []facts.Envelope, heading string) *facts.Envelope {
	for i := range sections {
		if payloadString(sections[i].Payload, "heading_text") == heading {
			return &sections[i]
		}
	}
	return nil
}

func assertLinkTargetPresent(t *testing.T, links []facts.Envelope, target string) {
	t.Helper()
	for _, link := range links {
		if payloadString(link.Payload, "target_uri") == target {
			return
		}
	}
	t.Fatalf("missing link target %q in %#v", target, links)
}

func hasWarningSection(sections []facts.Envelope, warning string) bool {
	for _, section := range sections {
		if hasPayloadBool(section.Payload, "contains_warnings") &&
			strings.Contains(payloadSourceMetadataValue(section.Payload, "warning"), warning) {
			return true
		}
	}
	return false
}

func hasPayloadBool(payload map[string]any, key string) bool {
	value, ok := payload[key]
	if !ok {
		return false
	}
	typed, ok := value.(bool)
	return ok && typed
}

func payloadSourceMetadataValue(payload map[string]any, key string) string {
	metadata, ok := payload["source_metadata"].(map[string]any)
	if !ok {
		return ""
	}
	value, ok := metadata[key].(string)
	if !ok {
		return ""
	}
	return value
}

func gotParsedFilePathCount(files []map[string]any, suffix string) int {
	count := 0
	for _, file := range files {
		if strings.HasSuffix(payloadString(file, "path"), suffix) {
			count++
		}
	}
	return count
}
