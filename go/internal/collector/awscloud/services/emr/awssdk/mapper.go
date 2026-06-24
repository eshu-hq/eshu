// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"github.com/aws/aws-sdk-go-v2/aws"
	emrtypes "github.com/aws/aws-sdk-go-v2/service/emr/types"
	emrserverlesstypes "github.com/aws/aws-sdk-go-v2/service/emrserverless/types"

	emrservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/emr"
)

func mapCluster(cluster emrtypes.Cluster) emrservice.Cluster {
	mapped := emrservice.Cluster{
		ARN:                  aws.ToString(cluster.ClusterArn),
		ID:                   aws.ToString(cluster.Id),
		Name:                 aws.ToString(cluster.Name),
		ReleaseLabel:         aws.ToString(cluster.ReleaseLabel),
		Applications:         clusterApplications(cluster.Applications),
		ServiceRole:          aws.ToString(cluster.ServiceRole),
		AutoScalingRole:      aws.ToString(cluster.AutoScalingRole),
		SecurityConfigName:   aws.ToString(cluster.SecurityConfiguration),
		LogEncryptionKMSKey:  aws.ToString(cluster.LogEncryptionKmsKeyId),
		LogURI:               aws.ToString(cluster.LogUri),
		MasterPublicDNSName:  aws.ToString(cluster.MasterPublicDnsName),
		ScaleDownBehavior:    string(cluster.ScaleDownBehavior),
		AutoTerminate:        aws.ToBool(cluster.AutoTerminate),
		TerminationProtected: aws.ToBool(cluster.TerminationProtected),
		VisibleToAllUsers:    aws.ToBool(cluster.VisibleToAllUsers),
		InstanceCollection:   string(cluster.InstanceCollectionType),
		Tags:                 mapTags(cluster.Tags),
	}
	if status := cluster.Status; status != nil {
		mapped.State = string(status.State)
		if timeline := status.Timeline; timeline != nil {
			mapped.CreatedAt = aws.ToTime(timeline.CreationDateTime)
			mapped.ReadyAt = aws.ToTime(timeline.ReadyDateTime)
			mapped.EndedAt = aws.ToTime(timeline.EndDateTime)
		}
	}
	if attrs := cluster.Ec2InstanceAttributes; attrs != nil {
		mapped.InstanceProfile = aws.ToString(attrs.IamInstanceProfile)
		mapped.SubnetID = aws.ToString(attrs.Ec2SubnetId)
		mapped.RequestedSubnetIDs = cloneStrings(attrs.RequestedEc2SubnetIds)
		mapped.AvailabilityZone = aws.ToString(attrs.Ec2AvailabilityZone)
		mapped.SecurityGroupIDs = clusterSecurityGroups(attrs)
	}
	return mapped
}

