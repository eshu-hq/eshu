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

	shimRaw, err := os.ReadFile(filepath.Join("..", "SHIM_MAP.md"))
	if err != nil {
		t.Fatalf("read SHIM_MAP.md: %v", err)
	}
	goModRaw, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "go.mod"))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	for _, offense := range pinParityOffenses(string(shimRaw), string(goModRaw)) {
		t.Fatal(offense)
	}
}

// pinParityOffenses compares the cutover contract (SHIM_MAP.md content)
// against the built module file (go.mod content) and reports every drift
// as a human-readable offense. Empty means parity holds.
func pinParityOffenses(shimContent, goModContent string) []string {
	var offenses []string
	pinned := parseShimPins(shimContent)
	built, replaceOffenses := parseGoModPins(goModContent)
	for _, offense := range replaceOffenses {
		offenses = append(offenses, "go.mod replaces an eshu-sdk module ("+offense+"); pin-parity needs exact published versions")
	}
	for module, want := range pinned {
		got, ok := built[module]
		if !ok {
			offenses = append(offenses, "SHIM_MAP.md pins "+module+" but the test binary was not built against it")
			continue
		}
		if got != want {
			offenses = append(offenses, "SDK pin drift for "+module+": SHIM_MAP.md says "+want+", go.mod builds "+got+"; update SHIM_MAP.md in the same reviewed change as the go.mod bump (Refs #8865)")
		}
	}
	for module, got := range built {
		if _, ok := pinned[module]; !ok {
			offenses = append(offenses, "go.mod builds "+module+"@"+got+" with no SHIM_MAP.md pin; record the new SDK dependency in SHIM_MAP.md in the same reviewed change (Refs #8865)")
		}
	}
	return offenses
}

// TestSBOM8865NewModuleFailsClosed is the regression for the repeat P1: a
// go.mod that gains a new eshu-sdk module (here hyphenated, which the old
// [a-z]+ readers could not even parse) alongside correct pins for the
// known two must report an offense so the reviewed change records it.
func TestSBOM8865NewModuleFailsClosed(t *testing.T) {
	t.Parallel()

	shim := "- `github.com/eshu-hq/eshu-sdk/collector v0.1.0-rc.3`\n" +
		"- `github.com/eshu-hq/eshu-sdk/factschema v0.1.0-rc.6`\n"
	goMod := "module github.com/eshu-hq/eshu/go\n\n" +
		"require (\n" +
		"\tgithub.com/eshu-hq/eshu-sdk/collector v0.1.0-rc.3\n" +
		"\tgithub.com/eshu-hq/eshu-sdk/factschema v0.1.0-rc.6\n" +
		"\tgithub.com/eshu-hq/eshu-sdk/new-module v0.1.0-rc.1\n" +
		")\n"
	offenses := pinParityOffenses(shim, goMod)
	for _, o := range offenses {
		t.Logf("offense: %s", o)
	}
	if len(offenses) == 0 {
		t.Fatal("pin parity passed with an unrecorded eshu-sdk module (new-module); a new SDK dependency must fail closed until SHIM_MAP.md records it (Refs #8865)")
	}
}

// parseShimPins extracts the exact pinned SDK versions recorded in the
// cutover contract doc content. Lines look like:
//
//   - `github.com/eshu-hq/eshu-sdk/collector v0.1.0-rc.3`
//
// The module-name class covers the real module-name charset (letters,
// digits, dots, hyphens, underscores) so hyphenated SDK modules cannot
// slip past the reader.
func parseShimPins(content string) map[string]string {
	re := regexp.MustCompile("`(" + regexp.QuoteMeta("github.com/eshu-hq/eshu-sdk/") + "[A-Za-z0-9][A-Za-z0-9._~-]*) (v[0-9][^`]*)`")
	pins := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(content, -1) {
		pins[string(m[1])] = string(m[2])
	}
	return pins
}

// parseGoModPins reports the SDK versions a module file builds against.
// It returns the parsed pins plus every eshu-sdk replace offense; a
// replace directive on an SDK module fails closed because pin-parity
// needs exact published versions, not a local checkout.
func parseGoModPins(content string) (map[string]string, []string) {
	pins := map[string]string{}
	requireRe := regexp.MustCompile(`(?m)^\s*(github\.com/eshu-hq/eshu-sdk/[A-Za-z0-9][A-Za-z0-9._~-]*)\s+(v[0-9][^\s]*)`)
	for _, m := range requireRe.FindAllStringSubmatch(content, -1) {
		pins[string(m[1])] = string(m[2])
	}
	return pins, sdkReplaceOffenses(content)
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
