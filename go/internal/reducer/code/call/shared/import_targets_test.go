// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package shared

import (
	"reflect"
	"testing"
)

// TestImportedTargetsEmptyCallName proves a call without a name yields no
// import targets however many import entries the file carries.
func TestImportedTargetsEmptyCallName(t *testing.T) {
	t.Parallel()

	imports := []map[string]any{
		{"name": "serve", "source": "./server", "alias": ""},
	}
	for _, call := range []map[string]any{
		{},
		{"name": "   "},
		{"full_name": "serve"},
	} {
		if got := ImportedTargets(imports, call); len(got) != 0 {
			t.Fatalf("ImportedTargets(%#v) = %#v, want empty", call, got)
		}
	}
}

// TestImportedTargetsExactAndAliasMatch proves exact-name and alias-name
// import entries both bind, resolved_source wins order, and duplicates
// collapse to one target.
func TestImportedTargetsExactAndAliasMatch(t *testing.T) {
	t.Parallel()

	call := map[string]any{"name": "serve", "full_name": "serve"}
	imports := []map[string]any{
		{"name": "serve", "source": "./server", "resolved_source": "./dist/server"},
		{"name": "other", "source": "./other", "alias": "serve"},
		{"name": "serve", "source": "./dist/server"},
		{"name": "unrelated", "source": "./unrelated"},
		{"name": "serve", "source": "   "},
	}
	got := ImportedTargets(imports, call)
	want := []ImportedTarget{
		{SymbolName: "serve", ImportSource: "./dist/server"},
		{SymbolName: "serve", ImportSource: "./server"},
		{SymbolName: "other", ImportSource: "./other"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ImportedTargets = %#v, want %#v", got, want)
	}
}

// TestImportedTargetsQualifiedAndNamespacePrefix proves dotted full names
// bind through the qualifying entry and namespace aliases bind by prefix.
func TestImportedTargetsQualifiedAndNamespacePrefix(t *testing.T) {
	t.Parallel()

	call := map[string]any{"name": "listen", "full_name": "server.listen"}
	imports := []map[string]any{
		{"name": "server", "source": "./server"},
		{"name": "*", "source": "./ns", "alias": "ns"},
	}
	got := ImportedTargets(imports, call)
	want := []ImportedTarget{
		{SymbolName: "server.listen", ImportSource: "./server"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ImportedTargets qualified = %#v, want %#v", got, want)
	}

	nsCall := map[string]any{"name": "run", "full_name": "ns.run"}
	nsImports := []map[string]any{
		{"name": "*", "source": "./ns", "alias": "ns"},
		{"name": "run", "source": "./direct"},
	}
	got = ImportedTargets(nsImports, nsCall)
	want = []ImportedTarget{
		{SymbolName: "run", ImportSource: "./ns"},
		{SymbolName: "run", ImportSource: "./direct"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ImportedTargets namespace = %#v, want %#v", got, want)
	}
}

// TestImportedTargetsDynamicImport proves import('module') full names bind
// the call name to the dynamic source.
func TestImportedTargetsDynamicImport(t *testing.T) {
	t.Parallel()

	call := map[string]any{"name": "start", "full_name": "import('./lazy').start"}
	got := ImportedTargets(nil, call)
	want := []ImportedTarget{{SymbolName: "start", ImportSource: "./lazy"}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ImportedTargets dynamic = %#v, want %#v", got, want)
	}
}

// TestCodeCallDynamicImportSource pins the import('...') source parser over
// quoting, spacing, and malformed shapes.
func TestCodeCallDynamicImportSource(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		full string
		want string
	}{
		{"single_quotes", "import('./lazy').start", "./lazy"},
		{"double_quotes", `import("./lazy").start`, "./lazy"},
		{"bare_import", "import('./lazy')", "./lazy"},
		{"spaced", "import(  './lazy'  )", "./lazy"},
		{"no_import", "server.listen", ""},
		{"empty", "", ""},
		{"unquoted", "import(./lazy)", ""},
		{"unterminated", "import('./lazy", ""},
		{"empty_parens", "import()", ""},
		{"blank_after_paren", "import(   ", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := codeCallDynamicImportSource(tc.full); got != tc.want {
				t.Fatalf("codeCallDynamicImportSource(%q) = %q, want %q", tc.full, got, tc.want)
			}
		})
	}
}

// TestImportEntrySources proves resolved_source precedes raw source with
// dedup and blank filtering.
func TestImportEntrySources(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		entry map[string]any
		want  []string
	}{
		{
			name:  "resolved_first",
			entry: map[string]any{"source": "./server", "resolved_source": "./dist/server"},
			want:  []string{"./dist/server", "./server"},
		},
		{
			name:  "raw_only",
			entry: map[string]any{"source": "./server"},
			want:  []string{"./server"},
		},
		{
			name:  "dedup",
			entry: map[string]any{"source": "./server", "resolved_source": "./server"},
			want:  []string{"./server"},
		},
		{
			name:  "blanks_dropped",
			entry: map[string]any{"source": "  ", "resolved_source": ""},
			want:  []string{},
		},
		{
			name:  "empty",
			entry: map[string]any{},
			want:  []string{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := ImportEntrySources(tc.entry); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ImportEntrySources = %#v, want %#v", got, tc.want)
			}
		})
	}
}

