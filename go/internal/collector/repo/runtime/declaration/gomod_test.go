// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"testing"
)

const realisticGoMod = `module github.com/acme/api

go 1.22.1

toolchain go1.22.3

require (
	github.com/stretchr/testify v1.9.0
	golang.org/x/mod v0.17.0 // indirect
)

replace github.com/acme/lib => ../lib
`

func TestEmitGoModReadsTheGoAndToolchainDirectives(t *testing.T) {
	t.Parallel()

	got := emitDecoded(t, "services/api/go.mod", realisticGoMod)
	checkFacts(t, got, []wantFact{
		{"services/api", "go_mod_go", "go", "<nil>", "1.22.1", "exact", "<nil>", 3, 0, 1, 22, 1},
		{"services/api", "go_mod_toolchain", "go", "<nil>", "go1.22.3", "exact", "<nil>", 5, 0, 1, 22, 3},
	})
}

func TestEmitGoModClassifiesDirectiveValues(t *testing.T) {
	t.Parallel()

	cases := []struct {
		line, wantRaw, wantKind, wantSource string
	}{
		{"go 1.22", "1.22", SpecKindPartial, "go_mod_go"},
		{"go 1.22.1", "1.22.1", SpecKindExact, "go_mod_go"},
		{"go 1.21rc1", "1.21rc1", SpecKindPartial, "go_mod_go"},
		{"go 1.22.0rc2", "1.22.0rc2", SpecKindExact, "go_mod_go"},
		{"go\t1.22 // minimum language level", "1.22", SpecKindPartial, "go_mod_go"},
		{`go "1.22"`, "1.22", SpecKindPartial, "go_mod_go"},
		{"go 1.22.3-custom", "1.22.3-custom", SpecKindAlias, "go_mod_go"},
		{"go latest", "latest", SpecKindAlias, "go_mod_go"},
		{"go ${GO_VERSION}", "${GO_VERSION}", SpecKindUnresolved, "go_mod_go"},
		{"toolchain go1.22.3", "go1.22.3", SpecKindExact, "go_mod_toolchain"},
		{"toolchain go1.23rc1", "go1.23rc1", SpecKindPartial, "go_mod_toolchain"},
		{"toolchain default", "default", SpecKindAlias, "go_mod_toolchain"},
		{"toolchain local", "local", SpecKindAlias, "go_mod_toolchain"},
		{"toolchain go1.22.3+auto", "go1.22.3+auto", SpecKindAlias, "go_mod_toolchain"},
		{"toolchain gccgo", "gccgo", SpecKindAlias, "go_mod_toolchain"},
		{"toolchain go1.22.3 // pinned by ci", "go1.22.3", SpecKindExact, "go_mod_toolchain"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.line, func(t *testing.T) {
			t.Parallel()
			got := emitDecoded(t, "go.mod", "module m\n"+tc.line+"\n")
			if len(got) != 1 {
				t.Fatalf("emitted %d facts, want 1: %+v", len(got), got)
			}
			g := got[0]
			if g.RawSpec != tc.wantRaw || g.SpecKind != tc.wantKind || g.SourceKind != tc.wantSource || g.Runtime != "go" || g.LineNumber != 2 {
				t.Errorf("fact = raw %q kind %q source %q runtime %q line %d, want %q %q %q go 2",
					g.RawSpec, g.SpecKind, g.SourceKind, g.Runtime, g.LineNumber, tc.wantRaw, tc.wantKind, tc.wantSource)
			}
			if tc.wantKind != SpecKindExact && tc.wantKind != SpecKindPartial && g.VersionMajor != nil {
				t.Errorf("%s %q carries version parts", tc.wantKind, tc.wantRaw)
			}
		})
	}
}

func TestEmitGoModNeverEmitsRequireReplaceOrBlockLines(t *testing.T) {
	t.Parallel()

	body := `module example.com/m

require (
	go 1.19
	toolchain go1.19
	example.com/dep v1.0.0
)

replace (
	go => ./local-go
	example.com/a v1.0.0 => example.com/b v1.0.1
)

exclude example.com/old v0.1.0
retract v1.0.0
godebug default=go1.21
tool example.com/cmd/stringer
`
	if got := emitDecoded(t, "go.mod", body); len(got) != 0 {
		t.Fatalf("emitted %+v from require, replace, exclude, retract, godebug and tool lines, want 0", got)
	}
}

func TestEmitGoModToleratesBOMCRLFAndIndentedDirectives(t *testing.T) {
	t.Parallel()

	body := "\xef\xbb\xbfmodule m\r\n\r\n\tgo   1.22.1   \r\n  toolchain\tgo1.22.3\r\n"
	got := emitDecoded(t, "go.mod", body)
	checkFacts(t, got, []wantFact{
		{".", "go_mod_go", "go", "<nil>", "1.22.1", "exact", "<nil>", 3, 0, 1, 22, 1},
		{".", "go_mod_toolchain", "go", "<nil>", "go1.22.3", "exact", "<nil>", 4, 0, 1, 22, 3},
	})
}

