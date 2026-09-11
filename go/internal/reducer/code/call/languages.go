// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package call

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/dart"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/elixir"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/golang"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/groovy"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/haskell"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/java"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/javascript"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/kotlin"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/perl"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/python"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/rust"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/swift"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/typescript"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// codeCallLanguageResolvers is the explicit per-language resolver registry.
// It replaces the former init()-time registration: each language leaf
// exposes its resolver list in its own declared phase order, and this map
// wires it under the language key(s) that name it in parser output.
// JavaScript and TypeScript each register under two keys (their bare and JSX/
// TSX variants), matching the pre-split behavior. resolver_test.go (now
// languages_test.go) swaps this variable directly, so it stays a package
// variable rather than a function-local literal.
var codeCallLanguageResolvers = map[string][]shared.Resolver{
	"dart":       dart.Resolvers(),
	"elixir":     elixir.Resolvers(),
	"go":         golang.Resolvers(),
	"groovy":     groovy.Resolvers(),
	"haskell":    haskell.Resolvers(),
	"java":       java.Resolvers(),
	"javascript": javascript.Resolvers(),
	"jsx":        javascript.Resolvers(),
	"kotlin":     kotlin.Resolvers(),
	"perl":       perl.Resolvers(),
	"python":     python.Resolvers(),
	"rust":       rust.Resolvers(),
	"swift":      swift.Resolvers(),
	"typescript": typescript.Resolvers(),
	"tsx":        typescript.Resolvers(),
}

func resolveLanguageSpecificCallee(
	ctx shared.ResolveContext,
	phase shared.Phase,
) (string, string, codeprovenance.Method) {
	for _, resolver := range codeCallLanguageResolvers[ctx.Language] {
		if resolver.Phase != phase || resolver.Resolve == nil {
			continue
		}
		entityID, calleeFile, method := resolver.Resolve(ctx)
		if entityID != "" {
			return entityID, calleeFile, method
		}
	}
	return "", "", ""
}

func codeCallLanguageResolverBlocksRepoFallback(ctx shared.ResolveContext) bool {
	switch ctx.Language {
	case "dart":
		return dart.BlocksRepoFallback(ctx)
	case "elixir":
		return elixir.BlocksRepoFallback(ctx)
	case "haskell":
		return haskell.QualifiedImportTargetExists(ctx)
	case "java":
		return java.BlocksRepoFallback(ctx)
	case "kotlin":
		return kotlin.BlocksRepoFallback(ctx)
	default:
		return false
	}
}
