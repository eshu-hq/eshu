// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"testing"

	runtimedeclarationv1 "github.com/eshu-hq/eshu/sdk/go/factschema/runtimedeclaration/v1"
)

// wantSpec is the end-to-end shape of one emitted fact for the operator tests:
// the raw text, its position on the line, and the spec fields a reader would
// filter on. -1 means the field must be nil.
type wantSpec struct {
	raw, kind            string
	line, order          int
	major, lower, upper  int
	lowerIncl, upperIncl int // -1 nil, 0 false, 1 true
}

func derefBool(p *bool) int {
	switch {
	case p == nil:
		return -1
	case *p:
		return 1
	}
	return 0
}

func checkSpecs(t *testing.T, path, body string, want []wantSpec) {
	t.Helper()
	got := emitDecoded(t, path, body)
	if len(got) != len(want) {
		t.Fatalf("Emit(%s, %q) emitted %d facts, want %d: %s", path, body, len(got), len(want), describeVersions(got))
	}
	for i, w := range want {
		g := got[i]
		if g.RawSpec != w.raw || g.SpecKind != w.kind || g.LineNumber != w.line || g.OrderIndex != w.order ||
			deref(g.VersionMajor) != w.major || deref(g.RangeLowerMajor) != w.lower || deref(g.RangeUpperMajor) != w.upper ||
			derefBool(g.RangeLowerInclusive) != w.lowerIncl || derefBool(g.RangeUpperInclusive) != w.upperIncl {
			t.Errorf("Emit(%s, %q) fact[%d] = %s\nwant %+v", path, body, i, describeVersions(got[i:i+1]), w)
		}
	}
}

func describeVersions(vs []runtimedeclarationv1.Version) string {
	out := ""
	for _, v := range vs {
		out += "{raw=" + v.RawSpec + " kind=" + v.SpecKind + "} "
	}
	return out
}

