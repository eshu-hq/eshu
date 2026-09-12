// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package swift

import (
	"github.com/eshu-hq/eshu/go/internal/reducer/code/call/shared"

	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// Resolvers returns the Swift language resolver list.
// Each call returns a fresh slice, so no caller can mutate another's view.
func Resolvers() []shared.Resolver {
	return []shared.Resolver{
		{
			Phase:   shared.PhaseBeforeRepoFallback,
			Resolve: resolveSwiftReceiverCallee,
		},
	}
}

// resolveSwiftReceiverCallee binds a Swift receiver-typed call to the uniquely
// named method on its inferred type within the caller's repository. Swift
// imports name modules rather than files, so there is no import-to-file binding;
// resolution is repo-scoped type inference, recorded as type_inferred provenance.
func resolveSwiftReceiverCallee(ctx shared.ResolveContext) (string, string, codeprovenance.Method) {
	return shared.ResolveReceiverMethodCallee(ctx)
}
