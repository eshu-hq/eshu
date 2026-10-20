// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package emr

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits EMR cluster, instance group, instance fleet, security
// configuration, EMR Serverless application, EMR Studio, and Studio
// session-mapping metadata facts plus relationship facts for one claimed
// account and region. It is metadata-only: step command lines, bootstrap
// action script bodies, security configuration policy bodies, and Serverless
// job-run entry-point arguments are never observed or persisted.
type Scanner struct {
	Client Client
}

// Scan observes EMR resources through the configured client.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("emr scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceEMR:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceEMR
	default:
		return nil, fmt.Errorf("emr scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	clusters, err := s.Client.ListClusters(ctx)
	if err != nil {
		return nil, fmt.Errorf("list EMR clusters: %w", err)
	}
	for _, cluster := range clusters {
		clusterEnvelopes, err := clusterEnvelopes(boundary, cluster)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, clusterEnvelopes...)
	}

	configs, err := s.Client.ListSecurityConfigurations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list EMR security configurations: %w", err)
	}
	for _, config := range configs {
		resource, err := awscloud.NewResourceEnvelope(securityConfigurationObservation(boundary, config))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, resource)
	}

	applications, err := s.Client.ListServerlessApplications(ctx)
	if err != nil {
		return nil, fmt.Errorf("list EMR Serverless applications: %w", err)
	}
	for _, application := range applications {
		applicationEnvelopes, err := serverlessApplicationEnvelopes(boundary, application)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, applicationEnvelopes...)
	}

	studios, err := s.Client.ListStudios(ctx)
	if err != nil {
		return nil, fmt.Errorf("list EMR Studios: %w", err)
	}
	for _, studio := range studios {
		studioEnvelopes, err := studioEnvelopes(boundary, studio)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, studioEnvelopes...)
	}

	return envelopes, nil
}

func clusterEnvelopes(boundary awscloud.Boundary, cluster Cluster) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(clusterObservation(boundary, cluster))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	for _, group := range cluster.InstanceGroups {
		groupResource, err := awscloud.NewResourceEnvelope(instanceGroupObservation(boundary, cluster, group))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, groupResource)
		if relationship, ok := clusterInstanceGroupRelationship(boundary, cluster, group); ok {
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}

	for _, fleet := range cluster.InstanceFleets {
		fleetResource, err := awscloud.NewResourceEnvelope(instanceFleetObservation(boundary, cluster, fleet))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, fleetResource)
		if relationship, ok := clusterInstanceFleetRelationship(boundary, cluster, fleet); ok {
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}

	for _, observation := range clusterRelationships(boundary, cluster) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func clusterObservation(boundary awscloud.Boundary, cluster Cluster) awscloud.ResourceObservation {
	clusterARN := strings.TrimSpace(cluster.ARN)
	clusterID := firstNonEmpty(clusterARN, cluster.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          clusterARN,
		ResourceID:   clusterID,
		ResourceType: awscloud.ResourceTypeEMRCluster,
		Name:         strings.TrimSpace(cluster.Name),
		State:        strings.TrimSpace(cluster.State),
		Tags:         cloneStringMap(cluster.Tags),
		Attributes: map[string]any{
			"cluster_id":                strings.TrimSpace(cluster.ID),
			"release_label":             strings.TrimSpace(cluster.ReleaseLabel),
			"applications":              cloneStrings(cluster.Applications),
			"service_role":              strings.TrimSpace(cluster.ServiceRole),
			"auto_scaling_role":         strings.TrimSpace(cluster.AutoScalingRole),
			"instance_profile":          strings.TrimSpace(cluster.InstanceProfile),
			"security_configuration":    strings.TrimSpace(cluster.SecurityConfigName),
			"log_encryption_kms_key_id": strings.TrimSpace(cluster.LogEncryptionKMSKey),
			"log_uri":                   strings.TrimSpace(cluster.LogURI),
			"master_public_dns_name":    strings.TrimSpace(cluster.MasterPublicDNSName),
			"scale_down_behavior":       strings.TrimSpace(cluster.ScaleDownBehavior),
			"auto_terminate":            cluster.AutoTerminate,
			"termination_protected":     cluster.TerminationProtected,
			"visible_to_all_users":      cluster.VisibleToAllUsers,
			"instance_collection_type":  strings.TrimSpace(cluster.InstanceCollection),
			"ec2_subnet_id":             strings.TrimSpace(cluster.SubnetID),
			"requested_ec2_subnet_ids":  cloneStrings(cluster.RequestedSubnetIDs),
			"ec2_availability_zone":     strings.TrimSpace(cluster.AvailabilityZone),
			"created_at":                timeOrNil(cluster.CreatedAt),
			"ready_at":                  timeOrNil(cluster.ReadyAt),
			"ended_at":                  timeOrNil(cluster.EndedAt),
		},
		CorrelationAnchors: []string{clusterARN, strings.TrimSpace(cluster.ID)},
		SourceRecordID:     clusterID,
	}
}

