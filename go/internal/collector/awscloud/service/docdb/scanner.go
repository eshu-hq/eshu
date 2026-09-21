// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docdb

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon DocumentDB metadata facts for one claimed account and
// region. It never connects to a database, reads documents or collections,
// reads snapshot contents, persists master user passwords or secrets, or
// reads cluster parameter values.
type Scanner struct {
	Client Client
}

// Scan observes DocumentDB clusters, cluster instances, cluster parameter
// groups, cluster snapshots, subnet groups, global clusters, and event
// subscriptions, plus the direct dependency relationships DocumentDB reports.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("docdb scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceDocDB:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceDocDB
	default:
		return nil, fmt.Errorf("docdb scanner received service_kind %q", boundary.ServiceKind)
	}

	clusters, err := s.Client.ListDBClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DocumentDB DB clusters: %w", err)
	}
	instances, err := s.Client.ListClusterInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DocumentDB cluster instances: %w", err)
	}
	parameterGroups, err := s.Client.ListClusterParameterGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DocumentDB cluster parameter groups: %w", err)
	}
	snapshots, err := s.Client.ListClusterSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DocumentDB cluster snapshots: %w", err)
	}
	subnetGroups, err := s.Client.ListSubnetGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DocumentDB subnet groups: %w", err)
	}
	globalClusters, err := s.Client.ListGlobalClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DocumentDB global clusters: %w", err)
	}
	eventSubscriptions, err := s.Client.ListEventSubscriptions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DocumentDB event subscriptions: %w", err)
	}

	subnets := subnetGroupIdentityMap(subnetGroups)
	clusterIDs := clusterIdentityMap(clusters)
	memberships := clusterMembershipMap(clusters)

	var envelopes []facts.Envelope
	envelopes, err = appendClusters(envelopes, boundary, clusters, subnets)
	if err != nil {
		return nil, err
	}
	envelopes, err = appendInstances(envelopes, boundary, instances, clusterIDs, memberships)
	if err != nil {
		return nil, err
	}
	envelopes, err = appendResources(envelopes, boundary, parameterGroups, snapshots, subnetGroups, eventSubscriptions)
	if err != nil {
		return nil, err
	}
	envelopes, err = appendGlobalClusters(envelopes, boundary, globalClusters)
	if err != nil {
		return nil, err
	}
	return envelopes, nil
}

