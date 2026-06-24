// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func init() {
	registerCodeCallLanguageResolvers(
		"swift",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveSwiftReceiverCallee,
		},
	)
}

// resolveSwiftReceiverCallee binds a Swift receiver-typed call to the uniquely
// named method on its inferred type within the caller's repository. Swift
// imports name modules rather than files, so there is no import-to-file binding;
// resolution is repo-scoped type inference, recorded as type_inferred provenance.
func resolveSwiftReceiverCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	return resolveReceiverMethodCallee(ctx)
}
