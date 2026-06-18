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