func TestEmitGoModRejectsExoticAndAmbiguousDirectives(t *testing.T) {
	t.Parallel()

	token := "gh" + "p_" + "0123456789abcdefghijklmnopqrstuvwxyzAB"
	cases := map[string]string{
		"extra tokens":          "go 1.22 1.23\n",
		"no value":              "go\n",
		"toolchain no value":    "toolchain\n",
		"credential-shaped go":  "go " + token + "\n",
		"credential toolchain":  "toolchain " + token + "\n",
		"non-ascii value":       "go 1.22é\n",
		"control byte in value": "go 1.22\x00\n",
		"url with password":     "toolchain https://u:" + "hunter2" + "@host/go\n",
		"over the value bound":  "go " + strings.Repeat("1.", 200) + "\n",
		"unterminated block":    "require (\ngo 1.22\n",
		"directive case":        "GO 1.22\nToolchain go1.22\n",
		"go.work style use":     "use ./a\n",
	}
	for name, body := range cases {
		name, body := name, body
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			for _, v := range emitDecoded(t, "go.mod", body) {
				if strings.Contains(v.RawSpec, token) || strings.Contains(v.RawSpec, "hunter2") {
					t.Fatalf("credential-shaped value reached raw_spec %q", v.RawSpec)
				}
			}
		})
	}
	// Anything the guard drops is reported, so the operator can find it.
	for _, body := range []string{"go " + token + "\n", "toolchain " + token + "\n", "go 1.22 1.23\n"} {
		envelopes, skip := EmitFile(testContext("go.mod"), "repo-1", "go.mod", body)
		if len(envelopes) != 0 || skip.Reason != SkipReasonValueRejected || skip.Rejected != 1 {
			t.Errorf("EmitFile(%q) = %d envelopes, skip %+v, want 0 and one rejected value", body[:8], len(envelopes), skip)
		}
	}
}

func TestEmitGoModRepeatedDirectiveIsAmbiguousAndEmitsNeither(t *testing.T) {
	t.Parallel()

	envelopes, skip := EmitFile(testContext("go.mod"), "repo-1", "go.mod", "module m\ngo 1.21\ngo 1.22\ntoolchain go1.22.3\n")
	got := emitDecoded(t, "go.mod", "module m\ngo 1.21\ngo 1.22\ntoolchain go1.22.3\n")
	if len(envelopes) != 1 || len(got) != 1 || got[0].SourceKind != "go_mod_toolchain" {
		t.Fatalf("emitted %d facts %+v, want only the unambiguous toolchain", len(envelopes), got)
	}
	if skip.Reason != SkipReasonValueRejected || skip.Rejected != 2 {
		t.Fatalf("skip = %+v, want both repeated go directives reported", skip)
	}
}

// TestEmitGoModMalformedRepeatMakesTheDirectiveAmbiguous proves a repeat that
// is itself malformed still counts as an occurrence: the go tool refuses the
// file, so the valid copy must not emit either.
func TestEmitGoModMalformedRepeatMakesTheDirectiveAmbiguous(t *testing.T) {
	t.Parallel()

	token := "gh" + "p_" + "0123456789abcdefghijklmnopqrstuvwxyzAB"
	cases := []struct {
		name, body string
		wantFacts  []string
		rejected   int
	}{
		{"extra tokens", "go 1.22\ngo 1.22 extra\n", nil, 2},
		{"malformed first", "go 1.22 extra\ngo 1.22\n", nil, 2},
		{"credential repeat", "toolchain go1.22.3\ntoolchain " + token + "\n", nil, 2},
		{"no value repeat", "go 1.22\ngo\n", nil, 2},
		{"other directive unaffected", "go 1.22\ngo 1.22 extra\ntoolchain go1.22.3\n", []string{"go_mod_toolchain"}, 2},
		{"single malformed", "go 1.22 extra\n", nil, 1},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			envelopes, skip := EmitFile(testContext("go.mod"), "repo-1", "go.mod", tc.body)
			got := emitDecoded(t, "go.mod", tc.body)
			if len(got) != len(tc.wantFacts) || len(envelopes) != len(tc.wantFacts) {
				t.Fatalf("emitted %+v, want only %v", got, tc.wantFacts)
			}
			for i, kind := range tc.wantFacts {
				if got[i].SourceKind != kind {
					t.Errorf("fact[%d] = %s, want %s", i, got[i].SourceKind, kind)
				}
			}
			if skip.Reason != SkipReasonValueRejected || skip.Rejected != tc.rejected {
				t.Errorf("skip = %+v, want rejected %d", skip, tc.rejected)
			}
		})
	}
}

func TestEmitGoModOnlyMatchesTheExactFileNameOutsideDependencyDirectories(t *testing.T) {
	t.Parallel()

	for _, path := range []string{
		"go.sum", "go.work", "GO.MOD", "Go.mod", "go.mod.bak", "app/go.mod.tmpl", "gomod",
		"vendor/golang.org/x/mod/go.mod", "app/vendor/x/go.mod", "node_modules/x/go.mod",
	} {
		if got := emitDecoded(t, path, "module m\ngo 1.22\n"); len(got) != 0 {
			t.Errorf("%s emitted %d facts, want 0", path, len(got))
		}
	}
	if got := emitDecoded(t, "tools/go.mod", "module m\ngo 1.22\n"); len(got) != 1 || got[0].ScopeDir != "tools" {
		t.Errorf("tools/go.mod = %+v, want one fact with scope_dir tools", got)
	}
}

func TestEmitGoModSkipsOverTheManifestByteCap(t *testing.T) {
	t.Parallel()

	body := "module m\ngo 1.22\n" + strings.Repeat("// padding\n", MaxManifestBytesPerFile/10)
	envelopes, skip := EmitFile(testContext("go.mod"), "repo-1", "go.mod", body)
	if len(envelopes) != 0 || skip.Reason != SkipReasonByteCap || skip.ByteCap != MaxManifestBytesPerFile {
		t.Fatalf("EmitFile = %d envelopes, skip %+v, want a byte-cap skip at the manifest cap", len(envelopes), skip)
	}
}
