// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awslightsailtypes "github.com/aws/aws-sdk-go-v2/service/lightsail/types"

	lightsailservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/lightsail"
)

func mapInstance(instance awslightsailtypes.Instance) lightsailservice.Instance {
	mapped := lightsailservice.Instance{
		ARN:              strings.TrimSpace(aws.ToString(instance.Arn)),
		Name:             strings.TrimSpace(aws.ToString(instance.Name)),
		BlueprintID:      strings.TrimSpace(aws.ToString(instance.BlueprintId)),
		BlueprintName:    strings.TrimSpace(aws.ToString(instance.BlueprintName)),
		BundleID:         strings.TrimSpace(aws.ToString(instance.BundleId)),
		PublicIPAddress:  strings.TrimSpace(aws.ToString(instance.PublicIpAddress)),
		PrivateIPAddress: strings.TrimSpace(aws.ToString(instance.PrivateIpAddress)),
		IPv6Addresses:    cloneStringSlice(instance.Ipv6Addresses),
		IsStaticIP:       aws.ToBool(instance.IsStaticIp),
		SSHKeyName:       strings.TrimSpace(aws.ToString(instance.SshKeyName)),
		CreatedAt:        aws.ToTime(instance.CreatedAt),
		Tags:             tagMap(instance.Tags),
	}
	if instance.State != nil {
		mapped.State = strings.TrimSpace(aws.ToString(instance.State.Name))
	}
	if instance.Location != nil {
		mapped.AvailabilityZone = strings.TrimSpace(aws.ToString(instance.Location.AvailabilityZone))
		mapped.RegionName = strings.TrimSpace(string(instance.Location.RegionName))
	}
	return mapped
}

func mapDatabase(database awslightsailtypes.RelationalDatabase) lightsailservice.Database {
	mapped := lightsailservice.Database{
		ARN:                strings.TrimSpace(aws.ToString(database.Arn)),
		Name:               strings.TrimSpace(aws.ToString(database.Name)),
		Engine:             strings.TrimSpace(aws.ToString(database.Engine)),
		EngineVersion:      strings.TrimSpace(aws.ToString(database.EngineVersion)),
		State:              strings.TrimSpace(aws.ToString(database.State)),
		BlueprintID:        strings.TrimSpace(aws.ToString(database.RelationalDatabaseBlueprintId)),
		BundleID:           strings.TrimSpace(aws.ToString(database.RelationalDatabaseBundleId)),
		MasterDatabaseName: strings.TrimSpace(aws.ToString(database.MasterDatabaseName)),
		MasterUsername:     strings.TrimSpace(aws.ToString(database.MasterUsername)),
		PubliclyAccessible: aws.ToBool(database.PubliclyAccessible),
		BackupRetention:    aws.ToBool(database.BackupRetentionEnabled),
		SecondaryAZ:        strings.TrimSpace(aws.ToString(database.SecondaryAvailabilityZone)),
		CreatedAt:          aws.ToTime(database.CreatedAt),
		Tags:               tagMap(database.Tags),
	}
	if database.MasterEndpoint != nil {
		mapped.EndpointAddress = strings.TrimSpace(aws.ToString(database.MasterEndpoint.Address))
		mapped.EndpointPort = database.MasterEndpoint.Port
	}
	if database.Location != nil {
		mapped.AvailabilityZone = strings.TrimSpace(aws.ToString(database.Location.AvailabilityZone))
		mapped.RegionName = strings.TrimSpace(string(database.Location.RegionName))
	}
	return mapped
}

