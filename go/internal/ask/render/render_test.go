package render

import (
	"strings"
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

func TestValidate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		format        Format
		content       string
		expectValid   bool
		issueContains string // non-empty: at least one issue must contain this substring
	}{
		{
			name:          "invalid JSON returns issue",
			format:        FormatJSON,
			content:       "{bad",
			expectValid:   false,
			issueContains: "JSON",
		},
		{
			name:        "valid JSON",
			format:      FormatJSON,
			content:     `{"a":1}`,
			expectValid: true,
		},
		{
			name:        "valid Mermaid",
			format:      FormatMermaid,
			content:     "graph TD\n  A-->B",
			expectValid: true,
		},
		{
			name:        "valid YAML",
			format:      FormatYAML,
			content:     "a: 1",
			expectValid: true,
		},
		{
			name:        "valid CSV",
			format:      FormatCSV,
			content:     "a,b\n1,2",
			expectValid: true,
		},
		{
			name:        "valid Markdown",
			format:      FormatMarkdown,
			content:     "# hi",
			expectValid: true,
		},
		{
			name:          "FormatAuto returns unresolved format issue",
			format:        FormatAuto,
			content:       "x",
			expectValid:   false,
			issueContains: "unresolved format",
		},
		{
			name:          "unknown format returns unresolved format issue",
			format:        Format("xml"),
			content:       "x",
			expectValid:   false,
			issueContains: "unresolved format",
		},
		{
			name:        "content returned unchanged for valid JSON",
			format:      FormatJSON,
			content:     `{"a":1}`,
			expectValid: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := Validate(tt.format, tt.content)

			if got.Content != tt.content {
				t.Errorf("content mutated: want %q, got %q", tt.content, got.Content)
			}
			if got.Format != tt.format {
				t.Errorf("format changed: want %q, got %q", tt.format, got.Format)
			}
			if got.Valid() != tt.expectValid {
				t.Errorf("Valid()=%v, want %v; issues=%v", got.Valid(), tt.expectValid, got.Issues)
			}
			if tt.issueContains != "" {
				found := false
				for _, issue := range got.Issues {
					if strings.Contains(issue, tt.issueContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected an issue containing %q; issues=%v", tt.issueContains, got.Issues)
				}
			}
		})
	}
}
