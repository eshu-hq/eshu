// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsredshifttypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
	awsserverlesstypes "github.com/aws/aws-sdk-go-v2/service/redshiftserverless/types"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	redshiftservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/redshift"
)

func mapCluster(raw awsredshifttypes.Cluster, boundary awscloud.Boundary) redshiftservice.Cluster {
	identifier := strings.TrimSpace(aws.ToString(raw.ClusterIdentifier))
	return redshiftservice.Cluster{
		ARN:                              clusterARN(boundary, identifier),
		Identifier:                       identifier,
		NodeType:                         strings.TrimSpace(aws.ToString(raw.NodeType)),
		ClusterStatus:                    strings.TrimSpace(aws.ToString(raw.ClusterStatus)),
		ClusterAvailabilityStatus:        strings.TrimSpace(aws.ToString(raw.ClusterAvailabilityStatus)),
		DBName:                           strings.TrimSpace(aws.ToString(raw.DBName)),
		Endpoint:                         endpointAddress(raw.Endpoint),
		EndpointPort:                     endpointPort(raw.Endpoint),
		ClusterCreateTime:                aws.ToTime(raw.ClusterCreateTime),
		AutomatedSnapshotRetentionPeriod: aws.ToInt32(raw.AutomatedSnapshotRetentionPeriod),
		ManualSnapshotRetentionPeriod:    aws.ToInt32(raw.ManualSnapshotRetentionPeriod),
		ClusterSecurityGroups:            clusterSecurityGroups(raw.ClusterSecurityGroups),
		VPCSecurityGroupIDs:              vpcSecurityGroupIDs(raw.VpcSecurityGroups),
		ClusterParameterGroup:            firstParameterGroupName(raw.ClusterParameterGroups),
		ClusterSubnetGroupName:           strings.TrimSpace(aws.ToString(raw.ClusterSubnetGroupName)),
		VPCID:                            strings.TrimSpace(aws.ToString(raw.VpcId)),
		AvailabilityZone:                 strings.TrimSpace(aws.ToString(raw.AvailabilityZone)),
		PreferredMaintenanceWindow:       strings.TrimSpace(aws.ToString(raw.PreferredMaintenanceWindow)),
		PendingModifiedValuesPresent:     raw.PendingModifiedValues != nil,
		ClusterVersion:                   strings.TrimSpace(aws.ToString(raw.ClusterVersion)),
		AllowVersionUpgrade:              aws.ToBool(raw.AllowVersionUpgrade),
		NumberOfNodes:                    aws.ToInt32(raw.NumberOfNodes),
		PubliclyAccessible:               aws.ToBool(raw.PubliclyAccessible),
		Encrypted:                        aws.ToBool(raw.Encrypted),
		KMSKeyID:                         strings.TrimSpace(aws.ToString(raw.KmsKeyId)),
		EnhancedVPCRouting:               aws.ToBool(raw.EnhancedVpcRouting),
		IAMRoleARNs:                      clusterIAMRoleARNs(raw.IamRoles),
		MaintenanceTrackName:             strings.TrimSpace(aws.ToString(raw.MaintenanceTrackName)),
		DeferredMaintenanceWindows:       deferredMaintenanceWindowIDs(raw.DeferredMaintenanceWindows),
		NextMaintenanceWindowStartTime:   aws.ToTime(raw.NextMaintenanceWindowStartTime),
		AvailabilityZoneRelocationStatus: strings.TrimSpace(aws.ToString(raw.AvailabilityZoneRelocationStatus)),
		MultiAZ:                          strings.EqualFold(strings.TrimSpace(aws.ToString(raw.MultiAZ)), "enabled"),
		Tags:                             redshiftTagsMap(raw.Tags),
	}
}

func mapClusterParameterGroup(
	boundary awscloud.Boundary,
	raw awsredshifttypes.ClusterParameterGroup,
) redshiftservice.ClusterParameterGroup {
	name := strings.TrimSpace(aws.ToString(raw.ParameterGroupName))
	return redshiftservice.ClusterParameterGroup{
		ARN:         parameterGroupARN(boundary, name),
		Name:        name,
		Family:      strings.TrimSpace(aws.ToString(raw.ParameterGroupFamily)),
		Description: strings.TrimSpace(aws.ToString(raw.Description)),
		Tags:        redshiftTagsMap(raw.Tags),
	}
}

