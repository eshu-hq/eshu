// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dart

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
)

// cov8DartFile builds one parsed-file envelope.
func cov8DartFile(repo, relative string, fileData map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":          repo,
			"relative_path":    relative,
			"parsed_file_data": fileData,
		},
	}
}

// cov8DartFunction builds one parsed function row.
func cov8DartFunction(name, uid string) map[string]any {
	return map[string]any{
		"name": name, "uid": uid, "line_number": 1, "end_line": 2,
	}
}

// TestCov8DartResolversListShape pins the single import resolver.
func TestCov8DartResolversListShape(t *testing.T) {
	t.Parallel()

	resolvers := Resolvers()
	if len(resolvers) != 1 {
		t.Fatalf("len(Resolvers()) = %d, want 1", len(resolvers))
	}
	if resolvers[0].Phase != shared.PhaseBeforeRepoFallback || resolvers[0].Resolve == nil {
		t.Errorf("Resolvers()[0] = %+v, want the before-fallback resolver", resolvers[0])
	}
}

// TestCov8DartImportCallee pins import-bound resolution: a relative
// import and a package: import both bind, while unmatched, ambiguous,
// and qualified calls stay empty.
func TestCov8DartImportCallee(t *testing.T) {
	t.Parallel()

	index := shared.BuildEntityIndex([]facts.Envelope{
		cov8DartFile("repo-dart", "lib/src/util.dart", map[string]any{
			"path":      "lib/src/util.dart",
			"functions": []any{cov8DartFunction("helper", "entity-helper")},
		}),
		cov8DartFile("repo-dart", "myapp/lib/src/presenter.dart", map[string]any{
			"path":      "/repo/myapp/lib/src/presenter.dart",
			"functions": []any{cov8DartFunction("present", "entity-present")},
		}),
	})
	resolve := Resolvers()[0].Resolve

	relative := shared.ResolveContext{
		Index: index, RepositoryID: "repo-dart",
		RelativePath: "lib/main.dart",
		RepositoryImports: map[string][]string{
			"helper": {"lib/src/util.dart"},
		},
		FileData: map[string]any{"imports": []any{
			map[string]any{"lang": "dart", "source": "src/util.dart"},
		}},
		Call:     map[string]any{"name": "helper"},
		Language: "dart",
	}
	entityID, file, method := resolve(relative)
	if entityID != "entity-helper" || file != "lib/src/util.dart" ||
		method != codeprovenance.MethodImportBinding {
		t.Errorf("Resolve(relative) = %q, %q, %q; want entity-helper via import binding",
			entityID, file, method)
	}

	packaged := shared.ResolveContext{
		Index: index, RepositoryID: "repo-dart",
		RawPath:      "/repo/myapp/lib/main.dart",
		RelativePath: "myapp/lib/main.dart",
		RepositoryImports: map[string][]string{
			"present": {"/repo/myapp/lib/src/presenter.dart"},
		},
		FileData: map[string]any{"imports": []any{
			map[string]any{"lang": "dart", "source": "package:myapp/src/presenter.dart"},
		}},
		Call:     map[string]any{"name": "present"},
		Language: "dart",
	}
	if entityID, _, _ := resolve(packaged); entityID != "entity-present" {
		t.Errorf("Resolve(package:) = %q, want entity-present", entityID)
	}

	for name, mutate := range map[string]func(*shared.ResolveContext){
		"no name": func(c *shared.ResolveContext) {
			c.Call = map[string]any{}
		},
		"qualified": func(c *shared.ResolveContext) {
			c.Call = map[string]any{"name": "helper", "full_name": "util.helper"}
		},
		"no imports": func(c *shared.ResolveContext) {
			c.RepositoryImports = nil
		},
		"unmatched source": func(c *shared.ResolveContext) {
			c.FileData = map[string]any{"imports": []any{
				map[string]any{"lang": "dart", "source": "src/other.dart"},
			}}
		},
	} {
		guarded := relative
		mutate(&guarded)
		if entityID, _, _ := resolve(guarded); entityID != "" {
			t.Errorf("Resolve(%s) = %q, want empty", name, entityID)
		}
	}
}

// TestCov8DartImportAmbiguityStaysEmpty pins the two-entity fail-closed:
// one name imported from two files resolves to nothing.
func TestCov8DartImportAmbiguityStaysEmpty(t *testing.T) {
	t.Parallel()

	index := shared.BuildEntityIndex([]facts.Envelope{
		cov8DartFile("repo-dart", "lib/src/a.dart", map[string]any{
			"path":      "lib/src/a.dart",
			"functions": []any{cov8DartFunction("helper", "entity-a-helper")},
		}),
		cov8DartFile("repo-dart", "lib/src/b.dart", map[string]any{
			"path":      "lib/src/b.dart",
			"functions": []any{cov8DartFunction("helper", "entity-b-helper")},
		}),
	})
	ctx := shared.ResolveContext{
		Index: index, RepositoryID: "repo-dart",
		RelativePath: "lib/main.dart",
		RepositoryImports: map[string][]string{
			"helper": {"lib/src/a.dart", "lib/src/b.dart"},
		},
		FileData: map[string]any{"imports": []any{
			map[string]any{"lang": "dart", "source": "src/a.dart"},
			map[string]any{"lang": "dart", "source": "src/b.dart"},
		}},
		Call:     map[string]any{"name": "helper"},
		Language: "dart",
	}
	if entityID, _, _ := Resolvers()[0].Resolve(ctx); entityID != "" {
		t.Errorf("Resolve(ambiguous) = %q, want empty", entityID)
	}
}

