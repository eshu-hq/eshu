// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package msk

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS MSK metadata facts for one claimed account and region. It
// never mutates clusters, configurations, or replicators, never reboots
// brokers, and never persists broker server.properties bodies, broker log
// contents, bootstrap broker endpoints, SCRAM secrets, topic data, or Kafka
// message contents.
type Scanner struct {
	Client Client
}

// Scan observes MSK clusters, broker configurations, and replicators through
// the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("msk scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceMSK:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceMSK
	default:
		return nil, fmt.Errorf("msk scanner received service_kind %q", boundary.ServiceKind)
	}

	clusters, err := s.Client.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list MSK clusters: %w", err)
	}
	configurations, err := s.Client.ListConfigurations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list MSK configurations: %w", err)
	}
	replicators, err := s.Client.ListReplicators(ctx)
	if err != nil {
		return nil, fmt.Errorf("list MSK replicators: %w", err)
	}

	var envelopes []facts.Envelope
	for _, cluster := range clusters {
		resource, err := awscloud.NewResourceEnvelope(clusterObservation(boundary, cluster))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, observation := range clusterRelationships(boundary, cluster) {
			relationship, err := awscloud.NewRelationshipEnvelope(observation)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relationship)
		}
	}
	for _, configuration := range configurations {
		resource, err := awscloud.NewResourceEnvelope(configurationObservation(boundary, configuration))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	for _, replicator := range replicators {
		resource, err := awscloud.NewResourceEnvelope(replicatorObservation(boundary, replicator))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, observation := range replicatorRelationships(boundary, replicator) {
			relationship, err := awscloud.NewRelationshipEnvelope(observation)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relationship)
		}
	}
	return envelopes, nil
}

