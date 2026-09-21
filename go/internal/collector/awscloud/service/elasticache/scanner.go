// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package elasticache

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS ElastiCache metadata facts for one claimed account and
// region. It never calls cluster, replication group, or user mutation APIs,
// never persists AUTH token values, cache keys, cache values, snapshot data,
// or user passwords, and never connects to running cache nodes.
type Scanner struct {
	Client Client
}

// Scan observes ElastiCache cache clusters, replication groups, parameter
// groups, subnet groups, users, user groups, and snapshot metadata through
// the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("elasticache scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceElastiCache:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceElastiCache
	default:
		return nil, fmt.Errorf("elasticache scanner received service_kind %q", boundary.ServiceKind)
	}

	clusters, err := s.Client.ListCacheClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ElastiCache cache clusters: %w", err)
	}
	replicationGroups, err := s.Client.ListReplicationGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ElastiCache replication groups: %w", err)
	}
	subnetGroups, err := s.Client.ListCacheSubnetGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ElastiCache cache subnet groups: %w", err)
	}
	parameterGroups, err := s.Client.ListCacheParameterGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ElastiCache cache parameter groups: %w", err)
	}
	users, err := s.Client.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ElastiCache users: %w", err)
	}
	userGroups, err := s.Client.ListUserGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ElastiCache user groups: %w", err)
	}
	snapshots, err := s.Client.ListSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ElastiCache snapshots: %w", err)
	}

	clusterIDs := clusterIdentityMap(clusters)
	userIDs := userIdentityMap(users)

	var envelopes []facts.Envelope
	for _, cluster := range clusters {
		resource, err := awscloud.NewResourceEnvelope(clusterObservation(boundary, cluster))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, relationship := range clusterRelationships(boundary, cluster) {
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	for _, group := range replicationGroups {
		resource, err := awscloud.NewResourceEnvelope(replicationGroupObservation(boundary, group))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, relationship := range replicationGroupRelationships(boundary, group, clusterIDs) {
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	for _, subnetGroup := range subnetGroups {
		resource, err := awscloud.NewResourceEnvelope(subnetGroupObservation(boundary, subnetGroup))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	for _, parameterGroup := range parameterGroups {
		resource, err := awscloud.NewResourceEnvelope(parameterGroupObservation(boundary, parameterGroup))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	for _, user := range users {
		resource, err := awscloud.NewResourceEnvelope(userObservation(boundary, user))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	for _, userGroup := range userGroups {
		resource, err := awscloud.NewResourceEnvelope(userGroupObservation(boundary, userGroup))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, relationship := range userGroupRelationships(boundary, userGroup, userIDs) {
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	for _, snapshot := range snapshots {
		resource, err := awscloud.NewResourceEnvelope(snapshotObservation(boundary, snapshot))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	return envelopes, nil
}

func clusterObservation(boundary awscloud.Boundary, cluster CacheCluster) awscloud.ResourceObservation {
	clusterARN := strings.TrimSpace(cluster.ARN)
	id := strings.TrimSpace(cluster.ID)
	resourceID := firstNonEmpty(clusterARN, id)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          clusterARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeElastiCacheCacheCluster,
		Name:         id,
		State:        strings.TrimSpace(cluster.Status),
		Tags:         cloneStringMap(cluster.Tags),
		Attributes:   clusterAttributes(cluster),
		CorrelationAnchors: []string{
			clusterARN,
			id,
			strings.TrimSpace(cluster.ReplicationGroupID),
		},
		SourceRecordID: resourceID,
	}
}

func clusterAttributes(cluster CacheCluster) map[string]any {
	return map[string]any{
		"engine":                      strings.TrimSpace(cluster.Engine),
		"engine_version":              strings.TrimSpace(cluster.EngineVersion),
		"node_type":                   strings.TrimSpace(cluster.NodeType),
		"num_cache_nodes":             cluster.NumCacheNodes,
		"preferred_availability_zone": strings.TrimSpace(cluster.PreferredAvailabilityZone),
		"cache_subnet_group_name":     strings.TrimSpace(cluster.SubnetGroupName),
		"vpc_id":                      strings.TrimSpace(cluster.VPCID),
		"subnet_ids":                  cloneStrings(cluster.SubnetIDs),
		"security_group_ids":          cloneStrings(cluster.SecurityGroupIDs),
		"cache_parameter_group_name":  strings.TrimSpace(cluster.ParameterGroupName),
		"replication_group_id":        strings.TrimSpace(cluster.ReplicationGroupID),
		"kms_key_id":                  strings.TrimSpace(cluster.KMSKeyID),
		"transit_encryption_enabled":  cluster.TransitEncryptionEnabled,
		"at_rest_encryption_enabled":  cluster.AtRestEncryptionEnabled,
		"auth_token_enabled":          cluster.AuthTokenEnabled,
		"snapshot_retention_limit":    cluster.SnapshotRetentionLimit,
		"snapshot_window":             strings.TrimSpace(cluster.SnapshotWindow),
		"auto_minor_version_upgrade":  cluster.AutoMinorVersionUpgrade,
		"notification_topic_arn":      strings.TrimSpace(cluster.NotificationTopicARN),
		"network_type":                strings.TrimSpace(cluster.NetworkType),
		"ip_discovery":                strings.TrimSpace(cluster.IPDiscovery),
	}
}

func replicationGroupObservation(boundary awscloud.Boundary, group ReplicationGroup) awscloud.ResourceObservation {
	groupARN := strings.TrimSpace(group.ARN)
	id := strings.TrimSpace(group.ID)
	resourceID := firstNonEmpty(groupARN, id)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          groupARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeElastiCacheReplicationGroup,
		Name:         id,
		State:        strings.TrimSpace(group.Status),
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"description":                strings.TrimSpace(group.Description),
			"member_cluster_ids":         cloneStrings(group.MemberClusters),
			"automatic_failover":         strings.TrimSpace(group.AutomaticFailover),
			"multi_az":                   strings.TrimSpace(group.MultiAZ),
			"cluster_enabled":            group.ClusterEnabled,
			"node_type":                  strings.TrimSpace(group.NodeType),
			"kms_key_id":                 strings.TrimSpace(group.KMSKeyID),
			"transit_encryption_enabled": group.TransitEncryptionEnabled,
			"at_rest_encryption_enabled": group.AtRestEncryptionEnabled,
			"auth_token_enabled":         group.AuthTokenEnabled,
			"snapshot_retention_limit":   group.SnapshotRetentionLimit,
			"snapshot_window":            strings.TrimSpace(group.SnapshotWindow),
			"data_tiering":               strings.TrimSpace(group.DataTiering),
			"network_type":               strings.TrimSpace(group.NetworkType),
			"ip_discovery":               strings.TrimSpace(group.IPDiscovery),
		},
		CorrelationAnchors: []string{groupARN, id},
		SourceRecordID:     resourceID,
	}
}

func subnetGroupObservation(boundary awscloud.Boundary, group SubnetGroup) awscloud.ResourceObservation {
	groupARN := strings.TrimSpace(group.ARN)
	name := strings.TrimSpace(group.Name)
	resourceID := firstNonEmpty(groupARN, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          groupARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeElastiCacheSubnetGroup,
		Name:         name,
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"description": strings.TrimSpace(group.Description),
			"vpc_id":      strings.TrimSpace(group.VPCID),
			"subnet_ids":  cloneStrings(group.SubnetIDs),
		},
		CorrelationAnchors: []string{groupARN, name},
		SourceRecordID:     resourceID,
	}
}

func parameterGroupObservation(boundary awscloud.Boundary, group ParameterGroup) awscloud.ResourceObservation {
	groupARN := strings.TrimSpace(group.ARN)
	name := strings.TrimSpace(group.Name)
	resourceID := firstNonEmpty(groupARN, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          groupARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeElastiCacheParameterGroup,
		Name:         name,
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"family":      strings.TrimSpace(group.Family),
			"description": strings.TrimSpace(group.Description),
			"is_global":   group.IsGlobal,
		},
		CorrelationAnchors: []string{groupARN, name},
		SourceRecordID:     resourceID,
	}
}

func userObservation(boundary awscloud.Boundary, user User) awscloud.ResourceObservation {
	userARN := strings.TrimSpace(user.ARN)
	id := strings.TrimSpace(user.ID)
	resourceID := firstNonEmpty(userARN, id)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          userARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeElastiCacheUser,
		Name:         strings.TrimSpace(user.Name),
		State:        strings.TrimSpace(user.Status),
		Tags:         cloneStringMap(user.Tags),
		Attributes: map[string]any{
			"engine":                 strings.TrimSpace(user.Engine),
			"authentication_type":    strings.TrimSpace(user.AuthenticationType),
			"password_count":         user.PasswordCount,
			"minimum_engine_version": strings.TrimSpace(user.MinimumEngineVersion),
			"user_group_ids":         cloneStrings(user.UserGroupIDs),
		},
		CorrelationAnchors: []string{userARN, id},
		SourceRecordID:     resourceID,
	}
}

func userGroupObservation(boundary awscloud.Boundary, group UserGroup) awscloud.ResourceObservation {
	groupARN := strings.TrimSpace(group.ARN)
	id := strings.TrimSpace(group.ID)
	resourceID := firstNonEmpty(groupARN, id)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          groupARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeElastiCacheUserGroup,
		Name:         id,
		State:        strings.TrimSpace(group.Status),
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"engine":   strings.TrimSpace(group.Engine),
			"user_ids": cloneStrings(group.UserIDs),
		},
		CorrelationAnchors: []string{groupARN, id},
		SourceRecordID:     resourceID,
	}
}

func snapshotObservation(boundary awscloud.Boundary, snapshot SnapshotMetadata) awscloud.ResourceObservation {
	snapshotARN := strings.TrimSpace(snapshot.ARN)
	name := strings.TrimSpace(snapshot.Name)
	resourceID := firstNonEmpty(snapshotARN, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          snapshotARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeElastiCacheSnapshot,
		Name:         name,
		State:        strings.TrimSpace(snapshot.Status),
		Tags:         cloneStringMap(snapshot.Tags),
		Attributes: map[string]any{
			"snapshot_source":             strings.TrimSpace(snapshot.SnapshotSource),
			"source_cache_cluster_id":     strings.TrimSpace(snapshot.SourceCacheClusterID),
			"source_replication_group_id": strings.TrimSpace(snapshot.SourceReplicationGrp),
		},
		CorrelationAnchors: []string{snapshotARN, name},
		SourceRecordID:     resourceID,
	}
}
