// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"strings"
	"time"

	awsv2 "github.com/aws/aws-sdk-go-v2/aws"
	awsredshifttypes "github.com/aws/aws-sdk-go-v2/service/redshift/types"
	awsserverlesstypes "github.com/aws/aws-sdk-go-v2/service/redshiftserverless/types"

	"github.com/eshu-hq/eshu/go/internal/collector/cloud/aws"
	redshiftservice "github.com/eshu-hq/eshu/go/internal/collector/cloud/aws/service/redshift"
)

func mapCluster(raw awsredshifttypes.Cluster, boundary aws.Boundary) redshiftservice.Cluster {
	identifier := strings.TrimSpace(awsv2.ToString(raw.ClusterIdentifier))
	return redshiftservice.Cluster{
		ARN:                              clusterARN(boundary, identifier),
		Identifier:                       identifier,
		NodeType:                         strings.TrimSpace(awsv2.ToString(raw.NodeType)),
		ClusterStatus:                    strings.TrimSpace(awsv2.ToString(raw.ClusterStatus)),
		ClusterAvailabilityStatus:        strings.TrimSpace(awsv2.ToString(raw.ClusterAvailabilityStatus)),
		DBName:                           strings.TrimSpace(awsv2.ToString(raw.DBName)),
		Endpoint:                         endpointAddress(raw.Endpoint),
		EndpointPort:                     endpointPort(raw.Endpoint),
		ClusterCreateTime:                awsv2.ToTime(raw.ClusterCreateTime),
		AutomatedSnapshotRetentionPeriod: awsv2.ToInt32(raw.AutomatedSnapshotRetentionPeriod),
		ManualSnapshotRetentionPeriod:    awsv2.ToInt32(raw.ManualSnapshotRetentionPeriod),
		ClusterSecurityGroups:            clusterSecurityGroups(raw.ClusterSecurityGroups),
		VPCSecurityGroupIDs:              vpcSecurityGroupIDs(raw.VpcSecurityGroups),
		ClusterParameterGroup:            firstParameterGroupName(raw.ClusterParameterGroups),
		ClusterSubnetGroupName:           strings.TrimSpace(awsv2.ToString(raw.ClusterSubnetGroupName)),
		VPCID:                            strings.TrimSpace(awsv2.ToString(raw.VpcId)),
		AvailabilityZone:                 strings.TrimSpace(awsv2.ToString(raw.AvailabilityZone)),
		PreferredMaintenanceWindow:       strings.TrimSpace(awsv2.ToString(raw.PreferredMaintenanceWindow)),
		PendingModifiedValuesPresent:     raw.PendingModifiedValues != nil,
		ClusterVersion:                   strings.TrimSpace(awsv2.ToString(raw.ClusterVersion)),
		AllowVersionUpgrade:              awsv2.ToBool(raw.AllowVersionUpgrade),
		NumberOfNodes:                    awsv2.ToInt32(raw.NumberOfNodes),
		PubliclyAccessible:               awsv2.ToBool(raw.PubliclyAccessible),
		Encrypted:                        awsv2.ToBool(raw.Encrypted),
		KMSKeyID:                         strings.TrimSpace(awsv2.ToString(raw.KmsKeyId)),
		EnhancedVPCRouting:               awsv2.ToBool(raw.EnhancedVpcRouting),
		IAMRoleARNs:                      clusterIAMRoleARNs(raw.IamRoles),
		MaintenanceTrackName:             strings.TrimSpace(awsv2.ToString(raw.MaintenanceTrackName)),
		DeferredMaintenanceWindows:       deferredMaintenanceWindowIDs(raw.DeferredMaintenanceWindows),
		NextMaintenanceWindowStartTime:   awsv2.ToTime(raw.NextMaintenanceWindowStartTime),
		AvailabilityZoneRelocationStatus: strings.TrimSpace(awsv2.ToString(raw.AvailabilityZoneRelocationStatus)),
		MultiAZ:                          strings.EqualFold(strings.TrimSpace(awsv2.ToString(raw.MultiAZ)), "enabled"),
		Tags:                             redshiftTagsMap(raw.Tags),
	}
}

