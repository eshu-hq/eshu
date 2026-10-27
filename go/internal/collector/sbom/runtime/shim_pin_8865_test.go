// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package runtime

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Pin-parity guard for #8865 (mirror-harness side, in-tree).
//
// The #4047 cutover gate compares extracted-path output against in-tree
// output on identical inputs. Identical inputs include the SDK: if go.mod
// moves to a newer eshu-sdk/collector or eshu-sdk/factschema without
// updating the cutover contract (SHIM_MAP.md), the extracted port — pinned
// separately in its own module — silently diverges from the in-tree
// baseline. This test fails loudly on that drift so the reviewed bump
// updates the map in the same change.
//
// Producer authority is out of scope here: the grant draft and the
// producer-record reconciliation stay with the owner/arbiter correlation
// gate. This guard covers build pins only.
func TestSBOM8865ShimPinsMatchGoMod(t *testing.T) {
	t.Parallel()

	pinned := readShimPins(t)
	built := readGoModPins(t)

	for module, want := range pinned {
		got, ok := built[module]
		if !ok {
			t.Fatalf("SHIM_MAP.md pins %s but the test binary was not built against it", module)
		}
		if got != want {
			t.Fatalf("SDK pin drift for %s: SHIM_MAP.md says %s, go.mod builds %s; update SHIM_MAP.md in the same reviewed change as the go.mod bump (Refs #8865)",
				module, want, got)
		}
	}
}

// readShimPins extracts the exact pinned SDK versions recorded in the
// cutover contract doc. Lines look like:
//
//   - `github.com/eshu-hq/eshu-sdk/collector v0.1.0-rc.3`
func readShimPins(t *testing.T) map[string]string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "SHIM_MAP.md"))
	if err != nil {
		t.Fatalf("read SHIM_MAP.md: %v", err)
	}
	re := regexp.MustCompile("`(" + regexp.QuoteMeta("github.com/eshu-hq/eshu-sdk/") + "[a-z]+) (v[0-9][^`]*)`")
	pins := map[string]string{}
	for _, m := range re.FindAllSubmatch(raw, -1) {
		pins[string(m[1])] = string(m[2])
	}
	for _, module := range []string{
		"github.com/eshu-hq/eshu-sdk/collector",
		"github.com/eshu-hq/eshu-sdk/factschema",
	} {
		if pins[module] == "" {
			t.Fatalf("SHIM_MAP.md records no exact pin for %s", module)
		}
	}
	return pins
}

// readGoModPins reports the SDK versions the lane builds against,
// read from the module file (`go test` runs with the package directory as
// the working directory, so this path is repo-deterministic). A replace
// directive on either SDK module fails closed: pin-parity needs exact
// published versions, not a local checkout.
func readGoModPins(t *testing.T) map[string]string {
	t.Helper()

	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	pins := map[string]string{}
	requireRe := regexp.MustCompile(`(?m)^\s*(github\.com/eshu-hq/eshu-sdk/[a-z]+)\s+(v[0-9][^\s]*)`)
	for _, m := range requireRe.FindAllSubmatch(raw, -1) {
		pins[string(m[1])] = string(m[2])
	}
	for _, offense := range sdkReplaceOffenses(string(raw)) {
		t.Fatalf("go.mod replaces an eshu-sdk module (%q); pin-parity needs exact published versions", offense)
	}
	for _, module := range []string{
		"github.com/eshu-hq/eshu-sdk/collector",
		"github.com/eshu-hq/eshu-sdk/factschema",
	} {
		if pins[module] == "" {
			t.Fatalf("go.mod records no pin for %s", module)
		}
	}
	return pins
}

// sdkReplaceOffenses reports every go.mod line that redirects an eshu-sdk
// module, covering both the single-line form (`replace <module> => ...`)
// and the parenthesized block form (`replace ( ... )`). Pin-parity needs
// exact published versions, so any redirect is an offense.
func sdkReplaceOffenses(goMod string) []string {
	var offenses []string
	inReplaceBlock := false
	for _, line := range strings.Split(goMod, "\n") {
		// go.mod carries only '//' line comments; strip them first so a
		// parenthesis inside a comment can neither close a replace block
		// nor mask a redirect. Comment-only lines are skipped entirely.
		trimmed := strings.TrimSpace(line)
		if i := strings.Index(trimmed, "//"); i >= 0 {
			trimmed = strings.TrimSpace(trimmed[:i])
		}
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "replace") {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "replace"))
			if strings.HasPrefix(rest, "(") {
				inReplaceBlock = true
				continue
			}
			if strings.Contains(trimmed, "eshu-hq/eshu-sdk/") {
				offenses = append(offenses, trimmed)
			}
			continue
		}
		if inReplaceBlock {
			if strings.Contains(trimmed, "eshu-hq/eshu-sdk/") {
				offenses = append(offenses, trimmed)
			}
			if strings.Contains(trimmed, ")") {
				inReplaceBlock = false
			}
		}
	}
	return offenses
}

// TestSBOM8865ReplaceFormsFailClosed pins the replace detector itself:
// single-line and block-form redirects of either SDK module are offenses,
// while unrelated redirects and a clean module file are not.
func TestSBOM8865ReplaceFormsFailClosed(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		goMod string
		want  int
	}{
		"single line replace": {
			goMod: "module example.test\n\nrequire github.com/eshu-hq/eshu-sdk/collector v0.1.0\n\nreplace github.com/eshu-hq/eshu-sdk/collector => ../local\n",
			want:  1,
		},
		"block replace": {
			goMod: "module example.test\n\nrequire github.com/eshu-hq/eshu-sdk/collector v0.1.0\n\nreplace (\n\tgithub.com/eshu-hq/eshu-sdk/collector => ../local\n)\n",
			want:  1,
		},
		"block replace factschema": {
			goMod: "module example.test\n\nreplace (\n\tgithub.com/eshu-hq/eshu-sdk/factschema => github.com/fork/factschema v0.0.1\n)\n",
			want:  1,
		},
		"unrelated replace": {
			goMod: "module example.test\n\nreplace github.com/tree-sitter/tree-sitter-elixir => github.com/elixir-lang/tree-sitter-elixir v0.3.5\n",
			want:  0,
		},
		"unrelated block replace": {
			goMod: "module example.test\n\nreplace (\n\tgithub.com/tree-sitter/tree-sitter-elixir => github.com/elixir-lang/tree-sitter-elixir v0.3.5\n)\n",
			want:  0,
		},
		"clean": {
			goMod: "module example.test\n\nrequire github.com/eshu-hq/eshu-sdk/collector v0.1.0\n",
			want:  0,
		},
		"paren comment before entry": {
			goMod: "module example.test\n\nreplace (\n\t// see docs (note)\n\tgithub.com/eshu-hq/eshu-sdk/collector => ../local\n)\n",
			want:  1,
		},
		"trailing paren comment on unrelated entry": {
			goMod: "module example.test\n\nreplace (\n\tgithub.com/tree-sitter/tree-sitter-elixir => github.com/elixir-lang/tree-sitter-elixir v0.3.5 // ok (pinned)\n\tgithub.com/eshu-hq/eshu-sdk/collector => ../local\n)\n",
			want:  1,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := sdkReplaceOffenses(tc.goMod); len(got) != tc.want {
				t.Fatalf("sdkReplaceOffenses() = %d offense(s) %q, want %d", len(got), got, tc.want)
			}
		})
	}
}
