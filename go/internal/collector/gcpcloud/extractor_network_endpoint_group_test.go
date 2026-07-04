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

	// A Cloud Run serverless NEG names an exactly resolvable Cloud Run service:
	// the service is a control-plane resource in the NEG's own project and
	// region, so it resolves to a run.googleapis.com/Service CAI name and emits
	// a typed edge, mirroring the Eventarc Trigger extractor's Cloud Run edge.
	wantService := "//run.googleapis.com/projects/demo-project/locations/us-central1/services/order-service"
	assertRelationship(t, got.Relationships, relationshipTypeNetworkEndpointGroupTargetsServerlessService, wantService, assetTypeRunService)
	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly 1 relationship (cloud run service), got %#v", got.Relationships)
	}
	foundService := false
	for _, a := range got.CorrelationAnchors {
		if a == wantService {
			foundService = true
		}
		// The urlMask routing template and the tag are data-plane routing
		// values and must never leave the parser.
		if a == "<tag>.domain.com/<service>" || a == "revision-0010" {
			t.Fatalf("urlMask/tag leaked into correlation anchors: %#v", got.CorrelationAnchors)
		}
	}
	if !foundService {
		t.Errorf("expected cloud run service anchor %q in %#v", wantService, got.CorrelationAnchors)
	}
}

func TestExtractNetworkEndpointGroupServerlessCloudRunURLMaskNoName(t *testing.T) {
	// A URL-mask Cloud Run NEG carries the cloudRun object with no fixed
	// service name; the Compute API parses <service> from the request URL at
	// runtime. The discriminator must still be set from sub-object presence, and
	// no service edge is emitted since there is no fixed service to resolve.
	const data = `{
		"name": "run-mask-neg",
		"networkEndpointType": "SERVERLESS",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"cloudRun": {"urlMask": "<service>.example.com"}
	}`

	got, err := extractNetworkEndpointGroup(networkEndpointGroupContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["serverless_type"] != "cloud_run" {
		t.Errorf("serverless_type = %v, want cloud_run", got.Attributes["serverless_type"])
	}
	if _, ok := got.Attributes["serverless_service"]; ok {
		t.Errorf("did not expect serverless_service for a url-mask NEG: %#v", got.Attributes)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no relationships for a url-mask NEG with no fixed service, got %#v", got.Relationships)
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
	// App Engine and Cloud Function serverless refs are surfaced as attributes
	// only, never edges: the App Engine app id is not derivable exactly from the
	// NEG (the app id need not equal the project id) and a Cloud Function
	// reference carries no gen1/gen2 or region qualifier, so neither resolves to
	// an exact CAI endpoint. Only the Cloud Run edge is emitted.
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no relationships for an app engine NEG, got %#v", got.Relationships)
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
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no relationships for a cloud function NEG, got %#v", got.Relationships)
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
	// A Google-API hostname pscTargetService names no CAI resource, so it is
	// fingerprinted, not turned into an edge. Only the network edge is emitted.
	if len(got.Relationships) != 1 {
		t.Fatalf("expected only the network edge for an API-hostname PSC NEG, got %#v", got.Relationships)
	}
	if _, ok := got.Attributes["psc_target_service_attachment"]; ok {
		t.Errorf("did not expect a service-attachment attribute for a hostname target: %#v", got.Attributes)
	}
}

func TestExtractNetworkEndpointGroupPSCTargetServiceAttachment(t *testing.T) {
	// When pscTargetService is a Producer Service Attachment self-link rather
	// than a Google API hostname, it names a resolvable
	// compute.googleapis.com/ServiceAttachment, so a typed edge is emitted (the
	// same asset type the ForwardingRule extractor already resolves) and the
	// opaque host fingerprint is not.
	const data = `{
		"name": "psc-sa-neg",
		"networkEndpointType": "PRIVATE_SERVICE_CONNECT",
		"region": "https://www.googleapis.com/compute/v1/projects/demo-project/regions/us-central1",
		"pscTargetService": "https://www.googleapis.com/compute/v1/projects/producer-project/regions/us-central1/serviceAttachments/my-psc-sa"
	}`

	got, err := extractNetworkEndpointGroup(networkEndpointGroupContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantSA := "//compute.googleapis.com/projects/producer-project/regions/us-central1/serviceAttachments/my-psc-sa"
	assertRelationship(t, got.Relationships, relationshipTypeNetworkEndpointGroupTargetsServiceAttachment, wantSA, assetTypeComputeServiceAttachment)
	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly 1 relationship (service attachment), got %#v", got.Relationships)
	}
	if got.Attributes["psc_target_service_attachment"] != wantSA {
		t.Errorf("psc_target_service_attachment = %v, want %v", got.Attributes["psc_target_service_attachment"], wantSA)
	}
	// The resolvable self-link is not also fingerprinted as an opaque hostname.
	if _, ok := got.Attributes["psc_target_service_fingerprint"]; ok {
		t.Errorf("did not expect a host fingerprint for a resolvable service-attachment target: %#v", got.Attributes)
	}
	foundSA := false
	for _, a := range got.CorrelationAnchors {
		if a == wantSA {
			foundSA = true
		}
	}
	if !foundSA {
		t.Errorf("expected service-attachment anchor %q in %#v", wantSA, got.CorrelationAnchors)
	}
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
