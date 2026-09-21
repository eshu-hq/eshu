// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package neptune

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits Amazon Neptune metadata facts for one claimed account and
// region. It covers both Neptune (provisioned, RDS-shaped) and Neptune
// Analytics (graph) resources. It never connects to a database or graph
// endpoint, runs a graph query, reads graph vertex or edge contents, reads
// snapshot contents, persists master user passwords or secrets, or reads
// cluster parameter values.
type Scanner struct {
	Client Client
}

// Scan observes Neptune DB clusters, cluster instances, cluster parameter
// groups, cluster snapshots, subnet groups, global clusters, Neptune Analytics
// graphs, and graph snapshots, plus the direct dependency relationships
// Neptune reports.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("neptune scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceNeptune:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceNeptune
	default:
		return nil, fmt.Errorf("neptune scanner received service_kind %q", boundary.ServiceKind)
	}

	clusters, err := s.Client.ListDBClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Neptune DB clusters: %w", err)
	}
	instances, err := s.Client.ListClusterInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Neptune cluster instances: %w", err)
	}
	parameterGroups, err := s.Client.ListClusterParameterGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Neptune cluster parameter groups: %w", err)
	}
	snapshots, err := s.Client.ListClusterSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Neptune cluster snapshots: %w", err)
	}
	subnetGroups, err := s.Client.ListSubnetGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Neptune subnet groups: %w", err)
	}
	globalClusters, err := s.Client.ListGlobalClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Neptune global clusters: %w", err)
	}
	graphs, err := s.Client.ListGraphs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Neptune Analytics graphs: %w", err)
	}
	graphSnapshots, err := s.Client.ListGraphSnapshots(ctx)
	if err != nil {
		return nil, fmt.Errorf("list Neptune Analytics graph snapshots: %w", err)
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
	envelopes, err = appendResources(envelopes, boundary, parameterGroups, snapshots, subnetGroups)
	if err != nil {
		return nil, err
	}
	envelopes, err = appendGlobalClusters(envelopes, boundary, globalClusters)
	if err != nil {
		return nil, err
	}
	envelopes, err = appendGraphs(envelopes, boundary, graphs)
	if err != nil {
		return nil, err
	}
	envelopes, err = appendGraphSnapshots(envelopes, boundary, graphSnapshots)
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
) ([]facts.Envelope, error) {
	observations := make([]awscloud.ResourceObservation, 0,
		len(parameterGroups)+len(snapshots)+len(subnetGroups))
	for _, group := range parameterGroups {
		observations = append(observations, parameterGroupObservation(boundary, group))
	}
	for _, snapshot := range snapshots {
		observations = append(observations, snapshotObservation(boundary, snapshot))
	}
	for _, subnetGroup := range subnetGroups {
		observations = append(observations, subnetGroupObservation(boundary, subnetGroup))
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

func appendGraphs(
	envelopes []facts.Envelope,
	boundary awscloud.Boundary,
	graphs []Graph,
) ([]facts.Envelope, error) {
	for _, graph := range graphs {
		resource, err := awscloud.NewResourceEnvelope(graphObservation(boundary, graph))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
		for _, relationship := range graphRelationships(boundary, graph) {
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}
	return envelopes, nil
}

func appendGraphSnapshots(
	envelopes []facts.Envelope,
	boundary awscloud.Boundary,
	graphSnapshots []GraphSnapshot,
) ([]facts.Envelope, error) {
	for _, snapshot := range graphSnapshots {
		resource, err := awscloud.NewResourceEnvelope(graphSnapshotObservation(boundary, snapshot))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	return envelopes, nil
}
