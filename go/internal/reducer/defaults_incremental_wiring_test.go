// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"testing"

	"github.com/eshu-hq/eshu/go/internal/reducer/awscloud"
)

// TestImplementedDefaultDomainDefinitionsCarriesIncrementalWiring pins the
// production wiring path: when the drift handlers carry a plan source and
// a shared evidence cache, the registered AWS drift handler carries them
// onto the run (#10878 review P2: the tested incremental path must be the
// wired one, not a nil-plan full fallback).
func TestImplementedDefaultDomainDefinitionsCarriesIncrementalWiring(t *testing.T) {
	t.Parallel()

	source := &awscloud.AWSDriftIncrementalPlanner{}
	cache := awscloud.NewAWSDriftEvidenceCache(1024)
	definitions := implementedDefaultDomainDefinitions(DefaultHandlers{
		DriftHandlers: DriftHandlers{
			AWSCloudRuntimeDriftEvidenceLoader:        &stubAWSCloudRuntimeDriftEvidenceLoader{},
			AWSCloudRuntimeDriftWriter:                &stubAWSCloudRuntimeDriftFindingWriter{},
			AWSCloudRuntimeDriftFencingTokenIssuer:    &stubAWSCloudRuntimeDriftFencingTokenIssuer{tokens: []int64{1}},
			AWSCloudRuntimeDriftIncrementalPlanSource: source,
			AWSCloudRuntimeDriftEvidenceCache:         cache,
		},
	})
	for _, def := range definitions {
		if def.Domain != DomainAWSCloudRuntimeDrift {
			continue
		}
		handler, ok := def.Handler.(awscloud.AWSCloudRuntimeDriftHandler)
		if !ok {
			t.Fatalf("aws_cloud_runtime_drift handler type = %T, want awscloud.AWSCloudRuntimeDriftHandler", def.Handler)
		}
		if handler.IncrementalPlanSource == nil {
			t.Fatal("registered handler IncrementalPlanSource is nil: production runs would full-fallback instead of narrowing")
		}
		if handler.EvidenceCache == nil {
			t.Fatal("registered handler EvidenceCache is nil: production runs would load directly instead of sharing")
		}
		return
	}
	t.Fatal("aws_cloud_runtime_drift not registered after wiring loader+writer+fencing token issuer")
}
