// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestStreamFactsEmitsStructuredDiagramDocumentationFactsAfterPreflight(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "components.puml"), `@startuml
component "API Gateway" as api
database "Fact Store" as pg
api --> pg : writes documentation facts
note right of api
[[docs/plantuml-runbook.md PlantUML Runbook]]
end note
@enduml
`)
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "architecture.drawio"), `<mxfile>
  <diagram name="Page-1">
    <mxGraphModel>
      <root>
        <mxCell id="2" value="Documentation API" vertex="1" parent="1"/>
        <mxCell id="3" value="Fact Store" link="docs/drawio-facts.md" vertex="1" parent="1"/>
        <mxCell id="4" value="writes facts" edge="1" source="2" target="3"/>
      </root>
    </mxGraphModel>
  </diagram>
</mxfile>`)
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "scene.excalidraw"), `{
  "type": "excalidraw",
  "elements": [
    {"id": "a", "type": "text", "text": "Documentation dashboard"},
    {"id": "b", "type": "text", "text": "Fact readback", "link": "docs/excalidraw-readback.md"}
  ]
}`)
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "architecture.svg"), `<svg xmlns="http://www.w3.org/2000/svg">
  <text>Documentation Graph</text>
  <text>tenant.example.invalid token=secret-marker</text>
  <a href="docs/svg-runbook.md"><text>SVG Runbook</text></a>
