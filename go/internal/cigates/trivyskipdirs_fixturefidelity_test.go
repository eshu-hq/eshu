// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// trivyHelperFilterPipelinePrefix and trivyHelperFilterPipelineSuffix are the
// two pieces of trivy_skip_dirs_csv's filter pipeline that do not vary with
// which path is being read -- the specs-file path expression between them
// differs, by design, between the real helper's own "${specs_path}"
// variable and validHelperScriptBody's fixture. Both pieces, byte-for-byte,
// must appear in the real committed helper AND in validHelperScriptBody, as
// the FULL, UNMODIFIED pipeline line -- see trivyFilterPipelineLine, which
// anchors the check to the whole line rather than to two independent
// substrings (round-3 review P3-6).
const (
	trivyHelperFilterPipelinePrefix = `sed $'s/\r$//' <"`
	trivyHelperFilterPipelineSuffix = `" | grep -v '^[[:space:]]*#' | grep -v '^[[:space:]]*$' | paste -sd, -`
)

// trivyFilterPipelineLine returns the single line of content containing
// trivyHelperFilterPipelinePrefix, with leading/trailing whitespace trimmed,
// and fails the test if no line contains it or if more than one does -- the
// same "exactly one, not merely present" arity convention the rest of this
// package's trivy checks use.
func trivyFilterPipelineLine(t *testing.T, content, label string) string {
	t.Helper()

	var matches []string
	for _, line := range strings.Split(content, "\n") {
		if strings.Contains(line, trivyHelperFilterPipelinePrefix) {
			matches = append(matches, strings.TrimSpace(line))
		}
	}
	switch len(matches) {
	case 1:
		return matches[0]
	case 0:
		t.Fatalf("%s: no line contains %q", label, trivyHelperFilterPipelinePrefix)
	default:
		t.Fatalf("%s: %d lines contain %q, want exactly 1: %v", label, len(matches), trivyHelperFilterPipelinePrefix, matches)
	}
	return ""
}

// TestTrivySkipDirsParity_FixtureHelperFilterMatchesRealHelper pins
// single-source review P3-4: validHelperScriptBody's doc comment (in
// trivyskipdirs_test.go) claims it "uses the same filters the real committed
// helper does" (#5925 F6), but that fixture is only ever fed to a Go
// reference-counting check -- never executed -- so nothing enforced the
// claim. If scripts/lib/trivy-skip-dirs.sh's real filters ever changed (a
// different line-ending strip, a differently anchored comment filter, a
// different join delimiter), the comment would silently go stale and every
// test built on validHelperScriptBody would keep passing against a fixture
// that no longer matches production. This test fails loudly instead.
//
// Round-3 review (P3-6) tightened the shape of that proof: the original
// version checked trivyHelperFilterPipelinePrefix and
// trivyHelperFilterPipelineSuffix as two INDEPENDENT substrings anywhere in
// the file, which a pipeline stage APPENDED after `paste -sd, -` (or
// inserted between the prefix and suffix) would keep satisfying, since both
// pieces would still be present verbatim -- the test would stay green while
// the real pipeline's actual behavior changed. Anchoring to the WHOLE,
// trimmed line that carries the pipeline instead closes that: the line must
// both START with the prefix (after only leading whitespace) and END with
// the suffix (nothing trailing it, not even whitespace), so a stage added
// before the prefix or after the suffix breaks the prefix/suffix anchors and
// fails the test. A stage spliced into the specs-path expression BETWEEN
// them does not break either anchor, and is not meant to: the middle of the
// line is unconstrained by design (see the doc comment above), since that is
// exactly where the real helper's own "${specs_path}" variable and this
// file's fixture value are expected to differ.
func TestTrivySkipDirsParity_FixtureHelperFilterMatchesRealHelper(t *testing.T) {
	t.Parallel()

	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	realHelperBytes, err := os.ReadFile(filepath.Join(repoRoot, "scripts", "lib", "trivy-skip-dirs.sh"))
	if err != nil {
		t.Fatalf("reading the real committed helper: %v", err)
	}

	realLine := trivyFilterPipelineLine(t, string(realHelperBytes), "the real committed helper")
	if !strings.HasPrefix(realLine, trivyHelperFilterPipelinePrefix) {
		t.Fatalf("the real committed helper's pipeline line %q does not START with %q -- its filters "+
			"changed; update trivyHelperFilterPipeline* and validHelperScriptBody to match",
			realLine, trivyHelperFilterPipelinePrefix)
	}
	if !strings.HasSuffix(realLine, trivyHelperFilterPipelineSuffix) {
		t.Fatalf("the real committed helper's pipeline line %q does not END with %q -- a stage was added "+
			"to the pipeline; update trivyHelperFilterPipeline* and validHelperScriptBody to match",
			realLine, trivyHelperFilterPipelineSuffix)
	}

	fixtureLine := trivyFilterPipelineLine(t, validHelperScriptBody(), "validHelperScriptBody's fixture")
	if !strings.HasPrefix(fixtureLine, trivyHelperFilterPipelinePrefix) {
		t.Errorf("validHelperScriptBody's fixture line %q does not START with %q -- it has drifted from "+
			"the real committed helper's filters", fixtureLine, trivyHelperFilterPipelinePrefix)
	}
	if !strings.HasSuffix(fixtureLine, trivyHelperFilterPipelineSuffix) {
		t.Errorf("validHelperScriptBody's fixture line %q does not END with %q -- it has drifted from "+
			"the real committed helper's filters", fixtureLine, trivyHelperFilterPipelineSuffix)
	}
}
