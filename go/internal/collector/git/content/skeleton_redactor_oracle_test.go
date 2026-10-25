// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"flag"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
)

// HashiCorp's lexer (hclsyntax) is the oracle for the skeleton redactor's
// lexical state. It is a test dependency only: the redactor's scanner is hand
// written so that it needs O(line) memory (see the evidence note, ruling 5).
//
// The oracle says which lines begin inside a block comment, a heredoc body, or
// a quoted template (a template holds a line only through a multi-line ${ }).
// Those lines are NON-CODE and the redactor must replace them whole. For a body
// that lexes cleanly the two directions must agree exactly: the scanner may not
// read a NON-CODE line as code (a leak), nor a code line as NON-CODE (over-
// redaction). A closing heredoc marker line is exempt from both.
var (
	oracleBodies = flag.Int("skeleton-oracle-bodies", 0,
		"extra bodies per generator for TestRedactSkeletonOracle (default 20000 when 0)")
	oracleCorpus = flag.String("skeleton-oracle-corpus", "",
		"directory of .tfvars files for TestRedactSkeletonOracleCorpus")
)

// oracleLines is what the oracle says about one body.
type oracleLines struct {
	starts    []int  // byte offset of each line
	nonCode   []bool // the line begins inside a comment, heredoc, or template
	exempt    []bool // the closing marker line of a heredoc
	clean     bool   // no invalid token, bad UTF-8, or quoted newline
	quotedEnd int    // line index of the first quoted newline, or -1
	dangling  bool   // an unterminated /*: HCL lexes it as / and *, we redact
	maxDepth  int    // deepest string, heredoc, interpolation, and brace nesting
}

func lexOracle(body string) oracleLines {
	or := oracleLines{quotedEnd: -1, clean: true}
	or.starts = append(or.starts, 0)
	for i := 0; i < len(body); i++ {
		if body[i] == '\n' {
			or.starts = append(or.starts, i+1)
		}
	}
	or.nonCode = make([]bool, len(or.starts))
	or.exempt = make([]bool, len(or.starts))
	tokens, _ := hclsyntax.LexConfig([]byte(body), "body.tf", hcl.InitialPos)

	// mark sets the lines that begin in [from, to).
	mark := func(flags []bool, from, to int) {
		for i, start := range or.starts {
			if start >= from && start < to {
				flags[i] = true
			}
		}
	}
	var quotes, heredocs []int // byte offsets where the open token ends
	var kinds []byte
	pop := func(want byte) {
		if n := len(kinds); n > 0 && kinds[n-1] == want {
			kinds = kinds[:n-1]
		}
	}
	for k, tok := range tokens {
		start, end := tok.Range.Start.Byte, tok.Range.End.Byte
		switch tok.Type {
		case hclsyntax.TokenComment:
			if strings.HasPrefix(body[start:end], "/*") {
				mark(or.nonCode, start+1, end)
			}
		case hclsyntax.TokenOQuote:
			quotes = append(quotes, end)
			kinds = append(kinds, 'q')
		case hclsyntax.TokenCQuote:
			if n := len(quotes); n > 0 {
				mark(or.nonCode, quotes[n-1], start)
				quotes = quotes[:n-1]
			}
			pop('q')
		case hclsyntax.TokenOHeredoc:
			heredocs = append(heredocs, end)
			kinds = append(kinds, 'h')
		case hclsyntax.TokenCHeredoc:
			if n := len(heredocs); n > 0 {
				mark(or.nonCode, heredocs[n-1], start)
				heredocs = heredocs[:n-1]
			}
			or.exempt[lineOf(or.starts, start)] = true
			pop('h')
		case hclsyntax.TokenTemplateInterp, hclsyntax.TokenTemplateControl:
			kinds = append(kinds, 'i')
		case hclsyntax.TokenOBrace:
			if len(kinds) > 0 {
				kinds = append(kinds, 'b')
			}
		case hclsyntax.TokenTemplateSeqEnd:
			if n := len(kinds); n > 0 && (kinds[n-1] == 'i' || kinds[n-1] == 'b') {
				kinds = kinds[:n-1]
			}
		case hclsyntax.TokenCBrace:
			pop('b')
		case hclsyntax.TokenQuotedNewline:
			or.clean = false
			if or.quotedEnd < 0 {
				or.quotedEnd = lineOf(or.starts, start)
			}
		case hclsyntax.TokenInvalid, hclsyntax.TokenBadUTF8:
			or.clean = false
		case hclsyntax.TokenSlash:
			if k+1 < len(tokens) && tokens[k+1].Type == hclsyntax.TokenStar && tokens[k+1].Range.Start.Byte == end {
				or.dangling = true
			}
		}
		if len(kinds) > or.maxDepth {
			or.maxDepth = len(kinds)
		}
	}
	// hclsyntax closes a heredoc only on a marker line that ends in a newline.
	// A last line with no newline whose text is a bare identifier may be that
	// marker, which the redactor treats as closing (it is only a marker).
	if last := len(or.starts) - 1; !strings.HasSuffix(body, "\n") {
		if marker := strings.TrimSpace(body[or.starts[last]:]); marker != "" {
			if _, ok := heredocMarker("<<" + marker); ok {
				or.exempt[last] = true
			}
		}
	}
	// A heredoc or quote the lexer never closed runs to the end of the body.
	for _, from := range append(quotes, heredocs...) {
		mark(or.nonCode, from, len(body)+1)
	}
	return or
}