func clusterApplications(applications []emrtypes.Application) []string {
	if len(applications) == 0 {
		return nil
	}
	names := make([]string, 0, len(applications))
	for _, application := range applications {
		if name := aws.ToString(application.Name); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// clusterSecurityGroups collects every EC2 security group an EMR on EC2 cluster
// references: managed master/slave groups, the service access group, and the
// additional master/slave groups.
func clusterSecurityGroups(attrs *emrtypes.Ec2InstanceAttributes) []string {
	var groups []string
	add := func(id string) {
		if id != "" {
			groups = append(groups, id)
		}
	}
	add(aws.ToString(attrs.EmrManagedMasterSecurityGroup))
	add(aws.ToString(attrs.EmrManagedSlaveSecurityGroup))
	add(aws.ToString(attrs.ServiceAccessSecurityGroup))
	groups = append(groups, cloneStrings(attrs.AdditionalMasterSecurityGroups)...)
	groups = append(groups, cloneStrings(attrs.AdditionalSlaveSecurityGroups)...)
	return groups
}

func mapInstanceGroup(group emrtypes.InstanceGroup) emrservice.InstanceGroup {
	mapped := emrservice.InstanceGroup{
		ID:            aws.ToString(group.Id),
		Name:          aws.ToString(group.Name),
		GroupType:     string(group.InstanceGroupType),
		InstanceType:  aws.ToString(group.InstanceType),
		Market:        string(group.Market),
		RequestedSize: aws.ToInt32(group.RequestedInstanceCount),
		RunningSize:   aws.ToInt32(group.RunningInstanceCount),
	}
	if status := group.Status; status != nil {
		mapped.State = string(status.State)
	}
	return mapped
}

func mapInstanceFleet(fleet emrtypes.InstanceFleet) emrservice.InstanceFleet {
	mapped := emrservice.InstanceFleet{
		ID:                     aws.ToString(fleet.Id),
		Name:                   aws.ToString(fleet.Name),
		FleetType:              string(fleet.InstanceFleetType),
		TargetOnDemandCapacity: aws.ToInt32(fleet.TargetOnDemandCapacity),
		TargetSpotCapacity:     aws.ToInt32(fleet.TargetSpotCapacity),
		ProvisionedOnDemand:    aws.ToInt32(fleet.ProvisionedOnDemandCapacity),
		ProvisionedSpot:        aws.ToInt32(fleet.ProvisionedSpotCapacity),
		InstanceTypeSpecs:      fleetInstanceTypes(fleet.InstanceTypeSpecifications),
	}
	if status := fleet.Status; status != nil {
		mapped.State = string(status.State)
	}
	return mapped
}

func fleetInstanceTypes(specs []emrtypes.InstanceTypeSpecification) []string {
	if len(specs) == 0 {
		return nil
	}
	types := make([]string, 0, len(specs))
	for _, spec := range specs {
		if instanceType := aws.ToString(spec.InstanceType); instanceType != "" {
			types = append(types, instanceType)
		}
	}
	return types
}

func mapStudio(studio emrtypes.Studio) emrservice.Studio {
	return emrservice.Studio{
		ARN:               aws.ToString(studio.StudioArn),
		ID:                aws.ToString(studio.StudioId),
		Name:              aws.ToString(studio.Name),
		AuthMode:          string(studio.AuthMode),
		VPCID:             aws.ToString(studio.VpcId),
		SubnetIDs:         cloneStrings(studio.SubnetIds),
		EngineSecGroupID:  aws.ToString(studio.EngineSecurityGroupId),
		WorkspaceSecGroup: aws.ToString(studio.WorkspaceSecurityGroupId),
		ServiceRole:       aws.ToString(studio.ServiceRole),
		UserRole:          aws.ToString(studio.UserRole),
		EncryptionKeyARN:  aws.ToString(studio.EncryptionKeyArn),
		URL:               aws.ToString(studio.Url),
		DefaultS3Location: aws.ToString(studio.DefaultS3Location),
		CreatedAt:         aws.ToTime(studio.CreationTime),
		Tags:              mapTags(studio.Tags),
	}
}

func mapSessionMapping(summary emrtypes.SessionMappingSummary) emrservice.StudioSessionMapping {
	return emrservice.StudioSessionMapping{
		StudioID:         aws.ToString(summary.StudioId),
		IdentityID:       aws.ToString(summary.IdentityId),
		IdentityName:     aws.ToString(summary.IdentityName),
		IdentityType:     string(summary.IdentityType),
		SessionPolicyARN: aws.ToString(summary.SessionPolicyArn),
		CreatedAt:        aws.ToTime(summary.CreationTime),
	}
}

func mapServerlessApplication(application emrserverlesstypes.Application) emrservice.ServerlessApplication {
	mapped := emrservice.ServerlessApplication{
		ARN:          aws.ToString(application.Arn),
		ID:           aws.ToString(application.ApplicationId),
		Name:         aws.ToString(application.Name),
		State:        string(application.State),
		ReleaseLabel: aws.ToString(application.ReleaseLabel),
		Type:         aws.ToString(application.Type),
		Architecture: string(application.Architecture),
		CreatedAt:    aws.ToTime(application.CreatedAt),
		UpdatedAt:    aws.ToTime(application.UpdatedAt),
		Tags:         cloneStringMap(application.Tags),
	}
	if network := application.NetworkConfiguration; network != nil {
		mapped.SubnetIDs = cloneStrings(network.SubnetIds)
		mapped.SecurityGroupIDs = cloneStrings(network.SecurityGroupIds)
	}
	if encryption := application.DiskEncryptionConfiguration; encryption != nil {
		mapped.DiskEncryptKMS = aws.ToString(encryption.EncryptionKeyArn)
	}
	if image := application.ImageConfiguration; image != nil {
		mapped.ImageURI = aws.ToString(image.ImageUri)
	}
	return mapped
}

// mapTags converts EMR key/value tag pairs into a scanner-owned map, dropping
// empty keys.
func mapTags(tags []emrtypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := aws.ToString(tag.Key)
		if key == "" {
			continue
		}
		output[key] = aws.ToString(tag.Value)
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
		if value != "" {
			output = append(output, value)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func cloneStringMap(input map[string]string) map[string]string {
	if len(input) == 0 {
		return nil
	}
	output := make(map[string]string, len(input))
	for key, value := range input {
		if key == "" {
			continue
		}
		output[key] = value
	}
	if len(output) == 0 {
		return nil
	}
	return output
}