// TestEmitWhitespaceFilesKeepSpacedOperatorsAndRangesWhole proves the
// tokenizer never cuts one declared expression into independent versions
// (#8969 review). A comparator separated from its version by spaces is one
// declaration with the same spec as the unspaced spelling. An operator-only
// token left after that (a hyphen range, a "||", a dangling comparator) means
// the value is not a list of versions: the package fails closed, so the
// whole value is one alias, never partial(18) + alias("-") + partial(20).
func TestEmitWhitespaceFilesKeepSpacedOperatorsAndRangesWhole(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, path, body string
		want             []wantSpec
	}{
		{
			"nvmrc hyphen range is one alias", ".nvmrc", "18 - 20\n",
			[]wantSpec{{"18 - 20", "alias", 1, 0, -1, -1, -1, -1, -1}},
		},
		{
			"nvmrc spaced equals matches the unspaced pin", ".nvmrc", "= 20\n",
			[]wantSpec{{"= 20", "partial", 1, 0, 20, -1, -1, -1, -1}},
		},
		{
			"nvmrc unspaced equals", ".nvmrc", "=20\n",
			[]wantSpec{{"=20", "partial", 1, 0, 20, -1, -1, -1, -1}},
		},
		{
			"nvmrc spaced comparators glue to their versions", ".nvmrc", ">= 18 < 21\n",
			[]wantSpec{
				{">= 18", "range", 1, 0, -1, 18, -1, 1, -1},
				{"< 21", "range", 1, 1, -1, -1, 21, -1, 0},
			},
		},
		{
			"nvmrc unspaced comparators match the spaced ones", ".nvmrc", ">=18 <21\n",
			[]wantSpec{
				{">=18", "range", 1, 0, -1, 18, -1, 1, -1},
				{"<21", "range", 1, 1, -1, -1, 21, -1, 0},
			},
		},
		{
			"nvmrc spaced disjunction is one alias", ".nvmrc", "^14 || >=16\n",
			[]wantSpec{{"^14 || >=16", "alias", 1, 0, -1, -1, -1, -1, -1}},
		},
		{
			"nvmrc dangling hyphen is one alias", ".nvmrc", "20 -\n",
			[]wantSpec{{"20 -", "alias", 1, 0, -1, -1, -1, -1, -1}},
		},
		{
			"nvmrc dangling comparator is one alias", ".nvmrc", "20 >=\n",
			[]wantSpec{{"20 >=", "alias", 1, 0, -1, -1, -1, -1, -1}},
		},
		{
			"nvmrc comparator before a name breaks the list", ".nvmrc", ">= latest 20\n",
			[]wantSpec{{">= latest 20", "alias", 1, 0, -1, -1, -1, -1, -1}},
		},
		{
			"nvmrc comparator before a name is one alias", ".nvmrc", ">= latest\n",
			[]wantSpec{{">= latest", "alias", 1, 0, -1, -1, -1, -1, -1}},
		},
		{
			"nvmrc placeholder hyphen range is one unresolved", ".nvmrc", "${LO} - ${HI}\n",
			[]wantSpec{{"${LO} - ${HI}", "unresolved", 1, 0, -1, -1, -1, -1, -1}},
		},
		{
			"node-version tab-spaced hyphen range", ".node-version", "18\t-\t20 # range\n",
			[]wantSpec{{"18\t-\t20", "alias", 1, 0, -1, -1, -1, -1, -1}},
		},
		{
			"python every line keeps its own rule", ".python-version", "3.11 - 3.12\n3.10.2\n",
			[]wantSpec{
				{"3.11 - 3.12", "alias", 1, 0, -1, -1, -1, -1, -1},
				{"3.10.2", "exact", 2, 0, 3, -1, -1, -1, -1},
			},
		},
		{
			"tool-versions fallback list stays two exact facts", ".tool-versions", "nodejs 20.11.1 18.19.0\n",
			[]wantSpec{
				{"20.11.1", "exact", 1, 0, 20, -1, -1, -1, -1},
				{"18.19.0", "exact", 1, 1, 18, -1, -1, -1, -1},
			},
		},
		{
			"tool-versions hyphen range is one alias", ".tool-versions", "nodejs 18 - 20\npython 3.12\n",
			[]wantSpec{
				{"18 - 20", "alias", 1, 0, -1, -1, -1, -1, -1},
				{"3.12", "partial", 2, 0, 3, -1, -1, -1, -1},
			},
		},
		{
			"tool-versions spaced equals", ".tool-versions", "nodejs = 20\n",
			[]wantSpec{{"= 20", "partial", 1, 0, 20, -1, -1, -1, -1}},
		},
		{
			"tool-versions spaced comparator then a fallback", ".tool-versions", "nodejs >= 18 20.1.0\n",
			[]wantSpec{
				{">= 18", "range", 1, 0, -1, 18, -1, 1, -1},
				{"20.1.0", "exact", 1, 1, 20, -1, -1, -1, -1},
			},
		},
		{
			"sdkmanrc hyphen range matches the whitespace files", ".sdkmanrc", "java=18 - 20\n",
			[]wantSpec{{"18 - 20", "alias", 1, 0, -1, -1, -1, -1, -1}},
		},
		{
			"sdkmanrc disjunction matches the whitespace files", ".sdkmanrc", "java=^14 || >=16\n",
			[]wantSpec{{"^14 || >=16", "alias", 1, 0, -1, -1, -1, -1, -1}},
		},
		{
			"sdkmanrc spaced equals", ".sdkmanrc", "java== 17\n",
			[]wantSpec{{"= 17", "partial", 1, 0, 17, -1, -1, -1, -1}},
		},
		{
			"rust legacy hyphen range is one alias", "rust-toolchain", "1.70 - 1.80\n",
			[]wantSpec{{"1.70 - 1.80", "alias", 1, 0, -1, -1, -1, -1, -1}},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkSpecs(t, tc.path, tc.body, tc.want)
		})
	}
}
