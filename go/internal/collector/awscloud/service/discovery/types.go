// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicediscovery

import (
	"context"
	"time"
)

// Client is the AWS Cloud Map (Service Discovery) read surface consumed by
// Scanner. Runtime adapters MUST translate AWS SDK responses into these
// scanner-owned metadata records.
//
// The interface is read-only by construction. It exposes one method that
// returns the whole Cloud Map inventory already resolved: every namespace with
// its services attached. The adapter owns pagination and the per-namespace
// service fan-out so the scanner sees a flat, metadata-only view. The interface
// deliberately excludes every Cloud Map mutation API (Create/Update/Delete for
// namespaces and services, RegisterInstance, DeregisterInstance,
// UpdateInstanceCustomHealthStatus, TagResource, UntagResource) and the
// instance discovery/read APIs (DiscoverInstances, DiscoverInstancesRevision,
// GetInstance, ListInstances, GetInstancesHealthStatus) that expose
// instance attribute maps. A reflection test on the adapter asserts the
// exclusion so a future SDK change cannot smuggle one in.
type Client interface {
	// ListNamespaceInventory returns every Cloud Map namespace visible to the
	// configured credentials, each with its services already resolved to
	// metadata-only records. Service instance counts are taken from the Cloud
	// Map service summary; instance attribute maps are never read.
	ListNamespaceInventory(context.Context) ([]Namespace, error)
}

// Namespace is the scanner-owned representation of one Cloud Map namespace and
// its services. It contains metadata only.
type Namespace struct {
	// ID is the Cloud Map namespace id. It is the join key services use to
	// reference their parent namespace.
	ID string
	// ARN is the namespace ARN Cloud Map reports.
	ARN string
	// Name is the namespace name. For a DNS namespace this is the DNS domain
	// name; for an HTTP namespace it is the HTTP namespace name.
	Name string
	// Type is the namespace type Cloud Map reports: DNS_PUBLIC, DNS_PRIVATE, or
	// HTTP.
	Type string
	// Description is the optional namespace description Cloud Map reports.
	Description string
	// ServiceCount is the number of services in the namespace Cloud Map reports.
	ServiceCount int32
	// HostedZoneID is the Route 53 hosted-zone id backing a DNS namespace, when
	// Cloud Map reports one. It is the bare zone id (for example Z123); the
	// relationship builder prepends the "/hostedzone/" prefix to match the
	// route53 scanner resource id. HTTP namespaces report no hosted zone.
	HostedZoneID string
	// HTTPName is the HTTP namespace discovery name Cloud Map reports for an HTTP
	// namespace, when set.
	HTTPName string
	// CreatedAt is the Cloud Map-reported creation time.
	CreatedAt time.Time
	// Tags carries Cloud Map resource tags as raw evidence. Do not infer
	// environment, owner, workload, or deployable-unit truth from tags here.
	Tags map[string]string

	// Services are the services that reside in this namespace.
	Services []Service
}

// Service is the scanner-owned representation of one Cloud Map service. It
// contains metadata only and never carries instance attribute maps.
type Service struct {
	// ID is the Cloud Map service id.
	ID string
	// ARN is the service ARN Cloud Map reports.
	ARN string
	// Name is the service name.
	Name string
	// NamespaceID is the id of the parent namespace Cloud Map reports for the
	// service.
	NamespaceID string
	// NamespaceName is the parent namespace name. It forms the first half of the
	// "namespaceName/serviceName" resource id the App Mesh virtual-node service
	// discovery edge joins.
	NamespaceName string
	// Description is the optional service description Cloud Map reports.
	Description string
	// InstanceCount is the number of instances registered with the service Cloud
	// Map reports on the service summary. The scanner records the count only and
	// never reads instance attribute maps.
	InstanceCount int32
	// DNSRoutingPolicy is the DnsConfig routing policy Cloud Map reports
	// (MULTIVALUE or WEIGHTED), when the service has a DNS config.
	DNSRoutingPolicy string
	// DNSRecords summarizes the DnsConfig record types and TTLs Cloud Map creates
	// when an instance registers. It carries no instance values.
	DNSRecords []DNSRecord
	// CreatedAt is the Cloud Map-reported creation time.
	CreatedAt time.Time
	// Tags carries Cloud Map resource tags as raw evidence. Do not infer
	// environment, owner, workload, or deployable-unit truth from tags here.
	Tags map[string]string
}

// DNSRecord summarizes one Cloud Map DnsConfig record entry. It records the DNS
// record type and TTL only; it never carries an instance value or attribute.
type DNSRecord struct {
	// Type is the DNS record type Cloud Map creates for the service (for example
	// A, AAAA, CNAME, SRV).
	Type string
	// TTL is the DNS record time-to-live in seconds Cloud Map reports, when set.
	TTL *int64
}
