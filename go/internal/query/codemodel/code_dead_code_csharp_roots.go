// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
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

// DeadCodeIsCSharpRoot reports whether the candidate is a C# entrypoint root.
func DeadCodeIsCSharpRoot(result map[string]any, entity *querycontract.EntityContent, stats *DeadCodePolicyStats) bool {
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
