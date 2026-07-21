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

func TestStreamFactsEmitsNotebookNarrativeDocumentation(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "notebooks", "analysis.ipynb"), `{
  "cells": [
    {
      "cell_type": "markdown",
      "id": "intro",
      "source": ["# Analysis\n", "\n", "See [runbook](../README.md).\n"],
      "attachments": {
        "diagram.png": {"image/png": "base64-data"}
      }
    },
    {
      "cell_type": "raw",
      "id": "ops-note",
      "source": ["Operational note\n", "\n", "Keep source facts bounded.\n"]
    },
    {
      "cell_type": "code",
      "id": "training",
      "source": ["print(\"code cell source must not become docs\")\n"],
      "outputs": [
        {"output_type": "stream", "name": "stdout", "text": ["Training complete\n"]},
        {"output_type": "execute_result", "data": {"text/plain": ["accuracy: 0.98\n"]}},
        {"output_type": "display_data", "data": {"text/html": ["<b>rich</b>"]}}
      ]
    }
  ],
  "metadata": {},
  "nbformat": 4,
  "nbformat_minor": 5
}`)

	envelopes := streamNotebookFacts(t, repoPath, "notebooks/analysis.ipynb")
	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	document := documents[0]
	if got, want := payloadString(document.Payload, "format"), "notebook"; got != want {
		t.Fatalf("document format = %q, want %q", got, want)
	}
	if got, want := payloadString(document.Payload, "title"), "Analysis"; got != want {
		t.Fatalf("document title = %q, want %q", got, want)
	}
	assertPayloadWarning(t, document.Payload, "rich_output_omitted")

	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 4; got != want {
		t.Fatalf("documentation_section count = %d, want %d: %#v", got, want, sections)
	}
	assertNotebookSection(t, sections[0], "Analysis", "cell-0-analysis", "cell:0:line:1", "intro")
	assertNotebookSection(t, sections[1], "Raw cell 1", "cell-1-raw", "cell:1", "ops-note")
	assertNotebookSection(t, sections[2], "Code output 2.1", "cell-2-output-1", "cell:2:output:0", "training")
	assertNotebookSection(t, sections[3], "Code output 2.2", "cell-2-output-2", "cell:2:output:1", "training")

	allContent := notebookSectionContent(sections)
	for _, want := range []string{"See [runbook](../README.md).", "Operational note", "Training complete", "accuracy: 0.98"} {
		if !strings.Contains(allContent, want) {
			t.Fatalf("notebook documentation content missing %q in %q", want, allContent)
		}
	}
	if strings.Contains(allContent, "code cell source must not become docs") {
		t.Fatalf("notebook documentation duplicated code cell source: %q", allContent)
	}
	assertPayloadWarning(t, sections[0].Payload, "binary_attachment_omitted")
	if got, want := payloadSourceMetadataValue(sections[0].Payload, "attachment_count"), "1"; got != want {
		t.Fatalf("attachment_count = %q, want %q", got, want)
	}

	links := factsByKind(envelopes, facts.DocumentationLinkFactKind)
	assertLinkTargetPresent(t, links, "../README.md")
}

