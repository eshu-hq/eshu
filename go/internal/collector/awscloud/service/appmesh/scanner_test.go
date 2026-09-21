// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package appmesh

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

const (
	meshARN           = "arn:aws:appmesh:us-east-1:123456789012:mesh/checkout"
	virtualServiceARN = "arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualService/checkout.apps.local"
	backendServiceARN = "arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualService/payments.apps.local"
	virtualNodeARN    = "arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualNode/checkout-node"
	virtualRouterARN  = "arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualRouter/checkout-router"
	routeARN          = "arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualRouter/checkout-router/route/checkout-route"
	virtualGatewayARN = "arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualGateway/edge"
	gatewayRouteARN   = "arn:aws:appmesh:us-east-1:123456789012:mesh/checkout/virtualGateway/edge/gatewayRoute/edge-route"
	acmCAARN          = "arn:aws:acm-pca:us-east-1:123456789012:certificate-authority/12345678-1234-1234-1234-123456789012"
)

func fullInventory() []Mesh {
	created := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	updated := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	return []Mesh{{
		ARN:              meshARN,
		Name:             "checkout",
		MeshOwner:        "123456789012",
		ResourceOwner:    "123456789012",
		EgressFilterType: "DROP_ALL",
		Status:           "ACTIVE",
		CreatedAt:        created,
		LastUpdatedAt:    updated,
		Tags:             map[string]string{"Environment": "prod"},
		VirtualServices: []VirtualService{{
			ARN:          virtualServiceARN,
			Name:         "checkout.apps.local",
			MeshName:     "checkout",
			ProviderKind: "virtual_router",
			ProviderName: "checkout-router",
			Status:       "ACTIVE",
		}},
		VirtualNodes: []VirtualNode{{
			ARN:                               virtualNodeARN,
			Name:                              "checkout-node",
			MeshName:                          "checkout",
			ServiceDiscoveryKind:              "aws_cloud_map",
			CloudMapNamespaceName:             "apps.local",
			CloudMapServiceName:               "checkout",
			BackendVirtualServiceNames:        []string{"payments.apps.local"},
			ClientTLSCertificateAuthorityARNs: []string{acmCAARN},
			Status:                            "ACTIVE",
		}},
		VirtualRouters: []VirtualRouter{{
			ARN:       virtualRouterARN,
			Name:      "checkout-router",
			MeshName:  "checkout",
			Listeners: []Listener{{Port: 8080, Protocol: "http"}},
			Status:    "ACTIVE",
			Routes: []Route{{
				ARN:               routeARN,
				Name:              "checkout-route",
				MeshName:          "checkout",
				VirtualRouterName: "checkout-router",
				VirtualRouterARN:  virtualRouterARN,
				ProtocolKind:      "http",
				PathPrefix:        "/checkout",
				Method:            "POST",
				HeaderMatches: []HeaderMatch{
					{Name: "x-tenant", MatchType: "exact", Value: "acme"},
					{Name: "Authorization", MatchType: "exact", Value: "Bearer super-secret-token"},
				},
				Status: "ACTIVE",
			}},
		}},
		VirtualGateways: []VirtualGateway{{
			ARN:       virtualGatewayARN,
			Name:      "edge",
			MeshName:  "checkout",
			Listeners: []Listener{{Port: 443, Protocol: "http2"}},
			Status:    "ACTIVE",
			GatewayRoutes: []GatewayRoute{{
				ARN:                      gatewayRouteARN,
				Name:                     "edge-route",
				MeshName:                 "checkout",
				VirtualGatewayName:       "edge",
				VirtualGatewayARN:        virtualGatewayARN,
				ProtocolKind:             "http",
				TargetVirtualServiceName: "checkout.apps.local",
				Status:                   "ACTIVE",
			}},
		}},
	}}
}

