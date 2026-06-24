// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package docdbelastic

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon DocumentDB Elastic Clusters metadata-only facts for one
// claimed account and region. It never reads document contents, collections,
// indexes, query results, the cluster endpoint connection string, or the admin
// password, and never mutates Elastic Cluster state. It reports elastic
// clusters plus the cluster-to-subnet, cluster-to-security-group,
// cluster-to-KMS-key, and cluster-to-admin-secret relationships.
type Scanner struct {
	// Client is the metadata-only DocumentDB Elastic snapshot source.
	Client Client
}

// Scan observes DocumentDB Elastic clusters and their direct VPC, KMS, and
// admin-secret dependency metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("docdbelastic scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceDocDBElastic:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceDocDBElastic
	default:
		return nil, fmt.Errorf("docdbelastic scanner received service_kind %q", boundary.ServiceKind)
	}

	snapshot, err := s.Client.Snapshot(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DocumentDB Elastic clusters: %w", err)
	}
	var envelopes []facts.Envelope
	if err := appendWarnings(&envelopes, snapshot.Warnings); err != nil {
		return nil, err
	}
	for _, cluster := range snapshot.Clusters {
		next, err := clusterEnvelopes(boundary, cluster)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, next...)
	}
	return envelopes, nil
}

func appendWarnings(envelopes *[]facts.Envelope, observations []awscloud.WarningObservation) error {
	for _, observation := range observations {
		envelope, err := awscloud.NewWarningEnvelope(observation)
		if err != nil {
			return err
		}
		*envelopes = append(*envelopes, envelope)
	}
	return nil
}

func clusterEnvelopes(boundary awscloud.Boundary, cluster Cluster) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(clusterObservation(boundary, cluster))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range clusterRelationships(boundary, cluster) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func clusterObservation(boundary awscloud.Boundary, cluster Cluster) awscloud.ResourceObservation {
	clusterARN := strings.TrimSpace(cluster.ARN)
	name := strings.TrimSpace(cluster.Name)
	resourceID := clusterResourceID(cluster)
	attributes := map[string]any{
		"auth_type":               strings.TrimSpace(cluster.AuthType),
		"admin_secret_configured": strings.TrimSpace(cluster.AdminSecretARN) != "",
		"kms_key_id":              strings.TrimSpace(cluster.KMSKeyID),
		"shard_capacity":          cluster.ShardCapacity,
		"shard_count":             cluster.ShardCount,
		"shard_instance_count":    cluster.ShardInstanceCount,
		"backup_retention_period": cluster.BackupRetentionPeriod,
		"subnet_ids":              cloneStrings(cluster.SubnetIDs),
		"security_group_ids":      cloneStrings(cluster.SecurityGroupIDs),
		"create_time":             timeOrNil(cluster.CreateTime),
	}
	if window := strings.TrimSpace(cluster.PreferredBackupWindow); window != "" {
		attributes["preferred_backup_window"] = window
	}
	if window := strings.TrimSpace(cluster.PreferredMaintenanceWindow); window != "" {
		attributes["preferred_maintenance_window"] = window
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                clusterARN,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypeDocDBElasticCluster,
		Name:               name,
		State:              strings.TrimSpace(cluster.Status),
		Tags:               cloneStringMap(cluster.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{clusterARN, name},
		SourceRecordID:     resourceID,
	}
}
