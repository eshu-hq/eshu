// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package lightsail

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Lightsail metadata-only facts for one claimed account
// and region. It never creates, deletes, reboots, starts, stops, or snapshots a
// Lightsail resource, and never reads instance access details, default key-pair
// private keys, or database master passwords.
type Scanner struct {
	Client Client
}

// Scan observes Lightsail instances, managed relational databases, load
// balancers, disks, and static IPs through the configured client and emits
// resource facts plus the Lightsail-internal relationship facts
// (load-balancer-to-instance, instance-to-disk, instance-to-static-IP). Every
// node resource_id and every relationship join key is the bare Lightsail
// resource name so the internal edges resolve the nodes this scanner publishes.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("lightsail scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceLightsail:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceLightsail
	default:
		return nil, fmt.Errorf("lightsail scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	instances, err := s.Client.ListInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Lightsail instances: %w", err)
	}
	for _, instance := range instances {
		envelope, err := awscloud.NewResourceEnvelope(instanceObservation(boundary, instance))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	databases, err := s.Client.ListDatabases(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Lightsail databases: %w", err)
	}
	for _, database := range databases {
		envelope, err := awscloud.NewResourceEnvelope(databaseObservation(boundary, database))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}

	loadBalancers, err := s.Client.ListLoadBalancers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Lightsail load balancers: %w", err)
	}
	for _, loadBalancer := range loadBalancers {
		next, err := loadBalancerEnvelopes(boundary, loadBalancer)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	disks, err := s.Client.ListDisks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Lightsail disks: %w", err)
	}
	for _, disk := range disks {
		next, err := diskEnvelopes(boundary, disk)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	staticIPs, err := s.Client.ListStaticIPs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Lightsail static IPs: %w", err)
	}
	for _, staticIP := range staticIPs {
		next, err := staticIPEnvelopes(boundary, staticIP)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}

	return envelopes, nil
}

