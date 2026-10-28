// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jvm

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
)

// cov8JavaConfig mirrors the Java binding: plain imports only, dotted
// paths map to .java files named after the type.
var cov8JavaConfig = ReceiverConfig{
	ImportTypes:       map[string]struct{}{"import": {}},
	SourceExtension:   ".java",
	MatchTypeFileName: true,
}

// cov8KotlinConfig mirrors the Kotlin binding: aliases introduce types
// and a type may live in any file of its package directory.
var cov8KotlinConfig = ReceiverConfig{
	ImportTypes:       map[string]struct{}{"import": {}, "alias": {}},
	SourceExtension:   ".kt",
	MatchTypeFileName: false,
}

// cov8JVMFile builds one parsed-file envelope.
func cov8JVMFile(repo, relative string, fileData map[string]any) facts.Envelope {
	return facts.Envelope{
		FactKind: "file",
		Payload: map[string]any{
			"repo_id":          repo,
			"relative_path":    relative,
			"parsed_file_data": fileData,
		},
	}
}

// cov8ServiceFile declares Service.run in its package-mirroring file.
func cov8ServiceFile(path string) map[string]any {
	return map[string]any{
		"path": path,
		"functions": []any{
			map[string]any{
				"name": "run", "uid": "entity-service-run",
				"class_context": "Service", "line_number": 3, "end_line": 5,
			},
		},
	}
}

// cov8ServiceIndex builds the single-service fixture. The service lives
// under a nested v1/ layout so the suite distinguishes suffix matching
// (the imported file ends with the package path) from prefix matching.
func cov8ServiceIndex() shared.EntityIndex {
	return shared.BuildEntityIndex([]facts.Envelope{
		cov8JVMFile("repo-jvm", "v1/com/example/Service.java",
			cov8ServiceFile("v1/com/example/Service.java")),
	})
}

// TestCov8ResolveReceiverImportBound pins import-bound resolution: the
// receiver type binds through its import to the unique declaring file.
func TestCov8ResolveReceiverImportBound(t *testing.T) {
	t.Parallel()

	index := cov8ServiceIndex()
	ctx := shared.ResolveContext{
		Index: index, RepositoryID: "repo-jvm",
		RelativePath: "com/example/App.java",
		RepositoryImports: map[string][]string{
			"Service": {"v1/com/example/Service.java"},
		},
		FileData: map[string]any{"imports": []any{
			map[string]any{
				"name": "com.example.Service", "source": "com.example.Service",
				"import_type": "import",
			},
		}},
		Call:     map[string]any{"name": "run", "inferred_obj_type": "Service"},
		Language: "java",
	}
	entityID, file, method := ResolveReceiverCallee(ctx, cov8JavaConfig)
	if entityID != "entity-service-run" || file != "v1/com/example/Service.java" ||
		method != codeprovenance.MethodImportBinding {
		t.Errorf("Resolve() = %q, %q, %q; want entity-service-run via import binding",
			entityID, file, method)
	}
	if !ImportedReceiverBlocksRepoFallback(ctx, cov8JavaConfig) {
		t.Error("BlocksRepoFallback(imported) = false, want true")
	}
}

// TestCov8ResolveReceiverInferenceFallback pins the type-inference
// fallback: with no usable import, a repo-unique Type.method still
// binds, recording type_inferred provenance.
func TestCov8ResolveReceiverInferenceFallback(t *testing.T) {
	t.Parallel()

	index := cov8ServiceIndex()
	ctx := shared.ResolveContext{
		Index: index, RepositoryID: "repo-jvm",
		RelativePath: "com/other/App.java",
		Call:         map[string]any{"name": "run", "inferred_obj_type": "Service"},
		Language:     "java",
	}
	entityID, _, method := ResolveReceiverCallee(ctx, cov8JavaConfig)
	if entityID != "entity-service-run" || method != codeprovenance.MethodTypeInferred {
		t.Errorf("Resolve() = %q, %q; want entity-service-run via type inference",
			entityID, method)
	}
	if ImportedReceiverBlocksRepoFallback(ctx, cov8JavaConfig) {
		t.Error("BlocksRepoFallback(no imports) = true, want false")
	}

	// Same-directory declarations win before the repo-wide fallback.
	near := shared.BuildEntityIndex([]facts.Envelope{
		cov8JVMFile("repo-jvm", "com/example/Service.java",
			cov8ServiceFile("com/example/Service.java")),
		cov8JVMFile("repo-jvm", "com/other/Service.java", map[string]any{
			"path": "com/other/Service.java",
			"functions": []any{
				map[string]any{
					"name": "run", "uid": "entity-other-run",
					"class_context": "Service", "line_number": 1, "end_line": 2,
				},
			},
		}),
	})
	nearCtx := ctx
	nearCtx.Index = near
	nearCtx.RelativePath = "com/other/App.java"
	if entityID, _, _ := ResolveReceiverCallee(nearCtx, cov8JavaConfig); entityID != "entity-other-run" {
		t.Errorf("Resolve(same dir) = %q, want entity-other-run", entityID)
	}
}

