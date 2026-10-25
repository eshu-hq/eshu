// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"math/rand/v2"
	"strings"
	"testing"
)

// The scan property test (#8833 rev-8833 round 3, H1) composes random lines out
// of comment, string, template, and heredoc tokens and checks the redactor
// against referenceHidden, a line-by-line model written for the test. It shares
// no code with the redactor's scanner: the model lexes by recursive descent, the
// redactor by one pass over a frame stack.

const (
	// scanKeySecret is a credential spelled as a plain key. In code the
	// redactor keeps it by design (the README's key caveat); inside a comment or
	// a heredoc body it must go. It is the probe for the comment and heredoc
	// state the redactor tracks.
	scanKeySecret = "Passw0rdHunter2"
	// scanValueSecret appears only as free text inside a token line. It is never
	// a key, so it must not survive anywhere.
	scanValueSecret = "ZqVx7Rk3Lm9"
)

var scanTokens = []string{
	"#", "//", "/*", "*/", `"`, "<<EOF", "<<-EOF", "<<~EOF", "EOF", "x = ", "${", "}", "{",
	"  ", "x = ", scanValueSecret,
}

// scanBody builds one random body: token lines, with a standalone
// scanKeySecret line now and then.
func scanBody(rng *rand.Rand) []string {
	lines := make([]string, 1+rng.IntN(10))
	for i := range lines {
		if rng.IntN(4) == 0 {
			lines[i] = scanKeySecret + ": 1"
			continue
		}
		parts := make([]string, rng.IntN(6))
		for j := range parts {
			parts[j] = scanTokens[rng.IntN(len(scanTokens))]
		}
		lines[i] = strings.Join(parts, " ")
	}
	return lines
}

func TestRedactSkeletonScanProperty(t *testing.T) {
	t.Parallel()

	rng := rand.New(rand.NewPCG(8833, 3))
	for n := 0; n < 20000; n++ {
		lines := scanBody(rng)
		body := strings.Join(lines, "\n") + "\n"
		checkSkeletonProperties(t, body)
		out, _, err := redactSkeleton(body)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(out, scanValueSecret) {
			t.Fatalf("value secret survived:\n in\n%s\n out\n%s", body, out)
		}
		outLines := strings.Split(out, "\n")
		hidden := referenceHidden(lines)
		for i, line := range lines {
			if line != scanKeySecret+": 1" {
				continue
			}
			want := line
			if hidden[i] {
				want = skeletonValueMarker
			}
			if outLines[i] != want {
				t.Fatalf("line %d: hidden=%v got %q, want %q\n in\n%s\n out\n%s", i+1, hidden[i], outLines[i], want, body, out)
			}
		}
	}
}

// referenceHidden says, per line, whether HCL reads the line as comment,
// interpolation, or heredoc body, so the redactor must replace it whole.
func referenceHidden(lines []string) []bool {
	b := &refBody{lines: lines, hidden: make([]bool, len(lines)), eolOK: true}
	b.code(false)
	return b.hidden
}

// refHeredocID returns the delimiter when the text ends in <<ID or <<-ID.
func refHeredocID(text string) string {
	at := strings.LastIndex(text, "<<")
	if at < 0 {
		return ""
	}
	id := strings.TrimPrefix(text[at+2:], "-")
	if id == "" || (id[0] >= '0' && id[0] <= '9') {
		return ""
	}
	for _, r := range id {
		if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return id
}

// refBody lexes a whole body by recursive descent, so an interpolation, a
// block comment, and a heredoc carry across lines on the Go call stack. Its
// methods return what they stopped on: "eof" at the end of the body, "close"
// for the } that ends an interpolation or object, "closed" for the quote that
// ends a string, and "string" for a string cut short by the end of its line.
type refBody struct {
	lines  []string
	hidden []bool
	row    int
	col    int
	// eolOK says the current line still ends in code, so a trailing <<ID opens
	// a heredoc. A line comment, a comment left open, or a string cut short by
	// the end of the line clears it.
	eolOK bool
}

// next ends the current line and enters the next one, which starts hidden when
// hide is set. A heredoc the ended line opened swallows its body first.
func (b *refBody) next(hide bool) bool {
	id := ""
	if b.eolOK {
		id = refHeredocID(strings.TrimSpace(b.lines[b.row]))
	}
	b.row, b.col, b.eolOK = b.row+1, 0, true
	for id != "" && b.row < len(b.lines) {
		b.hidden[b.row] = true
		closer := strings.TrimSpace(b.lines[b.row]) == id
		b.row++
		if closer {
			break
		}
	}
	if b.row >= len(b.lines) {
		return false
	}
	b.hidden[b.row] = hide
	return true
}

func (b *refBody) code(nested bool) string {
	for {
		line := b.lines[b.row]
		if b.col >= len(line) {
			if !b.next(nested) {
				return "eof"
			}
			continue
		}
		rest := line[b.col:]
		switch {
		case strings.HasPrefix(rest, "//"), strings.HasPrefix(rest, "#"):
			b.col, b.eolOK = len(line), false
		case strings.HasPrefix(rest, "/*"):
			b.col += 2
			if !b.block() {
				return "eof"
			}
		case strings.HasPrefix(rest, `"`):
			b.col++
			if b.str() == "eof" {
				return "eof"
			}
		case nested && strings.HasPrefix(rest, "{"):
			b.col++
			if end := b.code(true); end != "close" {
				return end
			}
		case nested && strings.HasPrefix(rest, "}"):
			b.col++
			return "close"
		default:
			b.col++
		}
	}
}

// block moves past the next */, over as many lines as it takes.
func (b *refBody) block() bool {
	for {
		if end := strings.Index(b.lines[b.row][b.col:], "*/"); end >= 0 {
			b.col += end + 2
			return true
		}
		b.eolOK = false
		if !b.next(true) {
			return false
		}
	}
}

// str reads a quoted template up to its closing quote or the end of its line.
func (b *refBody) str() string {
	for {
		line := b.lines[b.row]
		if b.col >= len(line) {
			b.eolOK = false
			return "string"
		}
		rest := line[b.col:]
		switch {
		case strings.HasPrefix(rest, `\`):
			b.col += 2
		case strings.HasPrefix(rest, `"`):
			b.col++
			return "closed"
		case strings.HasPrefix(rest, "$${"), strings.HasPrefix(rest, "%%{"):
			b.col += 3
		case strings.HasPrefix(rest, "${"), strings.HasPrefix(rest, "%{"):
			b.col += 2
			if end := b.code(true); end != "close" {
				return end
			}
		default:
			b.col++
		}
	}
}
