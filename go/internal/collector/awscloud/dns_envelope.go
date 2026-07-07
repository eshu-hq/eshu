// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	awsv1 "github.com/eshu-hq/eshu/sdk/go/factschema/aws/v1"
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
	payload, err := factschema.EncodeAWSDNSRecord(awsv1.DNSRecord{
		AccountID:            observation.Boundary.AccountID,
		Region:               observation.Boundary.Region,
		ServiceKind:          boundaryValue(observation.Boundary.ServiceKind),
		CollectorInstanceID:  boundaryValue(observation.Boundary.CollectorInstanceID),
		HostedZoneID:         hostedZoneID,
		HostedZoneName:       stringValuePtr(strings.TrimSpace(observation.HostedZoneName)),
		HostedZonePrivate:    boolValuePtr(observation.HostedZonePrivate),
		RecordName:           recordName,
		NormalizedRecordName: normalizedRecordName,
		RecordType:           recordType,
		SetIdentifier:        stringValuePtr(setIdentifier),
		TTL:                  observation.TTL,
		Values:               cloneStringSlice(observation.Values),
		AliasTarget:          aliasTargetPayload(observation.AliasTarget),
		RoutingPolicy:        routingPolicyPayload(observation.RoutingPolicy),
		CorrelationAnchors:   dnsRecordAnchors(recordName, observation.Values, observation.AliasTarget),
		HasAliasTarget:       boolValuePtr(observation.AliasTarget != nil),
		SourceHostedZoneName: stringValuePtr(strings.TrimSpace(observation.HostedZoneName)),
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode aws_dns_record payload: %w", err)
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

func normalizedDNSName(input string) string {
	return strings.TrimSuffix(strings.ToLower(strings.TrimSpace(input)), ".")
}

func aliasTargetPayload(input *DNSAliasTarget) *awsv1.DNSAliasTarget {
	if input == nil {
		return nil
	}
	dnsName := strings.TrimSpace(input.DNSName)
	return &awsv1.DNSAliasTarget{
		DNSName:               dnsName,
		NormalizedDNSName:     normalizedDNSName(dnsName),
		HostedZoneID:          strings.TrimSpace(input.HostedZoneID),
		EvaluateTargetHealth:  input.EvaluateTargetHealth,
		TargetIdentityFamily:  "dns_name",
		TargetIdentityVersion: "1.0.0",
	}
}

func routingPolicyPayload(input DNSRoutingPolicy) *awsv1.DNSRoutingPolicy {
	output := awsv1.DNSRoutingPolicy{
		Weight:                  input.Weight,
		Region:                  stringValuePtr(strings.TrimSpace(input.Region)),
		Failover:                stringValuePtr(strings.TrimSpace(input.Failover)),
		HealthCheckID:           stringValuePtr(strings.TrimSpace(input.HealthCheckID)),
		MultiValueAnswer:        input.MultiValueAnswer,
		TrafficPolicyInstanceID: stringValuePtr(strings.TrimSpace(input.TrafficPolicyInstanceID)),
		CIDRCollectionID:        stringValuePtr(strings.TrimSpace(input.CIDRCollectionID)),
		CIDRLocationName:        stringValuePtr(strings.TrimSpace(input.CIDRLocationName)),
	}
	geo := awsv1.DNSGeoLocation{
		ContinentCode:   stringValuePtr(strings.TrimSpace(input.GeoLocation.ContinentCode)),
		CountryCode:     stringValuePtr(strings.TrimSpace(input.GeoLocation.CountryCode)),
		SubdivisionCode: stringValuePtr(strings.TrimSpace(input.GeoLocation.SubdivisionCode)),
	}
	if hasAnyStringValue(*geo.ContinentCode, *geo.CountryCode, *geo.SubdivisionCode) {
		output.GeoLocation = &geo
	}
	if input.Weight == nil &&
		!hasAnyStringValue(*output.Region, *output.Failover, *output.HealthCheckID, *output.TrafficPolicyInstanceID, *output.CIDRCollectionID, *output.CIDRLocationName) &&
		input.MultiValueAnswer == nil && output.GeoLocation == nil {
		return nil
	}
	return &output
}

func hasAnyStringValue(values ...string) bool {
	for _, value := range values {
		if value != "" {
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
