// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"context"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

func newSourceLaneProvider(t *testing.T) *fixturePageProvider {
	t.Helper()
	page, err := ParseResourceGraphPage(loadFixture(t, "resources_source_lanes.json"))
	if err != nil {
		t.Fatalf("source-lane page: %v", err)
	}
	return &fixturePageProvider{pages: map[string]ResourceGraphPage{"": page}}
}

func TestCollectEmitsDNSAndImageReferencesWhenKeyed(t *testing.T) {
	result, err := NewCollector(newSourceLaneProvider(t), nil, WithRedactionKey(testRedactionKey(t))).
		Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	dnsFacts := factsOfKind(result.Facts, facts.AzureDNSRecordFactKind)
	if len(dnsFacts) != 1 {
		t.Fatalf("emitted %d DNS facts, want 1", len(dnsFacts))
	}
	if result.DNSRecordCount != len(dnsFacts) {
		t.Fatalf("DNSRecordCount = %d, want %d", result.DNSRecordCount, len(dnsFacts))
	}
	dnsPayload := dnsFacts[0].Payload
	if dnsPayload["record_type"] != "CNAME" {
		t.Fatalf("record_type = %#v, want CNAME", dnsPayload["record_type"])
	}
	if dnsPayload["record_name_fingerprint"] == "api" {
		t.Fatal("raw DNS record name leaked")
	}
	targets, ok := dnsPayload["target_fingerprints"].([]string)
	if !ok || len(targets) != 1 {
		t.Fatalf("target_fingerprints = %#v, want one fingerprint", dnsPayload["target_fingerprints"])
	}
	if targets[0] == "app.example.invalid" {
		t.Fatal("raw DNS target leaked")
	}
	if dnsPayload["ttl_seconds"] != int64(300) {
		t.Fatalf("ttl_seconds = %#v, want 300", dnsPayload["ttl_seconds"])
	}
	for _, env := range result.Facts {
		if env.FactKind != facts.AzureCloudResourceFactKind ||
			env.Payload["resource_type"] != "microsoft.network/dnszones/cname" {
			continue
		}
		extension := env.Payload["extension"].(map[string]any)
		data, _ := extension["data"].(map[string]any)
		if _, ok := data["CNAMERecord"]; ok {
			t.Fatal("DNS record object leaked through azure_cloud_resource extension")
		}
		if containsRawString(data, "app.example.invalid") {
			t.Fatal("raw DNS target leaked through azure_cloud_resource extension")
		}
	}

	imageFacts := factsOfKind(result.Facts, facts.AzureImageReferenceFactKind)
	if len(imageFacts) != 2 {
		t.Fatalf("emitted %d image-reference facts, want 2", len(imageFacts))
	}
	if result.ImageReferenceCount != len(imageFacts) {
		t.Fatalf("ImageReferenceCount = %d, want %d", result.ImageReferenceCount, len(imageFacts))
	}
	confidence := map[any]bool{}
	for _, env := range imageFacts {
		if env.Payload["container_name_fingerprint"] == "api" ||
			env.Payload["container_name_fingerprint"] == "worker" {
			t.Fatalf("raw container name leaked: %#v", env.Payload["container_name_fingerprint"])
		}
		confidence[env.Payload["tag_digest_confidence"]] = true
	}
	if !confidence[ImageConfidenceDigest] || !confidence[ImageConfidenceTag] {
		t.Fatalf("image confidence classes = %#v, want digest and tag", confidence)
	}
}

func TestCollectSkipsDNSAndImageReferencesWithoutKey(t *testing.T) {
	result, err := NewCollector(newSourceLaneProvider(t), nil).Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}
	if n := len(factsOfKind(result.Facts, facts.AzureDNSRecordFactKind)); n != 0 {
		t.Fatalf("expected no DNS facts without a redaction key, got %d", n)
	}
	if n := len(factsOfKind(result.Facts, facts.AzureImageReferenceFactKind)); n != 0 {
		t.Fatalf("expected no image-reference facts without a redaction key, got %d", n)
	}
	if result.DNSRecordCount != 0 || result.ImageReferenceCount != 0 {
		t.Fatalf(
			"source-lane counts = dns:%d image:%d, want both zero without key",
			result.DNSRecordCount,
			result.ImageReferenceCount,
		)
	}
	if result.ResourceCount == 0 {
		t.Fatal("resource facts should still emit without source-lane redaction key")
	}
}

func TestCollectSourceLaneEmissionHandlesEmptyUnsupportedMalformedAndDuplicateRows(t *testing.T) {
	result, err := NewCollector(newSourceLaneProvider(t), nil, WithRedactionKey(testRedactionKey(t))).
		Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	if result.ResourceCount != 6 {
		t.Fatalf("ResourceCount = %d, want every fixture row retained as resource evidence", result.ResourceCount)
	}
	if result.DNSRecordCount != 1 {
		t.Fatalf("DNSRecordCount = %d, want only the supported non-empty record", result.DNSRecordCount)
	}
	if result.ImageReferenceCount != 2 {
		t.Fatalf("ImageReferenceCount = %d, want duplicate and unsupported rows skipped", result.ImageReferenceCount)
	}
	imageKeys := stableKeySet(factsOfKind(result.Facts, facts.AzureImageReferenceFactKind))
	if len(imageKeys) != result.ImageReferenceCount {
		t.Fatalf("image stable keys = %d, want %d unique keys", len(imageKeys), result.ImageReferenceCount)
	}
}

func TestCollectSourceLaneEmissionPreservesPartialScopeWarning(t *testing.T) {
	provider := newSourceLaneProvider(t)
	provider.scopeErr = &ScopeAccess{
		Partial:             true,
		HiddenResourceCount: 2,
		Reason:              WarningPermissionHidden,
		Message:             "configured scope was only partially readable",
	}

	result, err := NewCollector(provider, nil, WithRedactionKey(testRedactionKey(t))).
		Collect(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Collect error: %v", err)
	}

	if result.DNSRecordCount != 1 || result.ImageReferenceCount != 2 {
		t.Fatalf(
			"source-lane counts = dns:%d image:%d, want source facts despite partial scope",
			result.DNSRecordCount,
			result.ImageReferenceCount,
		)
	}
	warnings := factsOfKind(result.Facts, facts.AzureCollectionWarningFactKind)
	if len(warnings) != 1 {
		t.Fatalf("warning count = %d, want 1 partial warning", len(warnings))
	}
	if warnings[0].Payload["warning_kind"] != WarningPermissionHidden {
		t.Fatalf("warning_kind = %#v, want permission_hidden", warnings[0].Payload["warning_kind"])
	}
	if !result.Partial {
		t.Fatal("result Partial = false, want true")
	}
}

func containsRawString(value any, needle string) bool {
	switch typed := value.(type) {
	case string:
		return strings.Contains(typed, needle)
	case map[string]any:
		for _, nested := range typed {
			if containsRawString(nested, needle) {
				return true
			}
		}
	case []any:
		for _, nested := range typed {
			if containsRawString(nested, needle) {
				return true
			}
		}
	}
	return false
}