func clusterObservation(boundary awscloud.Boundary, cluster Cluster) awscloud.ResourceObservation {
	clusterARN := strings.TrimSpace(cluster.ARN)
	attributes := map[string]any{
		"cluster_type":    strings.TrimSpace(cluster.Type),
		"creation_time":   timeOrNil(cluster.CreationTime),
		"current_version": strings.TrimSpace(cluster.CurrentVersion),
	}
	if cluster.Provisioned != nil {
		attributes["kafka_version"] = strings.TrimSpace(cluster.Provisioned.KafkaVersion)
		attributes["enhanced_monitoring"] = strings.TrimSpace(cluster.Provisioned.EnhancedMonitoring)
		attributes["number_of_broker_nodes"] = cluster.Provisioned.NumberOfBrokerNodes
		attributes["storage_mode"] = strings.TrimSpace(cluster.Provisioned.StorageMode)
		attributes["broker_node_group"] = brokerNodeGroupMap(cluster.Provisioned.BrokerNodeGroup)
		attributes["encryption_at_rest_kms_key"] = strings.TrimSpace(cluster.Provisioned.EncryptionAtRestKMSKey)
		attributes["encryption_in_transit"] = encryptionInTransitMap(cluster.Provisioned.EncryptionInTransit)
		attributes["client_authentication"] = clientAuthenticationMap(cluster.Provisioned.ClientAuthentication)
		if cluster.Provisioned.CurrentConfiguration != nil {
			attributes["current_configuration"] = map[string]any{
				"arn":      strings.TrimSpace(cluster.Provisioned.CurrentConfiguration.ARN),
				"revision": cluster.Provisioned.CurrentConfiguration.Revision,
			}
		}
	}
	if cluster.Serverless != nil {
		attributes["serverless_vpc_configs"] = serverlessVPCConfigMaps(cluster.Serverless.VPCConfigs)
		attributes["client_authentication"] = clientAuthenticationMap(cluster.Serverless.ClientAuthentication)
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                clusterARN,
		ResourceID:         firstNonEmpty(clusterARN, cluster.Name),
		ResourceType:       awscloud.ResourceTypeMSKCluster,
		Name:               strings.TrimSpace(cluster.Name),
		State:              strings.TrimSpace(cluster.State),
		Tags:               cloneStringMap(cluster.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{clusterARN, strings.TrimSpace(cluster.Name)},
		SourceRecordID:     firstNonEmpty(clusterARN, cluster.Name),
	}
}

func configurationObservation(boundary awscloud.Boundary, configuration Configuration) awscloud.ResourceObservation {
	configurationARN := strings.TrimSpace(configuration.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          configurationARN,
		ResourceID:   firstNonEmpty(configurationARN, configuration.Name),
		ResourceType: awscloud.ResourceTypeMSKConfiguration,
		Name:         strings.TrimSpace(configuration.Name),
		State:        strings.TrimSpace(configuration.State),
		Attributes: map[string]any{
			"creation_time":   timeOrNil(configuration.CreationTime),
			"description":     strings.TrimSpace(configuration.Description),
			"kafka_versions":  cloneStrings(configuration.KafkaVersions),
			"latest_revision": latestRevisionMap(configuration.LatestRevision),
		},
		CorrelationAnchors: []string{configurationARN, strings.TrimSpace(configuration.Name)},
		SourceRecordID:     firstNonEmpty(configurationARN, configuration.Name),
	}
}

func replicatorObservation(boundary awscloud.Boundary, replicator Replicator) awscloud.ResourceObservation {
	replicatorARN := strings.TrimSpace(replicator.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          replicatorARN,
		ResourceID:   firstNonEmpty(replicatorARN, replicator.Name),
		ResourceType: awscloud.ResourceTypeMSKReplicator,
		Name:         strings.TrimSpace(replicator.Name),
		State:        strings.TrimSpace(replicator.State),
		Tags:         cloneStringMap(replicator.Tags),
		Attributes: map[string]any{
			"creation_time":              timeOrNil(replicator.CreationTime),
			"current_version":            strings.TrimSpace(replicator.CurrentVersion),
			"service_execution_role_arn": strings.TrimSpace(replicator.ServiceExecutionRoleARN),
			"kafka_clusters":             replicatorKafkaClusterMaps(replicator.KafkaClusters),
			"replication_info":           replicationInfoMaps(replicator.ReplicationInfo),
		},
		CorrelationAnchors: []string{replicatorARN, strings.TrimSpace(replicator.Name)},
		SourceRecordID:     firstNonEmpty(replicatorARN, replicator.Name),
	}
}

func brokerNodeGroupMap(group BrokerNodeGroup) map[string]any {
	return map[string]any{
		"instance_type":      strings.TrimSpace(group.InstanceType),
		"client_subnets":     cloneStrings(group.ClientSubnets),
		"security_group_ids": cloneStrings(group.SecurityGroupIDs),
		"storage_gib":        group.StorageGiB,
	}
}

func encryptionInTransitMap(value EncryptionInTransit) map[string]any {
	return map[string]any{
		"client_broker": strings.TrimSpace(value.ClientBroker),
		"in_cluster":    value.InCluster,
	}
}

func clientAuthenticationMap(value ClientAuthentication) map[string]any {
	return map[string]any{
		"sasl_iam_enabled":            value.SASLIAMEnabled,
		"sasl_scram_enabled":          value.SASLSCRAMEnabled,
		"tls_enabled":                 value.TLSEnabled,
		"tls_certificate_authorities": cloneStrings(value.TLSCertificateAuthorities),
		"unauthenticated_enabled":     value.UnauthenticatedEnabled,
	}
}

func serverlessVPCConfigMaps(values []VPCConfig) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		out = append(out, map[string]any{
			"subnet_ids":         cloneStrings(value.SubnetIDs),
			"security_group_ids": cloneStrings(value.SecurityGroupIDs),
		})
	}
	return out
}

func latestRevisionMap(value ConfigurationRevisionSummary) map[string]any {
	return map[string]any{
		"revision":      value.Revision,
		"creation_time": timeOrNil(value.CreationTime),
		"description":   strings.TrimSpace(value.Description),
	}
}

func replicatorKafkaClusterMaps(values []ReplicatorKafkaCluster) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		out = append(out, map[string]any{
			"alias":                  strings.TrimSpace(value.Alias),
			"msk_cluster_arn":        strings.TrimSpace(value.MSKClusterARN),
			"vpc_subnet_ids":         cloneStrings(value.VPCSubnetIDs),
			"vpc_security_group_ids": cloneStrings(value.VPCSecurityGroupIDs),
		})
	}
	return out
}

func replicationInfoMaps(values []ReplicationInfo) []map[string]any {
	if len(values) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(values))
	for _, value := range values {
		out = append(out, map[string]any{
			"source_cluster_arn":                   strings.TrimSpace(value.SourceClusterARN),
			"target_cluster_arn":                   strings.TrimSpace(value.TargetClusterARN),
			"source_alias":                         strings.TrimSpace(value.SourceAlias),
			"target_alias":                         strings.TrimSpace(value.TargetAlias),
			"target_compression":                   strings.TrimSpace(value.TargetCompression),
			"topic_include_pattern_count":          value.TopicIncludePatternCount,
			"topic_exclude_pattern_count":          value.TopicExcludePatternCount,
			"consumer_group_include_pattern_count": value.ConsumerGroupIncludePatternCount,
			"consumer_group_exclude_pattern_count": value.ConsumerGroupExcludePatternCount,
		})
	}
	return out
}
