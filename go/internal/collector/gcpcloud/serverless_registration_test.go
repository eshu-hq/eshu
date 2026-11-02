// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"testing"

	_ "github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/extractor/serverless"
)

// TestServerlessExtractorsRegistered pins the serverless extractor wiring
// for the in-tree kernel: every serverless CAI asset type must resolve to a
// registered typed-depth extractor. The group registers from init functions
// in its own package, so this test fails if the side-effect import above is
// ever dropped (an unregistered type silently keeps the base observation in
// applyTypedDepth). The asset type strings are the canonical CAI values;
// they are repeated here literally so the test pins the wire contract rather
// than the constants. Carried over from the deleted in-tree command wiring
// test on the #8856 extraction.
func TestServerlessExtractorsRegistered(t *testing.T) {
	serverlessAssetTypes := []string{
		"appengine.googleapis.com/Application",
		"appengine.googleapis.com/Service",
		"cloudfunctions.googleapis.com/Function",
		"cloudfunctions.googleapis.com/CloudFunction",
		"cloudscheduler.googleapis.com/Job",
		"cloudtasks.googleapis.com/Queue",
		"composer.googleapis.com/Environment",
		"eventarc.googleapis.com/Trigger",
		"run.googleapis.com/Revision",
		"run.googleapis.com/Service",
		"workflows.googleapis.com/Workflow",
	}
	for _, assetType := range serverlessAssetTypes {
		if !HasAssetExtractor(assetType) {
			t.Errorf("no extractor registered for serverless asset type %q", assetType)
		}
	}
}
