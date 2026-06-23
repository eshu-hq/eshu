package reducer

import (
	"github.com/eshu-hq/eshu/go/internal/codeprovenance"
)

// javaReceiverResolverConfig binds the shared JVM imported-receiver resolver to
// Java's parser output: only `import` declarations introduce types, and dotted
// package paths map to `.java` source files.
var javaReceiverResolverConfig = jvmReceiverResolverConfig{
	importTypes:     map[string]struct{}{"import": {}},
	sourceExtension: ".java",
}

func init() {
	registerCodeCallLanguageResolvers(
		"java",
		codeCallLanguageResolver{
			phase:   codeCallLanguageResolverPhaseBeforeRepoFallback,
			resolve: resolveJavaSemanticCallee,
		},
	)
}

func resolveJavaSemanticCallee(ctx codeCallResolveContext) (string, string, codeprovenance.Method) {
	return resolveJVMReceiverCallee(ctx, javaReceiverResolverConfig)
}

func javaImportedReceiverBindingBlocksRepoFallback(ctx codeCallResolveContext) bool {
	return jvmImportedReceiverBindingBlocksRepoFallback(ctx, javaReceiverResolverConfig)
}
