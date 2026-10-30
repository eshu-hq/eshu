// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"strings"
	"testing"
)

// TestDefaultRegistryAdmitsPerlLane pins both admission hunks: cpanfile is
// its own metadata-only perl definition at any depth, .perl-version rides
// the raw_text pin list beside .php-version, and the snapshot sibling and
// the perl definition itself stay clear of cpanfile.
func TestDefaultRegistryAdmitsPerlLane(t *testing.T) {
	t.Parallel()

	registry := DefaultRegistry()
	for _, tc := range []struct{ path, key, language string }{
		{"cpanfile", "perl_cpanfile", "perl_cpanfile"},
		{"services/api/cpanfile", "perl_cpanfile", "perl_cpanfile"},
		{".perl-version", "raw_text", "raw_text"},
		{"services/api/.perl-version", "raw_text", "raw_text"},
	} {
		definition, ok := registry.LookupByPath(tc.path)
		if !ok {
			t.Fatalf("LookupByPath(%q) = false, want %q", tc.path, tc.key)
		}
		if definition.ParserKey != tc.key || definition.Language != tc.language {
			t.Fatalf("LookupByPath(%q) = %q/%q, want %q/%q",
				tc.path, definition.ParserKey, definition.Language, tc.key, tc.language)
		}
	}
	cpanfile, _ := registry.LookupByPath("cpanfile")
	if cpanfile.Ecosystem != "perl" || cpanfile.Admission != AdmissionStructuredFact {
		t.Fatalf("cpanfile = %q/%q, want perl/%q", cpanfile.Ecosystem, cpanfile.Admission, AdmissionStructuredFact)
	}
	if got := registry.ContentStoreForPath("cpanfile").Policy; got != ContentStoreMetadata {
		t.Fatalf("cpanfile content-store policy = %q, want %q", got, ContentStoreMetadata)
	}
	for _, path := range []string{"cpanfile.snapshot", "services/api/cpanfile.snapshot"} {
		if definition, ok := registry.LookupByPath(path); ok {
			t.Fatalf("LookupByPath(%q) resolved to %q, want no match", path, definition.ParserKey)
		}
	}
	perl, ok := registry.LookupByParserKey("perl")
	if !ok {
		t.Fatal(`parser registry has no "perl" definition`)
	}
	for _, name := range perl.ExactNames {
		if strings.EqualFold(name, "cpanfile") {
			t.Fatalf("perl definition binds %q, want only the perl_cpanfile definition to bind it", name)
		}
	}
	if len(perl.Extensions) != 2 || perl.Extensions[0] != ".pl" || perl.Extensions[1] != ".pm" {
		t.Fatalf("perl extensions = %v, want [.pl .pm]", perl.Extensions)
	}
}
