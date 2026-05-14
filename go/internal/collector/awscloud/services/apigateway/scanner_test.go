package apigateway

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsAPIGatewayMetadataOnlyFactsAndRelationships(t *testing.T) {
	restAPIID := "a1b2c3d4"
	v2APIID := "z9y8x7w6"
	lambdaARN := "arn:aws:lambda:us-east-1:123456789012:function:orders"
	certificateARN := "arn:aws:acm:us-east-1:123456789012:certificate/cert-1"
	logGroupARN := "arn:aws:logs:us-east-1:123456789012:log-group:/aws/apigateway/orders"
	observed := time.Date(2026, 5, 14, 19, 0, 0, 0, time.UTC)
	client := fakeClient{snapshot: Snapshot{
		RESTAPIs: []RESTAPI{{
			ID:                        restAPIID,
			Name:                      "orders-rest",
			Description:               "orders REST API",
			CreatedDate:               observed,
			Version:                   "2026-05",
			APIStatus:                 "AVAILABLE",
			APIKeySource:              "HEADER",
			DisableExecuteAPIEndpoint: true,
			EndpointTypes:             []string{"REGIONAL"},
			VPCEndpointIDs:            []string{"vpce-123"},
			Tags:                      map[string]string{"Environment": "prod"},
			Stages: []Stage{{
				APIID:                restAPIID,
				Name:                 "prod",
				DeploymentID:         "dep-1",
				Description:          "production",
				CreatedDate:          observed,
				LastUpdatedDate:      observed.Add(time.Hour),
				CacheClusterEnabled:  true,
				CacheClusterSize:     "0.5",
				CacheClusterStatus:   "AVAILABLE",
				TracingEnabled:       true,
				ClientCertificateID:  "client-cert-1",
				AccessLogDestination: logGroupARN,
				WebACLARN:            "arn:aws:wafv2:us-east-1:123456789012:regional/webacl/orders/a1",
				Tags:                 map[string]string{"Stage": "prod"},
			}},
			Integrations: []Integration{{
				APIKind:        APIKindREST,
				APIID:          restAPIID,
				ResourceID:     "res-1",
				ResourcePath:   "/orders",
				Method:         "POST",
				Type:           "AWS_PROXY",
				URI:            "arn:aws:apigateway:us-east-1:lambda:path/2015-03-31/functions/" + lambdaARN + "/invocations",
				ConnectionType: "INTERNET",
				TimeoutMillis:  29000,
			}},
		}},
		V2APIs: []V2API{{
			ID:                        v2APIID,
			Name:                      "orders-http",
			ProtocolType:              "HTTP",
			Endpoint:                  "https://z9y8x7w6.execute-api.us-east-1.amazonaws.com",
			CreatedDate:               observed,
			Description:               "orders HTTP API",
			DisableExecuteAPIEndpoint: true,
			APIGatewayManaged:         boolPtr(true),
			IPAddressType:             "dualstack",
			Tags:                      map[string]string{"Environment": "prod"},
			Stages: []Stage{{
				APIKind:              APIKindV2,
				APIID:                v2APIID,
				Name:                 "$default",
				DeploymentID:         "dep-v2",
				AutoDeploy:           boolPtr(true),
				APIGatewayManaged:    boolPtr(true),
				AccessLogDestination: logGroupARN,
			}},
			Integrations: []Integration{{
				APIKind:              APIKindV2,
				APIID:                v2APIID,
				IntegrationID:        "int-1",
				Method:               "POST",
				Type:                 "AWS_PROXY",
				URI:                  lambdaARN,
				ConnectionType:       "INTERNET",
				PayloadFormatVersion: "2.0",
				TimeoutMillis:        30000,
				APIGatewayManaged:    boolPtr(false),
			}},
		}},
		Domains: []DomainName{{
			APIKind:           APIKindREST,
			Name:              "api.example.com",
			ARN:               "arn:aws:apigateway:us-east-1::/domainnames/api.example.com",
			Status:            "AVAILABLE",
			EndpointTypes:     []string{"REGIONAL"},
			RegionalDomain:    "d-abc.execute-api.us-east-1.amazonaws.com",
			RegionalZoneID:    "Z1UJRXOUMOOFQ8",
			CertificateARNs:   []string{certificateARN},
			SecurityPolicy:    "TLS_1_2",
			ManagementPolicy:  "should-not-persist",
			ExecuteAPIPolicy:  "should-not-persist",
			MutualTLSTrustURI: "s3://private/truststore.pem",
			Tags:              map[string]string{"Domain": "orders"},
			Mappings: []Mapping{{
				APIKind: APIKindREST,
				Domain:  "api.example.com",
				Key:     "(none)",
				APIID:   restAPIID,
				Stage:   "prod",
			}},
		}, {
			APIKind:          APIKindV2,
			Name:             "http.example.com",
			ARN:              "arn:aws:apigateway:us-east-1::/domainnames/http.example.com",
			Status:           "AVAILABLE",
			EndpointTypes:    []string{"REGIONAL"},
			CertificateARNs:  []string{certificateARN},
			SecurityPolicy:   "TLS_1_2",
			APIMappingSelect: "$request.basepath",
			Mappings: []Mapping{{
				APIKind: APIKindV2,
				Domain:  "http.example.com",
				ID:      "map-1",
				Key:     "orders",
				APIID:   v2APIID,
				Stage:   "$default",
			}},
		}},
	}}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan() error = %v, want nil", err)
	}

	rest := resourceByTypeAndID(t, envelopes, awscloud.ResourceTypeAPIGatewayRESTAPI, restAPIID)
	assertAttribute(t, attributesOf(t, rest), "endpoint_types", []string{"REGIONAL"})
	assertAttribute(t, attributesOf(t, rest), "disable_execute_api_endpoint", true)
	v2 := resourceByTypeAndID(t, envelopes, awscloud.ResourceTypeAPIGatewayV2API, v2APIID)
	assertAttribute(t, attributesOf(t, v2), "protocol_type", "HTTP")
	assertAttribute(t, attributesOf(t, v2), "api_gateway_managed", true)
	stage := resourceByTypeAndID(t, envelopes, awscloud.ResourceTypeAPIGatewayStage, restAPIID+"/stages/prod")
	assertAttribute(t, attributesOf(t, stage), "access_log_destination_arn", logGroupARN)
	domain := resourceByTypeAndID(t, envelopes, awscloud.ResourceTypeAPIGatewayDomainName, "api.example.com")
	assertAttribute(t, attributesOf(t, domain), "mappings", []map[string]any{{
		"api_kind": "rest",
		"api_id":   restAPIID,
		"key":      "(none)",
		"stage":    "prod",
	}})

	for _, resource := range resourceEnvelopes(envelopes) {
		for _, forbidden := range []string{
			"api_key",
			"api_key_value",
			"authorizer_secret",
			"credentials",
			"credentials_arn",
			"execute_api_policy",
			"integration_credentials",
			"management_policy",
			"policy",
			"request_templates",
			"response_templates",
			"stage_variables",
			"truststore_uri",
		} {
			if _, exists := attributesOf(t, resource)[forbidden]; exists {
				t.Fatalf("%s attribute persisted in %#v; API Gateway scanner must stay metadata-only", forbidden, resource.Payload)
			}
		}
	}

	assertRelationshipTarget(t, envelopes, awscloud.RelationshipAPIGatewayAPIHasStage, restAPIID+"/stages/prod")
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipAPIGatewayDomainMapsToAPI, restAPIID)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipAPIGatewayDomainUsesACMCertificate, certificateARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipAPIGatewayStageLogsToResource, logGroupARN)
	assertRelationshipTarget(t, envelopes, awscloud.RelationshipAPIGatewayAPIIntegratesWithResource, lambdaARN)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceS3

	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func boolPtr(value bool) *bool {
	return &value
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "us-east-1",
		ServiceKind:         awscloud.ServiceAPIGateway,
		ScopeID:             "aws:123456789012:us-east-1",
		GenerationID:        "aws:123456789012:us-east-1:apigateway:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 14, 18, 30, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	snapshot Snapshot
}

func (c fakeClient) Snapshot(context.Context) (Snapshot, error) {
	return c.snapshot, nil
}

func resourceByTypeAndID(
	t *testing.T,
	envelopes []facts.Envelope,
	resourceType string,
	resourceID string,
) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSResourceFactKind {
			continue
		}
		if envelope.Payload["resource_type"] == resourceType && envelope.Payload["resource_id"] == resourceID {
			return envelope
		}
	}
	t.Fatalf("missing %s resource_id %q in %#v", resourceType, resourceID, envelopes)
	return facts.Envelope{}
}

func resourceEnvelopes(envelopes []facts.Envelope) []facts.Envelope {
	var resources []facts.Envelope
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSResourceFactKind {
			resources = append(resources, envelope)
		}
	}
	return resources
}

func assertRelationshipTarget(
	t *testing.T,
	envelopes []facts.Envelope,
	relationshipType string,
	targetResourceID string,
) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSRelationshipFactKind {
			continue
		}
		if envelope.Payload["relationship_type"] == relationshipType &&
			envelope.Payload["target_resource_id"] == targetResourceID {
			return
		}
	}
	t.Fatalf("missing relationship %q to %q in %#v", relationshipType, targetResourceID, envelopes)
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
