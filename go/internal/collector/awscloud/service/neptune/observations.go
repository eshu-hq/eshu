// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package neptune

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
)

func clusterObservation(boundary awscloud.Boundary, cluster DBCluster) awscloud.ResourceObservation {
	clusterARN := strings.TrimSpace(cluster.ARN)
	identifier := strings.TrimSpace(cluster.Identifier)
	resourceID := firstNonEmpty(clusterARN, cluster.ResourceID, identifier)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          clusterARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeNeptuneCluster,
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
		ResourceType: awscloud.ResourceTypeNeptuneClusterInstance,
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
		ResourceType: awscloud.ResourceTypeNeptuneClusterParameterGroup,
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

func snapshotObservation(boundary awscloud.Boundary, snapshot ClusterSnapshot) awscloud.ResourceObservation {
	snapshotARN := strings.TrimSpace(snapshot.ARN)
	identifier := strings.TrimSpace(snapshot.Identifier)
	resourceID := firstNonEmpty(snapshotARN, identifier)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          snapshotARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeNeptuneClusterSnapshot,
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
		ResourceType: awscloud.ResourceTypeNeptuneSubnetGroup,
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
		ResourceType: awscloud.ResourceTypeNeptuneGlobalCluster,
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

func graphObservation(boundary awscloud.Boundary, graph Graph) awscloud.ResourceObservation {
	graphARN := strings.TrimSpace(graph.ARN)
	id := strings.TrimSpace(graph.ID)
	name := strings.TrimSpace(graph.Name)
	resourceID := firstNonEmpty(graphARN, id, name)
	attributes := map[string]any{
		"graph_id":            id,
		"kms_key_id":          strings.TrimSpace(graph.KMSKeyID),
		"provisioned_memory":  graph.ProvisionedMemory,
		"replica_count":       graph.ReplicaCount,
		"public_connectivity": graph.PublicConnectivity,
		"deletion_protection": graph.DeletionProtection,
		"endpoint_address":    strings.TrimSpace(graph.EndpointAddress),
		"vector_search":       graph.VectorSearchDimension != nil,
	}
	if graph.VectorSearchDimension != nil {
		attributes["vector_search_dimension"] = *graph.VectorSearchDimension
	}
	return awscloud.ResourceObservation{
		Boundary:           boundary,
		ARN:                graphARN,
		ResourceID:         resourceID,
		ResourceType:       awscloud.ResourceTypeNeptuneGraph,
		Name:               name,
		State:              strings.TrimSpace(graph.Status),
		Tags:               cloneStringMap(graph.Tags),
		Attributes:         attributes,
		CorrelationAnchors: []string{graphARN, id, name, strings.TrimSpace(graph.EndpointAddress)},
		SourceRecordID:     resourceID,
	}
}

func graphSnapshotObservation(boundary awscloud.Boundary, snapshot GraphSnapshot) awscloud.ResourceObservation {
	snapshotARN := strings.TrimSpace(snapshot.ARN)
	id := strings.TrimSpace(snapshot.ID)
	name := strings.TrimSpace(snapshot.Name)
	resourceID := firstNonEmpty(snapshotARN, id, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          snapshotARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeNeptuneGraphSnapshot,
		Name:         name,
		State:        strings.TrimSpace(snapshot.Status),
		Tags:         cloneStringMap(snapshot.Tags),
		Attributes: map[string]any{
			"graph_snapshot_id": id,
			"kms_key_id":        strings.TrimSpace(snapshot.KMSKeyID),
			"source_graph_id":   strings.TrimSpace(snapshot.SourceGraphID),
		},
		CorrelationAnchors: []string{snapshotARN, id, name},
		SourceRecordID:     resourceID,
	}
}
