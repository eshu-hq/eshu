package awscloud

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewDNSRecordEnvelope builds the durable aws_dns_record fact for one Route 53
// DNS record observation.
func NewDNSRecordEnvelope(observation DNSRecordObservation) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	hostedZoneID := strings.TrimSpace(observation.HostedZoneID)
	recordName := strings.TrimSpace(observation.RecordName)
	recordType := strings.ToUpper(strings.TrimSpace(observation.RecordType))
	if hostedZoneID == "" {
		return facts.Envelope{}, fmt.Errorf("aws dns record observation requires hosted_zone_id")
	}
	if recordName == "" {
		return facts.Envelope{}, fmt.Errorf("aws dns record observation requires record_name")
	}
	if recordType == "" {
		return facts.Envelope{}, fmt.Errorf("aws dns record observation requires record_type")
	}
	normalizedRecordName := normalizedDNSName(recordName)
	setIdentifier := strings.TrimSpace(observation.SetIdentifier)
	stableKey := facts.StableID(facts.AWSDNSRecordFactKind, map[string]any{
		"account_id":             observation.Boundary.AccountID,
		"hosted_zone_id":         hostedZoneID,
		"normalized_record_name": normalizedRecordName,
		"record_type":            recordType,
		"region":                 observation.Boundary.Region,
		"set_identifier":         setIdentifier,
	})
	payload := map[string]any{
		"account_id":              observation.Boundary.AccountID,
		"region":                  observation.Boundary.Region,
		"service_kind":            observation.Boundary.ServiceKind,
		"collector_instance_id":   observation.Boundary.CollectorInstanceID,
		"hosted_zone_id":          hostedZoneID,
		"hosted_zone_name":        strings.TrimSpace(observation.HostedZoneName),
		"hosted_zone_private":     observation.HostedZonePrivate,
		"record_name":             recordName,
		"normalized_record_name":  normalizedRecordName,
		"record_type":             recordType,
		"set_identifier":          setIdentifier,
		"ttl":                     int64OrNil(observation.TTL),
		"values":                  cloneStringSlice(observation.Values),
		"alias_target":            aliasTargetMap(observation.AliasTarget),
		"routing_policy":          routingPolicyMap(observation.RoutingPolicy),
		"correlation_anchors":     dnsRecordAnchors(recordName, observation.Values, observation.AliasTarget),
		"has_alias_target":        observation.AliasTarget != nil,
		"source_hosted_zone_name": strings.TrimSpace(observation.HostedZoneName),
	}
	return newEnvelope(
		observation.Boundary,
		facts.AWSDNSRecordFactKind,
		facts.AWSDNSRecordSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, dnsRecordSourceID(hostedZoneID, recordName, recordType, setIdentifier)),
		observation.SourceURI,
		payload,
	), nil
}

func int64OrNil(input *int64) any {
	if input == nil {
		return nil
	}
	return *input
}

func normalizedDNSName(input string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(input)), ".")
}

func aliasTargetMap(input *DNSAliasTarget) map[string]any {
	if input == nil {
		return nil
	}
	dnsName := strings.TrimSpace(input.DNSName)
	return map[string]any{
		"dns_name":                dnsName,
		"normalized_dns_name":     normalizedDNSName(dnsName),
		"hosted_zone_id":          strings.TrimSpace(input.HostedZoneID),
		"evaluate_target_health":  input.EvaluateTargetHealth,
		"target_identity_family":  "dns_name",
		"target_identity_version": "1.0.0",
	}
}

func routingPolicyMap(input DNSRoutingPolicy) map[string]any {
	output := map[string]any{
		"weight":                     int64OrNil(input.Weight),
		"region":                     strings.TrimSpace(input.Region),
		"failover":                   strings.TrimSpace(input.Failover),
		"health_check_id":            strings.TrimSpace(input.HealthCheckID),
		"multi_value_answer":         boolOrNil(input.MultiValueAnswer),
		"traffic_policy_instance_id": strings.TrimSpace(input.TrafficPolicyInstanceID),
		"cidr_collection_id":         strings.TrimSpace(input.CIDRCollectionID),
		"cidr_location_name":         strings.TrimSpace(input.CIDRLocationName),
	}
	geo := map[string]any{
		"continent_code":   strings.TrimSpace(input.GeoLocation.ContinentCode),
		"country_code":     strings.TrimSpace(input.GeoLocation.CountryCode),
		"subdivision_code": strings.TrimSpace(input.GeoLocation.SubdivisionCode),
	}
	if hasAnyValue(geo) {
		output["geo_location"] = geo
	}
	if !hasAnyValue(output) {
		return nil
	}
	return output
}

func boolOrNil(input *bool) any {
	if input == nil {
		return nil
	}
	return *input
}

func hasAnyValue(input map[string]any) bool {
	for _, value := range input {
		switch typed := value.(type) {
		case nil:
			continue
		case string:
			if typed != "" {
				return true
			}
		default:
			return true
		}
	}
	return false
}

func dnsRecordAnchors(recordName string, values []string, aliasTarget *DNSAliasTarget) []string {
	anchors := []string{recordName, normalizedDNSName(recordName)}
	anchors = append(anchors, values...)
	if aliasTarget != nil {
		anchors = append(anchors, aliasTarget.DNSName, normalizedDNSName(aliasTarget.DNSName))
	}
	return normalizedAnchors(nil, anchors...)
}

func dnsRecordSourceID(hostedZoneID, recordName, recordType, setIdentifier string) string {
	parts := []string{hostedZoneID, recordType, normalizedDNSName(recordName)}
	if setIdentifier != "" {
		parts = append(parts, setIdentifier)
	}
	return strings.Join(parts, "#")
}

func cloneStringSlice(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, len(input))
	copy(output, input)
	return output
}
