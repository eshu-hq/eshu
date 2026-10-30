// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestStackManifestSpecAndFailClosedCases pins the #9308 fixture matrix for
// the stack.yaml resolver: exact, range, alias and unresolved values emit,
// while malformed files emit nothing with a skip reason and
// credential-shaped values are dropped and counted.
func TestStackManifestSpecAndFailClosedCases(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, kind string
		facts, rejected        int
		skip                   string
	}{
		{"stack-exact", "stack.yaml", "resolver: \"9.6.4\"\n", "exact", 1, 0, ""},
		{"stack-range", "stack.yaml", "resolver: \">=9.6 <9.9\"\n", "range", 1, 0, ""},
		{"stack-alias", "stack.yaml", "resolver: lts-22.43\n", "alias", 1, 0, ""},
		{"stack-unresolved", "stack.yaml", "resolver: ${STACK_RESOLVER}\n", "unresolved", 1, 0, ""},
		{"stack-malformed", "stack.yaml", "resolver: [unclosed\n", "", 0, 0, SkipReasonManifestUnparseable},
		{"stack-credential", "stack.yaml", "resolver: https://user:password@example.com/lts\n", "", 0, 1, SkipReasonValueRejected},
		{"stack-absent", "stack.yaml", "packages:\n  - .\n", "", 0, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extraction, ok := Extract(tc.path, tc.body)
			if !ok {
				t.Fatalf("%s not admitted", tc.path)
			}
			facts, skip := EmitExtraction(testContext(tc.path), "repo-1", tc.path, extraction)
			if len(facts) != tc.facts || skip.Rejected != tc.rejected || skip.Reason != tc.skip {
				t.Fatalf("facts=%d skip=%+v, want %d rejected=%d reason=%s", len(facts), skip, tc.facts, tc.rejected, tc.skip)
			}
			if tc.facts == 1 {
				if got := classifyDeclaration(sectionSourceKinds[extraction.declarations[0].section], extraction.declarations[0]).Kind; got != tc.kind {
					t.Fatalf("spec kind=%q, want %q", got, tc.kind)
				}
			}
		})
	}
}

// TestHaskellCabalManifestContentOnlyDispatch drives the manifestHaskellCabal
// Extract arm through the content-only path.
func TestHaskellCabalManifestContentOnlyDispatch(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path, body, kind, raw, spec string
		line, index                       int
	}{
		{"cabal exact", "demo.cabal", "tested-with: GHC==9.4.8\n", "cabal_tested_with", "GHC==9.4.8", "alias", 1, 0},
		{"cabal unresolved", "demo.cabal", "tested-with: ${GHC}\n", "cabal_tested_with", "${GHC}", "unresolved", 1, 0},
		{"nested cabal first", "services/api/demo.cabal", "name: demo\ntested-with: GHC==9.4.8,\n  GHC==9.6.4\n", "cabal_tested_with", "GHC==9.4.8", "alias", 2, 0},
		{"nested cabal second", "services/api/demo.cabal", "name: demo\ntested-with: GHC==9.4.8,\n  GHC==9.6.4\n", "cabal_tested_with", "GHC==9.6.4", "alias", 3, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := emitDecoded(t, tc.path, tc.body)
			if len(got) <= tc.index {
				t.Fatalf("Emit(%q) emitted %d declarations, want index %d", tc.path, len(got), tc.index)
			}
			first := got[tc.index]
			if first.SourceKind != tc.kind || first.RawSpec != tc.raw || first.SpecKind != tc.spec || first.Runtime != "haskell" || first.LineNumber != tc.line {
				t.Fatalf("fact = %+v, want kind=%s raw=%s spec=%s line=%d", first, tc.kind, tc.raw, tc.spec, tc.line)
			}
		})
	}
}

// TestHaskellCabalManifestSkips pins fail-closed, dropped, and capped reads.
func TestHaskellCabalManifestSkips(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, path, body, reason string
		rejected                 int
	}{
		{"cabal malformed", "demo.cabal", "name: demo\ntested-with: GHC==9.4.8\x00\n", SkipReasonManifestUnparseable, 0},
		{"cabal credential", "demo.cabal", "tested-with: GHC==9.4.8, https://user:secret@example.invalid\n", SkipReasonValueRejected, 1},
		{"cabal tokens", "demo.cabal", "tested-with: " + strings.Repeat("GHC==9.4.8,", 1025) + "\n", SkipReasonTokenCap, 0},
		{"cabal bytes", "demo.cabal", "tested-with: GHC==9.4.8, " + strings.Repeat("x", MaxManifestBytesPerFile), SkipReasonByteCap, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			envelopes, skip := EmitFile(testContext(tc.path), "repo-1", tc.path, tc.body)
			if tc.reason != SkipReasonValueRejected && len(envelopes) != 0 {
				t.Fatalf("%s emitted %d declarations", tc.name, len(envelopes))
			}
			wantCap := 0
			if tc.reason == SkipReasonByteCap {
				wantCap = MaxManifestBytesPerFile
			}
			if skip.Reason != tc.reason || skip.Rejected != tc.rejected || skip.ByteCap != wantCap {
				t.Fatalf("skip = %+v, want reason=%s rejected=%d", skip, tc.reason, tc.rejected)
			}
			for _, env := range envelopes {
				raw, err := json.Marshal(env.Payload)
				if err != nil {
					t.Fatal(err)
				}
				if strings.Contains(string(raw), "user:secret") {
					t.Fatal("credential-shaped value persisted in payload")
				}
			}
		})
	}
}

// TestStackDeclarationLineAndKind pins the source kind, runtime and source
// position of the emitted resolver declaration.
func TestStackDeclarationLineAndKind(t *testing.T) {
	facts := emitDecoded(t, "stack.yaml", "packages:\n  - .\nresolver: lts-22.43\n")
	if len(facts) != 1 || facts[0].SourceKind != "stack_yaml_resolver" || facts[0].Runtime != "haskell" || facts[0].RawSpec != "lts-22.43" || facts[0].LineNumber != 3 {
		t.Fatalf("declaration = %+v", facts)
	}
}

// TestStackLockfilesNotAdmitted pins the lane boundary: stack.yaml.lock,
// cabal.project and freeze files never reach the extractor.
func TestStackLockfilesNotAdmitted(t *testing.T) {
	for _, path := range []string{"stack.yaml.lock", "cabal.project", "stack.yaml.freeze", "sub/stack.yaml.lock"} {
		if extraction, ok := Extract(path, "resolver: lts-22.43\n"); ok {
			t.Fatalf("%s admitted with %+v", path, extraction)
		}
	}
}

// TestHaskellCabalClassifyPath pins .cabal admission and neighbor exclusion.
// stack.yaml is admitted by the stack lane, so it is not in the exclusion
// list; the stack boundary tests pin its admission.
func TestHaskellCabalClassifyPath(t *testing.T) {
	t.Parallel()
	if got := ClassifyPath("services/api/demo.cabal"); got != FileManifest {
		t.Fatalf("ClassifyPath(demo.cabal) = %v, want FileManifest", got)
	}
	for _, path := range []string{"cabal.project", "cabal.project.freeze", "stack.yaml.lock"} {
		if got := ClassifyPath(path); got != FileNotAdmitted {
			t.Fatalf("ClassifyPath(%q) = %v, want FileNotAdmitted", path, got)
		}
	}
}
