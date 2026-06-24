package skillgen

import (
	"errors"
	"reflect"
	"testing"
)

func TestFormatCommentBlock_Longhand(t *testing.T) {
	t.Parallel()
	got := FormatCommentBlock([]string{"docs/foo.md#1-5"})
	want := "<!-- eshu:byte-citation docs/foo.md#1-5 -->"
	if got != want {
		t.Fatalf("FormatCommentBlock() = %q, want %q", got, want)
	}
}

func TestFormatCommentBlock_SortsAndDedupes(t *testing.T) {
	t.Parallel()
	got := FormatCommentBlock([]string{
		"docs/zeta.md#1-2",
		"docs/alpha.md#1-2",
		"docs/zeta.md#1-2",
		"docs/mu.md#1-2",
	})
	want := "<!-- eshu:byte-citation docs/alpha.md#1-2 -->\n" +
		"<!-- eshu:byte-citation docs/mu.md#1-2 -->\n" +
		"<!-- eshu:byte-citation docs/zeta.md#1-2 -->"
	if got != want {
		t.Fatalf("FormatCommentBlock() = %q, want %q", got, want)
	}
}

func TestFormatCommentBlock_EmptyInputReturnsEmptyString(t *testing.T) {
	t.Parallel()
	if got := FormatCommentBlock(nil); got != "" {
		t.Fatalf("FormatCommentBlock(nil) = %q, want empty", got)
	}
	if got := FormatCommentBlock([]string{"", "  "}); got != "" {
		t.Fatalf("FormatCommentBlock(blanks) = %q, want empty", got)
	}
}

func TestNormalizeByteCitation_LonghandReturned(t *testing.T) {
	t.Parallel()
	got, err := NormalizeByteCitation("docs/foo.md#10-20", "/path/to/fragment.md")
	if err != nil {
		t.Fatalf("NormalizeByteCitation: %v", err)
	}
	if got != "docs/foo.md#10-20" {
		t.Fatalf("NormalizeByteCitation() = %q, want %q", got, "docs/foo.md#10-20")
	}
}

func TestNormalizeByteCitation_ResolvesShorthand(t *testing.T) {
	t.Parallel()
	got, err := NormalizeByteCitation("#10-20", "docs/foo.md")
	if err != nil {
		t.Fatalf("NormalizeByteCitation: %v", err)
	}
	if got != "docs/foo.md#10-20" {
		t.Fatalf("NormalizeByteCitation() = %q, want %q", got, "docs/foo.md#10-20")
	}
}

func TestNormalizeByteCitation_TrimsWhitespace(t *testing.T) {
	t.Parallel()
	got, err := NormalizeByteCitation("  docs/foo.md#1-5  ", "")
	if err != nil {
		t.Fatalf("NormalizeByteCitation: %v", err)
	}
	if got != "docs/foo.md#1-5" {
		t.Fatalf("NormalizeByteCitation() = %q, want %q", got, "docs/foo.md#1-5")
	}
}

func TestNormalizeByteCitation_RejectsEmpty(t *testing.T) {
	t.Parallel()
	_, err := NormalizeByteCitation("", "")
	if !errors.Is(err, ErrInvalidByteCitation) {
		t.Fatalf("NormalizeByteCitation(empty) error = %v, want ErrInvalidByteCitation", err)
	}
}

func TestNormalizeByteCitation_RejectsMissingAnchor(t *testing.T) {
	t.Parallel()
	_, err := NormalizeByteCitation("docs/foo.md", "")
	if !errors.Is(err, ErrInvalidByteCitation) {
		t.Fatalf("NormalizeByteCitation(no-#) error = %v, want ErrInvalidByteCitation", err)
	}
}

func TestNormalizeByteCitation_RejectsShorthandWithoutPath(t *testing.T) {
	t.Parallel()
	_, err := NormalizeByteCitation("#1-5", "")
	if !errors.Is(err, ErrInvalidByteCitation) {
		t.Fatalf("NormalizeByteCitation(shorthand-no-path) error = %v, want ErrInvalidByteCitation", err)
	}
}

func TestNormalizeByteCitation_RejectsMalformedAnchor(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		citation string
	}{
		{name: "empty start", citation: "docs/foo.md#-5"},
		{name: "empty end", citation: "docs/foo.md#1-"},
		{name: "non-numeric start", citation: "docs/foo.md#a-5"},
		{name: "non-numeric end", citation: "docs/foo.md#1-z"},
		{name: "zero start", citation: "docs/foo.md#0-5"},
		{name: "zero end", citation: "docs/foo.md#1-0"},
		{name: "descending", citation: "docs/foo.md#10-5"},
		{name: "non-numeric single-line", citation: "docs/foo.md#a"},
		{name: "zero single-line", citation: "docs/foo.md#0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NormalizeByteCitation(tt.citation, "")
			if !errors.Is(err, ErrInvalidByteCitation) {
				t.Fatalf("error = %v, want ErrInvalidByteCitation", err)
			}
		})
	}
}

func TestNormalizeByteCitation_AllowsSameStartEnd(t *testing.T) {
	t.Parallel()
	// A single-line anchor is valid for citations that point at one rule line.
	got, err := NormalizeByteCitation("docs/foo.md#14-14", "")
	if err != nil {
		t.Fatalf("NormalizeByteCitation: %v", err)
	}
	if got != "docs/foo.md#14-14" {
		t.Fatalf("NormalizeByteCitation() = %q, want %q", got, "docs/foo.md#14-14")
	}
}

func TestNormalizeByteCitation_AcceptsSingleLineAnchor(t *testing.T) {
	t.Parallel()
	// Per the S1 codex-review fix, the local-first fragment cites
	// go/internal/semanticqueue/README.md#10. The validator must accept
	// the single-line shorthand and return it unchanged (the longhand
	// "N-N" is the form used inside emitted comment blocks, but the
	// accepted input form is "N").
	got, err := NormalizeByteCitation("go/internal/semanticqueue/README.md#10", "")
	if err != nil {
		t.Fatalf("NormalizeByteCitation: %v", err)
	}
	if got != "go/internal/semanticqueue/README.md#10" {
		t.Fatalf("NormalizeByteCitation() = %q, want %q", got, "go/internal/semanticqueue/README.md#10")
	}
}

func TestCommentBlockContains_AllFragments(t *testing.T) {
	t.Parallel()
	// The S1 design requires that every generated skill carries the
	// byte_citation comment block, and the block must include a line per
	// fragment's citation. This is the property the roundtrip test relies
	// on.
	citations := []string{
		"docs/internal/agent-guide.md#14-22",
		"docs/public/reference/truth-label-protocol.md#10-21",
		"go/internal/semanticqueue/README.md#10",
	}
	block := FormatCommentBlock(citations)
	wantLines := []string{
		"<!-- eshu:byte-citation docs/internal/agent-guide.md#14-22 -->",
		"<!-- eshu:byte-citation docs/public/reference/truth-label-protocol.md#10-21 -->",
		"<!-- eshu:byte-citation go/internal/semanticqueue/README.md#10 -->",
	}
	if !reflect.DeepEqual(splitLines(block), wantLines) {
		t.Fatalf("lines = %v, want %v", splitLines(block), wantLines)
	}
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i, r := range s {
		if r == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
