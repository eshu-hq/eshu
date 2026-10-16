// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package fulltext_test

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/infra/fulltext"
)

func TestEscapeEveryLuceneSpecial(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in, want string
	}{
		{`\`, `\\`},
		{`+`, `\+`},
		{`-`, `\-`},
		{`!`, `\!`},
		{`(`, `\(`},
		{`)`, `\)`},
		{`:`, `\:`},
		{`^`, `\^`},
		{`[`, `\[`},
		{`]`, `\]`},
		{`"`, `\"`},
		{`{`, `\{`},
		{`}`, `\}`},
		{`~`, `\~`},
		{`*`, `\*`},
		{`?`, `\?`},
		{`|`, `\|`},
		{`&`, `\&`},
		{`/`, `\/`},
		{"a b", `a\ b`},
		{"a\tb", "a\\\tb"},
		{"a\nb", "a\\\nb"},
		{"a\rb", "a\\\rb"},
		{"a b", "a\\ b"},
		{"a b", "a\\ b"},
		{"a　b", "a\\　b"},
		{"aws_iam_role", "aws_iam_role"},
		{"AWS_IAM", "AWS_IAM"},
		{"é😀-svc", `é😀\-svc`},
		{"ünï", "ünï"},
		{"left\u001fright", "left\u001fright"},
		{"OR AND NOT", `OR\ AND\ NOT`},
		{"arn:aws:sns::123:topic", `arn\:aws\:sns\:\:123\:topic`},
		{`a+b-(c)[d]{e}^"f"~*?:\/ & | !`, `a\+b\-\(c\)\[d\]\{e\}\^\"f\"\~\*\?\:\\\/\ \&\ \|\ \!`},
		{"", ""},
	}
	for _, c := range cases {
		if got := fulltext.Escape(c.in); got != c.want {
			t.Errorf("Escape(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestLuceneWrapsEscapedTermAndAdmitsOverflow(t *testing.T) {
	t.Parallel()
	got := fulltext.Lucene("svc-1 x")
	want := `infra_search_key:*svc\-1\ x* OR infra_search_ovf:T`
	if got != want {
		t.Fatalf("Lucene() = %q, want %q", got, want)
	}
}
