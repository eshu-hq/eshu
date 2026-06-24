// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awselbtypes "github.com/aws/aws-sdk-go-v2/service/elasticloadbalancing/types"

	elbservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/elb"
)

// mapLoadBalancer converts an AWS SDK LoadBalancerDescription and its tag set
// into a scanner-owned LoadBalancer. The certificate body and private key are
// never read; only the public SSLCertificateId ARN is mapped.
func mapLoadBalancer(
	description awselbtypes.LoadBalancerDescription,
	tags map[string]string,
) elbservice.LoadBalancer {
	value := elbservice.LoadBalancer{
		Name:                      aws.ToString(description.LoadBalancerName),
		DNSName:                   aws.ToString(description.DNSName),
		CanonicalHostedZoneName:   aws.ToString(description.CanonicalHostedZoneName),
		CanonicalHostedZoneNameID: aws.ToString(description.CanonicalHostedZoneNameID),
		Scheme:                    aws.ToString(description.Scheme),
		VPCID:                     aws.ToString(description.VPCId),
		CreatedAt:                 aws.ToTime(description.CreatedTime),
		AvailabilityZones:         cloneStrings(description.AvailabilityZones),
		Subnets:                   cloneStrings(description.Subnets),
		SecurityGroups:            cloneStrings(description.SecurityGroups),
		InstanceIDs:               mapInstances(description.Instances),
		Listeners:                 mapListeners(description.ListenerDescriptions),
		HealthCheck:               mapHealthCheck(description.HealthCheck),
		Tags:                      tags,
	}
	if description.SourceSecurityGroup != nil {
		value.SourceSecurityGroupName = aws.ToString(description.SourceSecurityGroup.GroupName)
		value.SourceSecurityGroupOwnerAlias = aws.ToString(description.SourceSecurityGroup.OwnerAlias)
	}
	return value
}

// mapInstances extracts the registered EC2 instance ids from the reported
// instance list. Live health status is never read here.
func mapInstances(instances []awselbtypes.Instance) []string {
	if len(instances) == 0 {
		return nil
	}
	output := make([]string, 0, len(instances))
	for _, instance := range instances {
		if id := strings.TrimSpace(aws.ToString(instance.InstanceId)); id != "" {
			output = append(output, id)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// mapListeners converts reported listener descriptions into scanner-owned
// listeners.
func mapListeners(descriptions []awselbtypes.ListenerDescription) []elbservice.Listener {
	if len(descriptions) == 0 {
		return nil
	}
	output := make([]elbservice.Listener, 0, len(descriptions))
	for _, description := range descriptions {
		listener := description.Listener
		if listener == nil {
			continue
		}
		output = append(output, elbservice.Listener{
			Protocol:         aws.ToString(listener.Protocol),
			LoadBalancerPort: listener.LoadBalancerPort,
			InstanceProtocol: aws.ToString(listener.InstanceProtocol),
			InstancePort:     aws.ToInt32(listener.InstancePort),
			SSLCertificateID: aws.ToString(listener.SSLCertificateId),
		})
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

// mapHealthCheck converts the reported health-check configuration into the
// scanner-owned shape. It carries configuration only, never live instance
// health.
func mapHealthCheck(check *awselbtypes.HealthCheck) elbservice.HealthCheck {
	if check == nil {
		return elbservice.HealthCheck{}
	}
	return elbservice.HealthCheck{
		Target:             aws.ToString(check.Target),
		IntervalSeconds:    aws.ToInt32(check.Interval),
		TimeoutSeconds:     aws.ToInt32(check.Timeout),
		HealthyThreshold:   aws.ToInt32(check.HealthyThreshold),
		UnhealthyThreshold: aws.ToInt32(check.UnhealthyThreshold),
	}
}

// mapTags converts reported tags into a key/value map, dropping tags with an
// empty key.
func mapTags(tags []awselbtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
