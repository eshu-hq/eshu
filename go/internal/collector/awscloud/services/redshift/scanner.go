// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package redshift

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Redshift metadata facts for one claimed account and
// region. It covers both provisioned Redshift and Redshift Serverless control
// planes. It never opens warehouse connections, runs queries, reads tables,
// reads snapshot contents, or calls any mutation API. Master user passwords
// are never persisted; the scanner deliberately also omits master user names.
type Scanner struct {
	Client Client
}

// Scan observes Redshift clusters, parameter groups, subnet groups,
// snapshots, scheduled actions, and Serverless namespaces and workgroups
// through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("redshift scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceRedshift:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceRedshift
	default:
		return nil, fmt.Errorf("redshift scanner received service_kind %q", boundary.ServiceKind)
	}

	clusters, err := s.Client.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Redshift clusters: %w", err)
	}
	parameterGroups, err := s.Client.ListClusterParameterGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Redshift cluster parameter groups: %w", err)
	}
	subnetGroups, err := s.Client.ListClusterSubnetGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Redshift cluster subnet groups: %w", err)
	}
	snapshots, err := s.Client.ListClusterSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Redshift cluster snapshots: %w", err)
	}
	scheduledActions, err := s.Client.ListScheduledActions(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Redshift scheduled actions: %w", err)
	}
	namespaces, err := s.Client.ListServerlessNamespaces(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Redshift Serverless namespaces: %w", err)
	}
	workgroups, err := s.Client.ListServerlessWorkgroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Redshift Serverless workgroups: %w", err)
	}

	parameterGroupIDs := parameterGroupIdentityMap(parameterGroups)
	subnetGroupIDs := subnetGroupIdentityMap(subnetGroups)
	clusterIDs := clusterIdentityMap(clusters)
	namespaceIDs := namespaceIdentityMap(namespaces)

	var envelopes []facts.Envelope
	for _, group := range parameterGroups {
		resource, err := awscloud.NewResourceEnvelope(parameterGroupObservation(boundary, group))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	for _, group := range subnetGroups {
		grouped, err := subnetGroupEnvelopes(boundary, group)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, grouped...)
	}
	for _, cluster := range clusters {
		clusterEnvelopes, err := clusterEnvelopes(boundary, cluster, parameterGroupIDs, subnetGroupIDs)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, clusterEnvelopes...)
	}
	for _, snapshot := range snapshots {
		snapshotEnvelopes, err := snapshotEnvelopes(boundary, snapshot, clusterIDs)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, snapshotEnvelopes...)
	}
	for _, action := range scheduledActions {
		actionEnvelopes, err := scheduledActionEnvelopes(boundary, action, clusterIDs)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, actionEnvelopes...)
	}
	for _, namespace := range namespaces {
		namespaceEnvelopes, err := serverlessNamespaceEnvelopes(boundary, namespace)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, namespaceEnvelopes...)
	}
	for _, workgroup := range workgroups {
		workgroupEnvelopes, err := serverlessWorkgroupEnvelopes(boundary, workgroup, namespaceIDs)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, workgroupEnvelopes...)
	}
	return envelopes, nil
}

