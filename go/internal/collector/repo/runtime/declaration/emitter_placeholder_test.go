// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
)

// TestEmitKeepsAPlaceholderSpanAsOneToken proves a placeholder that contains
// spaces is one unresolved fact with raw_spec verbatim, not one fact per
// whitespace-separated fragment. Classify alone cannot prove this: the line is
// split into tokens before Classify runs.
func TestEmitKeepsAPlaceholderSpanAsOneToken(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name, path, body string
		want             []wantFact
	}{
		{
			"template placeholder with spaces", ".nvmrc", "{{ v }}\n",
			[]wantFact{{".", "nvmrc", "node", "<nil>", "{{ v }}", "unresolved", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"ci expression with spaces", ".nvmrc", "${{ inputs.node-version }}\n",
			[]wantFact{{".", "nvmrc", "node", "<nil>", "${{ inputs.node-version }}", "unresolved", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"shell brace placeholder with spaces", ".nvmrc", "${ NODE_VERSION }\n",
			[]wantFact{{".", "nvmrc", "node", "<nil>", "${ NODE_VERSION }", "unresolved", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"cookiecutter placeholder in a pin file that reads every line", ".python-version",
			"{{ cookiecutter.python_version }}\n",
			[]wantFact{{".", "python_version", "python", "<nil>", "{{ cookiecutter.python_version }}", "unresolved", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"a version then a placeholder are two tokens", ".nvmrc", "20 {{ v }}\n",
			[]wantFact{
				{".", "nvmrc", "node", "<nil>", "20", "partial", "<nil>", 1, 0, 20, -1, -1},
				{".", "nvmrc", "node", "<nil>", "{{ v }}", "unresolved", "<nil>", 1, 1, -1, -1, -1},
			},
		},
		{
			"an unterminated opener keeps the rest of the line", ".nvmrc", "${{ x\n",
			[]wantFact{{".", "nvmrc", "node", "<nil>", "${{ x", "unresolved", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"an unterminated opener after a version", ".nvmrc", "20 {{ x y\n",
			[]wantFact{
				{".", "nvmrc", "node", "<nil>", "20", "partial", "<nil>", 1, 0, 20, -1, -1},
				{".", "nvmrc", "node", "<nil>", "{{ x y", "unresolved", "<nil>", 1, 1, -1, -1, -1},
			},
		},
		{
			"adjacent placeholders with no space are one token", ".nvmrc", "{{ a }}{{ b }}\n",
			[]wantFact{{".", "nvmrc", "node", "<nil>", "{{ a }}{{ b }}", "unresolved", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"nested placeholders close on the matching closer", ".nvmrc", "{{ a {{ b }} }} 20\n",
			[]wantFact{
				{".", "nvmrc", "node", "<nil>", "{{ a {{ b }} }}", "unresolved", "<nil>", 1, 0, -1, -1, -1},
				{".", "nvmrc", "node", "<nil>", "20", "partial", "<nil>", 1, 1, 20, -1, -1},
			},
		},
		{
			"a placeholder inside a version token", ".nvmrc", "20.${ MINOR }\n",
			[]wantFact{{".", "nvmrc", "node", "<nil>", "20.${ MINOR }", "unresolved", "<nil>", 1, 0, -1, -1, -1}},
		},
		{
			"tool-versions placeholder after the tool name", ".tool-versions", "nodejs {{ v }} 18.0.0\n",
			[]wantFact{
				{".", "tool_versions", "node", "nodejs", "{{ v }}", "unresolved", "<nil>", 1, 0, -1, -1, -1},
				{".", "tool_versions", "node", "nodejs", "18.0.0", "exact", "<nil>", 1, 1, 18, 0, 0},
			},
		},
		{
			"plain rust-toolchain placeholder", "rust-toolchain", "{{ channel }}\n",
			[]wantFact{{".", "rust_toolchain", "rust", "<nil>", "{{ channel }}", "unresolved", "<nil>", 1, 0, -1, -1, -1}},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			checkFacts(t, emitDecoded(t, tc.path, tc.body), tc.want)
		})
	}
}

// TestEmitPlaceholderScanIsLinear proves a hostile line of unclosed openers
// stays one token and finishes in a bounded number of steps (the test asserts
// the result shape, not wall time).
func TestEmitPlaceholderScanIsLinear(t *testing.T) {
	t.Parallel()

	body := strings.Repeat("{{ ", 50000) + "\n"
	got := emitDecoded(t, ".nvmrc", body)
	if len(got) != 1 || got[0].SpecKind != SpecKindUnresolved {
		t.Fatalf("hostile opener line emitted %d facts, want 1 unresolved", len(got))
	}
}