func TestScannerEmitsAllResourceKinds(t *testing.T) {
	envelopes := scanOK(t, fullInventory())

	wantResources := map[string]string{
		awscloud.ResourceTypeAppMeshMesh:           meshARN,
		awscloud.ResourceTypeAppMeshVirtualService: virtualServiceARN,
		awscloud.ResourceTypeAppMeshVirtualNode:    virtualNodeARN,
		awscloud.ResourceTypeAppMeshVirtualRouter:  virtualRouterARN,
		awscloud.ResourceTypeAppMeshRoute:          routeARN,
		awscloud.ResourceTypeAppMeshVirtualGateway: virtualGatewayARN,
		awscloud.ResourceTypeAppMeshGatewayRoute:   gatewayRouteARN,
	}
	for resourceType, wantID := range wantResources {
		resource := resourceByType(t, envelopes, resourceType)
		if got := resource.Payload["resource_id"]; got != wantID {
			t.Fatalf("%s resource_id = %#v, want %q", resourceType, got, wantID)
		}
		if got := resource.Payload["arn"]; got != wantID {
			t.Fatalf("%s arn = %#v, want %q", resourceType, got, wantID)
		}
	}
}

func TestScannerEmitsInternalRelationshipsWithMatchingJoinKeys(t *testing.T) {
	envelopes := scanOK(t, fullInventory())

	cases := []struct {
		relationship string
		source       string
		target       string
		targetType   string
	}{
		{awscloud.RelationshipAppMeshVirtualServiceInMesh, virtualServiceARN, meshARN, awscloud.ResourceTypeAppMeshMesh},
		{awscloud.RelationshipAppMeshVirtualNodeBackendVirtualService, virtualNodeARN, backendServiceARN, awscloud.ResourceTypeAppMeshVirtualService},
		{awscloud.RelationshipAppMeshRouteInVirtualRouter, routeARN, virtualRouterARN, awscloud.ResourceTypeAppMeshVirtualRouter},
		{awscloud.RelationshipAppMeshVirtualGatewayInMesh, virtualGatewayARN, meshARN, awscloud.ResourceTypeAppMeshMesh},
	}
	for _, tc := range cases {
		rel := singleRelationship(t, envelopes, tc.relationship)
		if got := rel.Payload["source_resource_id"]; got != tc.source {
			t.Fatalf("%s source = %#v, want %q", tc.relationship, got, tc.source)
		}
		if got := rel.Payload["target_resource_id"]; got != tc.target {
			t.Fatalf("%s target = %#v, want %q", tc.relationship, got, tc.target)
		}
		targetType, _ := rel.Payload["target_type"].(string)
		if targetType == "" {
			t.Fatalf("%s target_type is empty; relationships must set a non-empty target_type", tc.relationship)
		}
		if targetType != tc.targetType {
			t.Fatalf("%s target_type = %q, want %q", tc.relationship, targetType, tc.targetType)
		}
	}
}

func TestScannerEmitsCertificateAuthorityRelationship(t *testing.T) {
	envelopes := scanOK(t, fullInventory())

	rel := singleRelationship(t, envelopes, awscloud.RelationshipAppMeshVirtualNodeTrustsCertificateAuthority)
	if got := rel.Payload["source_resource_id"]; got != virtualNodeARN {
		t.Fatalf("CA trust source = %#v, want %q", got, virtualNodeARN)
	}
	// App Mesh client TLS trust anchors are ACM Private CA (acm-pca)
	// certificate authority ARNs, not public ACM certificate ARNs. The join key
	// must therefore be the acm-pca CA ARN so the edge lands on the (future)
	// ACM Private CA certificate authority resource rather than dangling
	// against the public ACM scanner.
	if !strings.HasPrefix(acmCAARN, "arn:aws:acm-pca:") {
		t.Fatalf("test fixture CA ARN = %q, want an acm-pca certificate-authority ARN", acmCAARN)
	}
	if got := rel.Payload["target_resource_id"]; got != acmCAARN {
		t.Fatalf("CA trust target = %#v, want %q", got, acmCAARN)
	}
	if got := rel.Payload["target_arn"]; got != acmCAARN {
		t.Fatalf("CA trust target_arn = %#v, want %q", got, acmCAARN)
	}
	if got, _ := rel.Payload["target_type"].(string); got != awscloud.ResourceTypeACMPCACertificateAuthority {
		t.Fatalf("CA trust target_type = %q, want %q", got, awscloud.ResourceTypeACMPCACertificateAuthority)
	}
}

func TestScannerEmitsCloudMapServiceDiscoveryRelationship(t *testing.T) {
	envelopes := scanOK(t, fullInventory())

	rel := singleRelationship(t, envelopes, awscloud.RelationshipAppMeshVirtualNodeUsesCloudMapService)
	if got := rel.Payload["source_resource_id"]; got != virtualNodeARN {
		t.Fatalf("cloud map source = %#v, want %q", got, virtualNodeARN)
	}
	if got, _ := rel.Payload["target_type"].(string); got != awscloud.TargetTypeCloudMapService {
		t.Fatalf("cloud map target_type = %q, want %q", got, awscloud.TargetTypeCloudMapService)
	}
	if got := rel.Payload["target_resource_id"]; got != "apps.local/checkout" {
		t.Fatalf("cloud map target = %#v, want %q", got, "apps.local/checkout")
	}
}

