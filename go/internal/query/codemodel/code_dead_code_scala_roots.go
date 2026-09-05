// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

var scalaDeadCodeMetadataRootKinds = []string{
	"scala.main_method",
	"scala.app_object",
	"scala.trait_type",
	"scala.trait_method",
	"scala.trait_implementation_method",
	"scala.override_method",
	"scala.play_controller_action",
	"scala.akka_actor_receive",
	"scala.lifecycle_callback_method",
	"scala.junit_test_method",
	"scala.scalatest_suite_class",
}

// DeadCodeIsScalaRoot reports whether the candidate is a Scala entrypoint root.
func DeadCodeIsScalaRoot(result map[string]any, entity *querycontract.EntityContent, stats *DeadCodePolicyStats) bool {
	if strings.ToLower(deadCodeEntityLanguage(result, entity)) != "scala" {
		return false
	}
	rootKinds := deadCodeRootKinds(result, entity)
	if len(rootKinds) == 0 {
		return false
	}
	for _, rootKind := range scalaDeadCodeMetadataRootKinds {
		if slices.Contains(rootKinds, rootKind) {
			stats.ParserMetadataFrameworkRoots++
			return true
		}
	}
	return false
}