// TestCov8ResolveReceiverAmbiguityStaysEmpty pins the ambiguous
// fail-closed: two matching import paths resolve to nothing but still
// block the repo fallback.
func TestCov8ResolveReceiverAmbiguityStaysEmpty(t *testing.T) {
	t.Parallel()

	index := shared.BuildEntityIndex([]facts.Envelope{
		cov8JVMFile("repo-jvm", "v1/com/example/Service.java", map[string]any{
			"path": "v1/com/example/Service.java",
			"functions": []any{
				map[string]any{
					"name": "run", "uid": "entity-v1-run",
					"class_context": "Service", "line_number": 1, "end_line": 2,
				},
			},
		}),
		cov8JVMFile("repo-jvm", "v2/com/example/Service.java", map[string]any{
			"path": "v2/com/example/Service.java",
			"functions": []any{
				map[string]any{
					"name": "run", "uid": "entity-v2-run",
					"class_context": "Service", "line_number": 1, "end_line": 2,
				},
			},
		}),
	})
	ctx := shared.ResolveContext{
		Index: index, RepositoryID: "repo-jvm",
		RelativePath: "com/example/App.java",
		RepositoryImports: map[string][]string{
			"Service": {"v1/com/example/Service.java", "v2/com/example/Service.java"},
		},
		FileData: map[string]any{"imports": []any{
			map[string]any{
				"name": "com.example.Service", "source": "com.example.*",
				"import_type": "import",
			},
		}},
		Call:     map[string]any{"name": "run", "inferred_obj_type": "Service"},
		Language: "java",
	}
	if entityID, _, _ := ResolveReceiverCallee(ctx, cov8JavaConfig); entityID != "" {
		t.Errorf("Resolve(ambiguous) = %q, want empty", entityID)
	}
	if !ImportedReceiverBlocksRepoFallback(ctx, cov8JavaConfig) {
		t.Error("BlocksRepoFallback(ambiguous) = false, want true (attempted)")
	}
}

// TestCov8ResolveReceiverKotlinAlias pins alias and package-directory
// matching: `import a.b.Service as Svc` binds the Svc receiver through
// the declared type, in any file of the package.
func TestCov8ResolveReceiverKotlinAlias(t *testing.T) {
	t.Parallel()

	index := shared.BuildEntityIndex([]facts.Envelope{
		cov8JVMFile("repo-kt", "com/example/Helpers.kt", map[string]any{
			"path": "com/example/Helpers.kt",
			"functions": []any{
				map[string]any{
					"name": "run", "uid": "entity-kt-run",
					"class_context": "Service", "line_number": 1, "end_line": 2,
				},
			},
		}),
	})
	ctx := shared.ResolveContext{
		Index: index, RepositoryID: "repo-kt",
		RelativePath: "com/example/App.kt",
		RepositoryImports: map[string][]string{
			"Service": {"com/example/Helpers.kt"},
		},
		FileData: map[string]any{"imports": []any{
			map[string]any{
				"name": "com.example.Service", "source": "com.example.Service",
				"alias": "Svc", "import_type": "alias",
			},
		}},
		Call:     map[string]any{"name": "run", "inferred_obj_type": "Svc"},
		Language: "kotlin",
	}
	entityID, _, method := ResolveReceiverCallee(ctx, cov8KotlinConfig)
	if entityID != "entity-kt-run" || method != codeprovenance.MethodImportBinding {
		t.Errorf("Resolve(alias) = %q, %q; want entity-kt-run via import binding",
			entityID, method)
	}
}

// TestCov8ResolveReceiverGuards pins the empty shapes: no receiver, no
// repository, no imports, and a qualified-receiver mismatch all stay
// unresolved without blocking.
func TestCov8ResolveReceiverGuards(t *testing.T) {
	t.Parallel()

	index := cov8ServiceIndex()
	base := shared.ResolveContext{
		Index: index, RepositoryID: "repo-jvm",
		RelativePath: "com/example/App.java",
		RepositoryImports: map[string][]string{
			"Service": {"com/example/Service.java"},
		},
		FileData: map[string]any{"imports": []any{
			map[string]any{
				"name": "com.example.Service", "source": "com.example.Service",
				"import_type": "import",
			},
		}},
		Call:     map[string]any{"name": "run", "inferred_obj_type": "Service"},
		Language: "java",
	}
	for name, mutate := range map[string]func(*shared.ResolveContext){
		"no receiver": func(c *shared.ResolveContext) {
			c.Call = map[string]any{"name": "run"}
		},
		"no name": func(c *shared.ResolveContext) {
			c.Call = map[string]any{"inferred_obj_type": "Service"}
		},
		"no repository": func(c *shared.ResolveContext) { c.RepositoryID = "" },
		"unknown type": func(c *shared.ResolveContext) {
			c.Call = map[string]any{"name": "run", "inferred_obj_type": "Ghost"}
		},
	} {
		guarded := base
		mutate(&guarded)
		if entityID, _, _ := ResolveReceiverCallee(guarded, cov8JavaConfig); entityID != "" {
			t.Errorf("Resolve(%s) = %q, want empty", name, entityID)
		}
	}

	// Without a usable import the call still binds through the
	// repo-unique inference fallback: no imports, a foreign import
	// kind, and a qualified-receiver mismatch all land on
	// type_inferred rather than import_binding.
	for name, mutate := range map[string]func(*shared.ResolveContext){
		"no imports": func(c *shared.ResolveContext) {
			c.FileData = map[string]any{}
		},
		"foreign import kind": func(c *shared.ResolveContext) {
			c.FileData = map[string]any{"imports": []any{
				map[string]any{
					"name": "com.example.Service", "source": "com.example.Service",
					"import_type": "static",
				},
			}}
		},
		"qualified mismatch": func(c *shared.ResolveContext) {
			c.Call = map[string]any{
				"name": "run", "inferred_obj_type": "Service",
				"inferred_obj_qualified_type": "other.pkg.Service",
			}
		},
	} {
		guarded := base
		mutate(&guarded)
		entityID, _, method := ResolveReceiverCallee(guarded, cov8JavaConfig)
		if entityID != "entity-service-run" || method != codeprovenance.MethodTypeInferred {
			t.Errorf("Resolve(%s) = %q, %q; want entity-service-run via type inference",
				name, entityID, method)
		}
	}
}