func mapClusterSubnetGroup(
	boundary awscloud.Boundary,
	raw awsredshifttypes.ClusterSubnetGroup,
) redshiftservice.ClusterSubnetGroup {
	name := strings.TrimSpace(aws.ToString(raw.ClusterSubnetGroupName))
	return redshiftservice.ClusterSubnetGroup{
		ARN:         subnetGroupARN(boundary, name),
		Name:        name,
		VPCID:       strings.TrimSpace(aws.ToString(raw.VpcId)),
		Description: strings.TrimSpace(aws.ToString(raw.Description)),
		Status:      strings.TrimSpace(aws.ToString(raw.SubnetGroupStatus)),
		SubnetIDs:   subnetIDs(raw.Subnets),
		Tags:        redshiftTagsMap(raw.Tags),
	}
}

func mapClusterSnapshot(
	boundary awscloud.Boundary,
	raw awsredshifttypes.Snapshot,
) redshiftservice.ClusterSnapshot {
	identifier := strings.TrimSpace(aws.ToString(raw.SnapshotIdentifier))
	clusterIdentifier := strings.TrimSpace(aws.ToString(raw.ClusterIdentifier))
	return redshiftservice.ClusterSnapshot{
		ARN:                           snapshotARN(boundary, clusterIdentifier, identifier),
		Identifier:                    identifier,
		ClusterIdentifier:             clusterIdentifier,
		SnapshotType:                  strings.TrimSpace(aws.ToString(raw.SnapshotType)),
		Status:                        strings.TrimSpace(aws.ToString(raw.Status)),
		NodeType:                      strings.TrimSpace(aws.ToString(raw.NodeType)),
		NumberOfNodes:                 aws.ToInt32(raw.NumberOfNodes),
		DBName:                        strings.TrimSpace(aws.ToString(raw.DBName)),
		VPCID:                         strings.TrimSpace(aws.ToString(raw.VpcId)),
		Encrypted:                     aws.ToBool(raw.Encrypted),
		KMSKeyID:                      strings.TrimSpace(aws.ToString(raw.KmsKeyId)),
		SnapshotCreateTime:            aws.ToTime(raw.SnapshotCreateTime),
		ClusterCreateTime:             aws.ToTime(raw.ClusterCreateTime),
		SnapshotRetentionStartTime:    aws.ToTime(raw.SnapshotRetentionStartTime),
		ManualSnapshotRetentionPeriod: aws.ToInt32(raw.ManualSnapshotRetentionPeriod),
		EngineFullVersion:             strings.TrimSpace(aws.ToString(raw.EngineFullVersion)),
		AvailabilityZone:              strings.TrimSpace(aws.ToString(raw.AvailabilityZone)),
		SourceRegion:                  strings.TrimSpace(aws.ToString(raw.SourceRegion)),
		Tags:                          redshiftTagsMap(raw.Tags),
		RestorableNodeTypes:           cloneRawStrings(raw.RestorableNodeTypes),
	}
}

func mapScheduledAction(raw awsredshifttypes.ScheduledAction) redshiftservice.ScheduledAction {
	return redshiftservice.ScheduledAction{
		Name:                    strings.TrimSpace(aws.ToString(raw.ScheduledActionName)),
		Schedule:                strings.TrimSpace(aws.ToString(raw.Schedule)),
		IAMRoleARN:              strings.TrimSpace(aws.ToString(raw.IamRole)),
		Description:             strings.TrimSpace(aws.ToString(raw.ScheduledActionDescription)),
		State:                   strings.TrimSpace(string(raw.State)),
		StartTime:               aws.ToTime(raw.StartTime),
		EndTime:                 aws.ToTime(raw.EndTime),
		NextInvocationTime:      firstNextInvocationTime(raw.NextInvocations),
		TargetActionName:        targetActionName(raw.TargetAction),
		TargetClusterIdentifier: targetActionClusterIdentifier(raw.TargetAction),
	}
}

