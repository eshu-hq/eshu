package route53

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestScannerEmitsHostedZonesAndDNSRecords(t *testing.T) {
	ttl := int64(60)
	client := fakeClient{
		hostedZones: []HostedZone{{
			ID:                     "/hostedzone/Z123",
			Name:                   "example.com.",
			CallerReference:        "caller-1",
			Private:                false,
			ResourceRecordSetCount: 4,
			Tags:                   map[string]string{"team": "edge"},
		}},
		records: map[string][]RecordSet{
			"/hostedzone/Z123": {
				{
					Name:   "api.example.com.",
					Type:   "A",
					TTL:    &ttl,
					Values: []string{"192.0.2.10"},
				},
				{
					Name:   "app.example.com.",
					Type:   "CNAME",
					TTL:    &ttl,
					Values: []string{"api.example.com."},
				},
				{
					Name: "www.example.com.",
					Type: "A",
					AliasTarget: &AliasTarget{
						DNSName:              "dualstack.api-123.us-east-1.elb.amazonaws.com.",
						HostedZoneID:         "Z35SXDOTRQ7X7K",
						EvaluateTargetHealth: true,
					},
				},
				{
					Name:   "mail.example.com.",
					Type:   "MX",
					TTL:    &ttl,
					Values: []string{"10 mail.example.com."},
				},
			},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	counts := factKindCounts(envelopes)
	if counts[facts.AWSResourceFactKind] != 1 {
		t.Fatalf("aws_resource count = %d, want 1", counts[facts.AWSResourceFactKind])
	}
	if counts[facts.AWSDNSRecordFactKind] != 3 {
		t.Fatalf("aws_dns_record count = %d, want 3", counts[facts.AWSDNSRecordFactKind])
	}

	hostedZone := assertResourceType(t, envelopes, awscloud.ResourceTypeRoute53HostedZone)
	assertAttribute(t, hostedZone, "private_zone", false)
	assertAttribute(t, hostedZone, "record_set_count", int64(4))
	assertNoDNSRecord(t, envelopes, "mail.example.com.", "MX")

	aliasRecord := assertDNSRecord(t, envelopes, "www.example.com.", "A")
	assertPayloadBool(t, aliasRecord, "hosted_zone_private", false)
	aliasTarget, ok := aliasRecord.Payload["alias_target"].(map[string]any)
	if !ok {
		t.Fatalf("alias_target = %#v, want map", aliasRecord.Payload["alias_target"])
	}
	if got, _ := aliasTarget["hosted_zone_id"].(string); got != "Z35SXDOTRQ7X7K" {
		t.Fatalf("alias hosted_zone_id = %q", got)
	}
	if got, _ := aliasTarget["normalized_dns_name"].(string); got != "dualstack.api-123.us-east-1.elb.amazonaws.com" {
		t.Fatalf("alias normalized_dns_name = %q", got)
	}

	cname := assertDNSRecord(t, envelopes, "app.example.com.", "CNAME")
	values, ok := cname.Payload["values"].([]string)
	if !ok || strings.Join(values, ",") != "api.example.com." {
		t.Fatalf("CNAME values = %#v", cname.Payload["values"])
	}
}

func TestScannerPreservesPrivateZoneEvidence(t *testing.T) {
	client := fakeClient{
		hostedZones: []HostedZone{{
			ID:      "/hostedzone/ZPRIVATE",
			Name:    "svc.local.",
			Private: true,
		}},
		records: map[string][]RecordSet{
			"/hostedzone/ZPRIVATE": {{
				Name: "api.svc.local.",
				Type: "AAAA",
				AliasTarget: &AliasTarget{
					DNSName:      "internal-api.us-east-1.elb.amazonaws.com.",
					HostedZoneID: "Z35SXDOTRQ7X7K",
				},
			}},
		},
	}

	envelopes, err := (Scanner{Client: client}).Scan(context.Background(), testBoundary())
	if err != nil {
		t.Fatalf("Scan returned error: %v", err)
	}

	hostedZone := assertResourceType(t, envelopes, awscloud.ResourceTypeRoute53HostedZone)
	assertAttribute(t, hostedZone, "private_zone", true)
	record := assertDNSRecord(t, envelopes, "api.svc.local.", "AAAA")
	assertPayloadBool(t, record, "hosted_zone_private", true)
}

func TestScannerRejectsMismatchedServiceKind(t *testing.T) {
	boundary := testBoundary()
	boundary.ServiceKind = awscloud.ServiceELBv2
	_, err := (Scanner{Client: fakeClient{}}).Scan(context.Background(), boundary)
	if err == nil {
		t.Fatalf("Scan() error = nil, want service kind mismatch")
	}
}

func testBoundary() awscloud.Boundary {
	return awscloud.Boundary{
		AccountID:           "123456789012",
		Region:              "aws-global",
		ServiceKind:         awscloud.ServiceRoute53,
		ScopeID:             "aws:123456789012:aws-global",
		GenerationID:        "aws:123456789012:aws-global:route53:1",
		CollectorInstanceID: "aws-prod",
		FencingToken:        42,
		ObservedAt:          time.Date(2026, 5, 13, 12, 0, 0, 0, time.UTC),
	}
}

type fakeClient struct {
	hostedZones []HostedZone
	records     map[string][]RecordSet
}

func (c fakeClient) ListHostedZones(context.Context) ([]HostedZone, error) {
	return c.hostedZones, nil
}

func (c fakeClient) ListResourceRecordSets(_ context.Context, hostedZone HostedZone) ([]RecordSet, error) {
	return c.records[hostedZone.ID], nil
}

func factKindCounts(envelopes []facts.Envelope) map[string]int {
	counts := make(map[string]int)
	for _, envelope := range envelopes {
		counts[envelope.FactKind]++
	}
	return counts
}

func assertResourceType(t *testing.T, envelopes []facts.Envelope, resourceType string) facts.Envelope {
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

func assertDNSRecord(t *testing.T, envelopes []facts.Envelope, recordName string, recordType string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.AWSDNSRecordFactKind {
			continue
		}
		if envelope.Payload["record_name"] == recordName && envelope.Payload["record_type"] == recordType {
			return envelope
		}
	}
	t.Fatalf("missing dns record %s %s in %#v", recordName, recordType, envelopes)
	return facts.Envelope{}
}

func assertNoDNSRecord(t *testing.T, envelopes []facts.Envelope, recordName string, recordType string) {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.AWSDNSRecordFactKind &&
			envelope.Payload["record_name"] == recordName &&
			envelope.Payload["record_type"] == recordType {
			t.Fatalf("unexpected dns record %s %s", recordName, recordType)
		}
	}
}

func assertAttribute(t *testing.T, envelope facts.Envelope, key string, want any) {
	t.Helper()
	attributes, ok := envelope.Payload["attributes"].(map[string]any)
	if !ok {
		t.Fatalf("attributes = %#v, want map", envelope.Payload["attributes"])
	}
	if attributes[key] != want {
		t.Fatalf("attribute %s = %#v, want %#v", key, attributes[key], want)
	}
}

func assertPayloadBool(t *testing.T, envelope facts.Envelope, key string, want bool) {
	t.Helper()
	got, ok := envelope.Payload[key].(bool)
	if !ok || got != want {
		t.Fatalf("payload[%q] = %#v, want %v", key, envelope.Payload[key], want)
	}
}