func appendClusters(
	envelopes []facts.Envelope,
	boundary awscloud.Boundary,
	clusters []DBCluster,
	subnets map[string]subnetGroupIdentity,
) ([]facts.Envelope, error) {
	for _, cluster := range clusters {
		resource, err := awscloud.NewResourceEnvelope(clusterObservation(boundary, cluster))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, relationship := range clusterRelationships(boundary, cluster, subnets) {
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

func appendInstances(
	envelopes []facts.Envelope,
	boundary awscloud.Boundary,
	instances []ClusterInstance,
	clusterIDs map[string]string,
	memberships map[string]clusterMembership,
) ([]facts.Envelope, error) {
	for _, instance := range instances {
		resource, err := awscloud.NewResourceEnvelope(instanceObservation(boundary, instance))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, relationship := range instanceRelationships(boundary, instance, clusterIDs, memberships) {
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

func appendResources(
	envelopes []facts.Envelope,
	boundary awscloud.Boundary,
	parameterGroups []ClusterParameterGroup,
	snapshots []ClusterSnapshot,
	subnetGroups []SubnetGroup,
	eventSubscriptions []EventSubscription,
) ([]facts.Envelope, error) {
	observations := make([]awscloud.ResourceObservation, 0,
		len(parameterGroups)+len(snapshots)+len(subnetGroups)+len(eventSubscriptions))
	for _, group := range parameterGroups {
		observations = append(observations, parameterGroupObservation(boundary, group))
	}
	for _, snapshot := range snapshots {
		observations = append(observations, snapshotObservation(boundary, snapshot))
	}
	for _, subnetGroup := range subnetGroups {
		observations = append(observations, subnetGroupObservation(boundary, subnetGroup))
	}
	for _, subscription := range eventSubscriptions {
		observations = append(observations, eventSubscriptionObservation(boundary, subscription))
	}
	for _, observation := range observations {
		resource, err := awscloud.NewResourceEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	return envelopes, nil
}

func appendGlobalClusters(
	envelopes []facts.Envelope,
	boundary awscloud.Boundary,
	globalClusters []GlobalCluster,
) ([]facts.Envelope, error) {
	for _, globalCluster := range globalClusters {
		resource, err := awscloud.NewResourceEnvelope(globalClusterObservation(boundary, globalCluster))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, relationship := range globalClusterRelationships(boundary, globalCluster) {
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

func clusterObservation(boundary awscloud.Boundary, cluster DBCluster) awscloud.ResourceObservation {
	clusterARN := strings.TrimSpace(cluster.ARN)
	identifier := strings.TrimSpace(cluster.Identifier)
	resourceID := firstNonEmpty(clusterARN, cluster.ResourceID, identifier)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          clusterARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDocDBCluster,
		Name:         identifier,
		State:        strings.TrimSpace(cluster.Status),
		Tags:         cloneStringMap(cluster.Tags),
		Attributes: map[string]any{
			"cluster_resource_id":             strings.TrimSpace(cluster.ResourceID),
			"engine":                          strings.TrimSpace(cluster.Engine),
			"engine_version":                  strings.TrimSpace(cluster.EngineVersion),
			"endpoint_address":                strings.TrimSpace(cluster.EndpointAddress),
			"reader_endpoint_address":         strings.TrimSpace(cluster.ReaderEndpointAddress),
			"hosted_zone_id":                  strings.TrimSpace(cluster.HostedZoneID),
			"port":                            cluster.Port,
			"multi_az":                        cluster.MultiAZ,
			"storage_encrypted":               cluster.StorageEncrypted,
			"kms_key_id":                      strings.TrimSpace(cluster.KMSKeyID),
			"deletion_protection":             cluster.DeletionProtection,
			"backup_retention_period":         cluster.BackupRetentionPeriod,
			"db_subnet_group_name":            strings.TrimSpace(cluster.DBSubnetGroupName),
			"vpc_security_group_ids":          cloneStrings(cluster.VPCSecurityGroupIDs),
			"member_instance_ids":             clusterMemberIDs(cluster.Members),
			"writer_instance_ids":             writerInstanceIDs(cluster.Members),
			"db_cluster_parameter_group":      strings.TrimSpace(cluster.ParameterGroup),
			"enabled_cloudwatch_logs_exports": cloneStrings(cluster.EnabledCloudwatchLogsExports),
			"associated_role_arns":            cloneStrings(cluster.AssociatedRoleARNs),
		},
		CorrelationAnchors: []string{
			clusterARN,
			identifier,
			strings.TrimSpace(cluster.ResourceID),
			strings.TrimSpace(cluster.EndpointAddress),
			strings.TrimSpace(cluster.ReaderEndpointAddress),
		},
		SourceRecordID: resourceID,
	}
}

func instanceObservation(boundary awscloud.Boundary, instance ClusterInstance) awscloud.ResourceObservation {
	instanceARN := strings.TrimSpace(instance.ARN)
	identifier := strings.TrimSpace(instance.Identifier)
	resourceID := firstNonEmpty(instanceARN, instance.ResourceID, identifier)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          instanceARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDocDBClusterInstance,
		Name:         identifier,
		State:        strings.TrimSpace(instance.Status),
		Tags:         cloneStringMap(instance.Tags),
		Attributes: map[string]any{
			"dbi_resource_id":    strings.TrimSpace(instance.ResourceID),
			"class":              strings.TrimSpace(instance.Class),
			"engine":             strings.TrimSpace(instance.Engine),
			"engine_version":     strings.TrimSpace(instance.EngineVersion),
			"endpoint_address":   strings.TrimSpace(instance.EndpointAddress),
			"endpoint_port":      instance.EndpointPort,
			"hosted_zone_id":     strings.TrimSpace(instance.HostedZoneID),
			"availability_zone":  strings.TrimSpace(instance.AvailabilityZone),
			"storage_encrypted":  instance.StorageEncrypted,
			"kms_key_id":         strings.TrimSpace(instance.KMSKeyID),
			"cluster_identifier": strings.TrimSpace(instance.ClusterIdentifier),
			"promotion_tier":     instance.PromotionTier,
		},
		CorrelationAnchors: []string{
			instanceARN,
			identifier,
			strings.TrimSpace(instance.ResourceID),
			strings.TrimSpace(instance.EndpointAddress),
		},
		SourceRecordID: resourceID,
	}
}

func parameterGroupObservation(boundary awscloud.Boundary, group ClusterParameterGroup) awscloud.ResourceObservation {
	groupARN := strings.TrimSpace(group.ARN)
	name := strings.TrimSpace(group.Name)
	resourceID := firstNonEmpty(groupARN, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          groupARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDocDBClusterParameterGroup,
		Name:         name,
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"family":          strings.TrimSpace(group.Family),
			"description":     strings.TrimSpace(group.Description),
			"parameter_count": group.ParameterCount,
		},
		CorrelationAnchors: []string{groupARN, name},
		SourceRecordID:     resourceID,
	}
}

func snapshotObservation(boundary awscloud.Boundary, snapshot ClusterSnapshot) awscloud.ResourceObservation {
	snapshotARN := strings.TrimSpace(snapshot.ARN)
	identifier := strings.TrimSpace(snapshot.Identifier)
	resourceID := firstNonEmpty(snapshotARN, identifier)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          snapshotARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDocDBClusterSnapshot,
		Name:         identifier,
		State:        strings.TrimSpace(snapshot.Status),
		Tags:         cloneStringMap(snapshot.Tags),
		Attributes: map[string]any{
			"cluster_identifier": strings.TrimSpace(snapshot.ClusterIdentifier),
			"engine":             strings.TrimSpace(snapshot.Engine),
			"engine_version":     strings.TrimSpace(snapshot.EngineVersion),
			"snapshot_type":      strings.TrimSpace(snapshot.SnapshotType),
			"storage_encrypted":  snapshot.StorageEncrypted,
			"kms_key_id":         strings.TrimSpace(snapshot.KMSKeyID),
			"vpc_id":             strings.TrimSpace(snapshot.VPCID),
		},
		CorrelationAnchors: []string{snapshotARN, identifier},
		SourceRecordID:     resourceID,
	}
}

func subnetGroupObservation(boundary awscloud.Boundary, subnetGroup SubnetGroup) awscloud.ResourceObservation {
	subnetGroupARN := strings.TrimSpace(subnetGroup.ARN)
	name := strings.TrimSpace(subnetGroup.Name)
	resourceID := firstNonEmpty(subnetGroupARN, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          subnetGroupARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDocDBSubnetGroup,
		Name:         name,
		State:        strings.TrimSpace(subnetGroup.Status),
		Tags:         cloneStringMap(subnetGroup.Tags),
		Attributes: map[string]any{
			"description": strings.TrimSpace(subnetGroup.Description),
			"vpc_id":      strings.TrimSpace(subnetGroup.VPCID),
			"subnet_ids":  cloneStrings(subnetGroup.SubnetIDs),
		},
		CorrelationAnchors: []string{subnetGroupARN, name},
		SourceRecordID:     resourceID,
	}
}

func globalClusterObservation(boundary awscloud.Boundary, globalCluster GlobalCluster) awscloud.ResourceObservation {
	globalARN := strings.TrimSpace(globalCluster.ARN)
	identifier := strings.TrimSpace(globalCluster.Identifier)
	resourceID := firstNonEmpty(globalARN, globalCluster.ResourceID, identifier)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          globalARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDocDBGlobalCluster,
		Name:         identifier,
		State:        strings.TrimSpace(globalCluster.Status),
		Tags:         cloneStringMap(globalCluster.Tags),
		Attributes: map[string]any{
			"global_cluster_resource_id": strings.TrimSpace(globalCluster.ResourceID),
			"engine":                     strings.TrimSpace(globalCluster.Engine),
			"engine_version":             strings.TrimSpace(globalCluster.EngineVersion),
			"storage_encrypted":          globalCluster.StorageEncrypted,
			"deletion_protection":        globalCluster.DeletionProtection,
			"member_cluster_arns":        globalMemberClusterARNs(globalCluster.Members),
		},
		CorrelationAnchors: []string{globalARN, identifier, strings.TrimSpace(globalCluster.ResourceID)},
		SourceRecordID:     resourceID,
	}
}

func eventSubscriptionObservation(boundary awscloud.Boundary, subscription EventSubscription) awscloud.ResourceObservation {
	subscriptionARN := strings.TrimSpace(subscription.ARN)
	name := strings.TrimSpace(subscription.Name)
	resourceID := firstNonEmpty(subscriptionARN, name)
	state := "disabled"
	if subscription.Enabled {
		state = "enabled"
	}
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          subscriptionARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeDocDBEventSubscription,
		Name:         name,
		State:        firstNonEmpty(strings.TrimSpace(subscription.Status), state),
		Tags:         cloneStringMap(subscription.Tags),
		Attributes: map[string]any{
			"customer_aws_id":  strings.TrimSpace(subscription.CustomerAWSID),
			"enabled":          subscription.Enabled,
			"source_type":      strings.TrimSpace(subscription.SourceType),
			"sns_topic_arn":    strings.TrimSpace(subscription.SNSTopicARN),
			"source_ids":       cloneStrings(subscription.SourceIDs),
			"event_categories": cloneStrings(subscription.EventCategories),
		},
		CorrelationAnchors: []string{subscriptionARN, name},
		SourceRecordID:     resourceID,
	}
}
