package cloudfront

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsCloudFrontDistributionMetadataOnlyFactsAndRelationships(t *testing.T) {
	distributionARN := "arn:aws:cloudfront::123456789012:distribution/EDFDVBD632BHDS5"
	certificateARN := "arn:aws:acm:us-east-1:123456789012:certificate/cert-1"
	webACLARN := "arn:aws:wafv2:us-east-1:123456789012:global/webacl/orders/a1b2c3"
	lastModified := time.Date(2026, 5, 14, 12, 0, 0, 0, time.UTC)
	client := fakeClient{distributions: []Distribution{{
		ARN:              distributionARN,
		ID:               "EDFDVBD632BHDS5",
		DomainName:       "d111111abcdef8.cloudfront.net",
		Status:           "Deployed",
		Enabled:          true,
		Comment:          "orders edge",
		HTTPVersion:      "http2and3",
		IPV6Enabled:      true,
		LastModifiedTime: lastModified,
		PriceClass:       "PriceClass_100",
		Staging:          false,
		WebACLID:         webACLARN,
		Aliases:          []string{"orders.example.com"},
		Origins: []Origin{{
			ID:                    "orders-origin",
			DomainName:            "orders.s3.us-east-1.amazonaws.com",
			OriginPath:            "/public",
			OriginAccessControlID: "oac-123",
			CustomHeaderNames:     []string{"X-Origin-Auth"},
		}},
		DefaultCacheBehavior: CacheBehavior{
			TargetOriginID:          "orders-origin",
			ViewerProtocolPolicy:    "redirect-to-https",
			AllowedMethods:          []string{"GET", "HEAD"},
			CachedMethods:           []string{"GET", "HEAD"},
			CachePolicyID:           "cache-policy-1",
			OriginRequestPolicyID:   "origin-request-policy-1",
			ResponseHeadersPolicyID: "response-headers-policy-1",
			Compress:                boolPtr(true),
		},
		CacheBehaviors: []CacheBehavior{{
			PathPattern:             "/api/*",
			TargetOriginID:          "orders-origin",
			ViewerProtocolPolicy:    "https-only",
			AllowedMethods:          []string{"GET", "HEAD", "OPTIONS"},
			CachedMethods:           []string{"GET", "HEAD"},
			CachePolicyID:           "cache-policy-2",
			OriginRequestPolicyID:   "origin-request-policy-2",
			ResponseHeadersPolicyID: "response-headers-policy-2",
			Compress:                boolPtr(false),
		}},
		ViewerCertificate: ViewerCertificate{
			ACMCertificateARN:            certificateARN,
			CloudFrontDefaultCertificate: boolPtr(false),
			IAMCertificateID:             "iam-cert-1",
			MinimumProtocolVersion:       "TLSv1.2_2021",
			SSLSupportMethod:             "sni-only",
		},
		Tags: map[string]string{"Environment": "prod"},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	resource := resourceByType(t, envelopes, awscloud.ResourceTypeCloudFrontDistribution)
	if got, want := resource.Payload["arn"], distributionARN; got != want {
		t.Fatalf("distribution arn = %#v, want %q", got, want)
	}
	if got, want := resource.Payload["name"], "EDFDVBD632BHDS5"; got != want {
		t.Fatalf("distribution name = %#v, want %q", got, want)
	}
	if got, want := resource.Payload["state"], "Deployed"; got != want {
		t.Fatalf("distribution state = %#v, want %q", got, want)
	}
	attributes := attributesOf(t, resource)
	assertAttribute(t, attributes, "id", "EDFDVBD632BHDS5")
	assertAttribute(t, attributes, "domain_name", "d111111abcdef8.cloudfront.net")
	assertAttribute(t, attributes, "status", "Deployed")
	assertAttribute(t, attributes, "enabled", true)
	assertAttribute(t, attributes, "comment", "orders edge")
	assertAttribute(t, attributes, "http_version", "http2and3")
	assertAttribute(t, attributes, "ipv6_enabled", true)
	assertAttribute(t, attributes, "last_modified_time", lastModified)
	assertAttribute(t, attributes, "price_class", "PriceClass_100")
	assertAttribute(t, attributes, "staging", false)
	assertAttribute(t, attributes, "aliases", []string{"orders.example.com"})
	assertAttribute(t, attributes, "web_acl_id", webACLARN)
	assertAttribute(t, attributes, "viewer_certificate", map[string]any{
		"acm_certificate_arn":            certificateARN,
		"cloudfront_default_certificate": false,
		"iam_certificate_id":             "iam-cert-1",
		"minimum_protocol_version":       "TLSv1.2_2021",
		"ssl_support_method":             "sni-only",
	})
	assertAttribute(t, attributes, "origins", []map[string]any{{
		"id":                       "orders-origin",
		"domain_name":              "orders.s3.us-east-1.amazonaws.com",
		"origin_path":              "/public",
		"origin_access_control_id": "oac-123",
		"custom_header_names":      []string{"X-Origin-Auth"},
	}})
	assertAttribute(t, attributes, "default_cache_behavior", map[string]any{
		"target_origin_id":           "orders-origin",
		"viewer_protocol_policy":     "redirect-to-https",
		"allowed_methods":            []string{"GET", "HEAD"},
		"cached_methods":             []string{"GET", "HEAD"},
		"cache_policy_id":            "cache-policy-1",
		"origin_request_policy_id":   "origin-request-policy-1",
		"response_headers_policy_id": "response-headers-policy-1",
		"compress":                   true,
	})
	assertAttribute(t, attributes, "cache_behaviors", []map[string]any{{
		"path_pattern":               "/api/*",
		"target_origin_id":           "orders-origin",
		"viewer_protocol_policy":     "https-only",
		"allowed_methods":            []string{"GET", "HEAD", "OPTIONS"},
		"cached_methods":             []string{"GET", "HEAD"},
		"cache_policy_id":            "cache-policy-2",
		"origin_request_policy_id":   "origin-request-policy-2",
		"response_headers_policy_id": "response-headers-policy-2",
		"compress":                   false,
	}})
	for _, forbidden := range []string{
		"custom_header_values",
		"distribution_config",
		"origin_payload",
		"object_keys",
		"object_contents",
		"policy_document",
		"certificate_body",
		"private_key",
	} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("%s attribute persisted; CloudFront scanner must stay metadata-only", forbidden)
		}
	}

	certificate := relationshipByType(t, envelopes, awscloud.RelationshipCloudFrontDistributionUsesACMCertificate)
	if got, want := certificate.Payload["target_resource_id"], certificateARN; got != want {
		t.Fatalf("certificate target_resource_id = %#v, want %q", got, want)
	}
	if got, want := certificate.Payload["target_arn"], certificateARN; got != want {
		t.Fatalf("certificate target_arn = %#v, want %q", got, want)
	}
	webACL := relationshipByType(t, envelopes, awscloud.RelationshipCloudFrontDistributionUsesWAFWebACL)
	if got, want := webACL.Payload["target_resource_id"], webACLARN; got != want {
		t.Fatalf("waf target_resource_id = %#v, want %q", got, want)
	}
	if got, want := webACL.Payload["target_arn"], webACLARN; got != want {
		t.Fatalf("waf target_arn = %#v, want %q", got, want)
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func TestScannerDoesNotTreatClassicWAFIDAsARN(t *testing.T) {
	client := fakeClient{distributions: []Distribution{{
		ARN:      "arn:aws:cloudfront::123456789012:distribution/EDFDVBD632BHDS5",
		ID:       "EDFDVBD632BHDS5",
		WebACLID: "classic-waf-id",
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	relationship := relationshipByType(t, envelopes, awscloud.RelationshipCloudFrontDistributionUsesWAFWebACL)
	if got, want := relationship.Payload["target_resource_id"], "classic-waf-id"; got != want {
		t.Fatalf("target_resource_id = %#v, want %q", got, want)
	}
	if got := relationship.Payload["target_arn"]; got != "" {
		t.Fatalf("target_arn = %#v, want empty for non-ARN WAF ID", got)
	}
}

func TestScannerOmitsEmptyNestedCloudFrontSelectors(t *testing.T) {
	client := fakeClient{distributions: []Distribution{{
		ARN:      "arn:aws:cloudfront::123456789012:distribution/EDFDVBD632BHDS5",
		ID:       "EDFDVBD632BHDS5",
		Status:   "InProgress",
		Origins:  []Origin{{ID: "origin-1"}},
		Tags:     map[string]string{"Environment": "prod"},
		Aliases:  nil,
		WebACLID: "",
		CacheBehaviors: []CacheBehavior{
			{},
			{PathPattern: "/api/*", TargetOriginID: "origin-1"},
		},
	}}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	resource := resourceByType(t, envelopes, awscloud.ResourceTypeCloudFrontDistribution)
	attributes := attributesOf(t, resource)
	for _, absent := range []string{"default_cache_behavior", "viewer_certificate"} {
		if got, exists := attributes[absent]; exists {
			t.Fatalf("%s = %#v, want omitted for zero-value selector", absent, got)
		}
	}
	assertAttribute(t, attributes, "cache_behaviors", []map[string]any{{
		"path_pattern":     "/api/*",
		"target_origin_id": "origin-1",
	}})
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "aws-global",
		ServiceKind:         awscloud.ServiceCloudFront,
		ScopeID:             "aws:123456789012:aws-global",
		GenerationID:        "aws:123456789012:aws-global:cloudfront:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	distributions []Distribution
}

func (c fakeClient) ListDistributions(context.Context) ([]Distribution, error) {
	return c.distributions, nil
}

func resourceByType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q in %#v", resourceType, envelopes)
	return facts.Envelope{}
}

func relationshipByType(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q in %#v", relationshipType, envelopes)
	return facts.Envelope{}
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, attributes map[string]any, key string, want any) {
	t.Helper()
	got, exists := attributes[key]
	if !exists {
		t.Fatalf("missing attribute %q in %#v", key, attributes)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("attribute %q = %#v, want %#v", key, got, want)
	}
}
