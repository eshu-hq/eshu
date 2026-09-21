// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package memorydb

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS MemoryDB metadata facts for one claimed account and region.
// It never calls cluster, user, ACL, or snapshot mutation APIs, never persists
// user passwords, AUTH tokens, ACL access strings, or snapshot payload data, and
// never connects to running MemoryDB nodes.
type Scanner struct {
	Client Client
}

// Scan observes MemoryDB clusters, subnet groups, parameter groups, users,
// ACLs, and snapshot metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("memorydb scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceMemoryDB:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceMemoryDB
	default:
		return nil, fmt.Errorf("memorydb scanner received service_kind %q", boundary.ServiceKind)
	}

	clusters, err := s.Client.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list MemoryDB clusters: %w", err)
	}
	subnetGroups, err := s.Client.ListSubnetGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list MemoryDB subnet groups: %w", err)
	}
	parameterGroups, err := s.Client.ListParameterGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list MemoryDB parameter groups: %w", err)
	}
	users, err := s.Client.ListUsers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list MemoryDB users: %w", err)
	}
	acls, err := s.Client.ListACLs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list MemoryDB ACLs: %w", err)
	}
	snapshots, err := s.Client.ListSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list MemoryDB snapshots: %w", err)
	}

	subnetGroupIDs := subnetGroupIdentityMap(subnetGroups)
	userIDs := userIdentityMap(users)

	var envelopes []facts.Envelope
	for _, cluster := range clusters {
		resource, err := awscloud.NewResourceEnvelope(clusterObservation(boundary, cluster))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, relationship := range clusterRelationships(boundary, cluster, subnetGroupIDs) {
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
	for _, acl := range acls {
		resource, err := awscloud.NewResourceEnvelope(aclObservation(boundary, acl))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, relationship := range aclRelationships(boundary, acl, userIDs) {
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

func clusterObservation(boundary awscloud.Boundary, cluster Cluster) awscloud.ResourceObservation {
	clusterARN := strings.TrimSpace(cluster.ARN)
	name := strings.TrimSpace(cluster.Name)
	resourceID := firstNonEmpty(clusterARN, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          clusterARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeMemoryDBCluster,
		Name:         name,
		State:        strings.TrimSpace(cluster.Status),
		Tags:         cloneStringMap(cluster.Tags),
		Attributes:   clusterAttributes(cluster),
		CorrelationAnchors: []string{
			clusterARN,
			name,
		},
		SourceRecordID: resourceID,
	}
}

func clusterAttributes(cluster Cluster) map[string]any {
	return map[string]any{
		"description":                strings.TrimSpace(cluster.Description),
		"engine":                     strings.TrimSpace(cluster.Engine),
		"engine_version":             strings.TrimSpace(cluster.EngineVersion),
		"node_type":                  strings.TrimSpace(cluster.NodeType),
		"num_shards":                 cluster.NumberOfShards,
		"num_replicas_per_shard":     cluster.NumberOfReplicasPerShard,
		"acl_name":                   strings.TrimSpace(cluster.ACLName),
		"parameter_group_name":       strings.TrimSpace(cluster.ParameterGroupName),
		"subnet_group_name":          strings.TrimSpace(cluster.SubnetGroupName),
		"security_group_ids":         cloneStrings(cluster.SecurityGroupIDs),
		"kms_key_id":                 strings.TrimSpace(cluster.KMSKeyID),
		"sns_topic_arn":              strings.TrimSpace(cluster.SNSTopicARN),
		"tls_enabled":                cluster.TLSEnabled,
		"data_tiering":               strings.TrimSpace(cluster.DataTiering),
		"auto_minor_version_upgrade": cluster.AutoMinorVersionUpgrade,
		"snapshot_retention_limit":   cluster.SnapshotRetentionLimit,
		"snapshot_window":            strings.TrimSpace(cluster.SnapshotWindow),
		"maintenance_window":         strings.TrimSpace(cluster.MaintenanceWindow),
		"availability_mode":          strings.TrimSpace(cluster.AvailabilityMode),
		"network_type":               strings.TrimSpace(cluster.NetworkType),
		"ip_discovery":               strings.TrimSpace(cluster.IPDiscovery),
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
		ResourceType: awscloud.ResourceTypeMemoryDBSubnetGroup,
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
		ResourceType: awscloud.ResourceTypeMemoryDBParameterGroup,
		Name:         name,
		Tags:         cloneStringMap(group.Tags),
		Attributes: map[string]any{
			"family":      strings.TrimSpace(group.Family),
			"description": strings.TrimSpace(group.Description),
		},
		CorrelationAnchors: []string{groupARN, name},
		SourceRecordID:     resourceID,
	}
}

func userObservation(boundary awscloud.Boundary, user User) awscloud.ResourceObservation {
	userARN := strings.TrimSpace(user.ARN)
	name := strings.TrimSpace(user.Name)
	resourceID := firstNonEmpty(userARN, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          userARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeMemoryDBUser,
		Name:         name,
		State:        strings.TrimSpace(user.Status),
		Tags:         cloneStringMap(user.Tags),
		Attributes: map[string]any{
			"authentication_type":    strings.TrimSpace(user.AuthenticationType),
			"password_count":         user.PasswordCount,
			"access_string_present":  user.AccessStringPresent,
			"minimum_engine_version": strings.TrimSpace(user.MinimumEngineVersion),
			"acl_names":              cloneStrings(user.ACLNames),
		},
		CorrelationAnchors: []string{userARN, name},
		SourceRecordID:     resourceID,
	}
}

func aclObservation(boundary awscloud.Boundary, acl ACL) awscloud.ResourceObservation {
	aclARN := strings.TrimSpace(acl.ARN)
	name := strings.TrimSpace(acl.Name)
	resourceID := firstNonEmpty(aclARN, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          aclARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeMemoryDBACL,
		Name:         name,
		State:        strings.TrimSpace(acl.Status),
		Tags:         cloneStringMap(acl.Tags),
		Attributes: map[string]any{
			"minimum_engine_version": strings.TrimSpace(acl.MinimumEngineVersion),
			"user_names":             cloneStrings(acl.UserNames),
			"cluster_names":          cloneStrings(acl.ClusterNames),
		},
		CorrelationAnchors: []string{aclARN, name},
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
		ResourceType: awscloud.ResourceTypeMemoryDBSnapshot,
		Name:         name,
		State:        strings.TrimSpace(snapshot.Status),
		Tags:         cloneStringMap(snapshot.Tags),
		Attributes: map[string]any{
			"snapshot_source":     strings.TrimSpace(snapshot.Source),
			"source_cluster_name": strings.TrimSpace(snapshot.SourceClusterName),
		},
		CorrelationAnchors: []string{snapshotARN, name},
		SourceRecordID:     resourceID,
	}
}
