package query

import (
	"slices"
	"strings"
)

var csharpDeadCodeMetadataRootKinds = []string{
	"csharp.main_method",
	"csharp.constructor",
	"csharp.override_method",
	"csharp.interface_method",
	"csharp.interface_implementation_method",
	"csharp.aspnet_controller_action",
	"csharp.hosted_service_entrypoint",
	"csharp.test_method",
	"csharp.serialization_callback",
}

func deadCodeIsCSharpRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "c_sharp" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range csharpDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