</svg>`)

	observedAt := time.Date(2026, time.June, 9, 7, 30, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 4,
		DocumentationFileMetas: []ContentFileMeta{
			{RelativePath: "docs/components.puml", Digest: "sha256:puml", Language: "plantuml", CommitSHA: "abc123"},
			{RelativePath: "docs/architecture.drawio", Digest: "sha256:drawio", Language: "drawio", CommitSHA: "abc123"},
			{RelativePath: "docs/scene.excalidraw", Digest: "sha256:excalidraw", Language: "excalidraw", CommitSHA: "abc123"},
			{RelativePath: "docs/architecture.svg", Digest: "sha256:svg", Language: "svg", CommitSHA: "abc123"},
		},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	documentFacts := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documentFacts), 4; got != want {
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
	for _, want := range []string{"plantuml", "drawio", "excalidraw", "svg"} {
		if !formats[want] {
			t.Fatalf("missing diagram document format %q in %#v", want, formats)
		}
	}

	sectionFacts := factsByKind(envelopes, facts.DocumentationSectionFactKind)
	if got, want := len(sectionFacts), 4; got != want {
		t.Fatalf("documentation_section count = %d, want %d", got, want)
	}
	for _, want := range []string{
		"API Gateway",
		"Documentation API",
		"Documentation dashboard",
		"Documentation Graph",
		"SVG Runbook",
	} {
		assertSectionContentContains(t, sectionFacts, want)
	}
	for _, section := range sectionFacts {
		if got, want := payloadSourceMetadataValue(section.Payload, "format_family"), "diagram"; got != want {
			t.Fatalf("section format_family = %q, want %q", got, want)
		}
		if got, want := payloadSourceMetadataValue(section.Payload, "incident_media_source_class"), "diagram_label"; got != want {
			t.Fatalf("section incident_media_source_class = %q, want %q", got, want)
		}
		assertDocumentationFactLinkedRepository(t, section, "repository:r_12345678")
	}

	linkFacts := factsByKind(envelopes, facts.DocumentationLinkFactKind)
	if got, want := len(linkFacts), 4; got != want {
		t.Fatalf("documentation_link count = %d, want %d: %#v", got, want, linkFacts)
	}
	assertLinkTargetPresent(t, linkFacts, "docs/plantuml-runbook.md")
	assertLinkTargetPresent(t, linkFacts, "docs/drawio-facts.md")
	assertLinkTargetPresent(t, linkFacts, "docs/excalidraw-readback.md")
	assertLinkTargetPresent(t, linkFacts, "docs/svg-runbook.md")
	assertStructuredDiagramFactsDoNotLeak(t, envelopes, "tenant.example.invalid", "token=secret-marker")
	if got := len(factsByKind(envelopes, facts.DocumentationEntityMentionFactKind)); got != 0 {
		t.Fatalf("documentation_entity_mention count = %d, want 0 for structured diagrams", got)
	}
	if got := len(factsByKind(envelopes, facts.DocumentationClaimCandidateFactKind)); got != 0 {
		t.Fatalf("documentation_claim_candidate count = %d, want 0 for structured diagrams", got)
	}
}

func TestStructuredDiagramDocumentationUnsafePreflightSuppressesContent(t *testing.T) {
	t.Parallel()

	repoPath := t.TempDir()
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "script.svg"), `<svg><script>alert("x")</script><text>Unsafe</text></svg>`)
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "external.svg"), `<svg><a href="https://private.example.invalid/runbook"><text>Private</text></a></svg>`)
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "broken.drawio"), `<mxfile><diagram><mxCell value="Broken"></diagram></mxfile>`)
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "broken.excalidraw"), `{"type":"excalidraw","elements":[`)
	writeCollectorTestFile(t, filepath.Join(repoPath, "docs", "include.puml"), `@startuml
!include https://private.example.invalid/diagram.puml
component "Unsafe"
@enduml
`)

	observedAt := time.Date(2026, time.June, 9, 7, 45, 0, 0, time.UTC)
	repo := testCollectorRepositoryMetadata(repoPath)
	snapshot := RepositorySnapshot{
		FileCount: 5,
		DocumentationFileMetas: []ContentFileMeta{
			{RelativePath: "docs/script.svg", Digest: "sha256:script", Language: "svg"},
			{RelativePath: "docs/external.svg", Digest: "sha256:external", Language: "svg"},
			{RelativePath: "docs/broken.drawio", Digest: "sha256:drawio", Language: "drawio"},
			{RelativePath: "docs/broken.excalidraw", Digest: "sha256:excalidraw", Language: "excalidraw"},
			{RelativePath: "docs/include.puml", Digest: "sha256:include", Language: "plantuml"},
		},
	}

	collected := buildStreamingGeneration(repoPath, repo, "run-1", observedAt, snapshot, false, "")
	envelopes := drainFactChannel(collected.Facts)

	documentFacts := factsByKind(envelopes, facts.DocumentationDocumentFactKind)
	if got, want := len(documentFacts), 5; got != want {
		t.Fatalf("documentation_document count = %d, want %d", got, want)
	}
	wantWarnings := map[string]string{
		"docs/script.svg":        "unsupported_active_content",
		"docs/external.svg":      "external_reference_skipped",
		"docs/broken.drawio":     "malformed_xml",
		"docs/broken.excalidraw": "malformed_json",
		"docs/include.puml":      "unsupported_remote_include",
	}
	for _, document := range documentFacts {
		path := payloadSourceMetadataValue(document.Payload, "path")
		warning := payloadSourceMetadataValue(document.Payload, "warning")
		if !strings.Contains(warning, wantWarnings[path]) {
			t.Fatalf("document %q warning = %q, want %q", path, warning, wantWarnings[path])
		}
	}
	if got := len(factsByKind(envelopes, facts.DocumentationSectionFactKind)); got != 0 {
		t.Fatalf("documentation_section count = %d, want 0 for unsafe structured diagrams", got)
	}
	if got := len(factsByKind(envelopes, facts.DocumentationLinkFactKind)); got != 0 {
		t.Fatalf("documentation_link count = %d, want 0 for unsafe structured diagrams", got)
	}
	assertStructuredDiagramFactsDoNotLeak(t, envelopes, "private.example.invalid")
}

func TestCleanDiagramLinkTargetRejectsSensitiveLocalTargets(t *testing.T) {
	t.Parallel()

	for _, target := range []string{
		"docs/runbook.md?token=secret",
		"docs/runbook.md?api_key=secret",
		"docs/runbook.md?access_token=secret",
		"docs/runbook.md?auth_token=secret",
		"docs/runbook.md?password=secret",
		"docs/runbook.md?passwd=secret",
		"docs/contact/user@example.invalid",
	} {
		if got := cleanDiagramLinkTarget(target); got != "" {
			t.Fatalf("cleanDiagramLinkTarget(%q) = %q, want empty", target, got)
		}
	}
	if got, want := cleanDiagramLinkTarget("docs/runbook.md"), "docs/runbook.md"; got != want {
		t.Fatalf("cleanDiagramLinkTarget(%q) = %q, want %q", "docs/runbook.md", got, want)
	}
}

func assertStructuredDiagramFactsDoNotLeak(t *testing.T, envelopes []facts.Envelope, disallowed ...string) {
	t.Helper()

	encoded, err := json.Marshal(envelopes)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	for _, value := range disallowed {
		if strings.Contains(string(encoded), value) {
			t.Fatalf("diagram facts leaked %q: %s", value, encoded)
		}
	}
}
