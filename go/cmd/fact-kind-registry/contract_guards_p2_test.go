// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"strings"
	"testing"
)

// fixtureContractConsts is a minimal stand-in for the
// go/internal/facts/contract string constants the discovery scan
// resolves <local>.<Const> references against. Values mirror the real
// contract package spellings.
const fixtureContractConsts = `// SPDX-License-Identifier: MIT

package contract

type Disposition string

const (
	DispositionReadOnly Disposition = "read_only"
	DispositionUnconsumed Disposition = "unconsumed"
)

type AuthorityMode string

const (
	AuthorityCoreOwned AuthorityMode = "core_owned"
	AuthorityCoreIssuedGrant AuthorityMode = "core_issued_grant"
)
`

// TestDiscoveryAcceptsContractTypedConstants proves declarations may
// spell Disposition and AuthorityMode with the contract package's own
// typed constants (compile-checked) instead of raw string literals,
// resolving to the same values the literal path produces.
func TestDiscoveryAcceptsContractTypedConstants(t *testing.T) {
	files := fixtureFiles()
	files["go/internal/facts/cloud/fam.go"] = strings.ReplaceAll(strings.ReplaceAll(fixtureFamily,
		`Disposition: "unconsumed"`, `Disposition: contract.DispositionUnconsumed`),
		`AuthorityMode: "core_issued_grant"`, `AuthorityMode: contract.AuthorityCoreIssuedGrant`)
	files["go/internal/facts/contract/contract.go"] = fixtureContractConsts
	root := writeFixtureTree(t, files)
	if got := runGuards(t, root); got != "" {
		t.Fatalf("typed contract constants produced diagnostics:\n%s", got)
	}
}

// TestDiscoveryRejectsBadContractConstantReference proves an unknown
// contract constant fails closed with a file:line diagnostic naming
// the fix, and that a selector on another package is still rejected
// as a non-literal.
func TestDiscoveryRejectsBadContractConstantReference(t *testing.T) {
	cases := []struct {
		name  string
		field string
		want  string
	}{
		{
			name:  "unknown contract constant",
			field: `Disposition: contract.DispositionBogus`,
			want:  "unknown contract constant",
		},
		{
			name:  "selector on another package",
			field: `Disposition: other.DispositionReadOnly`,
			want:  "not a string literal",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := fixtureFiles()
			files["go/internal/facts/cloud/fam.go"] = strings.ReplaceAll(fixtureFamily,
				`Disposition: "unconsumed"`, tc.field)
			files["go/internal/facts/contract/contract.go"] = fixtureContractConsts
			root := writeFixtureTree(t, files)
			got := runGuards(t, root)
			if got == "" {
				t.Fatalf("bad constant reference %q passed; want diagnostic containing %q", tc.field, tc.want)
			}
			if !strings.Contains(got, tc.want) {
				t.Fatalf("diagnostics = %q, want substring %q", got, tc.want)
			}
			if !strings.Contains(got, ".go:") || !strings.Contains(got, "ACTION:") {
				t.Fatalf("diagnostics must carry file:line and ACTION, got %q", got)
			}
		})
	}
}

// TestDecodeSymbolsResolveAcrossFiles proves decode-seam learning is
// order-independent: a Decode seam in a lexically earlier file that
// references a *FactKind* constant from a later-parsed file still
// maps into decodeToKind.
func TestDecodeSymbolsResolveAcrossFiles(t *testing.T) {
	root := writeFixtureTree(t, map[string]string{
		"go/internal/facts/a_decode.go": "package facts\n\nfunc DecodeOrderWidget() string { return OrderWidgetFactKind }\n",
		"go/internal/facts/z_kinds.go":  "package facts\n\nconst OrderWidgetFactKind = \"test_order_widget\"\n",
	})
	symbols := loadKindSymbols(root, map[string]bool{"test_order_widget": true})
	if got := symbols.decodeToKind["DecodeOrderWidget"]; got != "test_order_widget" {
		t.Fatalf("DecodeOrderWidget mapped to %q, want %q (const lives in a later-parsed file)", got, "test_order_widget")
	}
}

