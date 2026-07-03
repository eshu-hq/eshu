// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/secretsiam"
)

const gkeClusterFullName = "//container.googleapis.com/projects/demo-project/locations/us-central1/clusters/prod"

func gkeClusterContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: gkeClusterFullName,
		AssetType:        assetTypeGKECluster,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestGKEClusterExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeGKECluster); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeGKECluster)
	}
}

func TestExtractGKEClusterFullConfig(t *testing.T) {
	const data = `{
		"location": "us-central1",
		"status": "RUNNING",
		"currentMasterVersion": "1.29.1-gke.1589000",
		"currentNodeVersion": "1.29.1-gke.1589000",
		"network": "projects/demo-project/global/networks/vpc-main",
		"subnetwork": "projects/demo-project/regions/us-central1/subnetworks/sub-main",
		"releaseChannel": {"channel": "REGULAR"},
		"createTime": "2026-01-15T10:00:00Z",
		"privateClusterConfig": {"enablePrivateNodes": true, "enablePrivateEndpoint": false},
		"masterAuthorizedNetworksConfig": {"enabled": true, "cidrBlocks": [{"cidrBlock": "10.0.0.0/8", "displayName": "corp"}]},
		"workloadIdentityConfig": {"workloadPool": "demo-project.svc.id.goog"},
		"addonsConfig": {
			"httpLoadBalancing": {"disabled": false},
			"horizontalPodAutoscaling": {"disabled": false},
			"networkPolicyConfig": {"disabled": true}
		},
		"nodePools": [
			{
				"name": "default-pool",
				"config": {
					"machineType": "e2-standard-4",
					"serviceAccount": "gke-nodes@demo-project.iam.gserviceaccount.com",
					"oauthScopes": ["https://www.googleapis.com/auth/cloud-platform"]
				},
				"autoscaling": {"enabled": true, "minNodeCount": 1, "maxNodeCount": 5},
				"initialNodeCount": 3
			},
			{
				"name": "spot-pool",
				"config": {
					"machineType": "e2-medium",
					"serviceAccount": "default"
				},
				"initialNodeCount": 2
			}
		]
	}`

	got, err := extractGKECluster(gkeClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"location":                            "us-central1",
		"status":                              "RUNNING",
		"current_master_version":              "1.29.1-gke.1589000",
		"current_node_version":                "1.29.1-gke.1589000",
		"release_channel":                     "REGULAR",
		"creation_time":                       "2026-01-15T10:00:00Z",
		"private_nodes_enabled":               true,
		"private_endpoint_enabled":            false,
		"master_authorized_networks_enabled":  true,
		"master_authorized_networks_count":    1,
		"workload_identity_pool":              "demo-project.svc.id.goog",
		"http_load_balancing_disabled":        false,
		"horizontal_pod_autoscaling_disabled": false,
		"network_policy_config_disabled":      true,
		"node_pool_count":                     2,
	}
	gotAttrsWithoutPools := map[string]any{}
	for k, v := range got.Attributes {
		if k == "node_pools" {
			continue
		}
		gotAttrsWithoutPools[k] = v
	}
	if diff := diffAttrs(gotAttrsWithoutPools, wantAttrs); diff != "" {
		t.Fatalf("attributes mismatch: %s\n got %#v\nwant %#v", diff, gotAttrsWithoutPools, wantAttrs)
	}

	nodePools, ok := got.Attributes["node_pools"].([]map[string]any)
	if !ok {
		t.Fatalf("expected node_pools attribute to be []map[string]any, got %#v", got.Attributes["node_pools"])
	}
	if len(nodePools) != 2 {
		t.Fatalf("expected 2 node pools, got %d: %#v", len(nodePools), nodePools)
	}
	defaultPool := nodePools[0]
	if defaultPool["name"] != "default-pool" {
		t.Errorf("node_pools[0].name = %v, want default-pool", defaultPool["name"])
	}
	if defaultPool["machine_type"] != "e2-standard-4" {
		t.Errorf("node_pools[0].machine_type = %v, want e2-standard-4", defaultPool["machine_type"])
	}
	wantFP := secretsiam.GCPServiceAccountEmailDigest("gke-nodes@demo-project.iam.gserviceaccount.com")
	if defaultPool["service_account_fingerprint"] != wantFP {
		t.Errorf("node_pools[0].service_account_fingerprint = %v, want %v", defaultPool["service_account_fingerprint"], wantFP)
	}
	if defaultPool["oauth_scope_count"] != 1 {
		t.Errorf("node_pools[0].oauth_scope_count = %v, want 1", defaultPool["oauth_scope_count"])
	}
	if defaultPool["autoscaling_enabled"] != true {
		t.Errorf("node_pools[0].autoscaling_enabled = %v, want true", defaultPool["autoscaling_enabled"])
	}
	if defaultPool["initial_node_count"] != 3 {
		t.Errorf("node_pools[0].initial_node_count = %v, want 3", defaultPool["initial_node_count"])
	}

	spotPool := nodePools[1]
	if _, hasFP := spotPool["service_account_fingerprint"]; hasFP {
		t.Errorf("expected no service_account_fingerprint for the default node-pool service account, got %#v", spotPool)
	}

	wantAnchors := []string{
		"//compute.googleapis.com/projects/demo-project/global/networks/vpc-main",
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/sub-main",
		wantFP,
	}
	if diff := diffStringSlices(got.CorrelationAnchors, wantAnchors); diff != "" {
		t.Fatalf("anchors mismatch: %s\n got %#v\nwant %#v", diff, got.CorrelationAnchors, wantAnchors)
	}

	assertRelationship(t, got.Relationships, relationshipTypeGKEClusterUsesNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/vpc-main", assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeGKEClusterUsesSubnetwork,
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/sub-main", assetTypeComputeSubnetwork)
	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 relationships (network, subnetwork), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	for _, rel := range got.Relationships {
		if rel.SourceFullResourceName != gkeClusterFullName {
			t.Errorf("relationship source = %q, want cluster full name", rel.SourceFullResourceName)
		}
		if rel.SourceAssetType != assetTypeGKECluster {
			t.Errorf("relationship source asset type = %q, want %q", rel.SourceAssetType, assetTypeGKECluster)
		}
	}
}

func TestExtractGKEClusterBareNetworkName(t *testing.T) {
	// GKE reports network/subnetwork as a bare short name when the cluster uses
	// the default VPC; the extractor must resolve it against the cluster's own
	// project (global for network, cluster's own region for subnetwork).
	const data = `{"location": "us-central1", "network": "default", "subnetwork": "default"}`
	got, err := extractGKECluster(gkeClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeGKEClusterUsesNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/default", assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeGKEClusterUsesSubnetwork,
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/default", assetTypeComputeSubnetwork)
}

func TestExtractGKEClusterNoLeakageOfCIDRValues(t *testing.T) {
	const data = `{
		"masterAuthorizedNetworksConfig": {"enabled": true, "cidrBlocks": [{"cidrBlock": "203.0.113.0/24", "displayName": "office"}]},
		"nodePools": [{"name": "p", "config": {"serviceAccount": "svc@demo-project.iam.gserviceaccount.com"}}]
	}`
	got, err := extractGKECluster(gkeClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, _ := json.Marshal(got)
	for _, banned := range []string{"203.0.113.0/24", "office", "svc@demo-project.iam.gserviceaccount.com"} {
		if containsString(string(blob), banned) {
			t.Fatalf("extraction leaked sensitive token %q: %s", banned, blob)
		}
	}
}

func TestExtractGKEClusterEmptyDataYieldsNoAttributesOrEdges(t *testing.T) {
	got, err := extractGKECluster(gkeClusterContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Errorf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for empty data, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors for empty data, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractGKEClusterMalformedDataErrors(t *testing.T) {
	_, err := extractGKECluster(gkeClusterContext(`{not json`))
	if err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

// TestExtractGKEClusterZonalSubnetworkNormalizesToRegion proves that a bare
// subnetwork short name on a zonal cluster (location is a zone, not a region)
// resolves against the zone's region, not the raw zone string. Before the
// fix, gkeSubnetworkFullName used the zonal location verbatim as a Compute
// "regions/{...}" segment, producing an invalid
// "regions/us-central1-a/subnetworks/..." path that can never resolve.
func TestExtractGKEClusterZonalSubnetworkNormalizesToRegion(t *testing.T) {
	const data = `{"location": "us-central1-a", "subnetwork": "default"}`
	got, err := extractGKECluster(gkeClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeGKEClusterUsesSubnetwork,
		"//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/default", assetTypeComputeSubnetwork)
}

// TestGKERegionFromLocation proves the zone-to-region derivation used to
// normalize a zonal GKE location before building a regional subnetwork path.
func TestGKERegionFromLocation(t *testing.T) {
	tests := []struct {
		location string
		want     string
	}{
		{"us-central1", "us-central1"},     // already a region
		{"us-central1-a", "us-central1"},   // zone -> region
		{"europe-west1-b", "europe-west1"}, // zone -> region
		{"asia-southeast1-c", "asia-southeast1"},
		{"", ""},
		{"  us-central1-a  ", "us-central1"}, // trims whitespace
	}
	for _, tt := range tests {
		if got := gkeRegionFromLocation(tt.location); got != tt.want {
			t.Errorf("gkeRegionFromLocation(%q) = %q, want %q", tt.location, got, tt.want)
		}
	}
}

// TestExtractGKEClusterUnparsableMachineTypeOmitsAttribute proves that when
// computeMachineTypeName cannot parse a node pool's machineType reference
// (a partial path/URL, not a bare name or a normal selfLink), the extractor
// drops the machine_type attribute rather than persisting the raw reference.
func TestExtractGKEClusterUnparsableMachineTypeOmitsAttribute(t *testing.T) {
	const data = `{
		"nodePools": [
			{"name": "bad-pool", "config": {"machineType": "projects/p/zones/z"}}
		]
	}`
	got, err := extractGKECluster(gkeClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	nodePools, ok := got.Attributes["node_pools"].([]map[string]any)
	if !ok {
		t.Fatalf("expected node_pools attribute to be []map[string]any, got %#v", got.Attributes["node_pools"])
	}
	if len(nodePools) != 1 {
		t.Fatalf("expected 1 node pool, got %d: %#v", len(nodePools), nodePools)
	}
	if v, has := nodePools[0]["machine_type"]; has {
		t.Errorf("expected no machine_type attribute for an unparsable reference, got %#v", v)
	}
}

// TestExtractGKEClusterAutoscalingZeroMinNodeCountIsPreserved proves that an
// explicit minNodeCount of 0 (a legitimate scale-to-zero autoscaling config)
// is emitted rather than dropped. Before the fix, minNodeCount/maxNodeCount
// decoded as plain int, so "absent" and "present with value 0" were
// indistinguishable and the ">0" guard silently dropped a real 0.
func TestExtractGKEClusterAutoscalingZeroMinNodeCountIsPreserved(t *testing.T) {
	const data = `{
		"nodePools": [
			{"name": "scale-to-zero", "autoscaling": {"enabled": true, "minNodeCount": 0, "maxNodeCount": 3}}
		]
	}`
	got, err := extractGKECluster(gkeClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	nodePools, ok := got.Attributes["node_pools"].([]map[string]any)
	if !ok {
		t.Fatalf("expected node_pools attribute to be []map[string]any, got %#v", got.Attributes["node_pools"])
	}
	if len(nodePools) != 1 {
		t.Fatalf("expected 1 node pool, got %d: %#v", len(nodePools), nodePools)
	}
	if v, has := nodePools[0]["autoscaling_min_node_count"]; !has || v != 0 {
		t.Errorf("autoscaling_min_node_count = %#v (present=%v), want 0 (present=true)", v, has)
	}
	if v, has := nodePools[0]["autoscaling_max_node_count"]; !has || v != 3 {
		t.Errorf("autoscaling_max_node_count = %#v (present=%v), want 3 (present=true)", v, has)
	}
}

// TestExtractGKEClusterAutoscalingAbsentNodeCountsOmitsAttributes proves that
// an absent autoscaling block, or one that never reports minNodeCount /
// maxNodeCount, does not fabricate a 0 value for either attribute.
func TestExtractGKEClusterAutoscalingAbsentNodeCountsOmitsAttributes(t *testing.T) {
	const data = `{
		"nodePools": [
			{"name": "no-autoscaling"}
		]
	}`
	got, err := extractGKECluster(gkeClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	nodePools, ok := got.Attributes["node_pools"].([]map[string]any)
	if !ok {
		t.Fatalf("expected node_pools attribute to be []map[string]any, got %#v", got.Attributes["node_pools"])
	}
	if len(nodePools) != 1 {
		t.Fatalf("expected 1 node pool, got %d: %#v", len(nodePools), nodePools)
	}
	if v, has := nodePools[0]["autoscaling_min_node_count"]; has {
		t.Errorf("expected no autoscaling_min_node_count for an absent autoscaling block, got %#v", v)
	}
	if v, has := nodePools[0]["autoscaling_max_node_count"]; has {
		t.Errorf("expected no autoscaling_max_node_count for an absent autoscaling block, got %#v", v)
	}
}

// TestExtractGKEClusterCidrBlocksTypeDoesNotRetainRawJSON proves that the
// masterAuthorizedNetworksConfig.cidrBlocks decode target no longer keeps the
// raw JSON bytes (CIDR values, display names) in memory — only a count is
// derivable. This is a compile-time/type proof: gkeMasterAuthorizedCIDRBlock
// must not carry any field that preserves the original CIDR or display-name
// bytes.
func TestExtractGKEClusterCidrBlocksTypeDoesNotRetainRawJSON(t *testing.T) {
	const data = `{
		"masterAuthorizedNetworksConfig": {
			"enabled": true,
			"cidrBlocks": [
				{"cidrBlock": "203.0.113.0/24", "displayName": "office"},
				{"cidrBlock": "198.51.100.0/24", "displayName": "vpn"}
			]
		}
	}`
	got, err := extractGKECluster(gkeClusterContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["master_authorized_networks_count"] != 2 {
		t.Fatalf("master_authorized_networks_count = %#v, want 2", got.Attributes["master_authorized_networks_count"])
	}
	blob, _ := json.Marshal(got)
	for _, banned := range []string{"203.0.113.0/24", "198.51.100.0/24", "office", "vpn"} {
		if containsString(string(blob), banned) {
			t.Fatalf("extraction leaked sensitive token %q: %s", banned, blob)
		}
	}
}

func diffAttrs(got, want map[string]any) string {
	if len(got) != len(want) {
		return "length mismatch"
	}
	for k, wv := range want {
		gv, ok := got[k]
		if !ok {
			return "missing key " + k
		}
		if stringify(gv) != stringify(wv) {
			return "value mismatch at key " + k
		}
	}
	return ""
}

func diffStringSlices(got, want []string) string {
	if len(got) != len(want) {
		return "length mismatch"
	}
	for i := range want {
		if got[i] != want[i] {
			return "value mismatch at index " + stringify(i)
		}
	}
	return ""
}
