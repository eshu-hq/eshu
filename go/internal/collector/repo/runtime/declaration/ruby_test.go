// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"testing"

	rubyparser "github.com/eshu-hq/eshu/go/internal/parser/ruby"
)

func TestRubyRuntimeSourceKindsAndGuard(t *testing.T) {
	for _, tc := range []struct {
		name, kind, value, spec string
		rejected                int
	}{
		{"exact", "gemfile_ruby", "3.3.1", "exact", 0},
		{"range", "gemfile_ruby", ">=3.2 <4", "range", 0},
		{"alias", "gemfile_ruby", "latest", "alias", 0},
		{"unresolved", "gemfile_ruby", "${RUBY_VERSION}", "unresolved", 0},
		{"credential", "gemfile_ruby", "0123456789abcdef-secret", "", 1},
		{"lock-exact", "gemfile_lock_ruby_version", "ruby 3.3.1p55", "exact", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := rubyparser.RubyRuntimeEvidence{State: "parsed_values", ByteSize: 100, Values: []rubyparser.RubyRuntimeValue{{SourceKind: tc.kind, Value: tc.value, Line: 2}}}
			extraction := ExtractRubyEvidence(e)
			facts, skip := EmitExtraction(testContext("Gemfile"), "repo-1", "Gemfile", extraction)
			if len(facts) != 1-tc.rejected || skip.Rejected != tc.rejected {
				t.Fatalf("facts=%d skip=%+v", len(facts), skip)
			}
			if tc.rejected == 0 {
				if got := classifyDeclaration(tc.kind, extraction.declarations[0]).Kind; got != tc.spec {
					t.Fatalf("kind=%s want=%s", got, tc.spec)
				}
			}
		})
	}
}

func TestRubyManifestContentOnlyEntrypoint(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, kind, spec string
		want                         int
	}{
		{"top-level Gemfile", "Gemfile", "ruby '3.3.1'\n", "gemfile_ruby", "exact", 1},
		{"lockfile", "Gemfile.lock", "RUBY VERSION\n   ruby 3.3.1p55\n", "gemfile_lock_ruby_version", "exact", 1},
		{"nested method", "Gemfile", "def configure\n  ruby '3.3.1'\nend\n", "", "", 0},
		{"nested class", "Gemfile", "class Configure\n  ruby '3.3.1'\nend\n", "", "", 0},
		{"nested module", "Gemfile", "module Configure\n  ruby '3.3.1'\nend\n", "", "", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			facts := emitDecoded(t, tc.path, tc.body)
			if len(facts) != tc.want {
				t.Fatalf("Emit(%q) emitted %d facts, want %d: %+v", tc.body, len(facts), tc.want, facts)
			}
			if tc.want == 1 && (facts[0].SourceKind != tc.kind || facts[0].SpecKind != tc.spec) {
				t.Fatalf("facts = %+v", facts)
			}
		})
	}
}
