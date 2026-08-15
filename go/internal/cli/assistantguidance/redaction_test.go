// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package assistantguidance

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// canarySentinel is planted INSIDE an ordinary-looking value in the operator's
// own instruction file. Nothing about the key it sits under looks sensitive:
// the point is that this package must not echo the operator's file content into
// its output regardless of what that content is, and must not let any of it
// reach the bytes of the managed block.
const canarySentinel = "ESHUCANARY6059"

// canaryCarriers vary the character immediately before the sentinel, because a
// screen that only matches at a token boundary passes vacuously on real data.
// Each carrier is a plausible line in a project instruction file.
func canaryCarriers() []struct{ name, line string } {
	return []struct{ name, line string }{
		{"segment start", canarySentinel + "-preamble"},
		{"letter", "note: deploy x" + canarySentinel},
		{"space", "note: deploy " + canarySentinel},
		{"at sign", "contact: releases@" + canarySentinel + ".example"},
		{"double quote", `note: "` + canarySentinel + `"`},
		{"colon", "note:" + canarySentinel},
		{"dot", "host: build." + canarySentinel + ".example"},
		{"dash", "flag: --tag-" + canarySentinel},
		{"slash", "path: vendor/" + canarySentinel + "/README"},
	}
}

// TestCanaryNeverReachesRenderedOutputOrTheManagedBlock is the redaction screen
// for this family. Every carrier runs the real install/status/uninstall path
// against a real temp directory, then asserts:
//
//   - POSITIVE CONTROL: the sentinel IS in the file on disk. This package
//     preserves the operator's content verbatim, so a run where the screen
//     finds the sentinel nowhere would be a broken screen, not a clean result.
//   - the managed block written to disk is byte-identical to
//     RenderBlock(GuidanceBody(p)) -- no operator byte crossed into it.
//   - none of the three rendered outputs contains the sentinel.
//   - the absolute project root (also operator-supplied, and able to carry a
//     username) does not appear in rendered output; only paths relative to it
//     are printed.
func TestCanaryNeverReachesRenderedOutputOrTheManagedBlock(t *testing.T) {
	p := claudePlatform(t)
	want := RenderBlock(GuidanceBody(p))

	for _, carrier := range canaryCarriers() {
		t.Run(carrier.name, func(t *testing.T) {
			// The root directory name carries the sentinel too, so an
			// absolute-path leak into output fails this test as well.
			root := filepath.Join(t.TempDir(), "proj-"+canarySentinel)
			if err := os.MkdirAll(root, 0o755); err != nil {
				t.Fatalf("mkdir root: %v", err)
			}
			path := filepath.Join(root, p.RelPath)
			seeded := "# Team Rules\n\n" + carrier.line + "\n"
			if err := os.WriteFile(path, []byte(seeded), 0o644); err != nil {
				t.Fatalf("seed: %v", err)
			}

			e := NewEngine(root)
			installed, err := e.Install([]Platform{p})
			if err != nil {
				t.Fatalf("install: %v", err)
			}
			onDisk := readFile(t, path)

			// Positive control: the screen must be able to see the sentinel.
			if !strings.Contains(onDisk, canarySentinel) {
				t.Fatalf("positive control failed: carrier %q is not in the file this screen reads:\n%s",
					carrier.line, onDisk)
			}
			if !strings.Contains(onDisk, carrier.line) {
				t.Fatalf("install mangled the operator's line %q:\n%s", carrier.line, onDisk)
			}

			// The managed block is a pure function of the platform.
			start, end, found := FindBlock(onDisk)
			if !found {
				t.Fatalf("no managed block written:\n%s", onDisk)
			}
			if got := onDisk[start:end]; got != want {
				t.Fatalf("managed block carries bytes it did not compose:\n want=%q\n  got=%q", want, got)
			}

			statused, err := e.Status([]Platform{p})
			if err != nil {
				t.Fatalf("status: %v", err)
			}
			uninstalled, err := e.Uninstall([]Platform{p})
			if err != nil {
				t.Fatalf("uninstall: %v", err)
			}

			renderings := map[string]string{
				"install": renderToString(t, func(w *bytes.Buffer) error {
					return RenderInstall(w, root, installed, false)
				}, false),
				"status": renderToString(t, func(w *bytes.Buffer) error {
					return RenderStatus(w, root, statused, false)
				}, false),
			}
			var uninstallBuf bytes.Buffer
			RenderUninstall(&uninstallBuf, root, uninstalled)
			renderings["uninstall"] = uninstallBuf.String()

			for name, out := range renderings {
				if out == "" {
					t.Fatalf("%s rendering was empty; the screen had nothing to check", name)
				}
				if strings.Contains(out, canarySentinel) {
					t.Fatalf("%s output leaked the operator's content (carrier %q):\n%s",
						name, carrier.line, out)
				}
				if strings.Contains(out, root) {
					t.Fatalf("%s output leaked the absolute project root:\n%s", name, out)
				}
			}
		})
	}
}

// TestGuidanceBodyIsIndependentOfOperatorInput states the property the canary
// test enforces case by case: for every platform the body is a fixed string,
// so no --path, --platform, or file content can change it.
func TestGuidanceBodyIsIndependentOfOperatorInput(t *testing.T) {
	for _, p := range SupportedPlatforms() {
		body := GuidanceBody(p)
		if strings.Contains(body, canarySentinel) {
			t.Fatalf("%s: guidance body contains the sentinel", p.ID)
		}
		// Same platform value, freshly looked up: identical bytes.
		again, ok := LookupPlatform(p.ID)
		if !ok {
			t.Fatalf("%s: platform not found on lookup", p.ID)
		}
		if GuidanceBody(again) != body {
			t.Fatalf("%s: guidance body is not a pure function of the platform", p.ID)
		}
	}
}
