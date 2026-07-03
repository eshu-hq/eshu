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
