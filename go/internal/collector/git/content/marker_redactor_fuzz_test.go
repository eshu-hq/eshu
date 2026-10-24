// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// FuzzRedactMarker checks the redactor invariants on arbitrary input: a body is
// either rejected as not text, or redacted with the line count kept, the output
// valid UTF-8, a second pass a no-op, and a planted credential (a listed key, a
// Bearer tail, a comma tail, an AWS key, a one-line PEM block) never surviving.
// Under go test it runs the seed corpus; run it with -fuzz for a longer search.
func FuzzRedactMarker(f *testing.F) {
	for _, s := range []string{
		"npmAuthToken: abc\n", "npmAuthToken: |\n  x\n", "-----BEGIN PRIVATE KEY-----\nabc\n-----END PRIVATE KEY-----\n",
		"a: \"b\nc\"\n", "npmAuthToken: \"a\nb\nc\"\nx: 1\n", "password=abcdefgh\r\n", "npmAuthIdent: &a !!str x # c\n",
		"x: AKIAABCDEFGHIJKLMNOP\n", "- npmAuthToken:\n  - a\n",
		"k: \"-----BEGIN PRIVATE KEY-----\\nZZ\\n-----END PRIVATE KEY-----\"\n", "# -----BEGIN PRIVATE KEY----- ZZ\n",
		"Authorization: Bearer ZZsEcReT\n", "db_password=abcdef,tailZZ\n", "-----BEGIN 0-----\n-----END 0-----\n00\n",
		"password　: abcdefghij\n", "ſecret: abcdefgh12345\n",
	} {
		f.Add(s)
	}
	keys := []string{"npmAuthToken", "npmAuthIdent"}
	f.Fuzz(func(t *testing.T, in string) {
		out, _, err := redactMarker(in, keys)
		if err != nil {
			if utf8.ValidString(in) && !strings.Contains(in, "\x00") {
				t.Fatalf("unexpected err %v", err)
			}
			return
		}
		if !utf8.ValidString(out) {
			t.Fatalf("output invalid utf8: %q", out)
		}
		if strings.Count(out, "\n") != strings.Count(in, "\n") {
			t.Fatalf("line count changed %q -> %q", in, out)
		}
		out2, _, _ := redactMarker(out, keys)
		if out2 != out {
			t.Fatalf("not fixed point:\n in  %q\n out %q\n out2 %q", in, out, out2)
		}
		for _, planted := range []string{
			"npmAuthToken: QqZzSeed9988\n" + in,
			in + "\nnpmAuthToken: QqZzSeed9988\n",
			in + "\nAuthorization: Bearer QqZzSeed9988\n",
			in + "\ndb_password=abcdef,QqZzSeed9988\n",
			in + "\n# rotate AKIAABCDEFGHIJKLMNOP QqZzSeed9988\n",
			in + "\nk: \"-----BEGIN PRIVATE KEY-----\\nQqZzSeed9988\\n-----END PRIVATE KEY-----\"\n",
			in + "\n  -----BEGIN PRIVATE KEY-----\nQqZzSeed9988\n-----END PRIVATE KEY-----\n",
			"npmAuthToken: x\n  -----BEGIN PRIVATE KEY-----\nQqZzSeed9988\n-----END PRIVATE KEY-----\n" + in,
		} {
			po, _, perr := redactMarker(planted, keys)
			if perr == nil && strings.Contains(po, "QqZzSeed9988") {
				t.Fatalf("planted seed survived: in=%q planted=%q out=%q", in, planted, po)
			}
		}
	})
}
