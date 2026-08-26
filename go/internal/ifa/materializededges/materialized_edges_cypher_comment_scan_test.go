// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"strings"
	"testing"
)

// stripCypherComments returns value with every `//`-to-end-of-line and
// `/* ... */` comment replaced by spaces, one space per byte, newlines kept.
//
// Stripping the comments out here rather than teaching the parenthesis walk to
// skip them, because the walk is not the only reader that misreads a comment.
// relationshipMergeKeywordPattern matches a MERGE written inside one, and the
// adjacency check after the node pattern's closing parenthesis fails when a
// comment sits between that `)` and the `-[`. One pre-pass fixes all three at
// the point where "which bytes are Cypher" is decided; three separate patches
// would leave the next reader added to this file to remember the rule again.
//
// Recognising a comment needs the same quote state the walk keeps -- `//` in
// `"http://example.test/x"` starts no comment -- so this pass carries it, and
// closingParen keeps its own for the quotes that survive into the stripped
// text. A backslash escapes the next byte, matching closingParen.
//
// Blanked rather than deleted so byte offsets and line positions survive:
// mergesRelationship indexes back into the text it was handed, and
// relationshipMergeLine quotes the RAW line at the position of the stripped
// line that matched. A stripper that removed bytes would shift both and the
// evidence would name a neighbouring line.
//
// An unterminated `/*` blanks to the end of the value, the same
// fail-toward-nothing shape closingParen's own doc records for an unterminated
// quote. Unterminated Cypher does not compile, so no live template reaches it.
func stripCypherComments(value string) string {
	out := []byte(value)
	var quote byte
	for i := 0; i < len(out); {
		c := out[i]
		if quote != 0 {
			switch c {
			case '\\':
				i += 2
				continue
			case quote:
				quote = 0
			}
			i++
			continue
		}
		switch {
		case c == '\'' || c == '"' || c == '`':
			quote = c
			i++
		case c == '/' && i+1 < len(out) && out[i+1] == '/':
			for ; i < len(out) && out[i] != '\n'; i++ {
				out[i] = ' '
			}
		case c == '/' && i+1 < len(out) && out[i+1] == '*':
			out[i], out[i+1] = ' ', ' '
			for i += 2; i < len(out); i++ {
				if out[i] == '*' && i+1 < len(out) && out[i+1] == '/' {
					out[i], out[i+1] = ' ', ' '
					i += 2
					break
				}
				if out[i] != '\n' {
					out[i] = ' '
				}
			}
		default:
			i++
		}
	}
	return string(out)
}

