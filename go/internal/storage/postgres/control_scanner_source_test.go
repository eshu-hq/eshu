// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

func TestScannerSourceRejectsGenericFactWithoutCanonicalObservation(t *testing.T) {
	source := workflow.ScannerImageSourceBinding{
		SourceFactID:              "legacy-running-image-fact",
		SourceCollectorInstanceID: "source-instance",
		SourceScopeID:             "source-scope",
		SourceGenerationID:        "source-generation",
		Registry:                  "registry.example.invalid",
		Repository:                "team/service",
		RuntimeDigest:             "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		RuntimeDigestKind:         workflow.ScannerRuntimeDigestManifest,
		RuntimeImageReference:     "registry.example.invalid/team/service:release",
	}
	_, err := proveScannerSource(context.Background(), nil,
		workflow.WorkItem{CollectorInstanceID: "scanner-instance"},
		workflow.ScannerImageTarget{}, source,
		workflow.RunningImageDiscoveryRequest{})
	if !errors.Is(err, ErrScannerTargetNotAuthorized) {
		t.Fatalf("generic fact without typed observation error = %v, want unauthorized", err)
	}
}