func mapLoadBalancer(loadBalancer awslightsailtypes.LoadBalancer) lightsailservice.LoadBalancer {
	mapped := lightsailservice.LoadBalancer{
		ARN:              strings.TrimSpace(aws.ToString(loadBalancer.Arn)),
		Name:             strings.TrimSpace(aws.ToString(loadBalancer.Name)),
		State:            strings.TrimSpace(string(loadBalancer.State)),
		DNSName:          strings.TrimSpace(aws.ToString(loadBalancer.DnsName)),
		Protocol:         strings.TrimSpace(string(loadBalancer.Protocol)),
		InstancePort:     loadBalancer.InstancePort,
		PublicPorts:      cloneInt32Slice(loadBalancer.PublicPorts),
		IPAddressType:    strings.TrimSpace(string(loadBalancer.IpAddressType)),
		HTTPSRedirection: aws.ToBool(loadBalancer.HttpsRedirectionEnabled),
		CreatedAt:        aws.ToTime(loadBalancer.CreatedAt),
		Attached:         instanceHealthNames(loadBalancer.InstanceHealthSummary),
		Tags:             tagMap(loadBalancer.Tags),
	}
	if loadBalancer.Location != nil {
		mapped.AvailabilityZone = strings.TrimSpace(aws.ToString(loadBalancer.Location.AvailabilityZone))
		mapped.RegionName = strings.TrimSpace(string(loadBalancer.Location.RegionName))
	}
	return mapped
}

func mapDisk(disk awslightsailtypes.Disk) lightsailservice.Disk {
	mapped := lightsailservice.Disk{
		ARN:          strings.TrimSpace(aws.ToString(disk.Arn)),
		Name:         strings.TrimSpace(aws.ToString(disk.Name)),
		State:        strings.TrimSpace(string(disk.State)),
		Path:         strings.TrimSpace(aws.ToString(disk.Path)),
		SizeInGb:     disk.SizeInGb,
		IOPS:         disk.Iops,
		IsAttached:   aws.ToBool(disk.IsAttached),
		IsSystemDisk: aws.ToBool(disk.IsSystemDisk),
		AttachedTo:   strings.TrimSpace(aws.ToString(disk.AttachedTo)),
		CreatedAt:    aws.ToTime(disk.CreatedAt),
		Tags:         tagMap(disk.Tags),
	}
	if disk.Location != nil {
		mapped.AvailabilityZone = strings.TrimSpace(aws.ToString(disk.Location.AvailabilityZone))
		mapped.RegionName = strings.TrimSpace(string(disk.Location.RegionName))
	}
	return mapped
}

func mapStaticIP(staticIP awslightsailtypes.StaticIp) lightsailservice.StaticIP {
	mapped := lightsailservice.StaticIP{
		ARN:        strings.TrimSpace(aws.ToString(staticIP.Arn)),
		Name:       strings.TrimSpace(aws.ToString(staticIP.Name)),
		IPAddress:  strings.TrimSpace(aws.ToString(staticIP.IpAddress)),
		IsAttached: aws.ToBool(staticIP.IsAttached),
		AttachedTo: strings.TrimSpace(aws.ToString(staticIP.AttachedTo)),
		CreatedAt:  aws.ToTime(staticIP.CreatedAt),
	}
	if staticIP.Location != nil {
		mapped.AvailabilityZone = strings.TrimSpace(aws.ToString(staticIP.Location.AvailabilityZone))
		mapped.RegionName = strings.TrimSpace(string(staticIP.Location.RegionName))
	}
	return mapped
}

// instanceHealthNames returns the bare instance names reported in a load
// balancer's instance-health summary, preserving the AWS-reported order and
// dropping blank entries. The relationship layer keys load-balancer-to-instance
// edges on these names, which match the instance node resource_id.
func instanceHealthNames(summaries []awslightsailtypes.InstanceHealthSummary) []string {
	if len(summaries) == 0 {
		return nil
	}
	names := make([]string, 0, len(summaries))
	for _, summary := range summaries {
		if name := strings.TrimSpace(aws.ToString(summary.InstanceName)); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// tagMap converts AWS Lightsail tags into a scanner-owned string map, dropping
// blank keys. Tags are raw AWS evidence; this package does not infer owner,
// environment, or workload truth from them.
func tagMap(tags []awslightsailtypes.Tag) map[string]string {
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

func cloneStringSlice(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneInt32Slice(input []int32) []int32 {
	if len(input) == 0 {
		return nil
	}
	output := make([]int32, len(input))
	copy(output, input)
	return output
}
