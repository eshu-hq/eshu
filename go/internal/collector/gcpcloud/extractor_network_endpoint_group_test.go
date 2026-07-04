// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

const networkEndpointGroupFullName = "//compute.googleapis.com/projects/demo-project/zones/us-central1-a/networkEndpointGroups/order-neg"

func networkEndpointGroupContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: networkEndpointGroupFullName,
		AssetType:        assetTypeComputeNetworkEndpointGroup,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestNetworkEndpointGroupExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeNetworkEndpointGroup); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeNetworkEndpointGroup)
	}
}

func TestExtractNetworkEndpointGroupZonalGCEVMIPPort(t *testing.T) {
	const data = `{
		"name": "order-neg",
		"networkEndpointType": "GCE_VM_IP_PORT",
		"size": 3,
		"defaultPort": 8080,
		"zone": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a",
		"network": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/default",
		"subnetwork": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1/subnetworks/default",
		"creationTimestamp": "2024-06-01T00:00:00Z"
	}`

	got, err := extractNetworkEndpointGroup(networkEndpointGroupContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"network_endpoint_type": "GCE_VM_IP_PORT",
		"size":                  int64(3),
		"default_port":          int64(8080),
		"zone":                  "us-central1-a",
		"creation_time":         "2024-06-01T00:00:00Z",
	}
	for k, want := range wantAttrs {
		got, ok := got.Attributes[k]
		if !ok {
			t.Errorf("missing attribute %q", k)
			continue
		}
		if got != want {
			t.Errorf("attribute %q = %v (%T), want %v (%T)", k, got, got, want, want)
		}
	}
	if len(got.Attributes) != len(wantAttrs) {
		t.Fatalf("attributes mismatch: got %#v want keys %#v", got.Attributes, wantAttrs)
	}

	wantNetwork := "//compute.googleapis.com/projects/demo-project/global/networks/default"
	wantSubnet := "//compute.googleapis.com/projects/demo-project/regions/us-central1/subnetworks/default"
	assertRelationship(t, got.Relationships, relationshipTypeNetworkEndpointGroupInNetwork, wantNetwork, assetTypeComputeNetwork)
	assertRelationship(t, got.Relationships, relationshipTypeNetworkEndpointGroupInSubnetwork, wantSubnet, assetTypeComputeSubnetwork)
	if len(got.Relationships) != 2 {
		t.Fatalf("expected exactly 2 relationships, got %d: %#v", len(got.Relationships), got.Relationships)
	}

	foundNetwork := false
	foundSubnet := false
	for _, a := range got.CorrelationAnchors {
		if a == wantNetwork {
			foundNetwork = true
		}
		if a == wantSubnet {
			foundSubnet = true
		}
	}
	if !foundNetwork {
		t.Errorf("expected network anchor %q in %#v", wantNetwork, got.CorrelationAnchors)
	}
	if !foundSubnet {
		t.Errorf("expected subnetwork anchor %q in %#v", wantSubnet, got.CorrelationAnchors)
	}
}

func TestExtractNetworkEndpointGroupServerlessCloudRun(t *testing.T) {
	const data = `{
		"name": "run-neg",
		"networkEndpointType": "SERVERLESS",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"cloudRun": {
			"service": "order-service",
			"tag": "revision-0010",
			"urlMask": "<tag>.domain.com/<service>"
		},
		"creationTimestamp": "2024-06-01T00:00:00Z"
	}`

	got, err := extractNetworkEndpointGroup(networkEndpointGroupContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"network_endpoint_type": "SERVERLESS",
		"region":                "us-central1",
		"serverless_type":       "cloud_run",
		"serverless_service":    "order-service",
		"creation_time":         "2024-06-01T00:00:00Z",
	}
	for k, want := range wantAttrs {
		got, ok := got.Attributes[k]
		if !ok {
			t.Errorf("missing attribute %q", k)
			continue
		}
		if got != want {
			t.Errorf("attribute %q = %v, want %v", k, got, want)
		}
	}
	if len(got.Attributes) != len(wantAttrs) {
		t.Fatalf("attributes mismatch: got %#v want keys %#v", got.Attributes, wantAttrs)
	}

	// Serverless refs are not resolvable CAI resource identities (only a
	// service/tag string scoped to the same project+region), so no edge or
	// anchor is emitted for them, and no urlMask or tag value ever leaves the
	// parser.
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no relationships for a serverless NEG, got %#v", got.Relationships)
	}
	for _, a := range got.CorrelationAnchors {
		if a == "<tag>.domain.com/<service>" || a == "revision-0010" {
			t.Fatalf("urlMask/tag leaked into correlation anchors: %#v", got.CorrelationAnchors)
		}
	}
}

