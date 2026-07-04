// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"reflect"
	"testing"
)

const apiGatewayFullName = "//apigateway.googleapis.com/projects/demo-project/locations/us-central1/gateways/prod-gw"

func apiGatewayContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: apiGatewayFullName,
		AssetType:        assetTypeAPIGatewayGateway,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestAPIGatewayExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeAPIGatewayGateway); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeAPIGatewayGateway)
	}
}

// TestExtractAPIGatewayRealCAIShape uses the real API Gateway v1 Gateway
// resource shape (name, createTime, updateTime, labels, displayName,
// apiConfig, state, defaultHostname) as documented at
// https://cloud.google.com/api-gateway/docs/reference/rest/v1/projects.locations.gateways.
// It proves the apiConfig edge, the region derived from the resource name, the
// defaultHostname fingerprint (never the raw hostname), and the display
// name/state/timestamps attributes.
func TestExtractAPIGatewayRealCAIShape(t *testing.T) {
	const data = `{
		"name": "projects/demo-project/locations/us-central1/gateways/prod-gw",
		"createTime": "2026-01-15T10:00:00Z",
		"updateTime": "2026-02-01T12:30:00Z",
		"labels": {"env": "prod"},
		"displayName": "Prod Gateway",
		"apiConfig": "projects/demo-project/locations/global/apis/prod-api/configs/prod-cfg",
		"state": "ACTIVE",
		"defaultHostname": "prod-gw-abc123.uc.gateway.dev"
	}`

	got, err := extractAPIGateway(apiGatewayContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantHostFP := pubSubPushEndpointHostFingerprint("prod-gw-abc123.uc.gateway.dev")
	wantAttrs := map[string]any{
		"display_name":                 "Prod Gateway",
		"state":                        "ACTIVE",
		"region":                       "us-central1",
		"creation_time":                "2026-01-15T10:00:00Z",
		"update_time":                  "2026-02-01T12:30:00Z",
		"default_hostname_fingerprint": wantHostFP,
	}
	if !reflect.DeepEqual(got.Attributes, wantAttrs) {
		t.Fatalf("attributes mismatch:\n got %#v\nwant %#v", got.Attributes, wantAttrs)
	}

	if len(got.Relationships) != 1 {
		t.Fatalf("expected 1 edge (api config), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	assertRelationship(t, got.Relationships, relationshipTypeAPIGatewayUsesAPIConfig,
		"//apigateway.googleapis.com/projects/demo-project/locations/global/apis/prod-api/configs/prod-cfg", assetTypeAPIGatewayAPIConfig)

	wantAnchors := []string{
		"//apigateway.googleapis.com/projects/demo-project/locations/global/apis/prod-api/configs/prod-cfg",
	}
	if !reflect.DeepEqual(got.CorrelationAnchors, wantAnchors) {
		t.Fatalf("anchors mismatch:\n got %#v\nwant %#v", got.CorrelationAnchors, wantAnchors)
	}
}

// TestExtractAPIGatewayAlreadyCAIPrefixedAPIConfigNotDoublePrefixed proves an
// apiConfig reference that already carries the CAI "//" prefix (defensive;
// live API Gateway responses report the bare relative form) is not
// double-prefixed.
func TestExtractAPIGatewayAlreadyCAIPrefixedAPIConfigNotDoublePrefixed(t *testing.T) {
	const data = `{
		"apiConfig": "//apigateway.googleapis.com/projects/demo-project/locations/global/apis/prod-api/configs/prod-cfg"
	}`

	got, err := extractAPIGateway(apiGatewayContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertRelationship(t, got.Relationships, relationshipTypeAPIGatewayUsesAPIConfig,
		"//apigateway.googleapis.com/projects/demo-project/locations/global/apis/prod-api/configs/prod-cfg", assetTypeAPIGatewayAPIConfig)
}

// TestExtractAPIGatewayNoAPIConfigNoEdge proves a Gateway resource with a
// blank apiConfig field yields no edge or anchor.
func TestExtractAPIGatewayNoAPIConfigNoEdge(t *testing.T) {
	const data = `{
		"state": "CREATING"
	}`

	got, err := extractAPIGateway(apiGatewayContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Fatalf("expected no edges without apiConfig, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Fatalf("expected no anchors without apiConfig, got %#v", got.CorrelationAnchors)
	}
	if got.Attributes["state"] != "CREATING" {
		t.Errorf("state = %v, want CREATING", got.Attributes["state"])
	}
}

// TestExtractAPIGatewayNoDefaultHostnameNoFingerprint proves a blank
// defaultHostname yields no fingerprint attribute rather than a fingerprint of
// an empty string.
func TestExtractAPIGatewayNoDefaultHostnameNoFingerprint(t *testing.T) {
	got, err := extractAPIGateway(apiGatewayContext(`{"state": "ACTIVE"}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := got.Attributes["default_hostname_fingerprint"]; ok {
		t.Errorf("default_hostname_fingerprint should be absent, got %#v", got.Attributes)
	}
}

// TestExtractAPIGatewayNeverPersistsRawHostnameOrAPIConfigDoubleForm proves the
// raw defaultHostname DNS name never leaves the parser, only its fingerprint.
func TestExtractAPIGatewayNeverPersistsRawHostname(t *testing.T) {
	const data = `{
		"defaultHostname": "leaky-secret-host.uc.gateway.dev"
	}`
	got, err := extractAPIGateway(apiGatewayContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if containsString(string(blob), "leaky-secret-host") {
		t.Fatalf("api gateway extraction leaked raw hostname: %s", blob)
	}
}

func TestExtractAPIGatewayMalformedDataErrors(t *testing.T) {
	if _, err := extractAPIGateway(apiGatewayContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestAPIGatewayRegionFromFullName(t *testing.T) {
	if got := apiGatewayRegionFromFullName(apiGatewayFullName); got != "us-central1" {
		t.Errorf("region = %q, want us-central1", got)
	}
	if got := apiGatewayRegionFromFullName("//apigateway.googleapis.com/projects/p/gateways/g"); got != "" {
		t.Errorf("a name with no /locations/ segment must yield no region, got %q", got)
	}
	if got := apiGatewayRegionFromFullName(""); got != "" {
		t.Errorf("blank name must yield no region, got %q", got)
	}
}
