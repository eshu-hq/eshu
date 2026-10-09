// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producerstore

import "testing"

// TestGoModuleName mirrors the parser's go.mod rule (goModulePath in
// go/internal/parser/go_package_module_import_path.go): after
// shared.NormalizeLineEndings, the first line of exactly two fields whose first
// is "module" names the module and the second field is taken verbatim. The
// parity test (TestGoModuleNameMatchesTheImportPathTheParserStamps) drives the
// parser's public pre-scan with the same inputs, so a drift fails there.
func TestGoModuleName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		content string
		want    string
	}{
		{"plain", "module github.com/acme/lib\n\ngo 1.24\n", "github.com/acme/lib"},
		{"leading comment lines", "// header\n\nmodule github.com/acme/lib\n", "github.com/acme/lib"},
		{"crlf", "module github.com/acme/lib\r\ngo 1.24\r\n", "github.com/acme/lib"},
		{"bare cr only", "module github.com/acme/lib\rgo 1.24\r", "github.com/acme/lib"},
		{"bare cr comment then module", "// header\rmodule github.com/acme/lib\r", "github.com/acme/lib"},
		{"stray cr beside lf is not a line break", "// c\rmodule a\nmodule b\n", "b"},
		{"indented", "  module   github.com/acme/lib  \n", "github.com/acme/lib"},
		{"trailing comment is not a module line", "module github.com/acme/lib // old\n", ""},
		{"quoted path stays verbatim", "module \"github.com/acme/lib\"\n", "\"github.com/acme/lib\""},
		{"first matching line wins", "module github.com/acme/one\nmodule github.com/acme/two\n", "github.com/acme/one"},
		{"module block opener is taken verbatim, as the parser does", "module (\n\tgithub.com/acme/lib\n)\n", "("},
		{"no module directive", "go 1.24\nrequire github.com/x/y v1.0.0\n", ""},
		{"empty", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := GoModuleName(tc.content); got != tc.want {
				t.Fatalf("GoModuleName(%q) = %q, want %q", tc.content, got, tc.want)
			}
		})
	}
}

func TestPackageManifestName(t *testing.T) {
	t.Parallel()

	for content, want := range map[string]string{
		`{"name":"@acme/logging","version":"1.0.0"}`: "@acme/logging",
		`{"name":"  lodash "}`:                       "lodash",
		`{"name":7}`:                                 "",
		`{"name":""}`:                                "",
		`{"version":"1.0.0"}`:                        "",
		`{"name": "@acme/logging",`:                  "",
		`[]`:                                         "",
		``:                                           "",
	} {
		if got := PackageManifestName(content); got != want {
			t.Errorf("PackageManifestName(%q) = %q, want %q", content, got, want)
		}
	}
}