func TestScannerEmitsDNSServiceDiscoveryRelationship(t *testing.T) {
	inventory := fullInventory()
	inventory[0].VirtualNodes[0].ServiceDiscoveryKind = "dns"
	inventory[0].VirtualNodes[0].CloudMapNamespaceName = ""
	inventory[0].VirtualNodes[0].CloudMapServiceName = ""
	inventory[0].VirtualNodes[0].DNSHostname = "checkout.apps.local"

	envelopes := scanOK(t, inventory)

	rel := singleRelationship(t, envelopes, awscloud.RelationshipAppMeshVirtualNodeUsesDNSHostname)
	if got, _ := rel.Payload["target_type"].(string); got != awscloud.TargetTypeDNSHostname {
		t.Fatalf("dns target_type = %q, want %q", got, awscloud.TargetTypeDNSHostname)
	}
	if got := rel.Payload["target_resource_id"]; got != "checkout.apps.local" {
		t.Fatalf("dns target = %#v, want %q", got, "checkout.apps.local")
	}
	// A DNS node must not also emit a Cloud Map relationship.
	if rels := relationshipsByType(envelopes, awscloud.RelationshipAppMeshVirtualNodeUsesCloudMapService); len(rels) != 0 {
		t.Fatalf("cloud map relationship emitted for DNS node: %d", len(rels))
	}
}

func TestScannerRedactsSensitiveHeaderMatchValuesButKeepsRouteShape(t *testing.T) {
	envelopes := scanOK(t, fullInventory())

	route := resourceByType(t, envelopes, awscloud.ResourceTypeAppMeshRoute)
	attributes := attributesOf(t, route)
	if got := attributes["path_prefix"]; got != "/checkout" {
		t.Fatalf("path_prefix = %#v, want %q", got, "/checkout")
	}
	if got := attributes["method"]; got != "POST" {
		t.Fatalf("method = %#v, want %q", got, "POST")
	}
	headers, ok := attributes["header_matches"].([]map[string]any)
	if !ok {
		t.Fatalf("header_matches = %#v, want []map[string]any", attributes["header_matches"])
	}
	if len(headers) != 2 {
		t.Fatalf("header_matches len = %d, want 2", len(headers))
	}
	byName := map[string]map[string]any{}
	for _, header := range headers {
		name, _ := header["name"].(string)
		byName[name] = header
	}

	nonSensitive := byName["x-tenant"]
	if nonSensitive == nil {
		t.Fatalf("missing x-tenant header match")
	}
	if got := nonSensitive["value"]; got != "acme" {
		t.Fatalf("non-sensitive header value = %#v, want %q (must be preserved)", got, "acme")
	}

	sensitive := byName["Authorization"]
	if sensitive == nil {
		t.Fatalf("missing Authorization header match; the header NAME must always be emitted")
	}
	value := sensitive["value"]
	if value == "Bearer super-secret-token" {
		t.Fatalf("Authorization header value persisted verbatim; sensitive header values must be redacted")
	}
	marker, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("Authorization header value = %#v, want redaction marker map", value)
	}
	if got, _ := marker["marker"].(string); !strings.HasPrefix(got, "redacted:") {
		t.Fatalf("Authorization marker = %#v, want redacted marker", marker["marker"])
	}
	// The header name and match type are routing shape and must survive.
	if got := sensitive["name"]; got != "Authorization" {
		t.Fatalf("Authorization name = %#v, want preserved", got)
	}
	if got := sensitive["match_type"]; got != "exact" {
		t.Fatalf("Authorization match_type = %#v, want %q", got, "exact")
	}
}

