// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package servicediscovery

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// namespaceRelationships emits the namespace-to-Route53-hosted-zone edge for a
// DNS namespace. The target keys on the "/hostedzone/<id>" resource id the
// route53 scanner emits so the edge joins that scanner's hosted-zone resource.
// HTTP namespaces report no hosted zone and emit no edge. Cloud Map does not
// report the VPC association for a private DNS namespace; the VPC is reached
// transitively through the private Route 53 hosted zone, which the route53
// scanner owns.
func namespaceRelationships(boundary awscloud.Boundary, namespace Namespace) []awscloud.RelationshipObservation {
	namespaceID := strings.TrimSpace(namespace.ID)
	hostedZoneID := strings.TrimSpace(namespace.HostedZoneID)
	if namespaceID == "" || hostedZoneID == "" {
		return nil
	}
	targetResourceID := "/hostedzone/" + hostedZoneID
	return []awscloud.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCloudMapNamespaceInHostedZone,
		SourceResourceID: namespaceID,
		SourceARN:        strings.TrimSpace(namespace.ARN),
		TargetResourceID: targetResourceID,
		TargetType:       awscloud.ResourceTypeRoute53HostedZone,
		Attributes:       map[string]any{"hosted_zone_id": hostedZoneID},
		SourceRecordID:   namespaceID + "->" + targetResourceID,
	}}
}

// serviceRelationships emits the service-to-namespace edge. The service is
// keyed by its "namespaceName/serviceName" resource id; the namespace target
// keys on the Cloud Map namespace id so the edge joins the namespace resource.
func serviceRelationships(boundary awscloud.Boundary, service Service) []awscloud.RelationshipObservation {
	resourceID := serviceResourceID(service)
	namespaceID := strings.TrimSpace(service.NamespaceID)
	if resourceID == "" || namespaceID == "" {
		return nil
	}
	return []awscloud.RelationshipObservation{{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCloudMapServiceInNamespace,
		SourceResourceID: resourceID,
		SourceARN:        strings.TrimSpace(service.ARN),
		TargetResourceID: namespaceID,
		TargetType:       awscloud.ResourceTypeCloudMapNamespace,
		SourceRecordID:   resourceID + "->" + namespaceID,
	}}
}