func TestStreamFactsEmitsEmptyAndMalformedNotebookDocuments(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "notebooks", "empty.ipynb"), `{"cells": [], "nbformat": 4}`)
	writeCollectorTestFile(t, filepath.Join(repoPath, "notebooks", "broken.ipynb"), `{"cells": [`)

	emptyEnvelopes := streamNotebookFacts(t, repoPath, "notebooks/empty.ipynb")
	emptyDocuments := factsByKind(emptyEnvelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(emptyDocuments), 1; got != want {
		t.Fatalf("empty notebook document count = %d, want %d", got, want)
	}
	assertPayloadWarning(t, emptyDocuments[0].Payload, "empty_notebook")
	if got, want := len(factsByKind(emptyEnvelopes, facts.DocumentationSectionFactKind)), 0; got != want {
		t.Fatalf("empty notebook section count = %d, want %d", got, want)
	}

	brokenEnvelopes := streamNotebookFacts(t, repoPath, "notebooks/broken.ipynb")
	brokenDocuments := factsByKind(brokenEnvelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(brokenDocuments), 1; got != want {
		t.Fatalf("malformed notebook document count = %d, want %d", got, want)
	}
	assertPayloadWarning(t, brokenDocuments[0].Payload, "malformed_notebook")
	if got, want := len(factsByKind(brokenEnvelopes, facts.DocumentationSectionFactKind)), 0; got != want {
		t.Fatalf("malformed notebook section count = %d, want %d", got, want)
	}
}

func TestStreamFactsBoundsLargeNotebookTextOutput(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	largeOutput := strings.Repeat("x", documentationMaxSectionChars+1024)
	writeCollectorTestFile(t, filepath.Join(repoPath, "notebooks", "large-output.ipynb"), `{
  "cells": [{
    "cell_type": "code",
    "id": "oversized",
    "source": ["print('skip source')\n"],
    "outputs": [{"output_type": "execute_result", "data": {"text/plain": `+quoteJSONStringArrayItem(largeOutput)+`}}]
  }],
  "nbformat": 4
}`)

	envelopes := streamNotebookFacts(t, repoPath, "notebooks/large-output.ipynb")
	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 1; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	content := payloadString(sections[0].Payload, "content")
	if len([]rune(content)) > documentationMaxSectionChars {
		t.Fatalf("output content length = %d, want <= %d", len([]rune(content)), documentationMaxSectionChars)
	}
	assertPayloadWarning(t, sections[0].Payload, "output_truncated")
}

func TestStreamFactsParsesNotebookLargerThanGenericDocumentationLimit(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	filler := strings.Repeat("x", documentationMaxBodyBytes+1024)
	writeCollectorTestFile(t, filepath.Join(repoPath, "notebooks", "large.ipynb"), `{
  "cells": [
    {"cell_type": "markdown", "id": "intro", "source": ["# Large Notebook\n", "Narrative survives.\n"]}
  ],
  "metadata": {"padding": "`+filler+`"},
  "nbformat": 4
}`)

	envelopes := streamNotebookFacts(t, repoPath, "notebooks/large.ipynb")
	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 1; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	if got, want := payloadString(sections[0].Payload, "heading_text"), "Large Notebook"; got != want {
		t.Fatalf("section heading = %q, want %q", got, want)
	}
	if content := payloadString(sections[0].Payload, "content"); !strings.Contains(content, "Narrative survives.") {
		t.Fatalf("large notebook content missing narrative: %q", content)
	}
}

func TestStreamFactsOmitsNotebookStderrOutput(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "notebooks", "stderr.ipynb"), `{
  "cells": [{
    "cell_type": "code",
    "id": "run",
    "source": ["raise RuntimeError('skip source')\n"],
    "outputs": [
      {"output_type": "stream", "name": "stderr", "text": ["Traceback details\n"]},
      {"output_type": "stream", "name": "stdout", "text": ["safe summary\n"]}
    ]
  }],
  "nbformat": 4
}`)

	envelopes := streamNotebookFacts(t, repoPath, "notebooks/stderr.ipynb")
	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertPayloadWarning(t, documents[0].Payload, "stderr_output_omitted")

	sections := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 1; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	content := notebookSectionContent(sections)
	if strings.Contains(content, "Traceback details") {
		t.Fatalf("stderr stream leaked into documentation content: %q", content)
	}
	if !strings.Contains(content, "safe summary") {
		t.Fatalf("stdout stream missing from documentation content: %q", content)
	}
}

func TestNotebookDocumentationStableIDsAndParserSupport(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "analysis.ipynb"), `{
  "cells": [
    {"cell_type": "markdown", "id": "same", "source": ["# Same\n", "Narrative.\n"]},
    {"cell_type": "code", "source": ["class NotebookGreeter:\n", "    pass\n"], "outputs": []},
    {"cell_type": "markdown", "source": ["# Same\n", "More narrative.\n"]}
  ],
  "nbformat": 4
}`)

	first := streamNotebookFacts(t, repoPath, "analysis.ipynb")
	second := streamNotebookFacts(t, repoPath, "analysis.ipynb")
	if got, want := notebookDocumentationFactKeys(first), notebookDocumentationFactKeys(second); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("notebook documentation fact keys changed:\nfirst=%#v\nsecond=%#v", got, want)
	}

	sections := factsByKind(first, facts.DocumentationSectionFactKind)
	if got, want := len(sections), 2; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	if got, want := payloadString(sections[0].Payload, "section_anchor"), "cell-0-same"; got != want {
		t.Fatalf("first section anchor = %q, want %q", got, want)
	}
	if got, want := payloadString(sections[1].Payload, "section_anchor"), "cell-2-same"; got != want {
		t.Fatalf("second section anchor = %q, want %q", got, want)
	}

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoPath})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}
	if gotParsedFilePathCount(snapshot.FileData, "analysis.ipynb") != 1 {
		t.Fatalf("analysis.ipynb was not preserved as parser-supported source data: %#v", snapshot.FileData)
	}
	if got, want := len(snapshot.DocumentationFileMetas), 1; got != want {
		t.Fatalf("len(DocumentationFileMetas) = %d, want %d", got, want)
	}

	collected := buildStreamingGeneration(
		snapshot.RepoPath,
		testCollectorRepositoryMetadata(snapshot.RepoPath),
		"run-1",
		time.Date(2026, time.June, 9, 3, 30, 0, 0, time.UTC),
		snapshot,
		false,
		"",
	)
	envelopes := drainFactChannel(collected.Facts)
	if got, want := len(factsByKind(envelopes, facts.DocumentationDocumentFactKind)), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	if got, want := len(factsByKind(envelopes, facts.DocumentationSectionFactKind)), 2; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
}

