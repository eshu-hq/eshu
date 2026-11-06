// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgresproof

import (
	"strings"
	"testing"
)

// TestBootstrapTemplateFlavourValidation pins the flavour contract: only a
// short safe charset becomes part of a template database name, so neither a
// hostile nor a mistyped flavour can escape the generated namespace or the
// 63-byte identifier limit.
func TestBootstrapTemplateFlavourValidation(t *testing.T) {
	t.Parallel()
	for _, flavour := range []string{"public", "public_nocsearch", "a", "x_y_2"} {
		if err := validateBootstrapTemplateFlavour(flavour); err != nil {
			t.Errorf("valid flavour %q rejected: %v", flavour, err)
		}
	}
	for _, flavour := range []string{"", "Public", "public-bootstrap", "a b", "x;DROP", strings.Repeat("a", 25)} {
		if err := validateBootstrapTemplateFlavour(flavour); err == nil {
			t.Errorf("invalid flavour %q accepted", flavour)
		}
	}
}

// TestBootstrapTemplateNameStaysInNamespace pins that generated template
// names fit the identifier limit and validate back through the namespace
// gate clone and discard rely on.
func TestBootstrapTemplateNameStaysInNamespace(t *testing.T) {
	t.Parallel()
	for _, flavour := range []string{"public", "public_nocsearch", strings.Repeat("a", 24)} {
		name, err := bootstrapTemplateName(flavour)
		if err != nil {
			t.Fatalf("flavour %q: %v", flavour, err)
		}
		if len(name) > 63 {
			t.Errorf("flavour %q: template name %q exceeds 63 bytes", flavour, name)
		}
		if err := validateBootstrapTemplateName(name); err != nil {
			t.Errorf("generated name %q refused by the namespace gate: %v", name, err)
		}
	}
	for _, name := range []string{"", "postgres", "template0", "eshu_content_index_proof_abcd", bootstrapTemplateNamePrefix + "UPPER", bootstrapTemplateNamePrefix + "has space"} {
		if err := validateBootstrapTemplateName(name); err == nil {
			t.Errorf("non-template name %q accepted by the namespace gate", name)
		}
	}
}

// TestBootstrapTemplateKeyScopesServerAndFlavour pins that the cache key
// separates proof servers and flavours, so two families on different servers
// never share a template under one flavour name.
func TestBootstrapTemplateKeyScopesServerAndFlavour(t *testing.T) {
	t.Parallel()
	a := bootstrapTemplateKey("postgres://eshu@host-a/postgres?sslmode=disable", "public")
	b := bootstrapTemplateKey("postgres://eshu@host-b/postgres?sslmode=disable", "public")
	c := bootstrapTemplateKey("postgres://eshu@host-a/postgres?sslmode=disable", "public_nocsearch")
	if a == b || a == c || b == c {
		t.Fatalf("cache keys collide: %q %q %q", a, b, c)
	}
	if got := bootstrapTemplateKey("dsn", "public"); !strings.HasPrefix(got, "dsn\x00") {
		t.Fatalf("cache key %q does not scope the DSN from the flavour", got)
	}
}

// TestDiscardBootstrapTemplatesEmpty is the no-op half of the TestMain
// teardown contract: a process that built no template discards nothing and
// reports no error.
func TestDiscardBootstrapTemplatesEmpty(t *testing.T) {
	t.Parallel()
	if errs := DiscardBootstrapTemplates(); len(errs) != 0 {
		t.Fatalf("empty discard reported %v", errs)
	}
}
