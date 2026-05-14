package apigateway

import (
	"context"
	"time"
)

const (
	// APIKindREST identifies classic API Gateway REST APIs.
	APIKindREST = "rest"
	// APIKindV2 identifies API Gateway v2 HTTP and WebSocket APIs.
	APIKindV2 = "v2"
)

// Client returns one bounded API Gateway metadata snapshot for a claimed
// account and region.
type Client interface {
	Snapshot(context.Context) (Snapshot, error)
}

// Snapshot is the scanner-owned metadata view of API Gateway REST, HTTP,
// WebSocket, stage, domain, mapping, and integration records.
type Snapshot struct {
	RESTAPIs []RESTAPI
	V2APIs   []V2API
	Domains  []DomainName
}

// RESTAPI is the metadata-only scanner view of an API Gateway REST API.
type RESTAPI struct {
	ID                        string
	Name                      string
	Description               string
	CreatedDate               time.Time
	Version                   string
	APIStatus                 string
	APIKeySource              string
	DisableExecuteAPIEndpoint bool
	EndpointTypes             []string
	VPCEndpointIDs            []string
	Tags                      map[string]string
	Stages                    []Stage
	Integrations              []Integration
	Policy                    string
}

// V2API is the metadata-only scanner view of an API Gateway HTTP or WebSocket
// API.
type V2API struct {
	ID                        string
	Name                      string
	ProtocolType              string
	Endpoint                  string
	CreatedDate               time.Time
	Description               string
	DisableExecuteAPIEndpoint bool
	APIGatewayManaged         *bool
	IPAddressType             string
	Tags                      map[string]string
	Stages                    []Stage
	Integrations              []Integration
}

// Stage is the metadata-only scanner view of an API Gateway stage. Stage
// variable values are intentionally excluded because they can contain secrets.
type Stage struct {
	APIKind              string
	APIID                string
	Name                 string
	DeploymentID         string
	Description          string
	CreatedDate          time.Time
	LastUpdatedDate      time.Time
	CacheClusterEnabled  bool
	CacheClusterSize     string
	CacheClusterStatus   string
	TracingEnabled       bool
	ClientCertificateID  string
	AccessLogDestination string
	WebACLARN            string
	AutoDeploy           *bool
	APIGatewayManaged    *bool
	Tags                 map[string]string
	StageVariables       map[string]string
}

// DomainName is the metadata-only scanner view of an API Gateway custom domain.
// Policy JSON and mutual TLS truststore URIs are intentionally excluded from
// emitted facts.
type DomainName struct {
	APIKind           string
	Name              string
	ARN               string
	Status            string
	EndpointTypes     []string
	RegionalDomain    string
	RegionalZoneID    string
	DistributionName  string
	DistributionZone  string
	CertificateARNs   []string
	SecurityPolicy    string
	APIMappingSelect  string
	Tags              map[string]string
	Mappings          []Mapping
	ManagementPolicy  string
	ExecuteAPIPolicy  string
	MutualTLSTrustURI string
}

// Mapping is custom-domain routing metadata for a REST base path mapping or v2
// API mapping.
type Mapping struct {
	APIKind string
	Domain  string
	ID      string
	Key     string
	APIID   string
	Stage   string
}

// Integration is API Gateway integration metadata. Credential ARNs, request
// templates, response templates, and payload bodies are intentionally excluded.
type Integration struct {
	APIKind              string
	APIID                string
	IntegrationID        string
	ResourceID           string
	ResourcePath         string
	Method               string
	Type                 string
	URI                  string
	ConnectionType       string
	ConnectionID         string
	PayloadFormatVersion string
	TimeoutMillis        int32
	APIGatewayManaged    *bool
	CredentialsARN       string
}
