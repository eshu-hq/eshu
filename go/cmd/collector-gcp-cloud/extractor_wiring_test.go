// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
)

// TestServerlessExtractorsRegistered proves the collector binary wires the
// serverless extractor group: every serverless CAI asset type must resolve to
// a registered typed-depth extractor. The group registers from init functions
// in its own package, so this test fails if the binary ever drops the
// side-effect import. The asset type strings are the canonical CAI values
// (see the assetType constants in the serverless extractor files); they are
// repeated here literally so the test pins the wire contract rather than the
// constants.
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
		if !gcpcloud.HasAssetExtractor(assetType) {
			t.Errorf("no extractor registered for serverless asset type %q", assetType)
		}
	}
}
