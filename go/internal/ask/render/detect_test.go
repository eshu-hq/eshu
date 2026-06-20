package render

import (
	"testing"
)

func TestDetectFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		question  string
		requested string
		want      Format
	}{
		{
			name:      "explicit json wins over question cue",
			question:  "draw a diagram",
			requested: "json",
			want:      FormatJSON,
		},
		{
			name:      "auto with mermaid cue",
			question:  "draw me a mermaid diagram",
			requested: "auto",
			want:      FormatMermaid,
		},
		{
			name:      "empty requested falls through to question",
			question:  "export as yaml",
			requested: "",
			want:      FormatYAML,
		},
		{
			name:      "auto with csv cue",
			question:  "give me a csv of repos",
			requested: "auto",
			want:      FormatCSV,
		},
		{
			name:      "auto with json cue",
			question:  "return as json please",
			requested: "auto",
			want:      FormatJSON,
		},
		{
			name:      "auto with no matching cue defaults to markdown",
			question:  "what services call checkout?",
			requested: "auto",
			want:      FormatMarkdown,
		},
		{
			name:      "diagram cue matches mermaid",
			question:  "show the dependency diagram",
			requested: "auto",
			want:      FormatMermaid,
		},
		{
			name:      "unknown requested falls through to question inference",
			question:  "as yaml",
			requested: "garbage-unknown",
			want:      FormatYAML,
		},
		// False-positive guards: bare format mentions must NOT trigger export format.
		{
			name:      "yaml mention in context is not export intent",
			question:  "Which services load YAML manifests?",
			requested: "auto",
			want:      FormatMarkdown,
		},
		{
			name:      "json mention in context is not export intent",
			question:  "which repos have a json config field?",
			requested: "auto",
			want:      FormatMarkdown,
		},
		{
			name:      "csv file mention is not export intent",
			question:  "show me the csv file location",
			requested: "auto",
			want:      FormatMarkdown,
		},
		{
			name:      "yaml configuration mention is not export intent",
			question:  "what services use yaml configuration?",
			requested: "auto",
			want:      FormatMarkdown,
		},
		// Intent true-positives: export-construction phrases must trigger the format.
		{
			name:      "output as json is export intent",
			question:  "output as json",
			requested: "auto",
			want:      FormatJSON,
		},
		{
			name:      "convert to yaml is export intent",
			question:  "convert to yaml",
			requested: "auto",
			want:      FormatYAML,
		},
		{
			name:      "in csv format is export intent",
			question:  "list all repos in csv format",
			requested: "auto",
			want:      FormatCSV,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetectFormat(tt.question, tt.requested)
			if got != tt.want {
				t.Errorf("DetectFormat(%q, %q) = %v, want %v",
					tt.question, tt.requested, got, tt.want)
			}
		})
	}
}
