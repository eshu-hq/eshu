package awscloud

import "time"

// CollectorKind is the durable collector_kind value for AWS cloud facts.
const CollectorKind = "aws"

// Boundary carries the durable scope-generation and claim identity shared by
// all facts emitted for one AWS claim.
type Boundary struct {
	AccountID           string
	Region              string
	ServiceKind         string
	ScopeID             string
	GenerationID        string
	CollectorInstanceID string
	FencingToken        int64
	ObservedAt          time.Time
}

// ResourceObservation describes one AWS resource reported by a service API.
type ResourceObservation struct {
	Boundary           Boundary
	ARN                string
	ResourceID         string
	ResourceType       string
	Name               string
	State              string
	Tags               map[string]string
	Attributes         map[string]any
	CorrelationAnchors []string
	SourceURI          string
	SourceRecordID     string
}

// RelationshipObservation describes one relationship reported by AWS APIs.
type RelationshipObservation struct {
	Boundary         Boundary
	RelationshipType string
	SourceResourceID string
	SourceARN        string
	TargetResourceID string
	TargetARN        string
	TargetType       string
	Attributes       map[string]any
	SourceURI        string
	SourceRecordID   string
}

// ImageReferenceObservation describes one ECR image digest and tag reference.
type ImageReferenceObservation struct {
	Boundary          Boundary
	RepositoryARN     string
	RepositoryName    string
	RegistryID        string
	ImageDigest       string
	ManifestDigest    string
	Tag               string
	PushedAt          time.Time
	ImageSizeInBytes  int64
	ManifestMediaType string
	ArtifactMediaType string
	SourceURI         string
	SourceRecordID    string
}

// DNSRecordObservation describes one Route 53 DNS record reported by AWS.
type DNSRecordObservation struct {
	Boundary          Boundary
	HostedZoneID      string
	HostedZoneName    string
	HostedZonePrivate bool
	RecordName        string
	RecordType        string
	SetIdentifier     string
	TTL               *int64
	Values            []string
	AliasTarget       *DNSAliasTarget
	RoutingPolicy     DNSRoutingPolicy
	SourceURI         string
	SourceRecordID    string
}

// DNSAliasTarget captures Route 53 alias target evidence without inferring
// ownership of the target resource.
type DNSAliasTarget struct {
	DNSName              string
	HostedZoneID         string
	EvaluateTargetHealth bool
}

// DNSRoutingPolicy captures non-secret Route 53 routing policy selectors.
type DNSRoutingPolicy struct {
	Weight                  *int64
	Region                  string
	Failover                string
	HealthCheckID           string
	MultiValueAnswer        *bool
	TrafficPolicyInstanceID string
	GeoLocation             DNSGeoLocation
	CIDRCollectionID        string
	CIDRLocationName        string
}

// DNSGeoLocation captures Route 53 geolocation routing selectors.
type DNSGeoLocation struct {
	ContinentCode   string
	CountryCode     string
	SubdivisionCode string
}

// RDSPostureObservation describes the derived security and operations posture
// for one RDS DB instance or Aurora DB cluster. Every field is metadata-only
// control-plane evidence returned by the RDS describe APIs: derived booleans,
// retention windows, and KMS/parameter/option-group identifiers. It never
// carries database contents, master usernames, connection secrets, snapshot
// payloads, log bodies, or Performance Insights samples.
type RDSPostureObservation struct {
	Boundary     Boundary
	ARN          string
	ResourceID   string
	ResourceType string
	Identifier   string
	Engine       string

	PubliclyAccessible               bool
	StorageEncrypted                 bool
	KMSKeyID                         string
	IAMDatabaseAuthenticationEnabled bool
	MultiAZ                          bool
	DeletionProtection               bool
	BackupRetentionPeriod            int32

	PerformanceInsightsEnabled       bool
	PerformanceInsightsRetentionDays int32
	PerformanceInsightsKMSKeyID      string

	CACertificateIdentifier string

	ParameterGroups []string
	OptionGroups    []string

	// SecurityParameters is a curated set of security-relevant non-default
	// parameter name/value pairs (for example rds.force_ssl). It is populated
	// only from already-reported configuration; the scanner must not read
	// database contents to fill it.
	SecurityParameters map[string]string

	SourceURI      string
	SourceRecordID string
}

// WarningObservation describes one non-fatal AWS scan warning.
type WarningObservation struct {
	Boundary       Boundary
	WarningKind    string
	ErrorClass     string
	Message        string
	SourceURI      string
	SourceRecordID string
	Attributes     map[string]any
}
