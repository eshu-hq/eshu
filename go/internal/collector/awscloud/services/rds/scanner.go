package rds

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS RDS metadata facts for one claimed account and region. It
// never connects to databases, reads snapshots or logs, persists usernames, or
// observes schemas, tables, or row data.
type Scanner struct {
	Client Client
}

// Scan observes RDS instances, clusters, subnet groups, and direct dependency
// metadata through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("rds scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "":
		boundary.ServiceKind = awscloud.ServiceRDS
	case awscloud.ServiceRDS:
	default:
		return nil, fmt.Errorf("rds scanner received service_kind %q", boundary.ServiceKind)
	}

	clusters, err := s.Client.ListDBClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list RDS DB clusters: %w", err)
	}
	subnetGroups, err := s.Client.ListDBSubnetGroups(ctx)
	if err != nil {
		return nil, fmt.Errorf("list RDS DB subnet groups: %w", err)
	}
	instances, err := s.Client.ListDBInstances(ctx)
	if err != nil {
		return nil, fmt.Errorf("list RDS DB instances: %w", err)
	}

	clusterIDs := clusterIdentityMap(clusters)
	clusterMemberships := clusterMembershipMap(clusters)
	subnetGroupIDs := subnetGroupIdentityMap(subnetGroups)
	var envelopes []facts.Envelope
	for _, cluster := range clusters {
		clusterEnvelopes, err := clusterEnvelopes(boundary, cluster, subnetGroupIDs)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, clusterEnvelopes...)
	}
	for _, subnetGroup := range subnetGroups {
		resource, err := awscloud.NewResourceEnvelope(subnetGroupObservation(boundary, subnetGroup))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}
	for _, instance := range instances {
		instanceEnvelopes, err := instanceEnvelopes(
			boundary,
			instance,
			clusterIDs,
			clusterMemberships,
			subnetGroupIDs,
		)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, instanceEnvelopes...)
	}
	return envelopes, nil
}

