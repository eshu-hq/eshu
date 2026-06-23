package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

func init() {
	registerCodeCallLanguageResolvers(
		"javascript",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveJavaScriptReceiverCallee,
		},
	)
	registerCodeCallLanguageResolvers(
		"jsx",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveJavaScriptReceiverCallee,
		},
	)
}

// resolveJavaScriptReceiverCallee binds a JavaScript or JSX receiver-typed call
// to the uniquely named method on its inferred class within the caller's
// repository. JavaScript has no static interface contracts like TypeScript, so
// the inferred receiver type names the class directly; resolution is repo-scoped
// type inference recorded as type_inferred provenance. Same-file dynamic alias
// resolution still runs earlier in the dispatch; this resolver closes the
// cross-file receiver-typed gap that left JavaScript only partially covered.
func resolveJavaScriptReceiverCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	return resolveReceiverMethodCallee(ctx)
}