func instanceGroupObservation(
	boundary awscloud.Boundary,
	cluster Cluster,
	group InstanceGroup,
) awscloud.ResourceObservation {
	groupID := scopedID(cluster, group.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   groupID,
		ResourceType: awscloud.ResourceTypeEMRInstanceGroup,
		Name:         strings.TrimSpace(group.Name),
		State:        strings.TrimSpace(group.State),
		Attributes: map[string]any{
			"instance_group_id":        strings.TrimSpace(group.ID),
			"cluster_id":               strings.TrimSpace(cluster.ID),
			"cluster_arn":              strings.TrimSpace(cluster.ARN),
			"instance_group_type":      strings.TrimSpace(group.GroupType),
			"instance_type":            strings.TrimSpace(group.InstanceType),
			"market":                   strings.TrimSpace(group.Market),
			"requested_instance_count": group.RequestedSize,
			"running_instance_count":   group.RunningSize,
		},
		CorrelationAnchors: []string{groupID, strings.TrimSpace(group.ID)},
		SourceRecordID:     groupID,
	}
}

func instanceFleetObservation(
	boundary awscloud.Boundary,
	cluster Cluster,
	fleet InstanceFleet,
) awscloud.ResourceObservation {
	fleetID := scopedID(cluster, fleet.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   fleetID,
		ResourceType: awscloud.ResourceTypeEMRInstanceFleet,
		Name:         strings.TrimSpace(fleet.Name),
		State:        strings.TrimSpace(fleet.State),
		Attributes: map[string]any{
			"instance_fleet_id":         strings.TrimSpace(fleet.ID),
			"cluster_id":                strings.TrimSpace(cluster.ID),
			"cluster_arn":               strings.TrimSpace(cluster.ARN),
			"instance_fleet_type":       strings.TrimSpace(fleet.FleetType),
			"target_on_demand_capacity": fleet.TargetOnDemandCapacity,
			"target_spot_capacity":      fleet.TargetSpotCapacity,
			"provisioned_on_demand":     fleet.ProvisionedOnDemand,
			"provisioned_spot":          fleet.ProvisionedSpot,
			"instance_type_specs":       cloneStrings(fleet.InstanceTypeSpecs),
		},
		CorrelationAnchors: []string{fleetID, strings.TrimSpace(fleet.ID)},
		SourceRecordID:     fleetID,
	}
}

func securityConfigurationObservation(
	boundary awscloud.Boundary,
	config SecurityConfiguration,
) awscloud.ResourceObservation {
	name := strings.TrimSpace(config.Name)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   name,
		ResourceType: awscloud.ResourceTypeEMRSecurityConfiguration,
		Name:         name,
		Attributes: map[string]any{
			"created_at": timeOrNil(config.CreatedAt),
		},
		CorrelationAnchors: []string{name},
		SourceRecordID:     name,
	}
}

func serverlessApplicationEnvelopes(
	boundary awscloud.Boundary,
	application ServerlessApplication,
) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(serverlessApplicationObservation(boundary, application))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}
	for _, observation := range serverlessApplicationRelationships(boundary, application) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func serverlessApplicationObservation(
	boundary awscloud.Boundary,
	application ServerlessApplication,
) awscloud.ResourceObservation {
	applicationARN := strings.TrimSpace(application.ARN)
	applicationID := firstNonEmpty(applicationARN, application.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          applicationARN,
		ResourceID:   applicationID,
		ResourceType: awscloud.ResourceTypeEMRServerlessApplication,
		Name:         strings.TrimSpace(application.Name),
		State:        strings.TrimSpace(application.State),
		Tags:         cloneStringMap(application.Tags),
		Attributes: map[string]any{
			"application_id":              strings.TrimSpace(application.ID),
			"release_label":               strings.TrimSpace(application.ReleaseLabel),
			"type":                        strings.TrimSpace(application.Type),
			"architecture":                strings.TrimSpace(application.Architecture),
			"image_uri":                   strings.TrimSpace(application.ImageURI),
			"disk_encryption_kms_key_arn": strings.TrimSpace(application.DiskEncryptKMS),
			"subnet_ids":                  cloneStrings(application.SubnetIDs),
			"security_group_ids":          cloneStrings(application.SecurityGroupIDs),
			"created_at":                  timeOrNil(application.CreatedAt),
			"updated_at":                  timeOrNil(application.UpdatedAt),
		},
		CorrelationAnchors: []string{applicationARN, strings.TrimSpace(application.ID)},
		SourceRecordID:     applicationID,
	}
}

