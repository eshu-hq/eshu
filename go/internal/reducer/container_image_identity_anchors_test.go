// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"slices"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestBuildContainerImageIdentityDecisionsCarriesSourceAnchors(t *testing.T) {
	t.Parallel()

	source := gitImageRefFact("git-tag", "registry.example.com/team/api:prod")
	source.ScopeID = "git-repository-scope:repo://example/api"
	source.Payload["workload_id"] = "workload:example-api"
	source.Payload["service_id"] = "service:example-api"

	decisions := BuildContainerImageIdentityDecisions([]facts.Envelope{
		source,
		ociTagFact("oci-tag", "prod", testContainerDigest, false, ""),
	})

	got := decisionsByRef(decisions)["registry.example.com/team/api:prod"]
	if !slices.Contains(got.SourceRepositoryIDs, "repo://example/api") {
		t.Fatalf("SourceRepositoryIDs = %#v, want repo://example/api", got.SourceRepositoryIDs)
	}
	if !slices.Contains(got.WorkloadIDs, "workload:example-api") {
		t.Fatalf("WorkloadIDs = %#v, want workload:example-api", got.WorkloadIDs)
	}
	if !slices.Contains(got.ServiceIDs, "service:example-api") {
		t.Fatalf("ServiceIDs = %#v, want service:example-api", got.ServiceIDs)
	}
}
