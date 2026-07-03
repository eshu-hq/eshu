// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpcloud

import (
	"encoding/json"
	"testing"
)

const urlMapFullName = "//compute.googleapis.com/projects/demo-project/global/urlMaps/web-map"

func urlMapContext(data string) ExtractContext {
	return ExtractContext{
		FullResourceName: urlMapFullName,
		AssetType:        assetTypeComputeUrlMap,
		ProjectID:        "demo-project",
		Data:             json.RawMessage(data),
	}
}

func TestURLMapExtractorIsRegistered(t *testing.T) {
	if _, ok := lookupAssetExtractor(assetTypeComputeUrlMap); !ok {
		t.Fatalf("expected %q extractor to self-register via init()", assetTypeComputeUrlMap)
	}
}

func TestExtractURLMapDefaultServiceOnly(t *testing.T) {
	const data = `{
		"name": "web-map",
		"defaultService": "https://www.googleapis.com/compute/v1/projects/demo-project/global/backendServices/default-backend",
		"creationTimestamp": "2024-06-01T00:00:00.000-07:00"
	}`

	got, err := extractURLMap(urlMapContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantAttrs := map[string]any{
		"creation_time": "2024-06-01T07:00:00Z",
	}
	if len(got.Attributes) != len(wantAttrs) || got.Attributes["creation_time"] != wantAttrs["creation_time"] {
		t.Fatalf("attributes mismatch: got %#v, want %#v", got.Attributes, wantAttrs)
	}

	wantBackend := "//compute.googleapis.com/projects/demo-project/global/backendServices/default-backend"
	assertRelationship(t, got.Relationships, relationshipTypeURLMapDefaultService, wantBackend, assetTypeComputeBackendService)
	if !containsStringSlice(got.CorrelationAnchors, wantBackend) {
		t.Errorf("expected correlation anchor for default service, got %#v", got.CorrelationAnchors)
	}
	if len(got.Relationships) != 1 {
		t.Fatalf("expected exactly 1 relationship, got %d: %#v", len(got.Relationships), got.Relationships)
	}
}

func TestExtractURLMapDefaultServiceBackendBucket(t *testing.T) {
	const data = `{
		"name": "cdn-map",
		"defaultService": "https://www.googleapis.com/compute/v1/projects/demo-project/global/backendBuckets/static-assets"
	}`

	got, err := extractURLMap(urlMapContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wantBucket := "//compute.googleapis.com/projects/demo-project/global/backendBuckets/static-assets"
	assertRelationship(t, got.Relationships, relationshipTypeURLMapDefaultService, wantBucket, assetTypeComputeBackendBucket)
}

func TestExtractURLMapHostRulesAndPathMatchers(t *testing.T) {
	const data = `{
		"name": "web-map",
		"defaultService": "projects/demo-project/global/backendServices/default-backend",
		"hostRules": [
			{"hosts": ["example.com", "www.example.com"], "pathMatcher": "matcher-1"},
			{"hosts": ["api.example.com"], "pathMatcher": "matcher-2"}
		],
		"pathMatchers": [
			{
				"name": "matcher-1",
				"defaultService": "projects/demo-project/global/backendServices/web-backend",
				"pathRules": [
					{"paths": ["/images/*"], "service": "projects/demo-project/global/backendServices/images-backend"},
					{"paths": ["/videos/*"], "service": "projects/demo-project/global/backendServices/videos-backend"}
				]
			},
			{
				"name": "matcher-2",
				"defaultService": "projects/demo-project/global/backendServices/api-backend",
				"pathRules": []
			}
		]
	}`

	got, err := extractURLMap(urlMapContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["host_rule_count"] != 2 {
		t.Errorf("host_rule_count = %v, want 2", got.Attributes["host_rule_count"])
	}
	if got.Attributes["path_matcher_count"] != 2 {
		t.Errorf("path_matcher_count = %v, want 2", got.Attributes["path_matcher_count"])
	}
	if got.Attributes["path_rule_count"] != 2 {
		t.Errorf("path_rule_count = %v, want 2", got.Attributes["path_rule_count"])
	}

	// default service + 2 pathMatcher defaultServices + 2 pathRule services = 5.
	if len(got.Relationships) != 5 {
		t.Fatalf("expected 5 relationships, got %d: %#v", len(got.Relationships), got.Relationships)
	}

	wantDefault := "//compute.googleapis.com/projects/demo-project/global/backendServices/default-backend"
	wantWeb := "//compute.googleapis.com/projects/demo-project/global/backendServices/web-backend"
	wantAPI := "//compute.googleapis.com/projects/demo-project/global/backendServices/api-backend"
	wantImages := "//compute.googleapis.com/projects/demo-project/global/backendServices/images-backend"
	wantVideos := "//compute.googleapis.com/projects/demo-project/global/backendServices/videos-backend"

	assertRelationship(t, got.Relationships, relationshipTypeURLMapDefaultService, wantDefault, assetTypeComputeBackendService)
	assertRelationship(t, got.Relationships, relationshipTypeURLMapPathMatcherService, wantWeb, assetTypeComputeBackendService)
	assertRelationship(t, got.Relationships, relationshipTypeURLMapPathMatcherService, wantAPI, assetTypeComputeBackendService)
	assertRelationship(t, got.Relationships, relationshipTypeURLMapPathRuleService, wantImages, assetTypeComputeBackendService)
	assertRelationship(t, got.Relationships, relationshipTypeURLMapPathRuleService, wantVideos, assetTypeComputeBackendService)

	// Raw host and path patterns must never leave the parser.
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, token := range []string{"example.com", "api.example.com", "/images/*", "/videos/*"} {
		if containsString(string(blob), token) {
			t.Fatalf("url map extraction leaked routing pattern %q: %s", token, blob)
		}
	}
}

func TestExtractURLMapNeverPersistsRawHostOrPathPatterns(t *testing.T) {
	const data = `{
		"name": "web-map",
		"hostRules": [{"hosts": ["secret-internal-host.example.com"], "pathMatcher": "m1"}],
		"pathMatchers": [
			{
				"name": "m1",
				"pathRules": [{"paths": ["/secret/admin/*"], "service": "projects/demo-project/global/backendServices/admin-backend"}]
			}
		]
	}`

	got, err := extractURLMap(urlMapContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, token := range []string{"secret-internal-host.example.com", "/secret/admin/*"} {
		if containsString(string(blob), token) {
			t.Fatalf("url map extraction leaked raw routing value %q: %s", token, blob)
		}
	}
}

func TestExtractURLMapPartialDataOmitsZeroValues(t *testing.T) {
	got, err := extractURLMap(urlMapContext(`{}`))
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

func TestExtractURLMapMalformedDataErrors(t *testing.T) {
	if _, err := extractURLMap(urlMapContext(`{not json`)); err == nil {
		t.Fatalf("expected an error for malformed resource data")
	}
}

func TestExtractURLMapUnresolvableBackendReferenceEmitsNoEdge(t *testing.T) {
	const data = `{
		"name": "web-map",
		"defaultService": "projects/demo-project/global/somethingElse/x"
	}`

	got, err := extractURLMap(urlMapContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for unresolvable backend reference, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors for unresolvable backend reference, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractURLMapRouteRulesDirectAndWeightedServices(t *testing.T) {
	const data = `{
		"name": "web-map",
		"pathMatchers": [
			{
				"name": "matcher-1",
				"routeRules": [
					{
						"service": "projects/demo-project/global/backendServices/direct-backend",
						"matchRules": [{"prefixMatch": "/secret/admin/*"}]
					},
					{
						"routeAction": {
							"weightedBackendServices": [
								{"backendService": "projects/demo-project/global/backendServices/canary-backend", "weight": 10},
								{"backendService": "projects/demo-project/global/backendServices/stable-backend", "weight": 90}
							]
						}
					}
				]
			}
		]
	}`

	got, err := extractURLMap(urlMapContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if got.Attributes["route_rule_count"] != 2 {
		t.Errorf("route_rule_count = %v, want 2", got.Attributes["route_rule_count"])
	}
	// pathRules/pathMatcher default service are absent here, so the only
	// relationships are: 1 direct route-rule service + 2 weighted-backend
	// services = 3.
	if len(got.Relationships) != 3 {
		t.Fatalf("expected 3 relationships, got %d: %#v", len(got.Relationships), got.Relationships)
	}

	wantDirect := "//compute.googleapis.com/projects/demo-project/global/backendServices/direct-backend"
	wantCanary := "//compute.googleapis.com/projects/demo-project/global/backendServices/canary-backend"
	wantStable := "//compute.googleapis.com/projects/demo-project/global/backendServices/stable-backend"

	assertRelationship(t, got.Relationships, relationshipTypeURLMapRouteRuleService, wantDirect, assetTypeComputeBackendService)
	assertRelationship(t, got.Relationships, relationshipTypeURLMapRouteRuleWeightedService, wantCanary, assetTypeComputeBackendService)
	assertRelationship(t, got.Relationships, relationshipTypeURLMapRouteRuleWeightedService, wantStable, assetTypeComputeBackendService)

	for _, want := range []string{wantDirect, wantCanary, wantStable} {
		if !containsStringSlice(got.CorrelationAnchors, want) {
			t.Errorf("expected correlation anchor %q, got %#v", want, got.CorrelationAnchors)
		}
	}

	// matchRules conditions and traffic-split weight are routing logic that
	// must never leave the parser.
	blob, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("marshal extraction: %v", err)
	}
	for _, token := range []string{"/secret/admin/*", "prefixMatch", "\"weight\"", ":10", ":90"} {
		if containsString(string(blob), token) {
			t.Fatalf("url map extraction leaked routing pattern %q: %s", token, blob)
		}
	}
}

func TestExtractURLMapRouteRuleUnresolvableBackendReferenceEmitsNoEdge(t *testing.T) {
	const data = `{
		"name": "web-map",
		"pathMatchers": [
			{
				"name": "matcher-1",
				"routeRules": [
					{
						"service": "projects/demo-project/global/somethingElse/x",
						"routeAction": {
							"weightedBackendServices": [
								{"backendService": "projects/demo-project/global/somethingElse/y"}
							]
						}
					}
				]
			}
		]
	}`

	got, err := extractURLMap(urlMapContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Attributes["route_rule_count"] != 1 {
		t.Errorf("route_rule_count = %v, want 1", got.Attributes["route_rule_count"])
	}
	if len(got.Relationships) != 0 {
		t.Errorf("expected no relationships for unresolvable route-rule backend references, got %#v", got.Relationships)
	}
	if len(got.CorrelationAnchors) != 0 {
		t.Errorf("expected no anchors for unresolvable route-rule backend references, got %#v", got.CorrelationAnchors)
	}
}

func TestExtractURLMapDedupesRepeatedBackendReference(t *testing.T) {
	// The same BackendService is referenced as both a pathMatcher's
	// defaultService and one of its pathRules[].service; the extractor must
	// emit exactly one edge per distinct (relationship_type, target) pair,
	// not a duplicate relationship fact per repetition.
	const data = `{
		"pathMatchers": [
			{
				"defaultService": "projects/p/global/backendServices/shared-backend",
				"pathRules": [
					{"service": "projects/p/global/backendServices/shared-backend"},
					{"service": "projects/p/global/backendServices/shared-backend"}
				]
			}
		]
	}`
	got, err := extractURLMap(urlMapContext(data))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got.Relationships) != 2 {
		t.Fatalf("expected 2 deduped edges (default-service + path-rule-service, each pointing at the same backend once), got %d: %#v", len(got.Relationships), got.Relationships)
	}
	if len(got.CorrelationAnchors) != 1 {
		t.Errorf("expected 1 deduped anchor, got %#v", got.CorrelationAnchors)
	}
}

func TestURLMapBackendEdgeDispatch(t *testing.T) {
	cases := []struct {
		name          string
		ref           string
		wantName      string
		wantAssetType string
	}{
		{"backend service full selflink", "https://www.googleapis.com/compute/v1/projects/p/global/backendServices/bs", "//compute.googleapis.com/projects/p/global/backendServices/bs", assetTypeComputeBackendService},
		{"backend bucket full selflink", "https://www.googleapis.com/compute/v1/projects/p/global/backendBuckets/bb", "//compute.googleapis.com/projects/p/global/backendBuckets/bb", assetTypeComputeBackendBucket},
		{"project-qualified partial", "projects/p/global/backendServices/bs", "//compute.googleapis.com/projects/p/global/backendServices/bs", assetTypeComputeBackendService},
		{"project-less partial resolved against source project", "global/backendServices/bs", "//compute.googleapis.com/projects/p/global/backendServices/bs", assetTypeComputeBackendService},
		{"unrecognized segment", "projects/p/global/somethingElse/x", "", ""},
		{"blank", "", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotName, gotAssetType := urlMapBackendEdge(tc.ref, "p")
			if gotName != tc.wantName || gotAssetType != tc.wantAssetType {
				t.Errorf("urlMapBackendEdge(%q) = (%q,%q), want (%q,%q)", tc.ref, gotName, gotAssetType, tc.wantName, tc.wantAssetType)
			}
		})
	}
}
