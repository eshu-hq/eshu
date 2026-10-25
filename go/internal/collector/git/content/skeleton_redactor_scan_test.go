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

// referenceHidden says, per line, whether HCL reads the line as comment or
// heredoc body, so the redactor must replace it whole.
func referenceHidden(lines []string) []bool {
	hidden := make([]bool, len(lines))
	inComment, heredoc := false, ""
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if heredoc != "" {
			hidden[i] = true
			if trimmed == heredoc {
				heredoc = ""
			}
			continue
		}
		hidden[i] = inComment
		end := (&refLexer{text: line}).lineEnd(inComment)
		inComment = end == "block"
		if end == "code" {
			heredoc = refHeredocID(trimmed)
		}
	}
	return hidden
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

// refLexer reads one line. Each method returns what it stopped on: "code" and
// "string" for the end of the line, "line" and "block" for a comment that runs
// past it, "close" for the } that ends an interpolation or object.
type refLexer struct {
	text string
	pos  int
}

func (l *refLexer) lineEnd(inComment bool) string {
	if inComment && !l.skipBlock() {
		return "block"
	}
	return l.code(false)
}

// skipBlock moves past the next */ and reports whether there was one.
func (l *refLexer) skipBlock() bool {
	end := strings.Index(l.text[l.pos:], "*/")
	if end < 0 {
		return false
	}
	l.pos += end + 2
	return true
}

func (l *refLexer) code(nested bool) string {
	for l.pos < len(l.text) {
		rest := l.text[l.pos:]
		switch {
		case strings.HasPrefix(rest, "//"), strings.HasPrefix(rest, "#"):
			return "line"
		case strings.HasPrefix(rest, "/*"):
			l.pos += 2
			if !l.skipBlock() {
				return "block"
			}
		case strings.HasPrefix(rest, `"`):
			l.pos++
			if end := l.str(); end != "closed" {
				return end
			}
		case nested && strings.HasPrefix(rest, "{"):
			l.pos++
			if end := l.code(true); end != "close" {
				return end
			}
		case nested && strings.HasPrefix(rest, "}"):
			l.pos++
			return "close"
		default:
			l.pos++
		}
	}
	if nested {
		return "string"
	}
	return "code"
}

// str reads a quoted template up to its closing quote.
func (l *refLexer) str() string {
	for l.pos < len(l.text) {
		rest := l.text[l.pos:]
		switch {
		case strings.HasPrefix(rest, `\`):
			l.pos += 2
		case strings.HasPrefix(rest, `"`):
			l.pos++
			return "closed"
		case strings.HasPrefix(rest, "$${"), strings.HasPrefix(rest, "%%{"):
			l.pos += 3
		case strings.HasPrefix(rest, "${"), strings.HasPrefix(rest, "%{"):
			l.pos += 2
			if end := l.code(true); end != "close" {
				return end
			}
		default:
			l.pos++
		}
	}
	return "string"
}