func mapServerlessNamespace(
	raw awsserverlesstypes.Namespace,
	tags map[string]string,
) redshiftservice.ServerlessNamespace {
	return redshiftservice.ServerlessNamespace{
		ARN:            strings.TrimSpace(aws.ToString(raw.NamespaceArn)),
		Name:           strings.TrimSpace(aws.ToString(raw.NamespaceName)),
		NamespaceID:    strings.TrimSpace(aws.ToString(raw.NamespaceId)),
		Status:         strings.TrimSpace(string(raw.Status)),
		DBName:         strings.TrimSpace(aws.ToString(raw.DbName)),
		DefaultIAMRole: strings.TrimSpace(aws.ToString(raw.DefaultIamRoleArn)),
		IAMRoleARNs:    cloneRawStrings(raw.IamRoles),
		KMSKeyID:       strings.TrimSpace(aws.ToString(raw.KmsKeyId)),
		LogExports:     logExports(raw.LogExports),
		CreationDate:   aws.ToTime(raw.CreationDate),
		Tags:           tags,
	}
}

func mapServerlessWorkgroup(
	raw awsserverlesstypes.Workgroup,
	tags map[string]string,
) redshiftservice.ServerlessWorkgroup {
	return redshiftservice.ServerlessWorkgroup{
		ARN:                strings.TrimSpace(aws.ToString(raw.WorkgroupArn)),
		Name:               strings.TrimSpace(aws.ToString(raw.WorkgroupName)),
		WorkgroupID:        strings.TrimSpace(aws.ToString(raw.WorkgroupId)),
		NamespaceName:      strings.TrimSpace(aws.ToString(raw.NamespaceName)),
		Status:             strings.TrimSpace(string(raw.Status)),
		BaseCapacity:       aws.ToInt32(raw.BaseCapacity),
		MaxCapacity:        aws.ToInt32(raw.MaxCapacity),
		EnhancedVPCRouting: aws.ToBool(raw.EnhancedVpcRouting),
		PubliclyAccessible: aws.ToBool(raw.PubliclyAccessible),
		ConfigParameters:   serverlessConfigParameters(raw.ConfigParameters),
		SubnetIDs:          cloneRawStrings(raw.SubnetIds),
		SecurityGroupIDs:   cloneRawStrings(raw.SecurityGroupIds),
		EndpointAddress:    serverlessEndpointAddress(raw.Endpoint),
		EndpointPort:       serverlessEndpointPort(raw.Endpoint),
		CreationDate:       aws.ToTime(raw.CreationDate),
		Tags:               tags,
	}
}

// clusterARN constructs the well-formed cluster ARN from the claim boundary
// and the reported cluster identifier. The provisioned Redshift Cluster shape
// does not return a ClusterArn field, so the adapter synthesizes it instead of
// inventing identity from ClusterNamespaceArn (which addresses the namespace).
func clusterARN(boundary awscloud.Boundary, identifier string) string {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return ""
	}
	return "arn:" + awscloud.PartitionForBoundary(boundary) + ":redshift:" + boundary.Region + ":" + boundary.AccountID + ":cluster:" + identifier
}

func firstNextInvocationTime(values []time.Time) time.Time {
	for _, value := range values {
		if !value.IsZero() {
			return value
		}
	}
	return time.Time{}
}

func endpointAddress(endpoint *awsredshifttypes.Endpoint) string {
	if endpoint == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(endpoint.Address))
}

func endpointPort(endpoint *awsredshifttypes.Endpoint) int32 {
	if endpoint == nil {
		return 0
	}
	return aws.ToInt32(endpoint.Port)
}

func clusterSecurityGroups(groups []awsredshifttypes.ClusterSecurityGroupMembership) []string {
	var output []string
	for _, group := range groups {
		if name := strings.TrimSpace(aws.ToString(group.ClusterSecurityGroupName)); name != "" {
			output = append(output, name)
		}
	}
	return output
}