func TestNativeSnapshotMalformedNotebookStillEmitsDocumentationWarning(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "broken.ipynb"), `{"cells": [`)

	engine, err := parser.DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}
	snapshotter := NativeRepositorySnapshotter{Engine: engine}
	snapshot, err := snapshotter.SnapshotRepository(context.Background(), SelectedRepository{RepoPath: repoPath})
	if err != nil {
		t.Fatalf("SnapshotRepository() error = %v, want nil", err)
	}
	if gotParsedFilePathCount(snapshot.FileData, "broken.ipynb") != 0 {
		t.Fatalf("malformed notebook was unexpectedly parsed as source data: %#v", snapshot.FileData)
	}
	if got, want := len(snapshot.DocumentationFileMetas), 1; got != want {
		t.Fatalf("len(DocumentationFileMetas) = %d, want %d", got, want)
	}

	collected := buildStreamingGeneration(
		snapshot.RepoPath,
		testCollectorRepositoryMetadata(snapshot.RepoPath),
		"run-1",
		time.Date(2026, time.June, 9, 4, 0, 0, 0, time.UTC),
		snapshot,
		false,
		"",
	)
	envelopes := drainFactChannel(collected.Facts)
	documents := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documents), 1; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	assertPayloadWarning(t, documents[0].Payload, "malformed_notebook")
	if got, want := len(factsByKind(envelopes, facts.DocumentationSectionFactKind)), 0; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
}

func streamNotebookFacts(t *testing.T, repoPath string, relativePath string) []facts.Envelope {
	t.Helper()

	collected := buildStreamingGeneration(
		repoPath,
		testCollectorRepositoryMetadata(repoPath),
		"run-1",
		time.Date(2026, time.June, 9, 3, 0, 0, 0, time.UTC),
		RepositorySnapshot{
			FileCount: 1,
			ContentFileMetas: []ContentFileMeta{{
				RelativePath: relativePath,
				Digest:       "sha256:notebook",
				Language:     "python",
				ArtifactType: "source",
				CommitSHA:    "abc123",
			}},
		},
		false,
		"",
	)
	return drainFactChannel(collected.Facts)
}

func assertNotebookSection(t *testing.T, envelope facts.Envelope, heading string, anchor string, startRef string, cellID string) {
	t.Helper()
	if got := payloadString(envelope.Payload, "heading_text"); got != heading {
		t.Fatalf("section heading = %q, want %q", got, heading)
	}
	if got := payloadString(envelope.Payload, "section_anchor"); got != anchor {
		t.Fatalf("section anchor = %q, want %q", got, anchor)
	}
	if got := payloadString(envelope.Payload, "source_start_ref"); got != startRef {
		t.Fatalf("source_start_ref = %q, want %q", got, startRef)
	}
	if got := payloadSourceMetadataValue(envelope.Payload, "cell_id"); got != cellID {
		t.Fatalf("cell_id = %q, want %q", got, cellID)
	}
}

func assertPayloadWarning(t *testing.T, payload map[string]any, warning string) {
	t.Helper()
	if !strings.Contains(payloadSourceMetadataValue(payload, "warning"), warning) {
		t.Fatalf("payload warning missing %q in %#v", warning, payload["source_metadata"])
	}
}

func notebookSectionContent(sections []facts.Envelope) string {
	var parts []string
	for _, section := range sections {
		parts = append(parts, payloadString(section.Payload, "content"))
	}
	return strings.Join(parts, "\n")
}

func notebookDocumentationFactKeys(envelopes []facts.Envelope) []string {
	keys := []string{}
	for _, envelope := range envelopes {
		switch envelope.FactKind {
		case facts.DocumentationDocumentFactKind, facts.DocumentationSectionFactKind, facts.DocumentationLinkFactKind:
			keys = append(keys, envelope.StableFactKey)
		}
	}
	return keys
}

func quoteJSONStringArrayItem(value string) string {
	escaped := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(value)
	return `["` + escaped + `"]`
}