func mapClusterParameterGroup(
	boundary aws.Boundary,
	raw awsredshifttypes.ClusterParameterGroup,
) redshiftservice.ClusterParameterGroup {
	name := strings.TrimSpace(awsv2.ToString(raw.ParameterGroupName))
	return redshiftservice.ClusterParameterGroup{
		ARN:         parameterGroupARN(boundary, name),
		Name:        name,
		Family:      strings.TrimSpace(awsv2.ToString(raw.ParameterGroupFamily)),
		Description: strings.TrimSpace(awsv2.ToString(raw.Description)),
		Tags:        redshiftTagsMap(raw.Tags),
	}
}

func mapClusterSubnetGroup(
	boundary aws.Boundary,
	raw awsredshifttypes.ClusterSubnetGroup,
) redshiftservice.ClusterSubnetGroup {
	name := strings.TrimSpace(awsv2.ToString(raw.ClusterSubnetGroupName))
	return redshiftservice.ClusterSubnetGroup{
		ARN:         subnetGroupARN(boundary, name),
		Name:        name,
		VPCID:       strings.TrimSpace(awsv2.ToString(raw.VpcId)),
		Description: strings.TrimSpace(awsv2.ToString(raw.Description)),
		Status:      strings.TrimSpace(awsv2.ToString(raw.SubnetGroupStatus)),
		SubnetIDs:   subnetIDs(raw.Subnets),
		Tags:        redshiftTagsMap(raw.Tags),
	}
}

func mapClusterSnapshot(
	boundary aws.Boundary,
	raw awsredshifttypes.Snapshot,
) redshiftservice.ClusterSnapshot {
	identifier := strings.TrimSpace(awsv2.ToString(raw.SnapshotIdentifier))
	clusterIdentifier := strings.TrimSpace(awsv2.ToString(raw.ClusterIdentifier))
	return redshiftservice.ClusterSnapshot{
		ARN:                           snapshotARN(boundary, clusterIdentifier, identifier),
		Identifier:                    identifier,
		ClusterIdentifier:             clusterIdentifier,
		SnapshotType:                  strings.TrimSpace(awsv2.ToString(raw.SnapshotType)),
		Status:                        strings.TrimSpace(awsv2.ToString(raw.Status)),
		NodeType:                      strings.TrimSpace(awsv2.ToString(raw.NodeType)),
		NumberOfNodes:                 awsv2.ToInt32(raw.NumberOfNodes),
		DBName:                        strings.TrimSpace(awsv2.ToString(raw.DBName)),
		VPCID:                         strings.TrimSpace(awsv2.ToString(raw.VpcId)),
		Encrypted:                     awsv2.ToBool(raw.Encrypted),
		KMSKeyID:                      strings.TrimSpace(awsv2.ToString(raw.KmsKeyId)),
		SnapshotCreateTime:            awsv2.ToTime(raw.SnapshotCreateTime),
		ClusterCreateTime:             awsv2.ToTime(raw.ClusterCreateTime),
		SnapshotRetentionStartTime:    awsv2.ToTime(raw.SnapshotRetentionStartTime),
		ManualSnapshotRetentionPeriod: awsv2.ToInt32(raw.ManualSnapshotRetentionPeriod),
		EngineFullVersion:             strings.TrimSpace(awsv2.ToString(raw.EngineFullVersion)),
		AvailabilityZone:              strings.TrimSpace(awsv2.ToString(raw.AvailabilityZone)),
		SourceRegion:                  strings.TrimSpace(awsv2.ToString(raw.SourceRegion)),
		Tags:                          redshiftTagsMap(raw.Tags),
		RestorableNodeTypes:           cloneRawStrings(raw.RestorableNodeTypes),
	}
}

