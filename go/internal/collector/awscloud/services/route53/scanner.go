package route53

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Route 53 hosted-zone and DNS-record facts for one claimed
// account boundary.
type Scanner struct {
	Client Client
}

// Scan observes Route 53 hosted zones and high-value DNS records through the
// configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("route53 scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceRoute53
	case awscloud.ServiceRoute53:
	default:
		return nil, fmt.Errorf("route53 scanner received service_kind %q", boundary.ServiceKind)
	}

	hostedZones, err := s.Client.ListHostedZones(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Route53 hosted zones: %w", err)
	}
	var envelopes []facts.Envelope
	for _, hostedZone := range hostedZones {
		zoneEnvelopes, err := s.hostedZoneEnvelopes(ctx, boundary, hostedZone)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, zoneEnvelopes...)
	}
	return envelopes, nil
}

func (s Scanner) hostedZoneEnvelopes(
	ctx context.Context,
	boundary awscloud.Boundary,
	hostedZone HostedZone,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(hostedZoneObservation(boundary, hostedZone))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	records, err := s.Client.ListResourceRecordSets(ctx, hostedZone)
	if err != nil {
		return nil, fmt.Errorf("list Route53 record sets for hosted zone %q: %w", hostedZone.ID, err)
	}
	for _, record := range records {
		if !supportedRecord(record) {
			continue
		}
		envelope, err := awscloud.NewDNSRecordEnvelope(recordObservation(boundary, hostedZone, record))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func hostedZoneObservation(boundary awscloud.Boundary, hostedZone HostedZone) awscloud.ResourceObservation {
	hostedZoneID := strings.TrimSpace(hostedZone.ID)
	hostedZoneARN := route53HostedZoneARN(hostedZoneID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          hostedZoneARN,
		ResourceID:   hostedZoneID,
		ResourceType: awscloud.ResourceTypeRoute53HostedZone,
		Name:         hostedZone.Name,
		Tags:         hostedZone.Tags,
		Attributes: map[string]any{
			"caller_reference":          strings.TrimSpace(hostedZone.CallerReference),
			"comment":                   strings.TrimSpace(hostedZone.Comment),
			"linked_service":            linkedServiceMap(hostedZone.LinkedService),
			"normalized_name":           normalizedDNSName(hostedZone.Name),
			"private_zone":              hostedZone.Private,
			"record_set_count":          hostedZone.ResourceRecordSetCount,
			"route53_hosted_zone_arn":   hostedZoneARN,
			"route53_hosted_zone_id":    hostedZoneID,
			"source_identity_family":    "route53_hosted_zone",
			"source_identity_version":   "1.0.0",
			"zone_visibility_evidence":  zoneVisibility(hostedZone.Private),
			"zone_visibility_is_direct": true,
		},
		CorrelationAnchors: []string{hostedZoneID, hostedZoneARN, hostedZone.Name, normalizedDNSName(hostedZone.Name)},
		SourceRecordID:     hostedZoneID,
	}
}

func recordObservation(
	boundary awscloud.Boundary,
	hostedZone HostedZone,
	record RecordSet,
) awscloud.DNSRecordObservation {
	return awscloud.DNSRecordObservation{
		Boundary:          boundary,
		HostedZoneID:      hostedZone.ID,
		HostedZoneName:    hostedZone.Name,
		HostedZonePrivate: hostedZone.Private,
		RecordName:        record.Name,
		RecordType:        record.Type,
		SetIdentifier:     record.SetIdentifier,
		TTL:               record.TTL,
		Values:            record.Values,
		AliasTarget:       aliasTargetObservation(record.AliasTarget),
		RoutingPolicy: awscloud.DNSRoutingPolicy{
			Weight:                  record.Weight,
			Region:                  record.Region,
			Failover:                record.Failover,
			HealthCheckID:           record.HealthCheckID,
			MultiValueAnswer:        record.MultiValueAnswer,
			TrafficPolicyInstanceID: record.TrafficPolicyInstanceID,
			GeoLocation: awscloud.DNSGeoLocation{
				ContinentCode:   record.GeoLocation.ContinentCode,
				CountryCode:     record.GeoLocation.CountryCode,
				SubdivisionCode: record.GeoLocation.SubdivisionCode,
			},
			CIDRCollectionID: record.CIDRRouting.CollectionID,
			CIDRLocationName: record.CIDRRouting.LocationName,
		},
		SourceRecordID: sourceRecordID(hostedZone.ID, record),
	}
}

func supportedRecord(record RecordSet) bool {
	if record.AliasTarget != nil {
		return true
	}
	switch strings.ToUpper(strings.TrimSpace(record.Type)) {
	case "A", "AAAA", "CNAME":
		return true
	default:
		return false
	}
}

func aliasTargetObservation(input *AliasTarget) *awscloud.DNSAliasTarget {
	if input == nil {
		return nil
	}
	return &awscloud.DNSAliasTarget{
		DNSName:              input.DNSName,
		HostedZoneID:         input.HostedZoneID,
		EvaluateTargetHealth: input.EvaluateTargetHealth,
	}
}

func linkedServiceMap(input LinkedService) map[string]any {
	if strings.TrimSpace(input.ServicePrincipal) == "" && strings.TrimSpace(input.Description) == "" {
		return nil
	}
	return map[string]any{
		"service_principal": strings.TrimSpace(input.ServicePrincipal),
		"description":       strings.TrimSpace(input.Description),
	}
}

func sourceRecordID(hostedZoneID string, record RecordSet) string {
	parts := []string{
		strings.TrimSpace(hostedZoneID),
		strings.ToUpper(strings.TrimSpace(record.Type)),
		normalizedDNSName(record.Name),
	}
	if strings.TrimSpace(record.SetIdentifier) != "" {
		parts = append(parts, strings.TrimSpace(record.SetIdentifier))
	}
	return strings.Join(parts, "#")
}

func route53HostedZoneARN(hostedZoneID string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(hostedZoneID), "/hostedzone/")
	if trimmed == "" {
		return ""
	}
	return "arn:aws:route53:::hostedzone/" + trimmed
}

func normalizedDNSName(input string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(input)), ".")
}

func zoneVisibility(private bool) string {
	if private {
		return "private"
	}
	return "public"
}
