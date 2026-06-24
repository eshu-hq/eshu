// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package diagrampreflight

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestPreflightAcceptsSafeDiagramMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceName string
		body       string
		wantFormat string
	}{
		{
			name:       "svg",
			sourceName: "architecture.svg",
			body:       `<svg xmlns="http://www.w3.org/2000/svg"><text>api</text><path d="M0 0"/></svg>`,
			wantFormat: FormatSVG,
		},
		{
			name:       "drawio",
			sourceName: "architecture.drawio",
			body:       `<mxfile><diagram><mxGraphModel><root><mxCell id="0"/></root></mxGraphModel></diagram></mxfile>`,
			wantFormat: FormatDrawIO,
		},
		{
			name:       "excalidraw",
			sourceName: "architecture.excalidraw",
			body:       `{"type":"excalidraw","elements":[{"type":"text","text":"api"}],"appState":{}}`,
			wantFormat: FormatExcalidraw,
		},
		{
			name:       "mermaid",
			sourceName: "architecture.mmd",
			body:       "flowchart LR\napi-->db\n",
			wantFormat: FormatMermaid,
		},
		{
			name:       "plantuml",
			sourceName: "architecture.puml",
			body:       "@startuml\ncomponent API\n@enduml\n",
			wantFormat: FormatPlantUML,
		},
		{
			name:       "d2",
			sourceName: "architecture.d2",
			body:       "api -> db\n",
			wantFormat: FormatD2,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Preflight(context.Background(), tt.sourceName, bytes.NewReader([]byte(tt.body)), int64(len(tt.body)), Options{})
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			if got := result.Format; got != tt.wantFormat {
				t.Fatalf("Format = %q, want %q", got, tt.wantFormat)
			}
			if !result.Safe {
				t.Fatalf("Safe = false, want true; warnings=%#v", result.Warnings)
			}
			if result.ElementCount == 0 {
				t.Fatal("ElementCount = 0, want bounded metadata counted")
			}
		})
	}
}

func TestPreflightClassifiesUnsupportedAndMalformed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceName string
		body       string
		wantClass  WarningClass
	}{
		{name: "unsupported", sourceName: "architecture.vsdx", body: "ignored", wantClass: WarningUnsupportedFormat},
		{name: "malformed_xml", sourceName: "architecture.svg", body: "<svg><text>", wantClass: WarningMalformedXML},
		{name: "malformed_json", sourceName: "architecture.excalidraw", body: `{"elements":[`, wantClass: WarningMalformedJSON},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Preflight(context.Background(), tt.sourceName, bytes.NewReader([]byte(tt.body)), int64(len(tt.body)), Options{})
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			assertWarning(t, result, tt.wantClass)
		})
	}
}

func TestPreflightClassifiesResourceLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceName string
		body       string
		options    Options
	}{
		{
			name:       "source_bytes",
			sourceName: "architecture.svg",
			body:       `<svg/>`,
			options:    Options{MaxSourceBytes: 2},
		},
		{
			name:       "xml_depth",
			sourceName: "architecture.svg",
			body:       `<svg><g><g><text>x</text></g></g></svg>`,
			options:    Options{MaxXMLJSONDepth: 2},
		},
		{
			name:       "xml_elements",
			sourceName: "architecture.svg",
			body:       `<svg><g/><g/><g/></svg>`,
			options:    Options{MaxElements: 2},
		},
		{
			name:       "json_depth",
			sourceName: "architecture.excalidraw",
			body:       `{"a":{"b":{"c":1}}}`,
			options:    Options{MaxXMLJSONDepth: 2},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Preflight(context.Background(), tt.sourceName, bytes.NewReader([]byte(tt.body)), int64(len(tt.body)), tt.options)
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			assertWarning(t, result, WarningResourceLimitExceeded)
		})
	}
}

func TestPreflightClassifiesUnsafeReferencesAndContent(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceName string
		body       string
		want       []WarningClass
	}{
		{
			name:       "svg_active_and_external",
			sourceName: "architecture.svg",
			body:       `<svg><script>alert(1)</script><image href="https://example.invalid/icon.png"/></svg>`,
			want:       []WarningClass{WarningUnsupportedActiveContent, WarningExternalReferenceSkipped},
		},
		{
			name:       "xml_external_entity",
			sourceName: "architecture.drawio",
			body:       `<!DOCTYPE svg [<!ENTITY ext SYSTEM "https://example.invalid/entity">]><mxfile/>`,
			want:       []WarningClass{WarningUnsupportedRemoteInclude},
		},
		{
			name:       "json_external_and_sensitive",
			sourceName: "architecture.excalidraw",
			body:       `{"elements":[{"link":"https://example.invalid/ref","customData":{"credential_marker":"redacted"}}]}`,
			want:       []WarningClass{WarningExternalReferenceSkipped, WarningSensitiveValueRedacted},
		},
		{
			name:       "text_include_and_active",
			sourceName: "architecture.puml",
			body:       "@startuml\n!includeurl https://example.invalid/shared.puml\nnote right: javascript:blocked\n@enduml\n",
			want:       []WarningClass{WarningUnsupportedRemoteInclude, WarningUnsupportedActiveContent, WarningExternalReferenceSkipped},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Preflight(context.Background(), tt.sourceName, bytes.NewReader([]byte(tt.body)), int64(len(tt.body)), Options{})
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			for _, class := range tt.want {
				assertWarning(t, result, class)
			}
		})
	}
}

func TestPreflightClassifiesCanceledContextAsTimeout(t *testing.T) {
	t.Parallel()

	body := `<svg><text>api</text></svg>`
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := Preflight(ctx, "architecture.svg", bytes.NewReader([]byte(body)), int64(len(body)), Options{})
	if err == nil {
		t.Fatal("Preflight() error = nil, want canceled context error")
	}
	assertWarning(t, result, WarningTimeout)
}

func TestPreflightResultJSONOmitsSourceAndDiagramText(t *testing.T) {
	t.Parallel()

	body := `<svg><text>member-name-must-not-leak</text><a href="https://example.invalid/link">x</a></svg>`
	result, err := Preflight(context.Background(), "private-source-name.svg", bytes.NewReader([]byte(body)), int64(len(body)), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal() error = %v, want nil", err)
	}
	jsonText := string(encoded)
	for _, disallowed := range []string{"member-name-must-not-leak", "private-source-name", "example.invalid"} {
		if strings.Contains(jsonText, disallowed) {
			t.Fatalf("result JSON leaked %q: %s", disallowed, jsonText)
		}
	}
}

func assertWarning(t *testing.T, result Result, class WarningClass) {
	t.Helper()

	for _, warning := range result.Warnings {
		if warning.Class == class && warning.Count > 0 {
			return
		}
	}
	t.Fatalf("missing warning %q in %#v", class, result.Warnings)
}