// TestReducerAliasChainsResolveToFixpoint proves chained decode
// aliases resolve regardless of declaration order: both the qualified
// (decodeA = foo.decodeB) and bare-identifier (decodeA = decodeB)
// chain forms reach the kind through an alias declared later.
func TestReducerAliasChainsResolveToFixpoint(t *testing.T) {
	root := writeFixtureTree(t, map[string]string{
		"go/internal/reducer/a_alias.go": "package reducer\n\nvar decodeOrderSelAlias = foo.decodeOrderBase\n\nvar decodeOrderIdentAlias = decodeOrderBase\n",
		"go/internal/reducer/z_alias.go": "package reducer\n\nvar decodeOrderBase = schemadecode.DecodeOrderWidget\n",
	})
	symbols := kindSymbols{
		identToKind:  map[string]string{},
		decodeToKind: map[string]string{"DecodeOrderWidget": "test_order_widget"},
	}
	collectReducerAliases(root, map[string]bool{"test_order_widget": true}, symbols)
	for _, name := range []string{"decodeOrderBase", "decodeOrderSelAlias", "decodeOrderIdentAlias"} {
		if got := symbols.decodeToKind[name]; got != "test_order_widget" {
			t.Fatalf("%s mapped to %q, want %q (chained alias must resolve to a fixpoint)", name, got, "test_order_widget")
		}
	}
}

// TestRenderRegistryYAMLQuotesReadSurface proves the generated
// legacy registry keeps the legacy file's double-quoted read_surface
// shape at family level and in overrides. Consumer BITES proofs pin
// that exact line shape byte-for-byte, so a bare-scalar reformat
// turns their setup red.
func TestRenderRegistryYAMLQuotesReadSurface(t *testing.T) {
	spec := specFile{Version: "1.1.0", Families: map[string]familySpec{
		"test": {
			LifecycleOwner: "go/internal/facts", SchemaVersion: "1.0.0",
			ReducerDomain: "test_materialization", ProjectionHook: "test_projection",
			AdmissionHook: "facts.ValidateSchemaVersion", ReadSurface: "GET /api/v0/test/things",
			ReadSurfaceOverrides: map[string]string{"test_resource": "GET /api/v0/test/other"},
			TruthProfile:         "provider_gated", Kinds: []string{"test_resource"},
		},
	}}
	data, err := renderRegistryYAML(spec)
	if err != nil {
		t.Fatalf("renderRegistryYAML() error = %v, want nil", err)
	}
	out := string(data)
	if !strings.Contains(out, `read_surface: "GET /api/v0/test/things"`) {
		t.Fatalf("registry YAML lost the quoted read_surface line:\n%s", out)
	}
	if !strings.Contains(out, `{test_resource: "GET /api/v0/test/other"}`) {
		t.Fatalf("registry YAML lost the quoted read_surface_overrides value:\n%s", out)
	}
}

// TestPackageSymbolsHonorBuildTags proves a top-level symbol that
// exists only behind an unsatisfied //go:build constraint does not
// satisfy binding Symbol validation.
func TestPackageSymbolsHonorBuildTags(t *testing.T) {
	root := writeFixtureTree(t, map[string]string{
		"pkg/plain.go":  "package p\n\nfunc PlainSymbol() {}\n",
		"pkg/tagged.go": "//go:build ignore\n\npackage p\n\nfunc TaggedOnlySymbol() {}\n",
	})
	got := packageSymbols(root + "/pkg")
	if !got["PlainSymbol"] {
		t.Fatal("packageSymbols missed PlainSymbol from the default-build file")
	}
	if got["TaggedOnlySymbol"] {
		t.Fatal("packageSymbols learned TaggedOnlySymbol from a //go:build ignore file; want it excluded")
	}
}

// TestConstSymbolsHonorBuildTags proves a *FactKind* constant that
// exists only behind an unsatisfied //go:build constraint is not
// learned as a kind symbol.
func TestConstSymbolsHonorBuildTags(t *testing.T) {
	root := writeFixtureTree(t, map[string]string{
		"go/internal/facts/plain_kinds.go":  "package facts\n\nconst PlainFactKind = \"test_plain\"\n",
		"go/internal/facts/tagged_kinds.go": "//go:build ignore\n\npackage facts\n\nconst TaggedFactKind = \"test_tagged\"\n",
	})
	symbols := loadKindSymbols(root, map[string]bool{"test_plain": true, "test_tagged": true})
	if got := symbols.identToKind["PlainFactKind"]; got != "test_plain" {
		t.Fatalf("PlainFactKind = %q, want %q", got, "test_plain")
	}
	if _, ok := symbols.identToKind["TaggedFactKind"]; ok {
		t.Fatal("TaggedFactKind learned from a //go:build ignore file; want it excluded")
	}
}
