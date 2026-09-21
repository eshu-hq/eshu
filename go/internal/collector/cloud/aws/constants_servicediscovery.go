// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awscloud

const (
	// ServiceServiceDiscovery identifies the AWS Cloud Map (Service Discovery)
	// regional metadata-only scan slice. The scanner covers namespaces and
	// services plus the relationships Cloud Map reports directly. It never calls
	// a Cloud Map mutation API and never reads instance attribute maps, which can
	// carry caller-defined secrets.
	ServiceServiceDiscovery = "servicediscovery"
)

const (
	// ResourceTypeCloudMapNamespace identifies an AWS Cloud Map namespace
	// metadata resource. The fact carries namespace identity (id, ARN, name),
	// the namespace type (DNS_PUBLIC, DNS_PRIVATE, or HTTP), the reported service
	// count, the backing Route 53 hosted-zone id for DNS namespaces, and the HTTP
	// name for HTTP namespaces. Services join to this resource by the namespace
	// id.
	ResourceTypeCloudMapNamespace = "aws_cloud_map_namespace"

	// ResourceTypeCloudMapService identifies an AWS Cloud Map service metadata
	// resource. The fact is keyed by the "namespaceName/serviceName" identity so
	// it resolves the forward-looking App Mesh virtual-node-to-Cloud-Map-service
	// edge, which targets the same "namespace/service" join key. The fact carries
	// the service id and ARN, the parent namespace id and name, the DNS config
	// summary (routing policy and DNS record types/TTLs), and the reported
	// instance count. Instance attribute maps are never read or persisted because
	// they can hold caller-defined secrets.
	ResourceTypeCloudMapService = "aws_cloud_map_service"
)

const (
	// RelationshipCloudMapServiceInNamespace records that a Cloud Map service
	// belongs to a namespace. The target is the parent namespace resource keyed
	// by the Cloud Map namespace id.
	RelationshipCloudMapServiceInNamespace = "cloud_map_service_in_namespace"

	// RelationshipCloudMapNamespaceInHostedZone records that a Cloud Map DNS
	// namespace is backed by a Route 53 hosted zone. The target is the Route 53
	// hosted-zone resource keyed by the "/hostedzone/<id>" resource id the
	// route53 scanner emits, so the edge joins that scanner's hosted-zone
	// resource. HTTP namespaces report no hosted zone and emit no edge. Cloud Map
	// does not report the VPC association for a private DNS namespace; the VPC is
	// reached transitively through the private Route 53 hosted zone, which the
	// route53 scanner owns.
	RelationshipCloudMapNamespaceInHostedZone = "cloud_map_namespace_in_hosted_zone"
)
