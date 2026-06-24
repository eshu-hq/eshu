// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

func init() {
	registerCodeCallLanguageResolvers(
		"go",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveGoPackageQualifiedCallee,
		},
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveGoMethodReturnChainCallee,
		},
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveGoSameDirectoryCallee,
		},
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseAfterRepoFallback,
			resolve: resolveGoCrossRepoExportCallee,
		},
	)
}