// TestCov8ReceiverCandidateNames pins candidate expansion: the plain
// Type.method plus recorded argument-type and arity signatures.
func TestCov8ReceiverCandidateNames(t *testing.T) {
	t.Parallel()

	got := receiverCandidateNames(map[string]any{
		"name": "run", "inferred_obj_type": "Service",
		"argument_types": []any{"string", "int"}, "argument_count": 2,
	}, "")
	if len(got) == 0 || got[0] != "Service.run" {
		t.Fatalf("candidates = %v, want Service.run first", got)
	}
	if len(got) < 3 {
		t.Errorf("candidates = %v, want widened signatures beyond the plain name", got)
	}
	override := receiverCandidateNames(map[string]any{"name": "run"}, "Service")
	if len(override) != 1 || override[0] != "Service.run" {
		t.Errorf("override candidates = %v, want [Service.run]", override)
	}
	if got := receiverCandidateNames(map[string]any{"name": "run"}, ""); len(got) != 0 {
		t.Errorf("candidates(no type) = %v, want nil", got)
	}
	if got := resolveReceiverCandidate(shared.ResolveContext{}, ""); got != "" {
		t.Errorf("receiverCandidate(empty) = %q, want empty", got)
	}
}

// TestCov8ImportSourceMatching pins the path matchers: Java needs the
// type file name, Kotlin needs the package directory, wildcards expand,
// and qualified receivers must equal the source.
func TestCov8ImportSourceMatching(t *testing.T) {
	t.Parallel()

	if !importSourceMatchesPath("com.example.Service", "Service", "com/example/Service.java", cov8JavaConfig) {
		t.Error("java type-file match = false, want true")
	}
	if importSourceMatchesPath("com.example.Service", "Service", "com/example/Other.java", cov8JavaConfig) {
		t.Error("java wrong-file match = true, want false")
	}
	if !importSourceMatchesPath("com.example.Service", "Service", "com/example/Helpers.kt", cov8KotlinConfig) {
		t.Error("kotlin package-dir match = false, want true")
	}
	if importSourceMatchesPath("com.example", "Service", "org/other/Helpers.kt", cov8KotlinConfig) {
		t.Error("kotlin wrong-dir match = true, want false")
	}
	if !importSourceMatchesPath("com.example.*", "Service", "com/example/Service.java", cov8JavaConfig) {
		t.Error("wildcard match = false, want true")
	}
	if importSourceMatchesPath("", "Service", "com/example/Service.java", cov8JavaConfig) {
		t.Error("empty source match = true, want false")
	}

	if !importSourceMatchesQualifiedReceiver("com.example.Service", "Service", "") {
		t.Error("unqualified receiver must pass")
	}
	if !importSourceMatchesQualifiedReceiver("com.example.Service", "Service", "plain") {
		t.Error("dotted-less qualified receiver must pass")
	}
	if !importSourceMatchesQualifiedReceiver("com.example.*", "Service", "com.example.Service") {
		t.Error("wildcard qualified match = false, want true")
	}
	if importSourceMatchesQualifiedReceiver("com.example.Service", "Service", "other.pkg.Service") {
		t.Error("qualified mismatch = true, want false")
	}
	if importSourceMatchesQualifiedReceiver("", "Service", "other.pkg.Service") {
		t.Error("empty source qualified = true, want false")
	}

	entry := map[string]any{
		"name": "com.example.Service", "source": "com.example.Service",
		"import_type": "import",
	}
	if got := importEntryDeclaredType(entry, "Service", cov8JavaConfig); got != "Service" {
		t.Errorf("declared type = %q, want Service", got)
	}
	if got := importEntryDeclaredType(entry, "", cov8JavaConfig); got != "" {
		t.Errorf("declared type(empty receiver) = %q, want empty", got)
	}
	if got := importEntryDeclaredType(entry, "Other", cov8JavaConfig); got != "" {
		t.Errorf("declared type(unrelated) = %q, want empty", got)
	}
}