func loadBalancerEnvelopes(boundary awscloud.Boundary, lb LoadBalancer) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(loadBalancerObservation(boundary, lb))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range loadBalancerInstanceRelationships(boundary, lb) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func diskEnvelopes(boundary awscloud.Boundary, disk Disk) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(diskObservation(boundary, disk))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := instanceDiskRelationship(boundary, disk); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func staticIPEnvelopes(boundary awscloud.Boundary, staticIP StaticIP) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(staticIPObservation(boundary, staticIP))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship := instanceStaticIPRelationship(boundary, staticIP); relationship != nil {
		envelope, err := awscloud.NewRelationshipEnvelope(*relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func instanceObservation(boundary awscloud.Boundary, instance Instance) awscloud.ResourceObservation {
	name := strings.TrimSpace(instance.Name)
	arn := strings.TrimSpace(instance.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   name,
		ResourceType: awscloud.ResourceTypeLightsailInstance,
		Name:         name,
		State:        strings.TrimSpace(instance.State),
		Tags:         instance.Tags,
		Attributes: map[string]any{
			"blueprint_id":       strings.TrimSpace(instance.BlueprintID),
			"blueprint_name":     strings.TrimSpace(instance.BlueprintName),
			"bundle_id":          strings.TrimSpace(instance.BundleID),
			"public_ip_address":  strings.TrimSpace(instance.PublicIPAddress),
			"private_ip_address": strings.TrimSpace(instance.PrivateIPAddress),
			"ipv6_addresses":     cloneStringSlice(instance.IPv6Addresses),
			"is_static_ip":       instance.IsStaticIP,
			"availability_zone":  strings.TrimSpace(instance.AvailabilityZone),
			"region_name":        strings.TrimSpace(instance.RegionName),
			"ssh_key_name":       strings.TrimSpace(instance.SSHKeyName),
			"created_at":         timeOrNil(instance.CreatedAt),
		},
		CorrelationAnchors: []string{name, arn},
		SourceRecordID:     name,
	}
}

func databaseObservation(boundary awscloud.Boundary, database Database) awscloud.ResourceObservation {
	name := strings.TrimSpace(database.Name)
	arn := strings.TrimSpace(database.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   name,
		ResourceType: awscloud.ResourceTypeLightsailDatabase,
		Name:         name,
		State:        strings.TrimSpace(database.State),
		Tags:         database.Tags,
		Attributes: map[string]any{
			"engine":                      strings.TrimSpace(database.Engine),
			"engine_version":              strings.TrimSpace(database.EngineVersion),
			"blueprint_id":                strings.TrimSpace(database.BlueprintID),
			"bundle_id":                   strings.TrimSpace(database.BundleID),
			"master_database_name":        strings.TrimSpace(database.MasterDatabaseName),
			"master_username":             strings.TrimSpace(database.MasterUsername),
			"endpoint_address":            strings.TrimSpace(database.EndpointAddress),
			"endpoint_port":               int32OrNil(database.EndpointPort),
			"publicly_accessible":         database.PubliclyAccessible,
			"backup_retention":            database.BackupRetention,
			"availability_zone":           strings.TrimSpace(database.AvailabilityZone),
			"secondary_availability_zone": strings.TrimSpace(database.SecondaryAZ),
			"region_name":                 strings.TrimSpace(database.RegionName),
			"created_at":                  timeOrNil(database.CreatedAt),
		},
		CorrelationAnchors: []string{name, arn},
		SourceRecordID:     name,
	}
}

func loadBalancerObservation(boundary awscloud.Boundary, lb LoadBalancer) awscloud.ResourceObservation {
	name := strings.TrimSpace(lb.Name)
	arn := strings.TrimSpace(lb.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   name,
		ResourceType: awscloud.ResourceTypeLightsailLoadBalancer,
		Name:         name,
		State:        strings.TrimSpace(lb.State),
		Tags:         lb.Tags,
		Attributes: map[string]any{
			"dns_name":           strings.TrimSpace(lb.DNSName),
			"protocol":           strings.TrimSpace(lb.Protocol),
			"instance_port":      int32OrNil(lb.InstancePort),
			"public_ports":       cloneInt32Slice(lb.PublicPorts),
			"ip_address_type":    strings.TrimSpace(lb.IPAddressType),
			"https_redirection":  lb.HTTPSRedirection,
			"availability_zone":  strings.TrimSpace(lb.AvailabilityZone),
			"region_name":        strings.TrimSpace(lb.RegionName),
			"attached_instances": cloneStringSlice(lb.Attached),
			"created_at":         timeOrNil(lb.CreatedAt),
		},
		CorrelationAnchors: []string{name, arn},
		SourceRecordID:     name,
	}
}

func diskObservation(boundary awscloud.Boundary, disk Disk) awscloud.ResourceObservation {
	name := strings.TrimSpace(disk.Name)
	arn := strings.TrimSpace(disk.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   name,
		ResourceType: awscloud.ResourceTypeLightsailDisk,
		Name:         name,
		State:        strings.TrimSpace(disk.State),
		Tags:         disk.Tags,
		Attributes: map[string]any{
			"path":              strings.TrimSpace(disk.Path),
			"size_in_gb":        int32OrNil(disk.SizeInGb),
			"iops":              int32OrNil(disk.IOPS),
			"is_attached":       disk.IsAttached,
			"is_system_disk":    disk.IsSystemDisk,
			"attached_to":       strings.TrimSpace(disk.AttachedTo),
			"availability_zone": strings.TrimSpace(disk.AvailabilityZone),
			"region_name":       strings.TrimSpace(disk.RegionName),
			"created_at":        timeOrNil(disk.CreatedAt),
		},
		CorrelationAnchors: []string{name, arn},
		SourceRecordID:     name,
	}
}

func staticIPObservation(boundary awscloud.Boundary, staticIP StaticIP) awscloud.ResourceObservation {
	name := strings.TrimSpace(staticIP.Name)
	arn := strings.TrimSpace(staticIP.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   name,
		ResourceType: awscloud.ResourceTypeLightsailStaticIP,
		Name:         name,
		Tags:         nil,
		Attributes: map[string]any{
			"ip_address":        strings.TrimSpace(staticIP.IPAddress),
			"is_attached":       staticIP.IsAttached,
			"attached_to":       strings.TrimSpace(staticIP.AttachedTo),
			"availability_zone": strings.TrimSpace(staticIP.AvailabilityZone),
			"region_name":       strings.TrimSpace(staticIP.RegionName),
			"created_at":        timeOrNil(staticIP.CreatedAt),
		},
		CorrelationAnchors: []string{name, arn},
		SourceRecordID:     name,
	}
}
