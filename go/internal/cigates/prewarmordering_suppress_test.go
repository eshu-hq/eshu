// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cigates_test

import (
	"strings"
	"testing"
)

// The shapes below all leave the pre-warm's own failure invisible to the
// job, which is the one thing scripts/ci/go-mod-download-retry.sh exists to
// prevent. Measured on /bin/bash under `-e` (the runner default per
// https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax,
// which runs `bash -e {0}` and, unlike an explicit `shell: bash`, does NOT
// add `-o pipefail`):
//
//	false || true            -> 0    false | tee /dev/null   -> 0
//	false || echo failed     -> 0    false &                 -> 0
//	false || exit 0          -> 0    false && echo x         -> 1
//
// so `||` swallows the status whatever follows it, a pipeline reports only
// its last command, and a backgrounded command reports nothing at all.

func prewarmSuppressionFixture(t *testing.T, prewarmStep string) string {
	t.Helper()
	return buildPrewarmRepo(t, "fixture.yml", `name: fixture
on: [push]
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      - uses: actions/setup-go@v6
        with:
          go-version-file: go/go.mod
          cache-dependency-path: go/go.sum
`+prewarmStep+`      - name: Build
        run: go build ./...
`)
}

// TestCheckSetupGoPrewarmOrderingPipedPrewarmViolation covers the most
// natural way to defeat this guard by accident rather than by intent: a
// maintainer keeps the pre-warm's output with `| tee`, and the step now
// reports tee's status instead of the download's.
func TestCheckSetupGoPrewarmOrderingPipedPrewarmViolation(t *testing.T) {
	t.Parallel()

	root := prewarmSuppressionFixture(t, `      - name: Pre-warm (piped)
        run: scripts/ci/go-mod-download-retry.sh go | tee prewarm.log
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("piped-prewarm fixture: got %d violations, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "suppress") {
		t.Fatalf("piped-prewarm fixture: message %q does not name suppression", got[0])
	}
}

// TestCheckSetupGoPrewarmOrderingPipedPrewarmWithShellBashClean is the other
// half of the pipeline rule, and the reason it cannot be a flat "a pipe is
// always suppression": `shell: bash` runs `bash --noprofile --norc -eo
// pipefail {0}`, so the same line does propagate the failure and flagging it
// would be a false RED against a correct workflow.
func TestCheckSetupGoPrewarmOrderingPipedPrewarmWithShellBashClean(t *testing.T) {
	t.Parallel()

	root := prewarmSuppressionFixture(t, `      - name: Pre-warm (piped, pipefail shell)
        shell: bash
        run: scripts/ci/go-mod-download-retry.sh go | tee prewarm.log
`)

	if got := prewarmOrderingErrs(t, root); len(got) != 0 {
		t.Fatalf("piped-prewarm-with-shell-bash fixture: got %d unexpected violations: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrderingPipedPrewarmWithSetPipefailClean is the
// same exemption reached from inside the run body instead of the step's
// shell selector.
func TestCheckSetupGoPrewarmOrderingPipedPrewarmWithSetPipefailClean(t *testing.T) {
	t.Parallel()

	root := prewarmSuppressionFixture(t, `      - name: Pre-warm (piped, set -o pipefail)
        run: |
          set -eo pipefail
          scripts/ci/go-mod-download-retry.sh go | tee prewarm.log
`)

	if got := prewarmOrderingErrs(t, root); len(got) != 0 {
		t.Fatalf("piped-prewarm-with-set-pipefail fixture: got %d unexpected violations: %v", len(got), got)
	}
}

// TestCheckSetupGoPrewarmOrderingBackgroundedPrewarmViolation: `&` is worse
// than a swallowed status. The download is still running when the job moves
// on, so the cache save can win the race against it.
func TestCheckSetupGoPrewarmOrderingBackgroundedPrewarmViolation(t *testing.T) {
	t.Parallel()

	root := prewarmSuppressionFixture(t, `      - name: Pre-warm (backgrounded)
        run: scripts/ci/go-mod-download-retry.sh go &
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("backgrounded-prewarm fixture: got %d violations, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "suppress") {
		t.Fatalf("backgrounded-prewarm fixture: message %q does not name suppression", got[0])
	}
}

// TestCheckSetupGoPrewarmOrderingRedirectionPrewarmClean guards the change
// that recognizes `&`: `2>&1` and `&>` are redirections, not the background
// operator, and splitting on them would tear a real pre-warm invocation in
// half and stop recognizing it.
func TestCheckSetupGoPrewarmOrderingRedirectionPrewarmClean(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		run  string
	}{
		{"module-then-redirect", `scripts/ci/go-mod-download-retry.sh go 2>&1`},
		// No module argument: the redirection must not be read as the
		// module, or a correct workflow is reported as warming "2>&1".
		{"redirect-only", `scripts/ci/go-mod-download-retry.sh 2>&1`},
		{"redirect-to-file", `scripts/ci/go-mod-download-retry.sh >prewarm.log 2>&1`},
		{"detached-redirect-target", `scripts/ci/go-mod-download-retry.sh > prewarm.log`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := prewarmSuppressionFixture(t, `      - name: Pre-warm (redirected)
        run: `+tc.run+`
`)

			if got := prewarmOrderingErrs(t, root); len(got) != 0 {
				t.Fatalf("%s fixture: got %d unexpected violations: %v", tc.name, len(got), got)
			}
		})
	}
}

// TestCheckSetupGoPrewarmOrderingOrChainedPrewarmViolations: `|| true` was
// already rejected, but the rule is not about the word "true" -- any
// right-hand side that succeeds makes the list exit 0.
func TestCheckSetupGoPrewarmOrderingOrChainedPrewarmViolations(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		run  string
	}{
		{"or-echo", `scripts/ci/go-mod-download-retry.sh go || echo "pre-warm failed"`},
		{"or-exit-zero", `scripts/ci/go-mod-download-retry.sh go || exit 0`},
		// Quoted because a bare trailing ":" is a YAML key indicator and
		// makes the fixture unparseable rather than unwarmed.
		{"or-colon", `"scripts/ci/go-mod-download-retry.sh go || :"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := prewarmSuppressionFixture(t, `      - name: Pre-warm
        run: `+tc.run+`
`)

			got := prewarmOrderingErrs(t, root)
			if len(got) != 1 {
				t.Fatalf("%s fixture: got %d violations, want 1: %v", tc.name, len(got), got)
			}
			if !strings.Contains(got[0], "suppress") {
				t.Fatalf("%s fixture: message %q does not name suppression", tc.name, got[0])
			}
		})
	}
}

// TestCheckSetupGoPrewarmOrderingFailingRightHandSideClean: an RHS that
// cannot itself exit 0 leaves the failure intact, so rejecting these would
// be a false RED against a workflow that is doing the right thing -- and the
// message would state something measurably untrue about it.
func TestCheckSetupGoPrewarmOrderingFailingRightHandSideClean(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		run  string
	}{
		{"or-exit-nonzero", `scripts/ci/go-mod-download-retry.sh go || exit 1`},
		{"or-false", `scripts/ci/go-mod-download-retry.sh go || false`},
		// `cmd; X` is NOT suppression under the runner's `bash -e`: -e exits
		// at the failing pre-warm and X never runs. Measured: `bash -e -c
		// 'false; true'` exits 1.
		{"semicolon-true", `scripts/ci/go-mod-download-retry.sh go; true`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			root := prewarmSuppressionFixture(t, `      - name: Pre-warm
        run: `+tc.run+`
`)

			if got := prewarmOrderingErrs(t, root); len(got) != 0 {
				t.Fatalf("%s fixture: got %d unexpected violations: %v", tc.name, len(got), got)
			}
		})
	}
}

// TestCheckSetupGoPrewarmOrderingPipefailToggledOffViolation: pipefail is
// state, not a one-way switch, so the LAST `set ±o pipefail` before the
// pre-warm decides. Measured: `bash -e -c 'set -o pipefail; set +o pipefail;
// false | tee /dev/null'` exits 0.
func TestCheckSetupGoPrewarmOrderingPipefailToggledOffViolation(t *testing.T) {
	t.Parallel()

	root := prewarmSuppressionFixture(t, `      - name: Pre-warm (pipefail turned back off)
        run: |
          set -eo pipefail
          set +o pipefail
          scripts/ci/go-mod-download-retry.sh go | tee prewarm.log
`)

	got := prewarmOrderingErrs(t, root)
	if len(got) != 1 {
		t.Fatalf("pipefail-toggled-off fixture: got %d violations, want 1: %v", len(got), got)
	}
	if !strings.Contains(got[0], "suppress") {
		t.Fatalf("pipefail-toggled-off fixture: message %q does not name suppression", got[0])
	}
}

// TestCheckSetupGoPrewarmOrderingAndChainedPrewarmClean is the control that
// keeps the `||` rule from swallowing `&&`: when the pre-warm fails, the
// right-hand side never runs and the list still exits non-zero.
func TestCheckSetupGoPrewarmOrderingAndChainedPrewarmClean(t *testing.T) {
	t.Parallel()

	root := prewarmSuppressionFixture(t, `      - name: Pre-warm
        run: scripts/ci/go-mod-download-retry.sh go && echo warmed
`)

	if got := prewarmOrderingErrs(t, root); len(got) != 0 {
		t.Fatalf("and-chained-prewarm fixture: got %d unexpected violations: %v", len(got), got)
	}
}