func instanceEnvelopes(
	boundary awscloud.Boundary,
	instance DBInstance,
	clusterIDs map[string]string,
	clusterMemberships map[string]clusterMembership,
	subnetGroupIDs map[string]string,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(instanceObservation(boundary, instance))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range instanceRelationships(
		boundary,
		instance,
		clusterIDs,
		clusterMemberships,
		subnetGroupIDs,
	) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func clusterEnvelopes(
	boundary awscloud.Boundary,
	cluster DBCluster,
	subnetGroupIDs map[string]string,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(clusterObservation(boundary, cluster))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, relationship := range clusterRelationships(boundary, cluster, subnetGroupIDs) {
		envelope, err := awscloud.NewRelationshipEnvelope(relationship)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func instanceObservation(boundary awscloud.Boundary, instance DBInstance) awscloud.ResourceObservation {
	instanceARN := strings.TrimSpace(instance.ARN)
	identifier := strings.TrimSpace(instance.Identifier)
	resourceID := firstNonEmpty(instanceARN, instance.ResourceID, identifier)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          instanceARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeRDSDBInstance,
		Name:         identifier,
		State:        strings.TrimSpace(instance.Status),
		Tags:         cloneStringMap(instance.Tags),
		Attributes:   instanceAttributes(instance),
		CorrelationAnchors: []string{
			instanceARN,
			identifier,
			strings.TrimSpace(instance.ResourceID),
			strings.TrimSpace(instance.EndpointAddress),
		},
		SourceRecordID: resourceID,
	}
}

func instanceAttributes(instance DBInstance) map[string]any {
	return map[string]any{
		"dbi_resource_id":                     strings.TrimSpace(instance.ResourceID),
		"class":                               strings.TrimSpace(instance.Class),
		"engine":                              strings.TrimSpace(instance.Engine),
		"engine_version":                      strings.TrimSpace(instance.EngineVersion),
		"endpoint_address":                    strings.TrimSpace(instance.EndpointAddress),
		"endpoint_port":                       instance.EndpointPort,
		"hosted_zone_id":                      strings.TrimSpace(instance.HostedZoneID),
		"availability_zone":                   strings.TrimSpace(instance.AvailabilityZone),
		"secondary_availability_zone":         strings.TrimSpace(instance.SecondaryAvailabilityZone),
		"multi_az":                            instance.MultiAZ,
		"publicly_accessible":                 instance.PubliclyAccessible,
		"storage_encrypted":                   instance.StorageEncrypted,
		"kms_key_id":                          strings.TrimSpace(instance.KMSKeyID),
		"iam_database_authentication_enabled": instance.IAMDatabaseAuthenticationEnabled,
		"deletion_protection":                 instance.DeletionProtection,
		"backup_retention_period":             instance.BackupRetentionPeriod,
		"db_subnet_group_name":                strings.TrimSpace(instance.DBSubnetGroupName),
		"vpc_id":                              strings.TrimSpace(instance.VPCID),
		"vpc_security_group_ids":              cloneStrings(instance.VPCSecurityGroupIDs),
		"cluster_identifier":                  strings.TrimSpace(instance.ClusterIdentifier),
		"parameter_groups":                    parameterGroupMaps(instance.ParameterGroups),
		"option_groups":                       optionGroupMaps(instance.OptionGroups),
		"monitoring_role_arn":                 strings.TrimSpace(instance.MonitoringRoleARN),
		"performance_insights_enabled":        instance.PerformanceInsightsEnabled,
		"performance_insights_kms_key_id":     strings.TrimSpace(instance.PerformanceInsightsKMSKeyID),
	}
}

func clusterObservation(boundary awscloud.Boundary, cluster DBCluster) awscloud.ResourceObservation {
	clusterARN := strings.TrimSpace(cluster.ARN)
	identifier := strings.TrimSpace(cluster.Identifier)
	resourceID := firstNonEmpty(clusterARN, cluster.ResourceID, identifier)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          clusterARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeRDSDBCluster,
		Name:         identifier,
		State:        strings.TrimSpace(cluster.Status),
		Tags:         cloneStringMap(cluster.Tags),
		Attributes:   clusterAttributes(cluster),
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

func clusterAttributes(cluster DBCluster) map[string]any {
	return map[string]any{
		"cluster_resource_id":                 strings.TrimSpace(cluster.ResourceID),
		"engine":                              strings.TrimSpace(cluster.Engine),
		"engine_version":                      strings.TrimSpace(cluster.EngineVersion),
		"endpoint_address":                    strings.TrimSpace(cluster.EndpointAddress),
		"reader_endpoint_address":             strings.TrimSpace(cluster.ReaderEndpointAddress),
		"hosted_zone_id":                      strings.TrimSpace(cluster.HostedZoneID),
		"port":                                cluster.Port,
		"multi_az":                            cluster.MultiAZ,
		"storage_encrypted":                   cluster.StorageEncrypted,
		"kms_key_id":                          strings.TrimSpace(cluster.KMSKeyID),
		"iam_database_authentication_enabled": cluster.IAMDatabaseAuthenticationEnabled,
		"deletion_protection":                 cluster.DeletionProtection,
		"backup_retention_period":             cluster.BackupRetentionPeriod,
		"db_subnet_group_name":                strings.TrimSpace(cluster.DBSubnetGroupName),
		"vpc_security_group_ids":              cloneStrings(cluster.VPCSecurityGroupIDs),
		"member_instance_ids":                 clusterMemberIDs(cluster.Members),
		"writer_instance_ids":                 writerInstanceIDs(cluster.Members),
		"db_cluster_parameter_group":          strings.TrimSpace(cluster.ParameterGroup),
		"associated_role_arns":                cloneStrings(cluster.AssociatedRoleARNs),
	}
}

func subnetGroupObservation(
	boundary awscloud.Boundary,
	subnetGroup DBSubnetGroup,
) awscloud.ResourceObservation {
	subnetGroupARN := strings.TrimSpace(subnetGroup.ARN)
	name := strings.TrimSpace(subnetGroup.Name)
	resourceID := firstNonEmpty(subnetGroupARN, name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          subnetGroupARN,
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeRDSDBSubnetGroup,
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
