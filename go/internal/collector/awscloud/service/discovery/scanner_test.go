// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicediscovery

import (
	"context"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

const (
	privateNamespaceID  = "ns-private0000000001"
	privateNamespaceARN = "arn:aws:servicediscovery:us-east-1:123456789012:namespace/ns-private0000000001"
	httpNamespaceID     = "ns-http00000000000001"
	httpNamespaceARN    = "arn:aws:servicediscovery:us-east-1:123456789012:namespace/ns-http00000000000001"
	checkoutServiceID   = "srv-checkout000000001"
	checkoutServiceARN  = "arn:aws:servicediscovery:us-east-1:123456789012:service/srv-checkout000000001"
	hostedZoneID        = "Z1234567890ABC"
)

type fakeClient struct {
	namespaces []Namespace
	err        error
}

func (f fakeClient) ListNamespaceInventory(context.Context) ([]Namespace, error) {
	return f.namespaces, f.err
}

func inventory() []Namespace {
	ttl := int64(60)
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	return []Namespace{
		{
			ID:           privateNamespaceID,
			ARN:          privateNamespaceARN,
			Name:         "apps.local",
			Type:         "DNS_PRIVATE",
			Description:  "internal services",
			ServiceCount: 1,
			HostedZoneID: hostedZoneID,
			CreatedAt:    created,
			Tags:         map[string]string{"Environment": "prod"},
			Services: []Service{{
				ID:               checkoutServiceID,
				ARN:              checkoutServiceARN,
				Name:             "checkout",
				NamespaceID:      privateNamespaceID,
				NamespaceName:    "apps.local",
				Description:      "checkout service",
				InstanceCount:    3,
				DNSRoutingPolicy: "MULTIVALUE",
				DNSRecords:       []DNSRecord{{Type: "A", TTL: &ttl}},
				CreatedAt:        created,
				Tags:             map[string]string{"team": "payments"},
			}},
		},
		{
			ID:           httpNamespaceID,
			ARN:          httpNamespaceARN,
			Name:         "http-apps",
			Type:         "HTTP",
			ServiceCount: 0,
			HTTPName:     "http-apps",
			CreatedAt:    created,
		},
	}
}

func scan(t *testing.T, namespaces []Namespace) []facts.Envelope {
	t.Helper()
	scanner := Scanner{Client: fakeClient{namespaces: namespaces}}
	envelopes, err := scanner.Scan(context.Background(), awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:servicediscovery:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 28, 14, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	return envelopes
}

// TestScanEmitsServiceResourceKeyedForAppMeshJoin is the dangling-edge closer
// gate: the App Mesh virtual-node-to-Cloud-Map-service edge keys its target on
// "namespaceName/serviceName" with target_type aws_cloud_map_service. The Cloud
// Map service resource MUST use the same resource_id and resource_type so the
// edge resolves.
func TestScanEmitsServiceResourceKeyedForAppMeshJoin(t *testing.T) {
	envelopes := scan(t, inventory())

	service := findResource(t, envelopes, awscloud.ResourceTypeCloudMapService)
	if got, want := service.Payload["resource_id"], "apps.local/checkout"; got != want {
		t.Fatalf("service resource_id = %#v, want %q (App Mesh edge target join key)", got, want)
	}
	if got, want := service.Payload["resource_type"], awscloud.ResourceTypeCloudMapService; got != want {
		t.Fatalf("service resource_type = %#v, want %q", got, want)
	}
	// The App Mesh scanner emits its Cloud Map edge with this exact target_type
	// and target_resource_id. Assert they match the resource this scanner emits.
	if awscloud.TargetTypeCloudMapService != awscloud.ResourceTypeCloudMapService {
		t.Fatalf("App Mesh TargetTypeCloudMapService %q != Cloud Map ResourceTypeCloudMapService %q",
			awscloud.TargetTypeCloudMapService, awscloud.ResourceTypeCloudMapService)
	}
}

// TestScanEmitsNamespaceResources confirms both DNS and HTTP namespaces emit a
// namespace resource keyed by the Cloud Map namespace id.
func TestScanEmitsNamespaceResources(t *testing.T) {
	envelopes := scan(t, inventory())

	private := findResourceByID(t, envelopes, awscloud.ResourceTypeCloudMapNamespace, privateNamespaceID)
	assertAttribute(t, private, "namespace_type", "DNS_PRIVATE")
	assertAttribute(t, private, "hosted_zone_id", hostedZoneID)
	assertAttribute(t, private, "service_count", int64(1))
	if got := private.Payload["arn"]; got != privateNamespaceARN {
		t.Fatalf("namespace arn = %#v, want %q", got, privateNamespaceARN)
	}

	http := findResourceByID(t, envelopes, awscloud.ResourceTypeCloudMapNamespace, httpNamespaceID)
	assertAttribute(t, http, "namespace_type", "HTTP")
	assertAttribute(t, http, "http_name", "http-apps")
}

// TestScanRecordsInstanceCountOnly confirms the service resource carries the
// instance count and never an instance attribute map.
func TestScanRecordsInstanceCountOnly(t *testing.T) {
	envelopes := scan(t, inventory())

	service := findResource(t, envelopes, awscloud.ResourceTypeCloudMapService)
	assertAttribute(t, service, "instance_count", int64(3))
	assertAttribute(t, service, "dns_routing_policy", "MULTIVALUE")

	attributes := resourceAttributes(t, service)
	for key := range attributes {
		switch key {
		case "instances", "instance_attributes", "attributes":
			t.Fatalf("service attributes carry forbidden instance attribute key %q", key)
		}
	}
}

// TestScanEmitsServiceInNamespaceEdge confirms the service-to-namespace edge
// keys on the namespace id with a non-empty target_type.
func TestScanEmitsServiceInNamespaceEdge(t *testing.T) {
	envelopes := scan(t, inventory())

	rel := findRelationship(t, envelopes, awscloud.RelationshipCloudMapServiceInNamespace)
	if got, want := rel.Payload["source_resource_id"], "apps.local/checkout"; got != want {
		t.Fatalf("service->namespace source = %#v, want %q", got, want)
	}
	if got := rel.Payload["target_resource_id"]; got != privateNamespaceID {
		t.Fatalf("service->namespace target = %#v, want %q", got, privateNamespaceID)
	}
	if got, _ := rel.Payload["target_type"].(string); got != awscloud.ResourceTypeCloudMapNamespace {
		t.Fatalf("service->namespace target_type = %q, want %q", got, awscloud.ResourceTypeCloudMapNamespace)
	}
}

// TestScanEmitsNamespaceInHostedZoneEdge confirms the namespace-to-Route53
// hosted-zone edge keys on the "/hostedzone/<id>" resource id the route53
// scanner emits, so the edge joins that scanner's hosted-zone resource.
func TestScanEmitsNamespaceInHostedZoneEdge(t *testing.T) {
	envelopes := scan(t, inventory())

	rel := findRelationship(t, envelopes, awscloud.RelationshipCloudMapNamespaceInHostedZone)
	if got := rel.Payload["source_resource_id"]; got != privateNamespaceID {
		t.Fatalf("namespace->hosted-zone source = %#v, want %q", got, privateNamespaceID)
	}
	if got, want := rel.Payload["target_resource_id"], "/hostedzone/"+hostedZoneID; got != want {
		t.Fatalf("namespace->hosted-zone target = %#v, want %q (route53 resource_id format)", got, want)
	}
	if got, _ := rel.Payload["target_type"].(string); got != awscloud.ResourceTypeRoute53HostedZone {
		t.Fatalf("namespace->hosted-zone target_type = %q, want %q", got, awscloud.ResourceTypeRoute53HostedZone)
	}
}

// TestScanHTTPNamespaceEmitsNoHostedZoneEdge confirms an HTTP namespace, which
// has no Route 53 hosted zone, emits no namespace-to-hosted-zone edge.
func TestScanHTTPNamespaceEmitsNoHostedZoneEdge(t *testing.T) {
	envelopes := scan(t, inventory())

	count := 0
	for _, envelope := range envelopes {
		relType, _ := envelope.Payload["relationship_type"].(string)
		if relType != awscloud.RelationshipCloudMapNamespaceInHostedZone {
			continue
		}
		if envelope.Payload["source_resource_id"] == httpNamespaceID {
			count++
		}
	}
	if count != 0 {
		t.Fatalf("HTTP namespace hosted-zone edges = %d, want 0", count)
	}
}

// TestScanSkipsServiceWithUnkeyableIdentity proves a service whose
// "namespaceName/serviceName" join key cannot be formed (blank namespace name
// or blank service name) emits NO Cloud Map service resource. Emitting one
// would let NewResourceEnvelope fall back to the ARN as resource_id, which
// breaks the App Mesh virtual-node-to-Cloud-Map-service edge that targets the
// namespaceName/serviceName identity.
func TestScanSkipsServiceWithUnkeyableIdentity(t *testing.T) {
	namespaces := inventory()
	// Blank the namespace name on the service so serviceResourceID() is empty.
	namespaces[0].Services[0].NamespaceName = ""

	envelopes := scan(t, namespaces)

	for _, envelope := range envelopes {
		if got, _ := envelope.Payload["resource_type"].(string); got == awscloud.ResourceTypeCloudMapService {
			t.Fatalf("emitted Cloud Map service resource with resource_id %#v for unkeyable service; want none",
				envelope.Payload["resource_id"])
		}
	}
}

// TestScanSkipsNamespaceWithBlankID proves a namespace with a blank id emits no
// namespace resource. The namespace resource is keyed by the Cloud Map
// namespace id; without it NewResourceEnvelope would key on the ARN while the
// namespace_id attribute stayed blank, producing an inconsistent fact shape.
func TestScanSkipsNamespaceWithBlankID(t *testing.T) {
	namespaces := inventory()
	namespaces[0].ID = "   "

	envelopes := scan(t, namespaces)

	for _, envelope := range envelopes {
		gotType, _ := envelope.Payload["resource_type"].(string)
		if gotType != awscloud.ResourceTypeCloudMapNamespace {
			continue
		}
		if envelope.Payload["resource_id"] == privateNamespaceID {
			continue
		}
		if envelope.Payload["arn"] == privateNamespaceARN {
			t.Fatalf("emitted namespace resource keyed on ARN fallback (resource_id %#v) for blank-id namespace; want none",
				envelope.Payload["resource_id"])
		}
	}
	// The HTTP namespace (valid id) must still be present.
	findResourceByID(t, envelopes, awscloud.ResourceTypeCloudMapNamespace, httpNamespaceID)
}

// TestScanRejectsServiceKindMismatch confirms a non-servicediscovery service
// kind is rejected rather than silently scanned.
func TestScanRejectsServiceKindMismatch(t *testing.T) {
	scanner := Scanner{Client: fakeClient{}}
	_, err := scanner.Scan(context.Background(), awscloud.Boundary{ServiceKind: "route53"})
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

// TestScanRequiresClient confirms a nil client is a configuration error.
func TestScanRequiresClient(t *testing.T) {
	_, err := Scanner{}.Scan(context.Background(), awscloud.Boundary{})
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required error")
	}
}

func findResource(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if got, _ := envelope.Payload["resource_type"].(string); got == resourceType {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q", resourceType)
	return facts.Envelope{}
}

func findResourceByID(t *testing.T, envelopes []facts.Envelope, resourceType, resourceID string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		gotType, _ := envelope.Payload["resource_type"].(string)
		if gotType == resourceType && envelope.Payload["resource_id"] == resourceID {
			return envelope
		}
	}
	t.Fatalf("missing resource_type %q with resource_id %q", resourceType, resourceID)
	return facts.Envelope{}
}

func findRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			return envelope
		}
	}
	t.Fatalf("missing relationship_type %q", relationshipType)
	return facts.Envelope{}
}

func resourceAttributes(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}

func assertAttribute(t *testing.T, envelope facts.Envelope, key string, want any) {
	t.Helper()
	attributes := resourceAttributes(t, envelope)
	if attributes[key] != want {
		t.Fatalf("attribute %s = %#v, want %#v", key, attributes[key], want)
	}
}
