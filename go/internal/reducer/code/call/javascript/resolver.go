// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package javascript

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// Resolvers returns the JavaScript/JSX language resolver list. code/call wires it
// under both the "javascript" and "jsx" language keys, matching the pre-split
// dual registration.
// Each call returns a fresh slice, so no caller can mutate another's view.
func Resolvers() []shared.Resolver {
	return []shared.Resolver{
		{
			Phase:   shared.PhaseBeforeRepoFallback,
			Resolve: resolveJavaScriptReceiverCallee,
		},
	}
}

// resolveJavaScriptReceiverCallee binds a JavaScript or JSX receiver-typed call
// to the uniquely named method on its inferred class within the caller's
// repository. JavaScript has no static interface contracts like TypeScript, so
// the inferred receiver type names the class directly; resolution is repo-scoped
// type inference recorded as type_inferred provenance. Same-file dynamic alias
// resolution still runs earlier in the dispatch; this resolver closes the
// cross-file receiver-typed gap that left JavaScript only partially covered.
func resolveJavaScriptReceiverCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	return shared.ResolveReceiverMethodCallee(ctx)
}
