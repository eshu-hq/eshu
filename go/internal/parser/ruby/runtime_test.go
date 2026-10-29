// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ruby

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

func TestBundlerRuntimeEvidence(t *testing.T) {
	for _, tc := range []struct {
		name, body, kind, value string
		line                    int
	}{
		{"Gemfile", "source 'https://rubygems.org'\nruby '>= 3.2', '< 4.0'\ngem 'rack'\n", "gemfile_ruby", ">= 3.2, < 4.0", 2},
		{"Gemfile.lock", "GEM\n  specs:\n    rack (3.0.0)\n\nRUBY VERSION\n   ruby 3.3.1p55\n", "gemfile_lock_ruby_version", "ruby 3.3.1p55", 6},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var payload map[string]any
			if tc.name == "Gemfile" {
				payload = parseBundlerGemfilePayload(tc.name, []byte(tc.body), false)
			} else {
				payload = parseBundlerLockfilePayload(tc.name, []byte(tc.body), false)
			}
			evidence, ok := payload["ruby_runtime_evidence"].(RubyRuntimeEvidence)
			if !ok || len(evidence.Values) != 1 || evidence.Values[0] != (RubyRuntimeValue{SourceKind: tc.kind, Value: tc.value, Line: tc.line}) {
				t.Fatalf("evidence = %#v", payload["ruby_runtime_evidence"])
			}
		})
	}
}

func TestMalformedGemfileNeverEmitsRuntimeEvidence(t *testing.T) {
	path := writeSource(t, "Gemfile", "ruby '3.3.1'\nif (\n")
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatal(err)
	}
	evidence := payload["ruby_runtime_evidence"].(RubyRuntimeEvidence)
	if evidence.State != "unavailable" || len(evidence.Values) != 0 {
		t.Fatalf("evidence = %+v", evidence)
	}
}

func TestMalformedLockRubyVersionFailsClosed(t *testing.T) {
	payload := parseBundlerLockfilePayload("Gemfile.lock", []byte("RUBY VERSION\n   ruby \n"), false)
	evidence := payload["ruby_runtime_evidence"].(RubyRuntimeEvidence)
	if evidence.State != "unavailable" || len(evidence.Values) != 0 {
		t.Fatalf("evidence = %+v", evidence)
	}
}

func TestConditionalGemfileRubyDoesNotClaimRuntime(t *testing.T) {
	path := writeSource(t, "Gemfile", "if false\n  ruby '3.3.1'\nend\n")
	payload, err := Parse(path, false, shared.Options{})
	if err != nil {
		t.Fatal(err)
	}
	evidence := payload["ruby_runtime_evidence"].(RubyRuntimeEvidence)
	if len(evidence.Values) != 0 {
		t.Fatalf("conditional runtime evidence = %+v", evidence)
	}
}