func clusterEnvelopes(
	boundary awscloud.Boundary,
	cluster Cluster,
	parameterGroupIDs map[string]string,
	subnetGroupIDs map[string]string,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(clusterObservation(boundary, cluster))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range clusterRelationships(boundary, cluster, parameterGroupIDs, subnetGroupIDs) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func subnetGroupEnvelopes(
	boundary awscloud.Boundary,
	group ClusterSubnetGroup,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(subnetGroupObservation(boundary, group))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	if relationship, ok := subnetGroupVPCRelationship(boundary, group); ok {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func snapshotEnvelopes(
	boundary awscloud.Boundary,
	snapshot ClusterSnapshot,
	clusterIDs map[string]string,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(snapshotObservation(boundary, snapshot))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range snapshotRelationships(boundary, snapshot, clusterIDs) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func scheduledActionEnvelopes(
	boundary awscloud.Boundary,
	action ScheduledAction,
	clusterIDs map[string]string,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(scheduledActionObservation(boundary, action))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range scheduledActionRelationships(boundary, action, clusterIDs) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func serverlessNamespaceEnvelopes(
	boundary awscloud.Boundary,
	namespace ServerlessNamespace,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(serverlessNamespaceObservation(boundary, namespace))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range serverlessNamespaceRelationships(boundary, namespace) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func serverlessWorkgroupEnvelopes(
	boundary awscloud.Boundary,
	workgroup ServerlessWorkgroup,
	namespaceIDs map[string]string,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(serverlessWorkgroupObservation(boundary, workgroup))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range serverlessWorkgroupRelationships(boundary, workgroup, namespaceIDs) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func clusterObservation(boundary awscloud.Boundary, cluster Cluster) awscloud.ResourceObservation {
	arn := strings.TrimSpace(cluster.ARN)
	identifier := strings.TrimSpace(cluster.Identifier)
	resourceID := firstNonEmpty(arn, identifier)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeRedshiftCluster,
		Name:         identifier,
		State:        strings.TrimSpace(cluster.ClusterStatus),
		Tags:         cloneStringMap(cluster.Tags),
		Attributes:   clusterAttributes(cluster),
		CorrelationAnchors: []string{
			arn,
			identifier,
			strings.TrimSpace(cluster.Endpoint),
		},
		SourceRecordID: resourceID,
	}
}

func clusterAttributes(cluster Cluster) map[string]any {
	return map[string]any{
		"node_type":                           strings.TrimSpace(cluster.NodeType),
		"cluster_availability_status":         strings.TrimSpace(cluster.ClusterAvailabilityStatus),
		"db_name":                             strings.TrimSpace(cluster.DBName),
		"endpoint_address":                    strings.TrimSpace(cluster.Endpoint),
		"endpoint_port":                       cluster.EndpointPort,
		"hosted_zone_id":                      strings.TrimSpace(cluster.HostedZoneID),
		"cluster_create_time":                 timeOrNil(cluster.ClusterCreateTime),
		"automated_snapshot_retention_period": cluster.AutomatedSnapshotRetentionPeriod,
		"manual_snapshot_retention_period":    cluster.ManualSnapshotRetentionPeriod,
		"cluster_security_groups":             cloneStrings(cluster.ClusterSecurityGroups),
		"vpc_security_group_ids":              cloneStrings(cluster.VPCSecurityGroupIDs),
		"cluster_parameter_group_name":        strings.TrimSpace(cluster.ClusterParameterGroup),
		"cluster_subnet_group_name":           strings.TrimSpace(cluster.ClusterSubnetGroupName),
		"vpc_id":                              strings.TrimSpace(cluster.VPCID),
		"availability_zone":                   strings.TrimSpace(cluster.AvailabilityZone),
		"preferred_maintenance_window":        strings.TrimSpace(cluster.PreferredMaintenanceWindow),
		"pending_modified_values_present":     cluster.PendingModifiedValuesPresent,
		"cluster_version":                     strings.TrimSpace(cluster.ClusterVersion),
		"allow_version_upgrade":               cluster.AllowVersionUpgrade,
		"number_of_nodes":                     cluster.NumberOfNodes,
		"publicly_accessible":                 cluster.PubliclyAccessible,
		"encrypted":                           cluster.Encrypted,
		"kms_key_id":                          strings.TrimSpace(cluster.KMSKeyID),
		"enhanced_vpc_routing":                cluster.EnhancedVPCRouting,
		"iam_role_arns":                       cloneStrings(cluster.IAMRoleARNs),
		"maintenance_track_name":              strings.TrimSpace(cluster.MaintenanceTrackName),
		"deferred_maintenance_windows":        cloneStrings(cluster.DeferredMaintenanceWindows),
		"next_maintenance_window_start_time":  timeOrNil(cluster.NextMaintenanceWindowStartTime),
		"availability_zone_relocation_status": strings.TrimSpace(cluster.AvailabilityZoneRelocationStatus),
		"multi_az":                            cluster.MultiAZ,
	}
}

func parameterGroupObservation(
	boundary awscloud.Boundary,
	group ClusterParameterGroup,
) awscloud.ResourceObservation {
	arn := strings.TrimSpace(group.ARN)
	name := strings.TrimSpace(group.Name)
	resourceID := firstNonEmpty(arn, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeRedshiftClusterParameterGroup,
		Name:         name,
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"family":      strings.TrimSpace(group.Family),
			"description": strings.TrimSpace(group.Description),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func subnetGroupObservation(
	boundary awscloud.Boundary,
	group ClusterSubnetGroup,
) awscloud.ResourceObservation {
	arn := strings.TrimSpace(group.ARN)
	name := strings.TrimSpace(group.Name)
	resourceID := firstNonEmpty(arn, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeRedshiftClusterSubnetGroup,
		Name:         name,
		State:        strings.TrimSpace(group.Status),
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"description": strings.TrimSpace(group.Description),
			"vpc_id":      strings.TrimSpace(group.VPCID),
			"subnet_ids":  cloneStrings(group.SubnetIDs),
		},
		CorrelationAnchors: []string{arn, name},
		SourceRecordID:     resourceID,
	}
}

func snapshotObservation(
	boundary awscloud.Boundary,
	snapshot ClusterSnapshot,
) awscloud.ResourceObservation {
	arn := strings.TrimSpace(snapshot.ARN)
	identifier := strings.TrimSpace(snapshot.Identifier)
	resourceID := firstNonEmpty(arn, identifier)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeRedshiftClusterSnapshot,
		Name:         identifier,
		State:        strings.TrimSpace(snapshot.Status),
		Tags:         cloneStringMap(snapshot.Tags),
		Attributes: map[string]any{
			"cluster_identifier":               strings.TrimSpace(snapshot.ClusterIdentifier),
			"snapshot_type":                    strings.TrimSpace(snapshot.SnapshotType),
			"node_type":                        strings.TrimSpace(snapshot.NodeType),
			"number_of_nodes":                  snapshot.NumberOfNodes,
			"db_name":                          strings.TrimSpace(snapshot.DBName),
			"vpc_id":                           strings.TrimSpace(snapshot.VPCID),
			"encrypted":                        snapshot.Encrypted,
			"kms_key_id":                       strings.TrimSpace(snapshot.KMSKeyID),
			"snapshot_create_time":             timeOrNil(snapshot.SnapshotCreateTime),
			"cluster_create_time":              timeOrNil(snapshot.ClusterCreateTime),
			"snapshot_retention_start_time":    timeOrNil(snapshot.SnapshotRetentionStartTime),
			"manual_snapshot_retention_period": snapshot.ManualSnapshotRetentionPeriod,
			"engine_full_version":              strings.TrimSpace(snapshot.EngineFullVersion),
			"availability_zone":                strings.TrimSpace(snapshot.AvailabilityZone),
			"source_region":                    strings.TrimSpace(snapshot.SourceRegion),
			"restorable_node_types":            cloneStrings(snapshot.RestorableNodeTypes),
		},
		CorrelationAnchors: []string{arn, identifier},
		SourceRecordID:     resourceID,
	}
}

func scheduledActionObservation(
	boundary awscloud.Boundary,
	action ScheduledAction,
) awscloud.ResourceObservation {
	name := strings.TrimSpace(action.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   name,
		ResourceType: awscloud.ResourceTypeRedshiftScheduledAction,
		Name:         name,
		State:        strings.TrimSpace(action.State),
		Attributes: map[string]any{
			"schedule":                  strings.TrimSpace(action.Schedule),
			"iam_role_arn":              strings.TrimSpace(action.IAMRoleARN),
			"description":               strings.TrimSpace(action.Description),
			"start_time":                timeOrNil(action.StartTime),
			"end_time":                  timeOrNil(action.EndTime),
			"next_invocation_time":      timeOrNil(action.NextInvocationTime),
			"target_action_name":        strings.TrimSpace(action.TargetActionName),
			"target_cluster_identifier": strings.TrimSpace(action.TargetClusterIdentifier),
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     name,
	}
}

func serverlessNamespaceObservation(
	boundary awscloud.Boundary,
	namespace ServerlessNamespace,
) awscloud.ResourceObservation {
	arn := strings.TrimSpace(namespace.ARN)
	name := strings.TrimSpace(namespace.Name)
	resourceID := firstNonEmpty(arn, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeRedshiftServerlessNamespace,
		Name:         name,
		State:        strings.TrimSpace(namespace.Status),
		Tags:         cloneStringMap(namespace.Tags),
		Attributes: map[string]any{
			"namespace_id":     strings.TrimSpace(namespace.NamespaceID),
			"db_name":          strings.TrimSpace(namespace.DBName),
			"default_iam_role": strings.TrimSpace(namespace.DefaultIAMRole),
			"iam_role_arns":    cloneStrings(namespace.IAMRoleARNs),
			"kms_key_id":       strings.TrimSpace(namespace.KMSKeyID),
			"log_exports":      cloneStrings(namespace.LogExports),
			"creation_date":    timeOrNil(namespace.CreationDate),
		},
		CorrelationAnchors: []string{arn, name, strings.TrimSpace(namespace.NamespaceID)},
		SourceRecordID:     resourceID,
	}
}

func serverlessWorkgroupObservation(
	boundary awscloud.Boundary,
	workgroup ServerlessWorkgroup,
) awscloud.ResourceObservation {
	arn := strings.TrimSpace(workgroup.ARN)
	name := strings.TrimSpace(workgroup.Name)
	resourceID := firstNonEmpty(arn, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          arn,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeRedshiftServerlessWorkgroup,
		Name:         name,
		State:        strings.TrimSpace(workgroup.Status),
		Tags:         cloneStringMap(workgroup.Tags),
		Attributes: map[string]any{
			"workgroup_id":         strings.TrimSpace(workgroup.WorkgroupID),
			"namespace_name":       strings.TrimSpace(workgroup.NamespaceName),
			"base_capacity":        workgroup.BaseCapacity,
			"max_capacity":         workgroup.MaxCapacity,
			"enhanced_vpc_routing": workgroup.EnhancedVPCRouting,
			"publicly_accessible":  workgroup.PubliclyAccessible,
			"config_parameters":    configParameterMaps(workgroup.ConfigParameters),
			"subnet_ids":           cloneStrings(workgroup.SubnetIDs),
			"security_group_ids":   cloneStrings(workgroup.SecurityGroupIDs),
			"endpoint_address":     strings.TrimSpace(workgroup.EndpointAddress),
			"endpoint_port":        workgroup.EndpointPort,
			"creation_date":        timeOrNil(workgroup.CreationDate),
		},
		CorrelationAnchors: []string{arn, name, strings.TrimSpace(workgroup.WorkgroupID), strings.TrimSpace(workgroup.EndpointAddress)},
		SourceRecordID:     resourceID,
	}
}
