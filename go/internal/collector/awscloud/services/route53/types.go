package route53

import "context"

// Client is the Route 53 read surface consumed by Scanner. Runtime adapters
// should translate AWS SDK responses into these scanner-owned types.
type Client interface {
	ListHostedZones(context.Context) ([]HostedZone, error)
	ListResourceRecordSets(context.Context, HostedZone) ([]RecordSet, error)
}

// HostedZone is the scanner-owned representation of a Route 53 hosted zone.
type HostedZone struct {
	ID                     string
	Name                   string
	CallerReference        string
	Comment                string
	Private                bool
	ResourceRecordSetCount int64
	LinkedService          LinkedService
	Tags                   map[string]string
}

// LinkedService captures the AWS service that owns a hosted zone, when Route 53
// reports one.
type LinkedService struct {
	ServicePrincipal string
	Description      string
}

// RecordSet is the scanner-owned representation of a Route 53 record set.
type RecordSet struct {
	Name                    string
	Type                    string
	SetIdentifier           string
	TTL                     *int64
	Values                  []string
	AliasTarget             *AliasTarget
	Weight                  *int64
	Region                  string
	Failover                string
	HealthCheckID           string
	MultiValueAnswer        *bool
	TrafficPolicyInstanceID string
	GeoLocation             GeoLocation
	CIDRRouting             CIDRRouting
}

// AliasTarget captures the target of a Route 53 alias record.
type AliasTarget struct {
	DNSName              string
	HostedZoneID         string
	EvaluateTargetHealth bool
}

// GeoLocation captures Route 53 geolocation routing selectors.
type GeoLocation struct {
	ContinentCode   string
	CountryCode     string
	SubdivisionCode string
}

// CIDRRouting captures Route 53 CIDR routing selectors.
type CIDRRouting struct {
	CollectionID string
	LocationName string
}