func mapScheduledAction(raw awsredshifttypes.ScheduledAction) redshiftservice.ScheduledAction {
	return redshiftservice.ScheduledAction{
		Name:                    strings.TrimSpace(awsv2.ToString(raw.ScheduledActionName)),
		Schedule:                strings.TrimSpace(awsv2.ToString(raw.Schedule)),
		IAMRoleARN:              strings.TrimSpace(awsv2.ToString(raw.IamRole)),
		Description:             strings.TrimSpace(awsv2.ToString(raw.ScheduledActionDescription)),
		State:                   strings.TrimSpace(string(raw.State)),
		StartTime:               awsv2.ToTime(raw.StartTime),
		EndTime:                 awsv2.ToTime(raw.EndTime),
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
		ARN:            strings.TrimSpace(awsv2.ToString(raw.NamespaceArn)),
		Name:           strings.TrimSpace(awsv2.ToString(raw.NamespaceName)),
		NamespaceID:    strings.TrimSpace(awsv2.ToString(raw.NamespaceId)),
		Status:         strings.TrimSpace(string(raw.Status)),
		DBName:         strings.TrimSpace(awsv2.ToString(raw.DbName)),
		DefaultIAMRole: strings.TrimSpace(awsv2.ToString(raw.DefaultIamRoleArn)),
		IAMRoleARNs:    cloneRawStrings(raw.IamRoles),
		KMSKeyID:       strings.TrimSpace(awsv2.ToString(raw.KmsKeyId)),
		LogExports:     logExports(raw.LogExports),
		CreationDate:   awsv2.ToTime(raw.CreationDate),
		Tags:           tags,
	}
}

func mapServerlessWorkgroup(
	raw awsserverlesstypes.Workgroup,
	tags map[string]string,
) redshiftservice.ServerlessWorkgroup {
	return redshiftservice.ServerlessWorkgroup{
		ARN:                strings.TrimSpace(awsv2.ToString(raw.WorkgroupArn)),
		Name:               strings.TrimSpace(awsv2.ToString(raw.WorkgroupName)),
		WorkgroupID:        strings.TrimSpace(awsv2.ToString(raw.WorkgroupId)),
		NamespaceName:      strings.TrimSpace(awsv2.ToString(raw.NamespaceName)),
		Status:             strings.TrimSpace(string(raw.Status)),
		BaseCapacity:       awsv2.ToInt32(raw.BaseCapacity),
		MaxCapacity:        awsv2.ToInt32(raw.MaxCapacity),
		EnhancedVPCRouting: awsv2.ToBool(raw.EnhancedVpcRouting),
		PubliclyAccessible: awsv2.ToBool(raw.PubliclyAccessible),
		ConfigParameters:   serverlessConfigParameters(raw.ConfigParameters),
		SubnetIDs:          cloneRawStrings(raw.SubnetIds),
		SecurityGroupIDs:   cloneRawStrings(raw.SecurityGroupIds),
		EndpointAddress:    serverlessEndpointAddress(raw.Endpoint),
		EndpointPort:       serverlessEndpointPort(raw.Endpoint),
		CreationDate:       awsv2.ToTime(raw.CreationDate),
		Tags:               tags,
	}
}

// clusterARN constructs the well-formed cluster ARN from the claim boundary
// and the reported cluster identifier. The provisioned Redshift Cluster shape
// does not return a ClusterArn field, so the adapter synthesizes it instead of
// inventing identity from ClusterNamespaceArn (which addresses the namespace).
func clusterARN(boundary aws.Boundary, identifier string) string {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" {
		return ""
	}
	return "arn:" + aws.PartitionForBoundary(boundary) + ":redshift:" + boundary.Region + ":" + boundary.AccountID + ":cluster:" + identifier
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
	return strings.TrimSpace(awsv2.ToString(endpoint.Address))
}

func endpointPort(endpoint *awsredshifttypes.Endpoint) int32 {
	if endpoint == nil {
		return 0
	}
	return awsv2.ToInt32(endpoint.Port)
}

