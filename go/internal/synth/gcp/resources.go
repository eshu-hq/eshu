// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcp

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/replay/cassette"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	gcpv1 "github.com/eshu-hq/eshu/sdk/go/factschema/gcp/v1"
)

// syntheticLocations is the bounded set of GCP region/zone strings assigned
// to generated resources. A fixed, small set keeps the corpus's location
// distribution reviewable and reproducible rather than drawing from an
// unbounded string space.
var syntheticLocations = []string{
	"us-central1",
	"us-east1",
	"europe-west1",
	"asia-southeast1",
	"global",
}

// resourceStates is the bounded set of provider-reported lifecycle states
// assigned to generated resources.
var resourceStates = []string{"ACTIVE", "RUNNING", "PROVISIONING", "READY"}

// buildResourceFacts generates g.opts.ResourceCount gcp_cloud_resource facts,
// cycling deterministically through assetTypeInventory (issue #4581: "the
// typed-depth extractor registry gives the cleanest asset-type inventory to
// synthesize against"). Generated resources are retained on g.resources so
// later fact kinds (relationships, DNS records, IAM observations) can
// reference real, already-generated resource identities instead of
// fabricating disconnected join keys.
func (g *generation) buildResourceFacts() ([]cassette.Fact, error) {
	facts := make([]cassette.Fact, 0, g.opts.ResourceCount)
	g.resources = make([]gcpv1.Resource, 0, g.opts.ResourceCount)

	for i := 0; i < g.opts.ResourceCount; i++ {
		assetType := assetTypeInventory[i%len(assetTypeInventory)]
		resource := g.buildOneResource(assetType, i)
		g.resources = append(g.resources, resource)

		payload, err := factschema.EncodeGCPCloudResource(resource)
		if err != nil {
			return nil, fmt.Errorf("synth/gcp: encode gcp_cloud_resource[%d]: %w", i, err)
		}
		fact, err := generateFact(factschema.FactKindGCPCloudResource, factKindSchemaVersions[factschema.FactKindGCPCloudResource], payload)
		if err != nil {
			return nil, err
		}
		fact.StableFactKey = resourceStableFactKey(g.opts.ProjectID, resource)
		facts = append(facts, fact)
	}
	return facts, nil
}

// buildOneResource synthesizes one schema-valid gcpv1.Resource for assetType.
// index disambiguates same-asset-type resources by name; the RNG supplies
// only cosmetic variety (location, state), never identity, so identity stays
// deterministic purely from (seed-derived asset ordering, index).
func (g *generation) buildOneResource(assetType string, index int) gcpv1.Resource {
	family := assetTypeFamily(assetType)
	displayName := fmt.Sprintf("synth-%s-%d", family, index)
	fullResourceName := fmt.Sprintf("//%s/projects/%s/%sName/%s", serviceHost(assetType), g.opts.ProjectID, family, displayName)

	location := syntheticLocations[g.rng.IntN(len(syntheticLocations))]
	state := resourceStates[g.rng.IntN(len(resourceStates))]
	projectID := g.opts.ProjectID

	return gcpv1.Resource{
		FullResourceName: fullResourceName,
		AssetType:        assetType,
		ProjectID:        &projectID,
		Location:         &location,
		DisplayName:      &displayName,
		State:            &state,
		AssetTypeFamily:  &family,
		Attributes: map[string]any{
			"attributes": map[string]any{
				"synthetic_index": index,
			},
		},
	}
}

// assetTypeFamily derives a short family token from a CAI asset type's
// service host (the segment before ".googleapis.com"), e.g.
// "compute.googleapis.com/Instance" -> "compute". This mirrors the shape of
// the collector's asset_type_family field (go/internal/reducer's
// service_kind derivation) closely enough for synthetic corpus purposes
// without importing collector internals: it is a self-contained string
// derivation, not a copy of collector logic.
func assetTypeFamily(assetType string) string {
	host, _, found := strings.Cut(assetType, ".googleapis.com/")
	if !found {
		return "unknown"
	}
	return host
}

// serviceHost returns the CAI resource-name host segment for an asset type
// (the part before the first "/"), used to build a plausible
// full_resource_name.
func serviceHost(assetType string) string {
	host, _, _ := strings.Cut(assetType, "/")
	return host
}

// resourceStableFactKey derives a deterministic dedup key for a generated
// resource, mirroring the shape (not the exact derivation) of the live
// collector's stable_fact_key: scoped by project, asset type, and resource
// name so two runs of the same seed produce identical keys and two distinct
// resources never collide.
func resourceStableFactKey(projectID string, resource gcpv1.Resource) string {
	return fmt.Sprintf("gcp:project:%s:%s:%s", projectID, resource.AssetType, resource.FullResourceName)
}