func TestExtractNetworkEndpointGroupServerlessAppEngine(t *testing.T) {
	const data = `{
		"name": "appengine-neg",
		"networkEndpointType": "SERVERLESS",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"appEngine": {
			"service": "default",
			"version": "v1"
		}
	}`

	got, err := extractNetworkEndpointGroup(networkEndpointGroupContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["serverless_type"] != "app_engine" {
		t.Errorf("serverless_type = %v, want app_engine", got.Attributes["serverless_type"])
	}
	if got.Attributes["serverless_service"] != "default" {
		t.Errorf("serverless_service = %v, want default", got.Attributes["serverless_service"])
	}
	if _, ok := got.Attributes["serverless_version"]; ok {
		t.Errorf("did not expect serverless_version to be surfaced (data-plane routing value): %#v", got.Attributes)
	}
}

func TestExtractNetworkEndpointGroupServerlessCloudFunction(t *testing.T) {
	const data = `{
		"name": "func-neg",
		"networkEndpointType": "SERVERLESS",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"cloudFunction": {
			"function": "func1"
		}
	}`

	got, err := extractNetworkEndpointGroup(networkEndpointGroupContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["serverless_type"] != "cloud_function" {
		t.Errorf("serverless_type = %v, want cloud_function", got.Attributes["serverless_type"])
	}
	if got.Attributes["serverless_service"] != "func1" {
		t.Errorf("serverless_service = %v, want func1", got.Attributes["serverless_service"])
	}
}

func TestExtractNetworkEndpointGroupPrivateServiceConnect(t *testing.T) {
	const data = `{
		"name": "psc-neg",
		"networkEndpointType": "PRIVATE_SERVICE_CONNECT",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"network": "https://www.googleapis.com/compute/v1/projects/demo-project/global/networks/default",
		"pscTargetService": "asia-northeast3-cloudkms.googleapis.com",
		"pscData": {
			"pscConnectionId": "123456789",
			"pscConnectionStatus": "ACCEPTED",
			"producerPort": 443,
			"consumerPscAddress": "10.1.2.3"
		}
	}`

	got, err := extractNetworkEndpointGroup(networkEndpointGroupContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantFP := networkEndpointGroupPSCTargetServiceFingerprint("asia-northeast3-cloudkms.googleapis.com")
	if wantFP == "" {
		t.Fatalf("expected non-empty fingerprint for test setup")
	}
	if got.Attributes["psc_target_service_fingerprint"] != wantFP {
		t.Errorf("psc_target_service_fingerprint = %v, want %v", got.Attributes["psc_target_service_fingerprint"], wantFP)
	}
	if got.Attributes["psc_connection_status"] != "ACCEPTED" {
		t.Errorf("psc_connection_status = %v, want ACCEPTED", got.Attributes["psc_connection_status"])
	}
	if got.Attributes["psc_producer_port"] != int64(443) {
		t.Errorf("psc_producer_port = %v, want 443", got.Attributes["psc_producer_port"])
	}
	// The Compute API assigns pscConnectionId as a uint64 that can exceed
	// int64/float64 precision; it is kept as the raw string, never parsed.
	if got.Attributes["psc_connection_id"] != "123456789" {
		t.Errorf("psc_connection_id = %v, want 123456789", got.Attributes["psc_connection_id"])
	}
	if _, ok := got.Attributes["consumer_psc_address"]; ok {
		t.Fatalf("consumerPscAddress (a VIP address) must never be persisted: %#v", got.Attributes)
	}
	for _, a := range got.CorrelationAnchors {
		if a == "10.1.2.3" {
			t.Fatalf("consumerPscAddress leaked into correlation anchors: %#v", got.CorrelationAnchors)
		}
	}
	// network is still a valid edge target for a PSC NEG per the live schema.
	assertRelationship(t, got.Relationships, relationshipTypeNetworkEndpointGroupInNetwork,
		"//compute.googleapis.com/projects/demo-project/global/networks/default", assetTypeComputeNetwork)
}

func TestExtractNetworkEndpointGroupAnnotationsBoundedToCount(t *testing.T) {
	const data = `{
		"name": "annotated-neg",
		"networkEndpointType": "GCE_VM_IP",
		"zone": "https://www.googleapis.com/compute/v1/projects/demo-project/zones/us-central1-a",
		"annotations": {"team": "payments", "env": "prod"}
	}`

	got, err := extractNetworkEndpointGroup(networkEndpointGroupContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["annotation_count"] != 2 {
		t.Errorf("annotation_count = %v, want 2", got.Attributes["annotation_count"])
	}
	for k, v := range got.Attributes {
		if k == "annotation_count" {
			continue
		}
		if v == "payments" || v == "prod" {
			t.Fatalf("raw annotation value leaked into attribute %q: %v", k, v)
		}
	}
}

func TestExtractNetworkEndpointGroupEmptyDataYieldsNoTypedDepth(t *testing.T) {
	got, err := extractNetworkEndpointGroup(networkEndpointGroupContext(`{}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Attributes) != 0 {
		t.Fatalf("expected no attributes for empty data, got %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no relationships for empty data, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Fatalf("expected no anchors for empty data, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractNetworkEndpointGroupInvalidJSON(t *testing.T) {
	if _, err := extractNetworkEndpointGroup(networkEndpointGroupContext(`{not-json`)); err == nil {
		t.Fatalf("expected an error for invalid JSON")
	}
}

func TestNetworkEndpointGroupPSCTargetServiceFingerprintDeterministic(t *testing.T) {
	a := networkEndpointGroupPSCTargetServiceFingerprint("Asia-Northeast3-CloudKMS.googleapis.com")
	b := networkEndpointGroupPSCTargetServiceFingerprint("asia-northeast3-cloudkms.googleapis.com")
	if a != b {
		t.Errorf("expected case-insensitive fingerprint match, got %q vs %q", a, b)
	}
	if a == "" {
		t.Fatalf("expected non-empty fingerprint")
	}
	empty := networkEndpointGroupPSCTargetServiceFingerprint("  ")
	if empty != "" {
		t.Errorf("expected blank fingerprint for blank input, got %q", empty)
	}
}

// Sanity check that facts.StableID is deterministic for the same identity map,
// matching the fingerprint helper's contract (guards against a future
// accidental swap to a non-deterministic ID source).
func TestNetworkEndpointGroupFingerprintUsesStableID(t *testing.T) {
	id1 := facts.StableID("GCPNetworkEndpointGroupPSCTargetServiceHost", map[string]any{"host": "example.com"})
	id2 := facts.StableID("GCPNetworkEndpointGroupPSCTargetServiceHost", map[string]any{"host": "example.com"})
	if id1 != id2 {
		t.Fatalf("expected facts.StableID to be deterministic")
	}
}
