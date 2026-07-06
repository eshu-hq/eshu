// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// DNSRecord is the schema-version-1 typed payload for "aws_dns_record".
type DNSRecord struct {
	AccountID            string            `json:"account_id"`
	Region               string            `json:"region"`
	ServiceKind          *string           `json:"service_kind,omitempty"`
	CollectorInstanceID  *string           `json:"collector_instance_id,omitempty"`
	HostedZoneID         string            `json:"hosted_zone_id"`
	HostedZoneName       *string           `json:"hosted_zone_name,omitempty"`
	HostedZonePrivate    *bool             `json:"hosted_zone_private,omitempty"`
	RecordName           string            `json:"record_name"`
	NormalizedRecordName string            `json:"normalized_record_name"`
	RecordType           string            `json:"record_type"`
	SetIdentifier        *string           `json:"set_identifier,omitempty"`
	TTL                  *int64            `json:"ttl,omitempty"`
	Values               []string          `json:"values,omitempty"`
	AliasTarget          *DNSAliasTarget   `json:"alias_target,omitempty"`
	RoutingPolicy        *DNSRoutingPolicy `json:"routing_policy,omitempty"`
	CorrelationAnchors   []string          `json:"correlation_anchors,omitempty"`
	HasAliasTarget       *bool             `json:"has_alias_target,omitempty"`
	SourceHostedZoneName *string           `json:"source_hosted_zone_name,omitempty"`
}

// DNSAliasTarget is the typed alias-target block inside an AWS DNS record.
type DNSAliasTarget struct {
	DNSName               string `json:"dns_name"`
	NormalizedDNSName     string `json:"normalized_dns_name"`
	HostedZoneID          string `json:"hosted_zone_id"`
	EvaluateTargetHealth  bool   `json:"evaluate_target_health"`
	TargetIdentityFamily  string `json:"target_identity_family"`
	TargetIdentityVersion string `json:"target_identity_version"`
}

// DNSRoutingPolicy is the typed routing-policy block inside an AWS DNS record.
type DNSRoutingPolicy struct {
	Weight                  *int64          `json:"weight,omitempty"`
	Region                  *string         `json:"region,omitempty"`
	Failover                *string         `json:"failover,omitempty"`
	HealthCheckID           *string         `json:"health_check_id,omitempty"`
	MultiValueAnswer        *bool           `json:"multi_value_answer,omitempty"`
	TrafficPolicyInstanceID *string         `json:"traffic_policy_instance_id,omitempty"`
	GeoLocation             *DNSGeoLocation `json:"geo_location,omitempty"`
	CIDRCollectionID        *string         `json:"cidr_collection_id,omitempty"`
	CIDRLocationName        *string         `json:"cidr_location_name,omitempty"`
}

// DNSGeoLocation is the typed geolocation selector inside a DNS routing policy.
type DNSGeoLocation struct {
	ContinentCode   *string `json:"continent_code,omitempty"`
	CountryCode     *string `json:"country_code,omitempty"`
	SubdivisionCode *string `json:"subdivision_code,omitempty"`
}