// TestCov8DartBlocksRepoFallback pins the fallback gate: explained
// imports (even unresolved) and basename-matching directives block the
// repo-unique-name guess, while unexplained calls do not.
func TestCov8DartBlocksRepoFallback(t *testing.T) {
	t.Parallel()

	matched := shared.ResolveContext{
		RelativePath:      "lib/main.dart",
		RepositoryImports: map[string][]string{"helper": {"lib/src/util.dart"}},
		FileData: map[string]any{"imports": []any{
			map[string]any{"lang": "dart", "source": "src/util.dart"},
		}},
		Call:     map[string]any{"name": "helper"},
		Language: "dart",
	}
	if !BlocksRepoFallback(matched) {
		t.Error("BlocksRepoFallback(matched imports) = false, want true")
	}

	basename := shared.ResolveContext{
		RelativePath: "lib/main.dart",
		FileData: map[string]any{"imports": []any{
			map[string]any{"lang": "dart", "source": "src/helper.dart"},
		}},
		Call:     map[string]any{"name": "helper"},
		Language: "dart",
	}
	if !BlocksRepoFallback(basename) {
		t.Error("BlocksRepoFallback(basename directive) = false, want true")
	}

	for name, ctx := range map[string]shared.ResolveContext{
		"no name": {Call: map[string]any{}, Language: "dart"},
		"qualified": {
			Call:     map[string]any{"name": "helper", "full_name": "util.helper"},
			Language: "dart",
		},
		"unexplained": {
			RelativePath: "lib/main.dart",
			Call:         map[string]any{"name": "helper"},
			Language:     "dart",
		},
	} {
		if BlocksRepoFallback(ctx) {
			t.Errorf("BlocksRepoFallback(%s) = true, want false", name)
		}
	}
}

// TestCov8DartImportEntryFiltering pins directive selection: export and
// foreign-language directives never explain a call, while plain and
// resolved_source entries do.
func TestCov8DartImportEntryFiltering(t *testing.T) {
	t.Parallel()

	fileData := map[string]any{"imports": []any{
		map[string]any{"lang": "dart", "source": "src/a.dart"},
		map[string]any{"lang": "dart", "source": "src/b.dart", "import_type": "export"},
		map[string]any{"lang": "dart", "source": "src/c.dart", "import_type": "reexport"},
		map[string]any{"lang": "go", "source": "example.com/x"},
		map[string]any{"source": "src/untyped.dart"},
	}}
	entries := dartImportEntries(fileData)
	if len(entries) != 2 {
		t.Fatalf("dartImportEntries() = %d entries, want 2 (plain + untyped)", len(entries))
	}

	withName := map[string]any{"source": "src/a.dart", "name": "src/a.dart"}
	if got := dartImportEntrySources(withName); len(got) != 1 {
		t.Errorf("sources(duplicate name) = %v, want deduped single", got)
	}
	resolved := map[string]any{"resolved_source": "src/r.dart", "name": "other"}
	got := dartImportEntrySources(resolved)
	if len(got) != 2 || got[0] != "src/r.dart" || got[1] != "other" {
		t.Errorf("sources(resolved+name) = %v, want both", got)
	}
}

// TestCov8DartImportSourceCandidates pins candidate expansion: relative
// sources join the caller directory and repository root, absolute and
// empty sources do not, and package: sources resolve under the caller
// package root.
func TestCov8DartImportSourceCandidates(t *testing.T) {
	t.Parallel()

	if got := dartImportSourceCandidates("", "", ""); len(got) != 0 {
		t.Errorf("candidates(empty) = %v, want nil", got)
	}
	got := dartImportSourceCandidates("", "lib/main.dart", "src/util.dart")
	found := false
	for _, candidate := range got {
		if candidate == "lib/src/util.dart" {
			found = true
		}
	}
	if !found {
		t.Errorf("candidates(relative) = %v, want lib/src/util.dart among them", got)
	}
	abs := dartImportSourceCandidates("", "lib/main.dart", "/abs/util.dart")
	for _, candidate := range abs {
		if candidate != "/abs/util.dart" {
			t.Errorf("candidates(absolute) = %v, want only the source itself", abs)
		}
	}
	packaged := dartImportSourceCandidates("/repo/myapp/lib/main.dart", "myapp/lib/main.dart", "package:myapp/src/util.dart")
	found = false
	for _, candidate := range packaged {
		if candidate == "/repo/myapp/lib/src/util.dart" {
			found = true
		}
	}
	if !found {
		t.Errorf("candidates(package:) = %v, want /repo/myapp/lib/src/util.dart among them", packaged)
	}
	if got := dartPackageImportSourceCandidates("", "", "package:broken"); len(got) != 0 {
		t.Errorf("package candidates(broken) = %v, want nil", got)
	}
	if got := dartCallerPackageRoot(""); got != "" {
		t.Errorf("packageRoot(empty) = %q, want empty", got)
	}
	if got := dartCallerPackageRoot("lib/main.dart"); got != "" {
		t.Errorf("packageRoot(no package) = %q, want empty", got)
	}
}

// TestCov8DartImportSourceBaseName pins basename derivation for the
// fallback gate, including package: stripping and extension removal.
func TestCov8DartImportSourceBaseName(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"src/helper.dart":             "helper",
		"package:myapp/src/util.dart": "util",
		"plain":                       "plain",
		"":                            "",
	}
	for source, want := range cases {
		if got := dartImportSourceBaseName(source); got != want {
			t.Errorf("baseName(%q) = %q, want %q", source, got, want)
		}
	}
}
