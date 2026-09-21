// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package dax

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon DynamoDB Accelerator (DAX) metadata-only facts for one
// claimed account and region. It never reads cached DynamoDB items, query
// results, or node endpoint payloads, and never mutates DAX state. It reports
// clusters, subnet groups, and parameter groups plus the cluster-to-subnet-group,
// cluster-to-security-group, cluster-to-IAM-role, subnet-group-to-VPC, and
// subnet-group-to-subnet relationships.
type Scanner struct {
	// Client is the metadata-only DAX snapshot source.
	Client Client
}

// Scan observes DAX clusters, subnet groups, and parameter groups plus their
// direct network and IAM dependency metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("dax scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceDAX:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceDAX
	default:
		return nil, fmt.Errorf("dax scanner received service_kind %q", boundary.ServiceKind)
	}

	clusters, err := s.Client.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DAX clusters: %w", err)
	}
	subnetGroups, err := s.Client.ListSubnetGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DAX subnet groups: %w", err)
	}
	parameterGroups, err := s.Client.ListParameterGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list DAX parameter groups: %w", err)
	}

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
	for _, group := range subnetGroups {
		resource, err := awscloud.NewResourceEnvelope(subnetGroupObservation(boundary, group))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, relationship := range subnetGroupRelationships(boundary, group) {
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	for _, group := range parameterGroups {
		resource, err := awscloud.NewResourceEnvelope(parameterGroupObservation(boundary, group))
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
		ResourceType: awscloud.ResourceTypeDAXCluster,
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
		"description":                  strings.TrimSpace(cluster.Description),
		"node_type":                    strings.TrimSpace(cluster.NodeType),
		"active_nodes":                 cluster.ActiveNodes,
		"total_nodes":                  cluster.TotalNodes,
		"network_type":                 strings.TrimSpace(cluster.NetworkType),
		"endpoint_encryption_type":     strings.TrimSpace(cluster.EndpointEncryptionType),
		"iam_role_arn":                 strings.TrimSpace(cluster.IAMRoleARN),
		"parameter_group_name":         strings.TrimSpace(cluster.ParameterGroupName),
		"subnet_group_name":            strings.TrimSpace(cluster.SubnetGroupName),
		"security_group_ids":           cloneStrings(cluster.SecurityGroupIDs),
		"sse_status":                   strings.TrimSpace(cluster.SSEStatus),
		"preferred_maintenance_window": strings.TrimSpace(cluster.PreferredMaintenanceWindow),
		"discovery_endpoint_address":   strings.TrimSpace(cluster.DiscoveryEndpointAddress),
		"discovery_endpoint_port":      cluster.DiscoveryEndpointPort,
	}
}

func subnetGroupObservation(boundary awscloud.Boundary, group SubnetGroup) awscloud.ResourceObservation {
	name := strings.TrimSpace(group.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   name,
		ResourceType: awscloud.ResourceTypeDAXSubnetGroup,
		Name:         name,
		Attributes: map[string]any{
			"description": strings.TrimSpace(group.Description),
			"vpc_id":      strings.TrimSpace(group.VPCID),
			"subnet_ids":  cloneStrings(group.SubnetIDs),
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     name,
	}
}

func parameterGroupObservation(boundary awscloud.Boundary, group ParameterGroup) awscloud.ResourceObservation {
	name := strings.TrimSpace(group.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   name,
		ResourceType: awscloud.ResourceTypeDAXParameterGroup,
		Name:         name,
		Attributes: map[string]any{
			"description": strings.TrimSpace(group.Description),
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     name,
	}
}
