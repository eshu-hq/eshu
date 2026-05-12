package query

import (
	"slices"
	"strings"
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

func deadCodeIsScalaRoot(result map[string]any, entity *EntityContent, stats *deadCodePolicyStats) bool {
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