// TestMatchImportedPath proves a relative import resolves against the
// candidate path set and misses return "".
func TestMatchImportedPath(t *testing.T) {
	t.Parallel()

	got := MatchImportedPath(
		"/repo/src/a.js", "src/a.js", "./b", "javascript",
		[]string{"/repo/src/other.js", "/repo/src/b.js"},
	)
	if got != "/repo/src/b.js" {
		t.Fatalf("MatchImportedPath = %q, want %q", got, "/repo/src/b.js")
	}
	if got := MatchImportedPath(
		"/repo/src/a.js", "src/a.js", "./missing", "javascript",
		[]string{"/repo/src/b.js"},
	); got != "" {
		t.Fatalf("MatchImportedPath miss = %q, want empty", got)
	}
}

// TestRepositoryRoot proves the root is rawPath minus the relativePath
// suffix, or "" when the suffix does not match.
func TestRepositoryRoot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		raw      string
		relative string
		want     string
	}{
		{"trims_suffix", "/repo/src/a.js", "src/a.js", "/repo"},
		{"repo_root_file", "/repo/a.js", "a.js", "/repo"},
		{"mismatch", "/other/src/a.js", "src/b.js", ""},
		{"blank_raw", "", "src/a.js", ""},
		{"blank_relative", "/repo/src/a.js", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := RepositoryRoot(tc.raw, tc.relative); got != tc.want {
				t.Fatalf("RepositoryRoot(%q, %q) = %q, want %q", tc.raw, tc.relative, got, tc.want)
			}
		})
	}
}

// TestCodeCallTrimJavaScriptRuntimeExtension proves JS runtime extensions
// trim while declaration extensions stay intact.
func TestCodeCallTrimJavaScriptRuntimeExtension(t *testing.T) {
	t.Parallel()

	cases := []struct {
		path string
		want string
	}{
		{"/repo/src/a.js", "/repo/src/a"},
		{"/repo/src/a.mjs", "/repo/src/a"},
		{"/repo/src/a.cjs", "/repo/src/a"},
		{"/repo/src/a.jsx", "/repo/src/a"},
		{"/repo/src/a.ts", ""},
		{"/repo/src/a.tsx", ""},
		{"/repo/src/a.py", ""},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			if got := codeCallTrimJavaScriptRuntimeExtension(tc.path); got != tc.want {
				t.Fatalf("trim(%q) = %q, want %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestCodeCallPythonModuleCandidates proves dotted module sources map to
// file and package-init candidates under the repository root.
func TestCodeCallPythonModuleCandidates(t *testing.T) {
	t.Parallel()

	got := codeCallPythonModuleCandidates("/repo", "pkg.mod")
	want := []string{"/repo/pkg/mod.py", "/repo/pkg/mod/__init__.py"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %#v, want %#v", got, want)
	}
	for _, source := range []string{"", "pkg/mod", "pkg", "   "} {
		if got := codeCallPythonModuleCandidates("/repo", source); got != nil {
			t.Fatalf("candidates(%q) = %#v, want nil", source, got)
		}
	}
	if got := codeCallPythonModuleCandidates("", "pkg.mod"); got != nil {
		t.Fatalf("candidates with empty root = %#v, want nil", got)
	}
}

// TestHasExplicitImportedTarget proves the guard reports an explicit
// binding only when an import entry names the call.
func TestHasExplicitImportedTarget(t *testing.T) {
	t.Parallel()

	fileData := map[string]any{
		"imports": []any{
			map[string]any{"name": "serve", "source": "./server"},
		},
	}
	if !HasExplicitImportedTarget(fileData, map[string]any{"name": "serve"}) {
		t.Fatal("HasExplicitImportedTarget = false for a bound call, want true")
	}
	if HasExplicitImportedTarget(fileData, map[string]any{"name": "other"}) {
		t.Fatal("HasExplicitImportedTarget = true for an unbound call, want false")
	}
	if HasExplicitImportedTarget(map[string]any{}, map[string]any{"name": "serve"}) {
		t.Fatal("HasExplicitImportedTarget = true without imports, want false")
	}
}
