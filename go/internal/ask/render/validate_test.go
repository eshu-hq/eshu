// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package render

import (
	"fmt"
	"strings"
	"testing"
)

// oversizeContent returns a string of length maxArtifactBytes+1 for oversize tests.
func oversizeContent() string {
	return strings.Repeat("a", maxArtifactBytes+1)
}

func TestValidateJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		wantIssues  bool
		issueSubstr string
	}{
		{
			name:       "valid JSON object",
			content:    `{"key": "value", "num": 42}`,
			wantIssues: false,
		},
		{
			name:       "valid JSON array",
			content:    `[1, 2, 3]`,
			wantIssues: false,
		},
		{
			name:        "invalid JSON",
			content:     `{not valid json`,
			wantIssues:  true,
			issueSubstr: "invalid JSON:",
		},
		{
			name:        "empty content",
			content:     "",
			wantIssues:  true,
			issueSubstr: "empty content",
		},
		{
			name:        "whitespace only",
			content:     "   \t\n  ",
			wantIssues:  true,
			issueSubstr: "empty content",
		},
		{
			name:        "oversize content",
			content:     oversizeContent(),
			wantIssues:  true,
			issueSubstr: "artifact exceeds size limit",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			issues := validateJSON(tc.content)
			if tc.wantIssues {
				if len(issues) == 0 {
					t.Fatalf("expected issues but got none")
				}
				if tc.issueSubstr != "" && !strings.Contains(issues[0], tc.issueSubstr) {
					t.Errorf("issue %q does not contain %q", issues[0], tc.issueSubstr)
				}
			} else {
				if len(issues) != 0 {
					t.Errorf("expected no issues but got: %v", issues)
				}
			}
		})
	}
}

func TestValidateYAML(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		wantIssues  bool
		issueSubstr string
	}{
		{
			name:       "valid YAML mapping",
			content:    "key: value\nnum: 42\n",
			wantIssues: false,
		},
		{
			name:       "valid YAML list",
			content:    "- item1\n- item2\n",
			wantIssues: false,
		},
		{
			name:        "invalid YAML (bad indentation)",
			content:     "key: value\n  bad:\n bad2:",
			wantIssues:  true,
			issueSubstr: "invalid YAML:",
		},
		{
			name:        "empty content",
			content:     "",
			wantIssues:  true,
			issueSubstr: "empty content",
		},
		{
			name:        "whitespace only",
			content:     "   \n\t",
			wantIssues:  true,
			issueSubstr: "empty content",
		},
		{
			name:        "oversize content",
			content:     oversizeContent(),
			wantIssues:  true,
			issueSubstr: "artifact exceeds size limit",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			issues := validateYAML(tc.content)
			if tc.wantIssues {
				if len(issues) == 0 {
					t.Fatalf("expected issues but got none")
				}
				if tc.issueSubstr != "" && !strings.Contains(issues[0], tc.issueSubstr) {
					t.Errorf("issue %q does not contain %q", issues[0], tc.issueSubstr)
				}
			} else {
				if len(issues) != 0 {
					t.Errorf("expected no issues but got: %v", issues)
				}
			}
		})
	}
}

func TestValidateCSV(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		wantIssues  bool
		issueSubstr string
	}{
		{
			name:       "valid CSV",
			content:    "name,age,city\nAlice,30,NYC\nBob,25,LA\n",
			wantIssues: false,
		},
		{
			name:       "valid single column CSV",
			content:    "header\nrow1\nrow2\n",
			wantIssues: false,
		},
		{
			name:        "inconsistent column count",
			content:     "a,b,c\n1,2\n3,4,5\n",
			wantIssues:  true,
			issueSubstr: "invalid CSV:",
		},
		{
			name:        "empty content",
			content:     "",
			wantIssues:  true,
			issueSubstr: "empty content",
		},
		{
			name:        "whitespace only",
			content:     "  \n\t  ",
			wantIssues:  true,
			issueSubstr: "empty content",
		},
		{
			name:        "oversize content",
			content:     oversizeContent(),
			wantIssues:  true,
			issueSubstr: "artifact exceeds size limit",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			issues := validateCSV(tc.content)
			if tc.wantIssues {
				if len(issues) == 0 {
					t.Fatalf("expected issues but got none")
				}
				if tc.issueSubstr != "" && !strings.Contains(issues[0], tc.issueSubstr) {
					t.Errorf("issue %q does not contain %q", issues[0], tc.issueSubstr)
				}
			} else {
				if len(issues) != 0 {
					t.Errorf("expected no issues but got: %v", issues)
				}
			}
		})
	}
}

func TestValidateMarkdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		wantIssues  bool
		issueSubstr string
	}{
		{
			name:       "valid markdown",
			content:    "# Heading\n\nSome paragraph text.\n",
			wantIssues: false,
		},
		{
			name:       "valid minimal markdown",
			content:    "hello world",
			wantIssues: false,
		},
		{
			name:        "empty content",
			content:     "",
			wantIssues:  true,
			issueSubstr: "empty content",
		},
		{
			name:        "whitespace only",
			content:     "   \n  ",
			wantIssues:  true,
			issueSubstr: "empty content",
		},
		{
			name:        "oversize content",
			content:     oversizeContent(),
			wantIssues:  true,
			issueSubstr: "artifact exceeds size limit",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			issues := validateMarkdown(tc.content)
			if tc.wantIssues {
				if len(issues) == 0 {
					t.Fatalf("expected issues but got none")
				}
				if tc.issueSubstr != "" && !strings.Contains(issues[0], tc.issueSubstr) {
					t.Errorf("issue %q does not contain %q", issues[0], tc.issueSubstr)
				}
			} else {
				if len(issues) != 0 {
					t.Errorf("expected no issues but got: %v", issues)
				}
			}
		})
	}
}

func TestValidateMermaid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		content     string
		wantIssues  bool
		issueSubstr string
	}{
		{
			name:       "valid flowchart",
			content:    "graph TD\n  A-->B",
			wantIssues: false,
		},
		{
			name:       "valid flowchart keyword",
			content:    "flowchart LR\n  A --> B --> C",
			wantIssues: false,
		},
		{
			name:       "valid sequenceDiagram",
			content:    "sequenceDiagram\n  Alice->>Bob: Hello",
			wantIssues: false,
		},
		{
			name:       "valid classDiagram",
			content:    "classDiagram\n  class Animal { +name string }",
			wantIssues: false,
		},
		{
			name:       "valid stateDiagram-v2",
			content:    "stateDiagram-v2\n  [*] --> Active",
			wantIssues: false,
		},
		{
			// Regression: ER cardinality tokens like ||--o{ contain an unmatched
			// '{' by design; bracket balance must NOT be checked.
			name:       "valid erDiagram with natural cardinality",
			content:    "erDiagram\n  CUSTOMER ||--o{ ORDER : places",
			wantIssues: false,
		},
		{
			name:       "valid gantt",
			content:    "gantt\n  title A Gantt",
			wantIssues: false,
		},
		{
			name:       "valid pie",
			content:    "pie\n  title Pets\n  \"Dogs\" : 386",
			wantIssues: false,
		},
		{
			name:       "valid journey",
			content:    "journey\n  title My working day",
			wantIssues: false,
		},
		{
			name:       "valid mindmap",
			content:    "mindmap\n  root((Root))",
			wantIssues: false,
		},
		{
			name:       "valid timeline",
			content:    "timeline\n  title Timeline",
			wantIssues: false,
		},
		{
			name:       "valid stateDiagram",
			content:    "stateDiagram\n  s1 --> s2",
			wantIssues: false,
		},
		{
			name:       "balanced brackets",
			content:    "graph TD\n  A[Node] --> B(Other)\n  C{Decision} --> D",
			wantIssues: false,
		},
		{
			// Regression: directive preamble must be skipped before keyword check.
			name:       "valid mermaid with directive preamble",
			content:    "%%{init: {'theme':'dark'}}%%\ngraph TD\n  A-->B",
			wantIssues: false,
		},
		{
			// Regression: YAML frontmatter must be skipped before keyword check.
			name:       "valid mermaid with yaml frontmatter",
			content:    "---\ntitle: My Diagram\n---\nsequenceDiagram\n  A->>B: hi",
			wantIssues: false,
		},
		{
			name:        "unrecognized diagram type",
			content:     "not a diagram\n  A-->B",
			wantIssues:  true,
			issueSubstr: "unrecognized mermaid diagram type",
		},
		{
			name:        "empty content",
			content:     "",
			wantIssues:  true,
			issueSubstr: "empty content",
		},
		{
			name:        "whitespace only",
			content:     "\n\n   \t",
			wantIssues:  true,
			issueSubstr: "empty content",
		},
		{
			name:        "oversize content",
			content:     oversizeContent(),
			wantIssues:  true,
			issueSubstr: "artifact exceeds size limit",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			issues := validateMermaid(tc.content)
			if tc.wantIssues {
				if len(issues) == 0 {
					t.Fatalf("expected issues but got none")
				}
				if tc.issueSubstr != "" {
					found := false
					for _, iss := range issues {
						if strings.Contains(iss, tc.issueSubstr) {
							found = true
							break
						}
					}
					if !found {
						t.Errorf("no issue contains %q; got: %v", tc.issueSubstr, issues)
					}
				}
			} else {
				if len(issues) != 0 {
					t.Errorf("expected no issues but got: %v", issues)
				}
			}
		})
	}
}

// TestValidateMermaidNoPanic ensures the validator never panics on hostile input.
func TestValidateMermaidNoPanic(t *testing.T) {
	t.Parallel()

	inputs := []string{
		"",
		"\x00\x01\x02",
		strings.Repeat("(", 10000),
		strings.Repeat("{[", 5000),
		"graph TD\n" + strings.Repeat("A-->B\n", 1000),
	}
	for i, s := range inputs {
		i, s := i, s
		t.Run(fmt.Sprintf("nopanic_%d", i), func(t *testing.T) {
			t.Parallel()
			// Must not panic.
			_ = validateMermaid(s)
		})
	}
}
