// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elb

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Classic (v1) Elastic Load Balancer resource and relationship
// facts for one claimed account and region.
type Scanner struct {
	// Client is the Classic ELB read surface. It must be non-nil.
	Client Client
}

// Scan observes Classic ELB topology through the configured client. It emits one
// aws_resource fact per load balancer and one aws_relationship fact per reported
// registered instance, subnet, security group, VPC, and HTTPS/SSL listener
// certificate.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("elb scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceELB:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceELB
	default:
		return nil, fmt.Errorf("elb scanner received service_kind %q", boundary.ServiceKind)
	}

	loadBalancers, err := s.Client.ListLoadBalancers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list classic load balancers: %w", err)
	}
	var envelopes []facts.Envelope
	for _, loadBalancer := range loadBalancers {
		loadBalancerEnvelopes, err := loadBalancerEnvelopes(boundary, loadBalancer)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, loadBalancerEnvelopes...)
	}
	return envelopes, nil
}

// loadBalancerEnvelopes builds the resource fact and every relationship fact for
// one Classic load balancer.
func loadBalancerEnvelopes(boundary awscloud.Boundary, loadBalancer LoadBalancer) ([]facts.Envelope, error) {
	loadBalancerARN := loadBalancerARN(boundary, loadBalancer.Name)
	resource, err := awscloud.NewResourceEnvelope(loadBalancerObservation(boundary, loadBalancer, loadBalancerARN))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range loadBalancerRelationships(boundary, loadBalancer, loadBalancerARN) {
		relationship, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, relationship)
	}
	return envelopes, nil
}

// loadBalancerObservation builds the aws_resource observation for a Classic load
// balancer. The synthesized ARN is the resource id and correlation anchor so
// downstream ARN-equality joins resolve to this node.
func loadBalancerObservation(
	boundary awscloud.Boundary,
	loadBalancer LoadBalancer,
	loadBalancerARN string,
) awscloud.ResourceObservation {
	name := strings.TrimSpace(loadBalancer.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          loadBalancerARN,
		ResourceID:   loadBalancerARN,
		ResourceType: awscloud.ResourceTypeELBLoadBalancer,
		Name:         name,
		Tags:         loadBalancer.Tags,
		Attributes: map[string]any{
			"availability_zones":                cloneStrings(loadBalancer.AvailabilityZones),
			"canonical_hosted_zone_name":        strings.TrimSpace(loadBalancer.CanonicalHostedZoneName),
			"canonical_hosted_zone_name_id":     strings.TrimSpace(loadBalancer.CanonicalHostedZoneNameID),
			"created_at":                        timeOrNil(loadBalancer.CreatedAt),
			"dns_name":                          strings.TrimSpace(loadBalancer.DNSName),
			"health_check":                      healthCheckMap(loadBalancer.HealthCheck),
			"instance_count":                    len(nonEmptyStrings(loadBalancer.InstanceIDs)),
			"listeners":                         listenerMaps(loadBalancer.Listeners),
			"scheme":                            strings.TrimSpace(loadBalancer.Scheme),
			"security_groups":                   cloneStrings(loadBalancer.SecurityGroups),
			"source_security_group_name":        strings.TrimSpace(loadBalancer.SourceSecurityGroupName),
			"source_security_group_owner_alias": strings.TrimSpace(loadBalancer.SourceSecurityGroupOwnerAlias),
			"subnets":                           cloneStrings(loadBalancer.Subnets),
			"vpc_id":                            strings.TrimSpace(loadBalancer.VPCID),
		},
		CorrelationAnchors: []string{loadBalancerARN, name, strings.TrimSpace(loadBalancer.DNSName)},
		SourceRecordID:     loadBalancerARN,
	}
}

// listenerMaps converts reported listeners into attribute maps. Certificate ARNs
// are kept (they are public identifiers) but never the certificate body.
func listenerMaps(listeners []Listener) []map[string]any {
	if len(listeners) == 0 {
		return nil
	}
	output := make([]map[string]any, 0, len(listeners))
	for _, listener := range listeners {
		output = append(output, map[string]any{
			"instance_port":      listener.InstancePort,
			"instance_protocol":  strings.TrimSpace(listener.InstanceProtocol),
			"load_balancer_port": listener.LoadBalancerPort,
			"protocol":           strings.TrimSpace(listener.Protocol),
			"ssl_certificate_id": strings.TrimSpace(listener.SSLCertificateID),
		})
	}
	return output
}

// healthCheckMap converts the reported health-check configuration into an
// attribute map. It carries configuration only, never live instance health.
func healthCheckMap(check HealthCheck) map[string]any {
	return map[string]any{
		"healthy_threshold":   check.HealthyThreshold,
		"interval_seconds":    check.IntervalSeconds,
		"target":              strings.TrimSpace(check.Target),
		"timeout_seconds":     check.TimeoutSeconds,
		"unhealthy_threshold": check.UnhealthyThreshold,
	}
}

// timeOrNil returns the UTC time, or nil when the time is the zero value, so an
// unreported timestamp serializes as null rather than the Go zero time.
func timeOrNil(input time.Time) any {
	if input.IsZero() {
		return nil
	}
	return input.UTC()
}