// lineOf returns the index of the line holding the byte offset.
func lineOf(starts []int, offset int) int {
	line := 0
	for i, start := range starts {
		if start <= offset {
			line = i
		}
	}
	return line
}

// scanInside runs the redactor's scanner alone over a body and returns, per
// line, whether the line begins inside something.
func scanInside(body string) []bool {
	lines := strings.Split(body, "\n")
	inside := make([]bool, len(lines))
	var st scanState
	for i, line := range lines {
		inside[i] = st.inside()
		st.line(strings.TrimSuffix(line, "\r"), false)
	}
	return inside
}

// oracleCounts tallies a differential run for the evidence note.
type oracleCounts struct{ bodies, clean, compared, lines, leaks, over int }

// checkSkeletonOracle compares the redactor with the oracle on one body and
// reports a failure through t. It returns whether the body was compared in
// both directions.
func checkSkeletonOracle(t *testing.T, body string, counts *oracleCounts) {
	t.Helper()
	or := lexOracle(body)
	out, _, err := redactSkeleton(body)
	if err != nil {
		t.Fatalf("redactSkeleton: %v", err)
	}
	outLines := strings.Split(out, "\n")
	inside := scanInside(body)
	counts.bodies++
	// (i) No leak: a NON-CODE line the oracle sees before any quoted newline is
	// replaced whole, however the body lexes.
	for i := range or.nonCode {
		if or.nonCode[i] && !or.exempt[i] && (or.quotedEnd < 0 || i <= or.quotedEnd) && !isWholeLine(outLines[i]) {
			counts.leaks++
			t.Fatalf("line %d is NON-CODE for hclsyntax but stored %q\n body %q\n out  %q", i+1, outLines[i], body, out)
		}
	}
	// (iv) A quoted newline is invalid HCL: every later line goes whole.
	if or.quotedEnd >= 0 {
		for i := or.quotedEnd + 1; i < len(outLines); i++ {
			if !isWholeLine(outLines[i]) {
				t.Fatalf("line %d follows a quoted newline but stored %q\n body %q\n out  %q", i+1, outLines[i], body, out)
			}
		}
		return
	}
	if !or.clean {
		return
	}
	counts.clean++
	if or.dangling || or.maxDepth > maxScanDepth {
		return // by design the redactor redacts more than HCL reads as text
	}
	// (ii) No over-redaction: the scanner and the oracle agree on every line.
	counts.compared++
	for i := range or.nonCode {
		counts.lines++
		if or.exempt[i] || inside[i] == or.nonCode[i] {
			continue
		}
		if or.nonCode[i] {
			counts.leaks++
		} else {
			counts.over++
		}
		t.Fatalf("line %d: scanner inside=%v, hclsyntax NON-CODE=%v\n body %q", i+1, inside[i], or.nonCode[i], body)
	}
}

// isWholeLine says a stored line carries no source text: it is blank or one
// marker.
func isWholeLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "" || skeletonMarkerValue.MatchString(trimmed)
}

func oracleBodyCount() int {
	if *oracleBodies > 0 {
		return *oracleBodies
	}
	return 20000
}

func TestRedactSkeletonOracle(t *testing.T) {
	t.Parallel()

	for _, gen := range []struct {
		name string
		seed uint64
		body func(*rand.Rand) string
	}{
		{"token soup", 1, tokenSoupBody},
		{"valid HCL", 2, validBody},
		{"valid JSON", 3, jsonBody},
	} {
		var counts oracleCounts
		rng := rand.New(rand.NewPCG(8833, gen.seed))
		n := oracleBodyCount()
		for i := 0; i < n; i++ {
			body := gen.body(rng)
			checkSkeletonOracle(t, body, &counts)
			checkSkeletonProperties(t, body)
		}
		t.Logf("%s: %d bodies, %d lex clean, %d compared both ways over %d lines, %d leaks, %d over-redacted",
			gen.name, counts.bodies, counts.clean, counts.compared, counts.lines, counts.leaks, counts.over)
		// A generator that never reaches the two-direction comparison proves
		// nothing about over-redaction.
		if gen.name != "token soup" && counts.compared*2 < counts.bodies {
			t.Errorf("%s: only %d of %d bodies were compared both ways", gen.name, counts.compared, counts.bodies)
		}
		if gen.name == "token soup" && counts.compared < counts.bodies/50 {
			t.Errorf("%s: only %d of %d bodies were compared both ways", gen.name, counts.compared, counts.bodies)
		}
	}
}

// TestRedactSkeletonOracleCorpus runs the oracle over a directory of real
// .tfvars files: -skeleton-oracle-corpus=<dir>. It is a one-shot check, not
// committed data.
func TestRedactSkeletonOracleCorpus(t *testing.T) {
	if *oracleCorpus == "" {
		t.Skip("set -skeleton-oracle-corpus=<dir> to run")
	}
	var counts oracleCounts
	err := filepath.WalkDir(*oracleCorpus, func(path string, entry os.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		checkSkeletonOracle(t, string(raw), &counts)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("corpus: %d files, %d lex clean, %d compared both ways over %d lines, %d leaks, %d over-redacted",
		counts.bodies, counts.clean, counts.compared, counts.lines, counts.leaks, counts.over)
}
