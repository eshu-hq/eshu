// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// kotlinReceiverResolverConfig binds the shared JVM imported-receiver resolver
// to Kotlin's parser output: both `import` and aliased `import ... as` (emitted
// as import_type "alias") introduce types, and dotted package paths map to `.kt`
// source files. matchTypeFileName stays false because Kotlin allows a type to be
// declared in any file (e.g. `class Service` in `Domain.kt`); the prescan import
// map already points the declared type at its real file, so the resolver matches
// the package directory and trusts that mapping instead of the filename.
var kotlinReceiverResolverConfig = jvmReceiverResolverConfig{
	importTypes: map[string]struct{}{
		"import": {},
		"alias":  {},
	},
	sourceExtension: ".kt",
}

func init() {
	registerCodeCallLanguageResolvers(
		"kotlin",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveKotlinSemanticCallee,
		},
	)
}

// resolveKotlinSemanticCallee resolves a Kotlin receiver-typed call to its
// imported declaration, then to a repository-scoped type-inference candidate,
// mirroring the Java resolver against Kotlin's `.kt` layout and aliased imports.
func resolveKotlinSemanticCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	return resolveJVMReceiverCallee(ctx, kotlinReceiverResolverConfig)
}

// kotlinImportedReceiverBindingBlocksRepoFallback reports whether the Kotlin
// file imported the receiver type, so the dispatch must not fall back to an
// ambiguous repo-unique-name guess after the resolver declines.
func kotlinImportedReceiverBindingBlocksRepoFallback(ctx codeCallResolveContext) bool {
	return jvmImportedReceiverBindingBlocksRepoFallback(ctx, kotlinReceiverResolverConfig)
}
