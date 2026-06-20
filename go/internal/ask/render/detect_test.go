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
