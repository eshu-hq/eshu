// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import "testing"

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
