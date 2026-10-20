// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lightsail

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

// loadBalancerInstanceRelationships builds one edge per distinct Lightsail
// instance AWS reports attached to the load balancer. The edge is keyed on the
// bare load balancer name (the load balancer node resource_id) as the source
// and the bare instance name (the instance node resource_id) as the target, so
// both ends join the Lightsail nodes this scanner publishes rather than
// dangling. Duplicate instance names within a single load balancer collapse to
// one edge, and the iteration order follows the AWS-reported list rather than a
// re-sort so a stable instance name, not a list index, is the identity.
func loadBalancerInstanceRelationships(boundary awscloud.Boundary, lb LoadBalancer) []awscloud.RelationshipObservation {
	source := strings.TrimSpace(lb.Name)
	if source == "" || len(lb.Attached) == 0 {
		return nil
	}
	observations := make([]awscloud.RelationshipObservation, 0, len(lb.Attached))
	seen := make(map[string]struct{}, len(lb.Attached))
	for _, instanceName := range lb.Attached {
		target := strings.TrimSpace(instanceName)
		if target == "" {
			continue
		}
		if _, ok := seen[target]; ok {
			continue
		}
		seen[target] = struct{}{}
		observations = append(observations, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipLightsailLoadBalancerTargetsInstance,
			SourceResourceID: source,
			TargetResourceID: target,
			TargetType:       awscloud.ResourceTypeLightsailInstance,
			SourceRecordID:   source + "->" + awscloud.RelationshipLightsailLoadBalancerTargetsInstance + ":" + target,
		})
	}
	if len(observations) == 0 {
		return nil
	}
	return observations
}

// instanceDiskRelationship builds the instance-to-disk edge when AWS reports the
// disk attached to a Lightsail instance. The edge is keyed on the bare instance
// name (`disk.AttachedTo`, the instance node resource_id) as the source and the
// bare disk name (the disk node resource_id) as the target, so both ends join
// the Lightsail nodes this scanner publishes. It returns nil when the disk is
// unattached or the attachment target is not an instance name.
func instanceDiskRelationship(boundary awscloud.Boundary, disk Disk) *awscloud.RelationshipObservation {
	diskName := strings.TrimSpace(disk.Name)
	instanceName := strings.TrimSpace(disk.AttachedTo)
	if diskName == "" || instanceName == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipLightsailInstanceAttachedToDisk,
		SourceResourceID: instanceName,
		TargetResourceID: diskName,
		TargetType:       awscloud.ResourceTypeLightsailDisk,
		SourceRecordID:   instanceName + "->" + awscloud.RelationshipLightsailInstanceAttachedToDisk + ":" + diskName,
	}
}

// instanceStaticIPRelationship builds the instance-to-static-IP edge when AWS
// reports the static IP attached to a Lightsail instance. The edge is keyed on
// the bare instance name (`staticIP.AttachedTo`, the instance node resource_id)
// as the source and the bare static IP name (the static IP node resource_id) as
// the target, so both ends join the Lightsail nodes this scanner publishes. It
// returns nil when the static IP is unattached.
func instanceStaticIPRelationship(boundary awscloud.Boundary, staticIP StaticIP) *awscloud.RelationshipObservation {
	staticIPName := strings.TrimSpace(staticIP.Name)
	instanceName := strings.TrimSpace(staticIP.AttachedTo)
	if staticIPName == "" || instanceName == "" {
		return nil
	}
	return &awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipLightsailInstanceAttachedToStaticIP,
		SourceResourceID: instanceName,
		TargetResourceID: staticIPName,
		TargetType:       awscloud.ResourceTypeLightsailStaticIP,
		SourceRecordID:   instanceName + "->" + awscloud.RelationshipLightsailInstanceAttachedToStaticIP + ":" + staticIPName,
	}
}