func TestScannerNeverPersistsCertificateBodyOnVirtualNode(t *testing.T) {
	envelopes := scanOK(t, fullInventory())

	node := resourceByType(t, envelopes, awscloud.ResourceTypeAppMeshVirtualNode)
	attributes := attributesOf(t, node)
	for _, forbidden := range []string{"certificate", "certificate_body", "certificate_chain", "private_key", "tls_certificate"} {
		if _, exists := attributes[forbidden]; exists {
			t.Fatalf("attribute %q persisted; App Mesh scanner must never store client TLS certificate material", forbidden)
		}
	}
	// The safe ACM reference must be present as ARNs only.
	cas, ok := attributes["client_tls_certificate_authority_arns"].([]string)
	if !ok || len(cas) != 1 || cas[0] != acmCAARN {
		t.Fatalf("client_tls_certificate_authority_arns = %#v, want [%q]", attributes["client_tls_certificate_authority_arns"], acmCAARN)
	}
}

func TestScannerBackendRelationshipUsesPartitionFromParentARN(t *testing.T) {
	inventory := fullInventory()
	govMesh := "arn:aws-us-gov:appmesh:us-gov-west-1:123456789012:mesh/checkout"
	govNode := "arn:aws-us-gov:appmesh:us-gov-west-1:123456789012:mesh/checkout/virtualNode/checkout-node"
	govBackend := "arn:aws-us-gov:appmesh:us-gov-west-1:123456789012:mesh/checkout/virtualService/payments.apps.local"
	inventory[0].ARN = govMesh
	inventory[0].VirtualNodes[0].ARN = govNode
	inventory[0].VirtualNodes[0].ClientTLSCertificateAuthorityARNs = nil

	envelopes := scanOK(t, inventory)
	rel := singleRelationship(t, envelopes, awscloud.RelationshipAppMeshVirtualNodeBackendVirtualService)
	if got := rel.Payload["target_resource_id"]; got != govBackend {
		t.Fatalf("backend target = %#v, want %q (partition must come from the node ARN, not hardcoded aws)", got, govBackend)
	}
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceECR
	_, err := scanner(t).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func TestScannerRequiresClient(t *testing.T) {
	_, err := Scanner{RedactionKey: testRedactionKey(t)}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want client-required rejection")
	}
}

func TestScannerRequiresRedactionKey(t *testing.T) {
	_, err := Scanner{Client: fakeClient{}}.Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want redaction-key-required rejection")
	}
}

func TestScannerSurfacesClientError(t *testing.T) {
	_, err := (Scanner{Client: fakeClient{err: errBoom}, RedactionKey: testRedactionKey(t)}).Scan(context.Background(), testBoundary())
	if err == nil {
		t.Fatalf("Scan() error = nil, want wrapped client error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Fatalf("Scan() error = %v, want wrapped boom", err)
	}
}

// helpers

func scanner(t *testing.T) Scanner {
	t.Helper()
	return Scanner{Client: fakeClient{}, RedactionKey: testRedactionKey(t)}
}

func scanOK(t *testing.T, inventory []Mesh) []facts.Envelope {
	t.Helper()
	envelopes, err := (Scanner{Client: fakeClient{meshes: inventory}, RedactionKey: testRedactionKey(t)}).
		Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}
	return envelopes
}

func testRedactionKey(t *testing.T) redact.Key {
	t.Helper()
	key, err := redact.NewKey([]byte("appmesh-redaction-key"))
	if err != nil {
		t.Fatalf("NewKey() error = %v", err)
	}
	return key
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceAppMesh,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:appmesh:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 28, 14, 30, 0, 0, time.UTC),
	}
}

var errBoom = errBoomError{}

type errBoomError struct{}

func (errBoomError) Error() string { return "boom" }

type fakeClient struct {
	meshes []Mesh
	err    error
}

func (c fakeClient) ListMeshInventory(context.Context) ([]Mesh, error) {
	if c.err != nil {
		return nil, c.err
	}
	return c.meshes, nil
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
	t.Fatalf("missing resource_type %q", resourceType)
	return facts.Envelope{}
}

func relationshipsByType(envelopes []facts.Envelope, relationshipType string) []facts.Envelope {
	var result []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if got, _ := envelope.Payload["relationship_type"].(string); got == relationshipType {
			result = append(result, envelope)
		}
	}
	return result
}

func singleRelationship(t *testing.T, envelopes []facts.Envelope, relationshipType string) facts.Envelope {
	t.Helper()
	rels := relationshipsByType(envelopes, relationshipType)
	if len(rels) != 1 {
		t.Fatalf("relationship %q count = %d, want 1", relationshipType, len(rels))
	}
	return rels[0]
}

func attributesOf(t *testing.T, envelope facts.Envelope) map[string]any {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	return attributes
}