func clusterSecurityGroups(groups []awsredshifttypes.ClusterSecurityGroupMembership) []string {
	var output []string
	for _, group := range groups {
		if name := strings.TrimSpace(awsv2.ToString(group.ClusterSecurityGroupName)); name != "" {
			output = append(output, name)
		}
	}
	return output
}

func vpcSecurityGroupIDs(groups []awsredshifttypes.VpcSecurityGroupMembership) []string {
	var ids []string
	for _, group := range groups {
		if id := strings.TrimSpace(awsv2.ToString(group.VpcSecurityGroupId)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func firstParameterGroupName(groups []awsredshifttypes.ClusterParameterGroupStatus) string {
	for _, group := range groups {
		if name := strings.TrimSpace(awsv2.ToString(group.ParameterGroupName)); name != "" {
			return name
		}
	}
	return ""
}

func clusterIAMRoleARNs(roles []awsredshifttypes.ClusterIamRole) []string {
	var arns []string
	for _, role := range roles {
		if arn := strings.TrimSpace(awsv2.ToString(role.IamRoleArn)); arn != "" {
			arns = append(arns, arn)
		}
	}
	return arns
}

func deferredMaintenanceWindowIDs(windows []awsredshifttypes.DeferredMaintenanceWindow) []string {
	var ids []string
	for _, window := range windows {
		if id := strings.TrimSpace(awsv2.ToString(window.DeferMaintenanceIdentifier)); id != "" {
			ids = append(ids, id)
		}
	}
	return ids
}

func subnetIDs(subnets []awsredshifttypes.Subnet) []string {
	var ids []string
	for _, subnet := range subnets {
		if id := strings.TrimSpace(awsv2.ToString(subnet.SubnetIdentifier)); id != "" {
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
		return strings.TrimSpace(awsv2.ToString(action.PauseCluster.ClusterIdentifier))
	case action.ResumeCluster != nil:
		return strings.TrimSpace(awsv2.ToString(action.ResumeCluster.ClusterIdentifier))
	case action.ResizeCluster != nil:
		return strings.TrimSpace(awsv2.ToString(action.ResizeCluster.ClusterIdentifier))
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
		key := strings.TrimSpace(awsv2.ToString(param.ParameterKey))
		if key == "" {
			continue
		}
		output = append(output, redshiftservice.ServerlessConfigParameter{
			Key:   key,
			Value: strings.TrimSpace(awsv2.ToString(param.ParameterValue)),
		})
	}
	return output
}

func serverlessEndpointAddress(endpoint *awsserverlesstypes.Endpoint) string {
	if endpoint == nil {
		return ""
	}
	return strings.TrimSpace(awsv2.ToString(endpoint.Address))
}

func serverlessEndpointPort(endpoint *awsserverlesstypes.Endpoint) int32 {
	if endpoint == nil {
		return 0
	}
	return awsv2.ToInt32(endpoint.Port)
}

func redshiftTagsMap(tags []awsredshifttypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	output := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(awsv2.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = awsv2.ToString(tag.Value)
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
		key := strings.TrimSpace(awsv2.ToString(tag.Key))
		if key == "" {
			continue
		}
		output[key] = awsv2.ToString(tag.Value)
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

func parameterGroupARN(boundary aws.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return "arn:" + aws.PartitionForBoundary(boundary) + ":redshift:" + boundary.Region + ":" + boundary.AccountID + ":parametergroup:" + name
}

func subnetGroupARN(boundary aws.Boundary, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	return "arn:" + aws.PartitionForBoundary(boundary) + ":redshift:" + boundary.Region + ":" + boundary.AccountID + ":subnetgroup:" + name
}

func snapshotARN(boundary aws.Boundary, clusterIdentifier string, identifier string) string {
	clusterIdentifier = strings.TrimSpace(clusterIdentifier)
	identifier = strings.TrimSpace(identifier)
	if clusterIdentifier == "" || identifier == "" {
		return ""
	}
	return "arn:" + aws.PartitionForBoundary(boundary) + ":redshift:" + boundary.Region + ":" + boundary.AccountID + ":snapshot:" + clusterIdentifier + "/" + identifier
}
