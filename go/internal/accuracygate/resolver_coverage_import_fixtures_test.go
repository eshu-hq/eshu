package accuracygate_test

import (
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// This file holds the import-binding and class-qualified resolver coverage
// fixtures (dart, elixir, groovy, haskell, perl, rust). The receiver/interface
// resolver fixtures and the shared envelope helpers live in
// resolver_coverage_fixtures_test.go.

// dartResolverCoverageFixture exercises the Dart import-call binding resolver.
func dartResolverCoverageFixture() resolverCoverageFixture {
	calleePath := "/repo/lib/src/helper.dart"
	return resolverCoverageFixture{
		language:         "dart",
		callerUID:        "uid:dart-caller",
		calleeUID:        "uid:dart-helper",
		resolutionMethod: methodImportBinding,
		envelopes: []facts.Envelope{
			{FactKind: "repository", Payload: map[string]any{
				"repo_id":     "repo-dart",
				"imports_map": map[string][]string{"helper": {calleePath}},
			}},
			fileEnvelopeWithImports("repo-dart", "lib/service.dart", "/repo/lib/service.dart",
				[]any{fn("run", "uid:dart-caller", 4, 6)},
				[]any{map[string]any{"name": "src/helper.dart", "lang": "dart"}},
				[]any{call(map[string]any{"name": "helper", "full_name": "helper", "line_number": 5, "lang": "dart"})},
			),
			fileEnvelope("repo-dart", "lib/src/helper.dart", calleePath,
				[]any{fn("helper", "uid:dart-helper", 2, 3)},
				nil,
				nil,
			),
		},
	}
}

// elixirResolverCoverageFixture exercises the Elixir alias-call binding resolver.
func elixirResolverCoverageFixture() resolverCoverageFixture {
	calleePath := "/repo/lib/basic.ex"
	return resolverCoverageFixture{
		language:         "elixir",
		callerUID:        "uid:elixir-caller",
		calleeUID:        "uid:elixir-greet",
		resolutionMethod: methodImportBinding,
		envelopes: []facts.Envelope{
			{FactKind: "repository", Payload: map[string]any{
				"repo_id":     "repo-elixir",
				"imports_map": map[string][]string{"Demo.Basic": {calleePath}},
			}},
			fileEnvelopeWithImports("repo-elixir", "lib/worker.ex", "/repo/lib/worker.ex",
				[]any{fnClass("caller", "uid:elixir-caller", "Demo.Worker", 5, 7)},
				[]any{map[string]any{"name": "Demo.Basic", "alias": "Basic", "lang": "elixir", "import_type": "alias"}},
				[]any{call(map[string]any{"name": "greet", "full_name": "Basic.greet", "inferred_obj_type": "Basic", "line_number": 6, "lang": "elixir"})},
			),
			fileEnvelope("repo-elixir", "lib/basic.ex", calleePath,
				[]any{fnClass("greet", "uid:elixir-greet", "Demo.Basic", 2, 4)},
				nil,
				nil,
			),
		},
	}
}

// groovyResolverCoverageFixture exercises the Groovy class-qualified call resolver.
func groovyResolverCoverageFixture() resolverCoverageFixture {
	return resolverCoverageFixture{
		language:         "groovy",
		callerUID:        "uid:groovy-call",
		calleeUID:        "uid:groovy-deploy",
		resolutionMethod: methodTypeInferred,
		envelopes: []facts.Envelope{
			{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-groovy"}},
			fileEnvelope("repo-groovy", "vars/deployPipeline.groovy", "vars/deployPipeline.groovy",
				[]any{fn("call", "uid:groovy-call", 2, 4)},
				nil,
				[]any{call(map[string]any{
					"name":              "deployApp",
					"full_name":         "DeployHelper.deployApp",
					"inferred_obj_type": "DeployHelper",
					"line_number":       3,
					"lang":              "groovy",
				})},
			),
			fileEnvelope("repo-groovy", "src/org/example/DeployHelper.groovy", "src/org/example/DeployHelper.groovy",
				[]any{fnClass("deployApp", "uid:groovy-deploy", "DeployHelper", 2, 4)},
				nil,
				nil,
			),
		},
	}
}

// haskellResolverCoverageFixture exercises the Haskell qualified-import resolver.
func haskellResolverCoverageFixture() resolverCoverageFixture {
	calleePath := "/repo/src/Data/Text.hs"
	return resolverCoverageFixture{
		language:         "haskell",
		callerUID:        "uid:haskell-caller",
		calleeUID:        "uid:haskell-pack",
		resolutionMethod: methodImportBinding,
		envelopes: []facts.Envelope{
			{FactKind: "repository", Payload: map[string]any{
				"repo_id":     "repo-haskell",
				"imports_map": map[string][]string{"Data.Text": {calleePath}},
			}},
			fileEnvelopeWithImports("repo-haskell", "app/Main.hs", "/repo/app/Main.hs",
				[]any{fn("caller", "uid:haskell-caller", 5, 7)},
				[]any{map[string]any{"name": "Data.Text", "alias": "T", "lang": "haskell"}},
				[]any{call(map[string]any{"name": "pack", "full_name": "T.pack", "line_number": 6, "lang": "haskell"})},
			),
			fileEnvelope("repo-haskell", "src/Data/Text.hs", calleePath,
				[]any{fn("pack", "uid:haskell-pack", 2, 3)},
				nil,
				nil,
			),
		},
	}
}

// perlResolverCoverageFixture exercises the Perl imported-receiver path resolver.
func perlResolverCoverageFixture() resolverCoverageFixture {
	calleePath := "/repo/lib/App/Util.pm"
	return resolverCoverageFixture{
		language:         "perl",
		callerUID:        "uid:perl-caller",
		calleeUID:        "uid:perl-execute",
		resolutionMethod: methodImportBinding,
		envelopes: []facts.Envelope{
			{FactKind: "repository", Payload: map[string]any{
				"repo_id":     "repo-perl",
				"imports_map": map[string][]string{"App::Util": {calleePath}},
			}},
			fileEnvelopeWithImports("repo-perl", "bin/worker.pl", "/repo/bin/worker.pl",
				[]any{fnClass("run", "uid:perl-caller", "Worker", 4, 7)},
				[]any{map[string]any{"name": "App::Util", "lang": "perl"}},
				[]any{call(map[string]any{"name": "App::Util::execute", "full_name": "App::Util::execute", "line_number": 5, "lang": "perl"})},
			),
			fileEnvelope("repo-perl", "lib/App/Util.pm", calleePath,
				[]any{fnClass("execute", "uid:perl-execute", "Util", 3, 5)},
				nil,
				nil,
			),
		},
	}
}

// rustResolverCoverageFixture exercises the Rust trait-bound receiver resolver: a
// generic caller bounded by `T: Area` binds `shape.area()` to the Area trait
// method.
func rustResolverCoverageFixture() resolverCoverageFixture {
	return resolverCoverageFixture{
		language:         "rust",
		callerUID:        "uid:rust-compare",
		calleeUID:        "uid:rust-area",
		resolutionMethod: methodTypeInferred,
		envelopes: []facts.Envelope{
			{FactKind: "repository", Payload: map[string]any{"repo_id": "repo-rust"}},
			fileEnvelope("repo-rust", "src/lib.rs", "src/lib.rs",
				[]any{
					map[string]any{"uid": "uid:rust-area", "name": "area", "line_number": 2, "end_line": 2, "lang": "rust", "trait_context": "Area"},
					map[string]any{"uid": "uid:rust-draw-area", "name": "area", "line_number": 6, "end_line": 6, "lang": "rust", "trait_context": "Draw"},
					map[string]any{"uid": "uid:rust-compare", "name": "compare", "line_number": 10, "end_line": 14, "lang": "rust", "where_predicates": []any{"T: Area"}},
				},
				nil,
				[]any{call(map[string]any{"name": "area", "full_name": "shape.area", "line_number": 13, "lang": "rust", "inferred_obj_type": "T"})},
			),
		},
	}
}
