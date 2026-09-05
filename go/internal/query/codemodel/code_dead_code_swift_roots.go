// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

var swiftDeadCodeMetadataRootKinds = []string{
	"swift.main_function",
	"swift.main_type",
	"swift.swiftui_app_type",
	"swift.swiftui_body",
	"swift.protocol_type",
	"swift.protocol_method",
	"swift.protocol_implementation_method",
	"swift.constructor",
	"swift.override_method",
	"swift.ui_application_delegate_type",
	"swift.ui_application_delegate_method",
	"swift.vapor_route_handler",
	"swift.xctest_method",
	"swift.swift_testing_method",
}

// DeadCodeIsSwiftRoot reports whether the candidate is a Swift entrypoint root.
func DeadCodeIsSwiftRoot(result map[string]any, entity *querycontract.EntityContent, stats *DeadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "swift" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range swiftDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