func studioEnvelopes(boundary awscloud.Boundary, studio Studio) ([]facts.Envelope, error) {
	resource, err := awscloud.NewResourceEnvelope(studioObservation(boundary, studio))
	if err != nil {
		return nil, err
	}
	envelopes := []facts.Envelope{resource}

	for _, mapping := range studio.SessionMappings {
		mappingResource, err := awscloud.NewResourceEnvelope(sessionMappingObservation(boundary, studio, mapping))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, mappingResource)
		if relationship, ok := studioSessionMappingRelationship(boundary, studio, mapping); ok {
			envelope, err := awscloud.NewRelationshipEnvelope(relationship)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
		}
	}

	for _, observation := range studioRelationships(boundary, studio) {
		envelope, err := awscloud.NewRelationshipEnvelope(observation)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func studioObservation(boundary awscloud.Boundary, studio Studio) awscloud.ResourceObservation {
	studioARN := strings.TrimSpace(studio.ARN)
	studioID := firstNonEmpty(studioARN, studio.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          studioARN,
		ResourceID:   studioID,
		ResourceType: awscloud.ResourceTypeEMRStudio,
		Name:         strings.TrimSpace(studio.Name),
		Tags:         cloneStringMap(studio.Tags),
		Attributes: map[string]any{
			"studio_id":                   strings.TrimSpace(studio.ID),
			"auth_mode":                   strings.TrimSpace(studio.AuthMode),
			"vpc_id":                      strings.TrimSpace(studio.VPCID),
			"subnet_ids":                  cloneStrings(studio.SubnetIDs),
			"engine_security_group_id":    strings.TrimSpace(studio.EngineSecGroupID),
			"workspace_security_group_id": strings.TrimSpace(studio.WorkspaceSecGroup),
			"service_role":                strings.TrimSpace(studio.ServiceRole),
			"user_role":                   strings.TrimSpace(studio.UserRole),
			"encryption_key_arn":          strings.TrimSpace(studio.EncryptionKeyARN),
			"url":                         strings.TrimSpace(studio.URL),
			"default_s3_location":         strings.TrimSpace(studio.DefaultS3Location),
			"created_at":                  timeOrNil(studio.CreatedAt),
		},
		CorrelationAnchors: []string{studioARN, strings.TrimSpace(studio.ID)},
		SourceRecordID:     studioID,
	}
}

func sessionMappingObservation(
	boundary awscloud.Boundary,
	studio Studio,
	mapping StudioSessionMapping,
) awscloud.ResourceObservation {
	mappingID := sessionMappingID(studio, mapping)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   mappingID,
		ResourceType: awscloud.ResourceTypeEMRStudioSessionMapping,
		Name:         strings.TrimSpace(mapping.IdentityName),
		Attributes: map[string]any{
			"studio_id":          strings.TrimSpace(studio.ID),
			"studio_arn":         strings.TrimSpace(studio.ARN),
			"identity_id":        strings.TrimSpace(mapping.IdentityID),
			"identity_name":      strings.TrimSpace(mapping.IdentityName),
			"identity_type":      strings.TrimSpace(mapping.IdentityType),
			"session_policy_arn": strings.TrimSpace(mapping.SessionPolicyARN),
			"created_at":         timeOrNil(mapping.CreatedAt),
		},
		CorrelationAnchors: []string{mappingID},
		SourceRecordID:     mappingID,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		trimmed := strings.TrimSpace(key)
		if trimmed == "" {
			continue
		}
		output[trimmed] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	output := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
