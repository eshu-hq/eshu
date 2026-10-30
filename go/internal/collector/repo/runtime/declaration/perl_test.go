// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestPerlCpanfileContentOnlyEntrypoint proves the cpanfile manifest path:
// literal requires 'perl' lines become perl facts, the rest is silent or
// a counted rejection, and the snapshot sibling is never admitted.
func TestPerlCpanfileContentOnlyEntrypoint(t *testing.T) {
	t.Parallel()

	if got := ClassifyPath("cpanfile"); got != FileManifest {
		t.Fatalf("ClassifyPath(%q) = %v, want FileManifest", "cpanfile", got)
	}
	if _, ok := Extract("cpanfile.snapshot", "requires 'perl', '5.38.0';\n"); ok || ClassifyPath("cpanfile.snapshot") != FileNotAdmitted {
		t.Fatal("cpanfile.snapshot is admitted")
	}
	for _, tc := range []struct {
		name, body, kind, spec string
		want, rejected         int
	}{
		{"top-level requires", "requires 'perl', '5.38.0';\n", "cpanfile_perl", "exact", 1, 0},
		{"fat comma", "requires \"perl\" => \"5.020\";\n", "cpanfile_perl", "partial", 1, 0},
		{"phase block keeps its line", "on 'configure' => sub {\n  requires 'perl', '5.008';\n};\n", "cpanfile_perl", "partial", 1, 0},
		{"unresolved placeholder", "requires 'perl', '${PERL_VERSION}';\n", "cpanfile_perl", "unresolved", 1, 0},
		{"other module is silent", "requires 'Moose', '2.2010';\n", "", "", 0, 0},
		{"credential-shaped version is dropped", "requires 'perl', '0123456789abcdef-secret';\n", "", "", 0, 1},
		{"dynamic version is dropped", "requires 'perl', $version;\n", "", "", 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes, skip := EmitFile(testContext("cpanfile"), "repo-1", "cpanfile", tc.body)
			if len(envelopes) != tc.want || skip.Rejected != tc.rejected {
				t.Fatalf("Emit(%q) emitted %d facts, want %d (skip %+v, want %d rejected)", tc.name, len(envelopes), tc.want, skip, tc.rejected)
			}
			if tc.want == 0 {
				return
			}
			got := emitDecoded(t, "cpanfile", tc.body)
			if len(got) != 1 || got[0].SourceKind != tc.kind || got[0].Runtime != "perl" || got[0].SpecKind != tc.spec {
				t.Fatalf("facts = %+v, want kind %q spec %q", got, tc.kind, tc.spec)
			}
		})
	}
}

// TestPerlCpanfileEvidenceGuard proves the credential guard before emission:
// only the cpanfile_perl source kind survives, and a credential-shaped value
// is dropped and counted, never emitted.
func TestPerlCpanfileEvidenceGuard(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ kind, value string }{
		{"cpanfile_perl", "0123456789abcdef-secret"},
		{"cpanfile_lock_perl", "5.38.0"},
	} {
		e := shared.RuntimeEvidence{State: "parsed_values", ByteSize: 100, Values: []shared.RuntimeValue{{SourceKind: tc.kind, Runtime: "perl", Value: tc.value, Line: 2}}}
		facts, skip := EmitExtraction(testContext("cpanfile"), "repo-1", "cpanfile", ExtractPerlEvidence(e))
		if len(facts) != 0 || skip.Rejected != 1 {
			t.Errorf("%s/%s: facts=%d skip=%+v, want no facts and one rejection", tc.kind, tc.value, len(facts), skip)
		}
	}
}

// TestPerlVersionPinValues pins hunk (b): the pin body reads like the other
// single-runtime pins, spec shapes classify, and credential-shaped or
// malformed bodies fail closed with a counted skip.
func TestPerlVersionPinValues(t *testing.T) {
	t.Parallel()

	got := emitDecoded(t, "app/.perl-version", "5.38.2\n")
	checkFacts(t, got, []wantFact{
		{"app", "perl_version", "perl", "<nil>", "5.38.2", "exact", "<nil>", 1, 0, 5, 38, 2},
	})
	for _, tc := range []struct{ raw, kind string }{
		{"5.38.2", "exact"}, {"^5.38", "range"}, {"latest", "alias"}, {"${PERL_VERSION}", "unresolved"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			t.Parallel()

			got := emitDecoded(t, ".perl-version", tc.raw+"\n")
			if len(got) != 1 || got[0].SourceKind != "perl_version" || got[0].Runtime != "perl" || got[0].RawSpec != tc.raw || got[0].SpecKind != tc.kind {
				t.Fatalf(".perl-version %q declarations = %+v", tc.raw, got)
			}
		})
	}
	for _, tc := range []struct{ name, body string }{
		{"credential", "0123456789abcdef-secret\n"},
		{"invalid UTF-8", "\xff\n"},
		{"invalid punctuation", "5.38;secret\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			envelopes, skip := EmitFile(testContext(".perl-version"), "repo-1", ".perl-version", tc.body)
			if len(envelopes) != 0 || skip.Reason != SkipReasonValueRejected || skip.Rejected != 1 {
				t.Fatalf("malformed pin emitted %d declarations, skip %+v", len(envelopes), skip)
			}
		})
	}
}
