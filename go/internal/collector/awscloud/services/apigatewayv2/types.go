// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package apigatewayv2

import (
	"context"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// Client returns one bounded API Gateway v2 metadata snapshot for a claimed
// account and region. Runtime adapters translate AWS SDK responses into the
// scanner-owned types below.
//
// The interface deliberately exposes no mutation operations, no
// GetIntegrationResponse / GetRouteResponse / GetModelTemplate reads (those
// surface request/response mapping templates), and no ExportApi (which would
// dump the full OpenAPI body). A reflection test in the SDK adapter enforces
// that those forbidden methods stay absent.
type Client interface {
	Snapshot(context.Context) (Snapshot, error)
}

// Snapshot is the scanner-owned metadata view of API Gateway v2 HTTP and
// WebSocket APIs, their stages, routes, integrations, authorizers, custom
// domains, and VPC links.
type Snapshot struct {
	Warnings []awscloud.WarningObservation
	APIs     []API
	Domains  []DomainName
	VPCLinks []VPCLink
}

// API is the metadata-only scanner view of an API Gateway v2 HTTP or WebSocket
// API. It carries control-plane topology only; route selection expressions and
// request transformation behavior are never present on any nested type.
type API struct {
	ID                        string
	Name                      string
	ProtocolType              string
	Endpoint                  string
	CreatedDate               time.Time
	Description               string
	Version                   string
	DisableExecuteAPIEndpoint bool
	APIGatewayManaged         *bool
	IPAddressType             string
	Tags                      map[string]string
	Stages                    []Stage
	Routes                    []Route
	Integrations              []Integration
	Authorizers               []Authorizer
}

// Stage is the metadata-only scanner view of an API Gateway v2 stage. Stage
// variable values are intentionally excluded because they can contain secrets.
type Stage struct {
	APIID                string
	Name                 string
	DeploymentID         string
	Description          string
	CreatedDate          time.Time
	LastUpdatedDate      time.Time
	AutoDeploy           *bool
	APIGatewayManaged    *bool
	ClientCertificateID  string
	AccessLogDestination string
	Tags                 map[string]string
}

// Route is the metadata-only scanner view of an API Gateway v2 route. It
// records the route key, route id, target reference, and authorization summary
// only. Request models, request parameter constraints, model selection
// expressions, and route response selection expressions are intentionally
// excluded because they describe request transformation, not topology.
type Route struct {
	APIID             string
	RouteID           string
	RouteKey          string
	Target            string
	AuthorizationType string
	AuthorizerID      string
	APIKeyRequired    *bool
	APIGatewayManaged *bool
	OperationName     string
}

// Integration is the metadata-only scanner view of an API Gateway v2
// integration. It records the integration id, type, backend URI/target, and
// connection metadata only. Request templates, request/response parameter
// mappings, template selection expressions, and credential ARNs are
// intentionally excluded because they are transformation behavior or secrets.
type Integration struct {
	APIID                string
	IntegrationID        string
	Type                 string
	Subtype              string
	URI                  string
	Method               string
	ConnectionType       string
	ConnectionID         string
	PayloadFormatVersion string
	Description          string
	TimeoutMillis        int32
	APIGatewayManaged    *bool
}

// Authorizer is the metadata-only scanner view of an API Gateway v2 authorizer.
// It records the authorizer name, type, identity sources, and JWT issuer/
// audience only. The Lambda authorizer invocation URI, authorizer credential
// ARN, and identity validation expression are intentionally excluded: the URI
// and credential ARN are the Lambda authorizer payload path and its execution
// secret, never persisted.
type Authorizer struct {
	APIID          string
	AuthorizerID   string
	Name           string
	Type           string
	IdentitySource []string
	JWTIssuer      string
	JWTAudience    []string
}

// DomainName is the metadata-only scanner view of an API Gateway v2 custom
// domain. Mutual TLS truststore URIs are intentionally excluded.
type DomainName struct {
	Name             string
	ARN              string
	Status           string
	EndpointTypes    []string
	RegionalDomain   string
	RegionalZoneID   string
	CertificateARNs  []string
	SecurityPolicy   string
	APIMappingSelect string
	Tags             map[string]string
	Mappings         []Mapping
}

// Mapping is custom-domain routing metadata for an API Gateway v2 API mapping.
type Mapping struct {
	Domain string
	ID     string
	Key    string
	APIID  string
	Stage  string
}

// VPCLink is the metadata-only scanner view of an API Gateway v2 VPC link used
// by private integrations. It records the link id, name, status, subnet ids,
// and security group ids.
type VPCLink struct {
	ID               string
	Name             string
	Status           string
	StatusMessage    string
	Version          string
	SubnetIDs        []string
	SecurityGroupIDs []string
	CreatedDate      time.Time
	Tags             map[string]string
}