// TestCypherCommentsDoNotHideARelationshipMerge holds the relationship-MERGE
// reader to templates that carry Cypher comments (#6181).
//
// The reader walked a node pattern counting parentheses and tracking quotes,
// and knew nothing about `//` or `/* */`. Both gaps fail in the false-GREEN
// direction the whole guard exists to prevent — a port whose only write site is
// one of these templates is classified node-only, its family never enters the
// enumeration, and no ledger row is ever missing for it:
//
//   - An unbalanced `)` in a comment decremented the real depth, so the node
//     pattern closed early or never closed at all.
//   - A comment sitting between the node pattern and its `-[` broke the
//     adjacency the reader requires.
//   - An apostrophe in a comment — `// the repo's id` — opened a quoted region
//     that swallowed the rest of the template. That one is not old: the quote
//     tracking that introduced it was added on this branch, so it is a
//     regression this change caused rather than a gap it inherited.
//
// The negative cases are the other half. A merge written inside a comment is
// not a write site, and reporting one would put a node-only port in the direct
// family table — loud and wrong. And a `//` or `/*` inside a string VALUE is
// not a comment: a template keyed on a URL is ordinary, and a comment stripper
// that does not know about quotes would delete the rest of that line and
// reintroduce exactly the false-green it was added to close.
func TestCypherCommentsDoNotHideARelationshipMerge(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		cypher   string
		want     bool
		wantLine string
	}{
		{
			name:     "line comment between the node pattern and the relationship",
			cypher:   "MERGE (n:Label)  // the node, not the edge\n-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:     true,
			wantLine: "",
		},
		{
			name:     "unbalanced paren in a line comment inside the node pattern",
			cypher:   "MERGE (n:Label { // keyed on the coalesced id (see #6181\n  id: $id\n})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:     true,
			wantLine: "",
		},
		{
			name:     "apostrophe in a line comment inside the node pattern",
			cypher:   "MERGE (n:Label { // the repo's id\n  id: $id\n})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:     true,
			wantLine: "",
		},
		{
			name:     "unbalanced paren in a block comment inside the node pattern",
			cypher:   "MERGE (n:Label /* id ) is the key */ {id: $id})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:     true,
			wantLine: "MERGE (n:Label /* id ) is the key */ {id: $id})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
		},
		{
			name:     "apostrophe in a block comment inside the node pattern",
			cypher:   "MERGE (n:Label /* the repo's id */ {id: $id})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:     true,
			wantLine: "MERGE (n:Label /* the repo's id */ {id: $id})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
		},
		{
			name:     "block comment spanning the lines of a node pattern",
			cypher:   "MERGE (n:Label {\n  /* the id ) below is\n     the merge key ' */\n  id: $id\n})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:     true,
			wantLine: "",
		},
		{
			name:     "double slash inside a string property value",
			cypher:   `MERGE (n:Repo {url: "http://example.test/x"})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)`,
			want:     true,
			wantLine: `MERGE (n:Repo {url: "http://example.test/x"})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)`,
		},
		{
			name:     "block-comment opener inside a string property value",
			cypher:   `MERGE (n:Repo {glob: "/*"})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)`,
			want:     true,
			wantLine: `MERGE (n:Repo {glob: "/*"})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)`,
		},
		{
			name:   "a commented-out merge is not a write site",
			cypher: "// MERGE (a)-[rel:REVIEW_PROBE_FLOWS_TO]->(b)",
			want:   false,
		},
		{
			name:   "a block-commented merge is not a write site",
			cypher: "/* MERGE (a)-[rel:REVIEW_PROBE_FLOWS_TO]->(b) */",
			want:   false,
		},
		{
			name:   "a node-only merge whose trailing comment quotes a relationship",
			cypher: "MERGE (n:Label) // -[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			line, merges := relationshipMergeLine(tc.cypher)
			if merges != tc.want {
				t.Fatalf("relationshipMergeLine(%q) merges = %t, want %t", tc.cypher, merges, tc.want)
			}
			if tc.wantLine != "" && line != tc.wantLine {
				t.Errorf("evidence line = %q, want %q", line, tc.wantLine)
			}
		})
	}
}

// TestStripCypherCommentsBlanksCommentsInPlace holds the stripper to the two
// properties the callers depend on (#6181).
//
// Byte offsets survive, because mergesRelationship reports an index back into
// the value it was handed and relationshipMergeLine pairs a stripped line with
// the RAW line at the same position to quote as evidence. A stripper that
// deleted bytes would shift both, and the evidence line would name a neighbour.
//
// String literals survive untouched, because a `//` or `/*` inside one is not
// a comment. Blanking it would delete real pattern text and hand back exactly
// the false-green the caller is trying to avoid.
func TestStripCypherCommentsBlanksCommentsInPlace(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		value string
		want  string
	}{
		{
			name:  "line comment becomes spaces and the newline survives",
			value: "MERGE (a) // tail\nMERGE (b)",
			want:  "MERGE (a)        \nMERGE (b)",
		},
		{
			name:  "block comment becomes spaces and its newlines survive",
			value: "MERGE (a) /* one\ntwo */ (b)",
			want:  "MERGE (a)       \n       (b)",
		},
		{
			name:  "a comment marker inside a string is not a comment",
			value: `MERGE (n {url: "http://x", glob: '/*'}) // gone`,
			want:  `MERGE (n {url: "http://x", glob: '/*'})        `,
		},
		{
			name:  "an unterminated block comment runs to the end",
			value: "MERGE (a) /* never closed",
			want:  "MERGE (a)                ",
		},
		{
			name:  "a lone slash is not a comment",
			value: "MERGE (n {ratio: 1/2})",
			want:  "MERGE (n {ratio: 1/2})",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := stripCypherComments(tc.value)
			if len(got) != len(tc.value) {
				t.Fatalf("stripCypherComments(%q) changed length %d -> %d; every caller's byte offsets and line numbering shift with it", tc.value, len(tc.value), len(got))
			}
			if got != tc.want {
				t.Errorf("stripCypherComments(%q)\n got %q\nwant %q", tc.value, got, tc.want)
			}
			if strings.Count(got, "\n") != strings.Count(tc.value, "\n") {
				t.Errorf("newline count changed; relationshipMergeLine pairs stripped lines with raw lines by position")
			}
		})
	}
}
