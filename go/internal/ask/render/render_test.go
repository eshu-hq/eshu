package render

import (
	"testing"
)

func TestFormatConstants(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		format   Format
		expected string
	}{
		{"FormatAuto", FormatAuto, "auto"},
		{"FormatMarkdown", FormatMarkdown, "markdown"},
		{"FormatMermaid", FormatMermaid, "mermaid"},
		{"FormatJSON", FormatJSON, "json"},
		{"FormatYAML", FormatYAML, "yaml"},
		{"FormatCSV", FormatCSV, "csv"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.format) != tt.expected {
				t.Errorf("expected %q, got %q", tt.expected, string(tt.format))
			}
		})
	}
}

func TestKnownFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected Format
		expectOK bool
	}{
		{"JSON uppercase", "JSON", FormatJSON, true},
		{"yaml with spaces", " yaml ", FormatYAML, true},
		{"auto", "auto", FormatAuto, true},
		{"markdown", "markdown", FormatMarkdown, true},
		{"mermaid", "mermaid", FormatMermaid, true},
		{"csv", "csv", FormatCSV, true},
		{"unknown xml", "xml", "", false},
		{"empty string", "", "", false},
		{"mixed case", "MaRkDoWn", FormatMarkdown, true},
		{"with tabs", "\tmermaid\t", FormatMermaid, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := KnownFormat(tt.input)
			if ok != tt.expectOK {
				t.Errorf("expected ok=%v, got ok=%v", tt.expectOK, ok)
			}
			if got != tt.expected {
				t.Errorf("expected format %q, got %q", tt.expected, got)
			}
		})
	}
}

func TestArtifactValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		artifact    Artifact
		expectValid bool
	}{
		{"no issues", Artifact{Issues: nil}, true},
		{"empty issues slice", Artifact{Issues: []string{}}, true},
		{"single issue", Artifact{Issues: []string{"x"}}, false},
		{"multiple issues", Artifact{Issues: []string{"x", "y"}}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.artifact.Valid()
			if got != tt.expectValid {
				t.Errorf("expected Valid()=%v, got %v", tt.expectValid, got)
			}
		})
	}
}
