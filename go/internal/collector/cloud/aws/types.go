// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

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

// EC2InstancePostureObservation describes the derived security and operations
// posture for one EC2 instance. Every field is metadata-only control-plane
// evidence read from the existing DescribeInstances pass: IMDS settings,
// user-data PRESENCE (a boolean only), detailed-monitoring and EBS-optimized
// flags, public-IP association, the attached instance-profile ARN, per-volume
// block-device metadata, and tenancy / Nitro-enclave state.
//
// It never carries the user-data content (which can embed secrets), instance
// console output, environment variables, command-line arguments, or any other
// instance payload. The builder only ever sees presence booleans and safe
// identifiers; the raw user-data string never reaches it.
//
// Pointer booleans distinguish an unknown setting (nil, the control-plane read
// returned no value) from an observed false. Plain booleans are scanner-derived
// summaries that default to false when the underlying configuration is absent.
type EC2InstancePostureObservation struct {
	Boundary   Boundary
	ARN        string
	InstanceID string
	State      string

	// IMDS (instance metadata service) settings. IMDSv2Required reflects
	// HttpTokens == "required"; HTTPEndpoint reflects the endpoint state
	// ("enabled"/"disabled"); HTTPPutResponseHopLimit is the token TTL hop limit.
	IMDSv2Required          *bool
	HTTPEndpoint            string
	HTTPPutResponseHopLimit *int32

	// UserDataPresent is true when the instance has user-data attached. The
	// scanner derives this from a presence read; the user-data CONTENT is never
	// fetched or persisted. Nil means presence could not be determined.
	UserDataPresent *bool

	DetailedMonitoring  bool
	EBSOptimized        bool
	PublicIPAssociated  bool
	PublicIPAddress     string
	InstanceProfileARN  string
	Tenancy             string
	NitroEnclaveEnabled bool

	// BlockDevices carries per-volume block-device metadata reported by
	// DescribeInstances. Per-volume encryption state is NOT on this response, so
	// Encrypted stays nil here; the #1304 reducer joins each volume id to its
	// encryption and KMS evidence.
	BlockDevices []EC2BlockDevicePosture

	SourceURI      string
	SourceRecordID string
}

// EC2BlockDevicePosture is one instance block-device mapping entry: the device
// name, attached volume id, delete-on-termination flag, and attachment status.
// Encrypted is a pointer so an unknown encryption state (the common case from
// DescribeInstances, which does not report it) stays distinct from observed
// false; reducers resolve it from volume evidence.
type EC2BlockDevicePosture struct {
	DeviceName          string
	VolumeID            string
	DeleteOnTermination bool
	Status              string
	Encrypted           *bool
}

// IAMPermissionObservation describes one normalized, metadata-only IAM policy
// statement attached to a principal. It is the derived projection of a single
// statement: effect, action set, resource pattern, and a condition summary.
//
// It deliberately carries NO raw policy JSON body and NO condition values
// (which can embed source IPs, tags, or other sensitive selectors). The scanner
// normalizes the statement at the SDK boundary and emits only identifiers and
// derived booleans.
type IAMPermissionObservation struct {
	Boundary      Boundary
	PrincipalARN  string
	PrincipalType string
	PolicySource  string
	PolicyARN     string
	PolicyName    string
	StatementSID  string
	Effect        string
	Actions       []string
	NotActions    []string
	Resources     []string
	NotResources  []string
	// ConditionKeys lists the condition keys present on the statement (for
	// example aws:SourceIp). Values are intentionally omitted; only the key
	// identifiers are kept as a derived condition summary.
	ConditionKeys []string
	// ConditionOperators lists the condition operators present on the statement
	// (for example StringEquals or ForAnyValue:StringLike). Values are omitted.
	ConditionOperators []string
	// AssumePrincipals lists the principals a trust statement grants assume-role
	// to. It is only meaningful when PolicySource is IAMPolicySourceTrust.
	AssumePrincipals []string
	SourceURI        string
	SourceRecordID   string
}

// ResourcePolicyPermissionObservation describes one normalized, metadata-only
// statement from a resource-based policy (an S3 bucket policy or KMS key policy)
// attached to the AWS resource it controls. It is the resource-side analog of
// IAMPermissionObservation: the derived projection of a single statement plus
// the derived grantee-principal facts.
//
// It deliberately carries NO raw policy JSON body, NO statement Sid in the
// persisted payload (StatementSID feeds only the stable source-record id), and
// NO condition values (which can embed source IPs, tags, VPC ids, or other
// sensitive selectors). The scanner normalizes the statement at the SDK
// boundary and emits only identifiers, derived booleans, and the grantee
// account ids / principal types.
type ResourcePolicyPermissionObservation struct {
	Boundary Boundary
	// ResourceARN is the ARN of the resource the policy is attached to (the S3
	// bucket or KMS key), i.e. the resource the grant applies to.
	ResourceARN string
	// ResourceType is the resource type the policy is attached to, for example
	// ResourceTypeS3Bucket or ResourceTypeKMSKey.
	ResourceType string
	// StatementSID is the source statement Sid. It is used only to keep the
	// derived fact's source-record id stable across re-observation; it is never
	// written into the persisted payload.
	StatementSID string
	Effect       string
	Actions      []string
	NotActions   []string
	Resources    []string
	NotResources []string
	// ConditionKeys lists the condition keys present on the statement (for
	// example aws:SourceIp). Values are intentionally omitted; only the key
	// identifiers are kept as a derived condition summary.
	ConditionKeys []string
	// ConditionOperators lists the condition operators present on the statement
	// (for example StringEquals or ForAnyValue:StringLike). Values are omitted.
	ConditionOperators []string
	// PrincipalAccountIDs lists the 12-digit account ids derived from the
	// statement's AWS-type principals. Public/anonymous and service/federated
	// principals contribute no account id.
	PrincipalAccountIDs []string
	// PrincipalARNs lists the AWS-type principal ARNs named by the statement.
	// Like IAMPermissionObservation.AssumePrincipals these are ARNs (never raw
	// policy JSON); they let the CAN_PERFORM follow-up resolve the grantee when
	// an account id alone is insufficient.
	PrincipalARNs []string
	// PrincipalTypes lists the principal element kinds present on the statement
	// (aws / service / federated / canonical), derived from the Principal keys.
	PrincipalTypes []string
	// IsPublic reports whether the statement grants to an anonymous/public
	// principal (Principal "*" or {"AWS":"*"}).
	IsPublic bool
	// IsCrossAccount reports whether any named principal account differs from the
	// resource-owner account.
	IsCrossAccount bool
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
