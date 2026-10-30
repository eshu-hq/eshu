// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package perl

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestExtractCpanfileRuntimeEvidenceAcceptsLiteralPerlRequires pins the
// literal-only contract: only a quoted 'perl' module and version yields a value.
func TestExtractCpanfileRuntimeEvidenceAcceptsLiteralPerlRequires(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, body, value string
		line              int
	}{
		{"single quotes", "requires 'perl', '5.38.0';\n", "5.38.0", 1},
		{"double quotes and fat comma", "requires \"perl\" => \"5.020\";\n", "5.020", 1},
		{"parenthesized call", "requires('perl', '5.010');\n", "5.010", 1},
		{"phase block", "on 'configure' => sub {\n  requires 'perl', '5.008';\n};\n", "5.008", 2},
		{"trailing comment", "requires 'perl', '5.026'; # minimum\n", "5.026", 1},
		{"range", "requires 'perl', '>= 5.20, < 6';\n", ">= 5.20, < 6", 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			evidence := ExtractCpanfileRuntimeEvidence("cpanfile", []byte(tc.body))
			if evidence.State != "parsed_values" || len(evidence.Values) != 1 {
				t.Fatalf("evidence = %+v, want one parsed value", evidence)
			}
			got := evidence.Values[0]
			if got.SourceKind != "cpanfile_perl" || got.Runtime != "perl" || got.Value != tc.value || got.Line != tc.line {
				t.Fatalf("value = %+v, want cpanfile_perl/perl %q at line %d", got, tc.value, tc.line)
			}
			if evidence.Digest != shared.RuntimeDigest([]byte(tc.body)) {
				t.Fatalf("digest = %q, want the source revision binding", evidence.Digest)
			}
		})
	}
}

// TestExtractCpanfileRuntimeEvidenceFailsClosed proves no execution or
// evaluation: dynamic modules and versions are counted as rejected, never emitted.
func TestExtractCpanfileRuntimeEvidenceFailsClosed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ name, body string }{
		{"dynamic module", "requires $module, '1.0';\n"},
		{"dynamic version", "requires 'perl', $version;\n"},
		{"concatenated version", "requires 'perl', '5.0' . $suffix;\n"},
		{"interpolated version", "requires \"perl\", \"5.0$minor\";\n"},
		{"function call version", "requires 'perl', minimum_perl();\n"},
		{"bare requires names no version", "requires 'perl';\n"},
		{"trailing code after version", "requires 'perl', '5.030' if $] >= 5.030;\n"},
		{"credential-shaped version", "requires 'perl', '0123456789abcdef-secret';\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			evidence := ExtractCpanfileRuntimeEvidence("cpanfile", []byte(tc.body))
			if len(evidence.Values) != 0 || evidence.Rejected == 0 {
				t.Fatalf("evidence = %+v, want no values and a counted rejection", evidence)
			}
		})
	}
}

// TestExtractCpanfileRuntimeEvidenceIgnoresOtherModules keeps the evidence
// perl-only: other modules and keywords are not runtime pins.
func TestExtractCpanfileRuntimeEvidenceIgnoresOtherModules(t *testing.T) {
	t.Parallel()

	body := "requires 'Moose', '2.2010';\ntest_requires 'perl', '5.030';\nrecommends 'perl', '5.036';\n"
	evidence := ExtractCpanfileRuntimeEvidence("cpanfile", []byte(body))
	if len(evidence.Values) != 0 || evidence.Rejected != 0 {
		t.Fatalf("evidence = %+v, want silence, not values or rejections", evidence)
	}
}

// TestExtractCpanfileRuntimeEvidenceBindsNameAndCap pins the transport
// guards: only cpanfile is readable, and an over-cap body reports its size.
func TestExtractCpanfileRuntimeEvidenceBindsNameAndCap(t *testing.T) {
	t.Parallel()

	if evidence := ExtractCpanfileRuntimeEvidence("cpanfile.snapshot", []byte("requires 'perl', '5.38.0';\n")); evidence.State != "unavailable" {
		t.Fatalf("snapshot evidence = %+v, want unavailable", evidence)
	}
	if evidence := ExtractCpanfileRuntimeEvidence("cpanfile", make([]byte, 1<<20+1)); evidence.State != "over_cap" || evidence.ByteSize != 1<<20+1 {
		t.Fatalf("over-cap evidence = %+v, want over_cap with the body size", evidence)
	}
}
