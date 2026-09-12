// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package java

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/jvm"
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// javaReceiverResolverConfig binds the shared JVM imported-receiver resolver to
// Java's parser output: only `import` declarations introduce types, and dotted
// package paths map to `.java` source files.
var javaReceiverResolverConfig = jvm.ReceiverConfig{
	ImportTypes:       map[string]struct{}{"import": {}},
	SourceExtension:   ".java",
	MatchTypeFileName: true,
}

// Resolvers returns the Java language resolver list.
// Each call returns a fresh slice, so no caller can mutate another's view.
func Resolvers() []shared.Resolver {
	return []shared.Resolver{
		{
			Phase:   shared.PhaseBeforeRepoFallback,
			Resolve: resolveJavaSemanticCallee,
		},
	}
}

func resolveJavaSemanticCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	return jvm.ResolveReceiverCallee(ctx, javaReceiverResolverConfig)
}

// BlocksRepoFallback reports whether the Java file imported the receiver
// type, so the dispatch must not fall back to an ambiguous
// repo-unique-name guess after the resolver declines.
func BlocksRepoFallback(ctx shared.ResolveContext) bool {
	return jvm.ImportedReceiverBlocksRepoFallback(ctx, javaReceiverResolverConfig)
}