func vpcSecurityGroupIDs(groups []awsredshifttypes.VpcSecurityGroupMembership) []string {
	var ids []string
	for _, group := range groups {
		if id := strings.TrimSpace(aws.ToString(group.VpcSecurityGroupId)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func firstParameterGroupName(groups []awsredshifttypes.ClusterParameterGroupStatus) string {
	for _, group := range groups {
		if name := strings.TrimSpace(aws.ToString(group.ParameterGroupName)); name != "" {
			return name
		}
	}
	return ""
}

func clusterIAMRoleARNs(roles []awsredshifttypes.ClusterIamRole) []string {
	var arns []string
	for _, role := range roles {
		if arn := strings.TrimSpace(aws.ToString(role.IamRoleArn)); arn != "" {
			arns = append(arns, arn)
		}
	}
	return arns
}

func deferredMaintenanceWindowIDs(windows []awsredshifttypes.DeferredMaintenanceWindow) []string {
	var ids []string
	for _, window := range windows {
		if id := strings.TrimSpace(aws.ToString(window.DeferMaintenanceIdentifier)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func subnetIDs(subnets []awsredshifttypes.Subnet) []string {
	var ids []string
	for _, subnet := range subnets {
		if id := strings.TrimSpace(aws.ToString(subnet.SubnetIdentifier)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func targetActionName(action *awsredshifttypes.ScheduledActionType) string {
	if action == nil {
		return ""
	}
	switch {
	case action.PauseCluster != nil:
		return "PauseCluster"
	case action.ResumeCluster != nil:
		return "ResumeCluster"
	case action.ResizeCluster != nil:
		return "ResizeCluster"
	}
	return ""
}

func targetActionClusterIdentifier(action *awsredshifttypes.ScheduledActionType) string {
	if action == nil {
		return ""
	}
	switch {
	case action.PauseCluster != nil:
		return strings.TrimSpace(aws.ToString(action.PauseCluster.ClusterIdentifier))
	case action.ResumeCluster != nil:
		return strings.TrimSpace(aws.ToString(action.ResumeCluster.ClusterIdentifier))
	case action.ResizeCluster != nil:
		return strings.TrimSpace(aws.ToString(action.ResizeCluster.ClusterIdentifier))
	}
	return ""
}

func logExports(exports []awsserverlesstypes.LogExport) []string {
	var output []string
	for _, export := range exports {
		if value := strings.TrimSpace(string(export)); value != "" {
			output = append(output, value)
		}
	}
	return output
}

func serverlessConfigParameters(params []awsserverlesstypes.ConfigParameter) []redshiftservice.ServerlessConfigParameter {
	var output []redshiftservice.ServerlessConfigParameter
	for _, param := range params {
		key := strings.TrimSpace(aws.ToString(param.ParameterKey))
		if key == "" {
			continue
		}
		output = append(output, redshiftservice.ServerlessConfigParameter{
			Key:   key,
			Value: strings.TrimSpace(aws.ToString(param.ParameterValue)),
		})
	}
	return output
}

func serverlessEndpointAddress(endpoint *awsserverlesstypes.Endpoint) string {
	if endpoint == nil {
		return ""
	}
	return strings.TrimSpace(aws.ToString(endpoint.Address))
}

func serverlessEndpointPort(endpoint *awsserverlesstypes.Endpoint) int32 {
	if endpoint == nil {
		return 0
	}
	return aws.ToInt32(endpoint.Port)
}

func redshiftTagsMap(tags []awsredshifttypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
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

func serverlessTagsMap(tags []awsserverlesstypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
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

func cloneRawStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	output := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			output = append(output, trimmed)
		}
	}
	if len(output) == 0 {
		return nil
	}
	return output
}

func parameterGroupARN(boundary awscloud.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return "arn:" + awscloud.PartitionForBoundary(boundary) + ":redshift:" + boundary.Region + ":" + boundary.AccountID + ":parametergroup:" + name
}

func subnetGroupARN(boundary awscloud.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return "arn:" + awscloud.PartitionForBoundary(boundary) + ":redshift:" + boundary.Region + ":" + boundary.AccountID + ":subnetgroup:" + name
}

func snapshotARN(boundary awscloud.Boundary, clusterIdentifier string, identifier string) string {
	clusterIdentifier = strings.TrimSpace(clusterIdentifier)
	identifier = strings.TrimSpace(identifier)
	if clusterIdentifier == "" || identifier == "" {
		return ""
	}
	return "arn:" + awscloud.PartitionForBoundary(boundary) + ":redshift:" + boundary.Region + ":" + boundary.AccountID + ":snapshot:" + clusterIdentifier + "/" + identifier
}
