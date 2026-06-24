// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awssdk

import (
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfn "github.com/aws/aws-sdk-go-v2/service/cloudformation"
	cfntypes "github.com/aws/aws-sdk-go-v2/service/cloudformation/types"

	cfnservice "github.com/eshu-hq/eshu/go/internal/collector/awscloud/services/cloudformation"
)

// mapStack converts a CloudFormation Stack into the scanner type. Parameter
// values and stack outputs are handled carefully: only parameter keys are
// retained, and output values are passed through raw for the scanner to redact
// by key. The template body is never available on a Stack and is never fetched.
func mapStack(stack cfntypes.Stack) cfnservice.Stack {
	return cfnservice.Stack{
		ID:                          strings.TrimSpace(aws.ToString(stack.StackId)),
		Name:                        strings.TrimSpace(aws.ToString(stack.StackName)),
		Status:                      string(stack.StackStatus),
		StatusReason:                strings.TrimSpace(aws.ToString(stack.StackStatusReason)),
		Description:                 strings.TrimSpace(aws.ToString(stack.Description)),
		RoleARN:                     strings.TrimSpace(aws.ToString(stack.RoleARN)),
		Capabilities:                capabilityStrings(stack.Capabilities),
		NotificationARNs:            cloneStrings(stack.NotificationARNs),
		ParentID:                    strings.TrimSpace(aws.ToString(stack.ParentId)),
		RootID:                      strings.TrimSpace(aws.ToString(stack.RootId)),
		ChangeSetID:                 strings.TrimSpace(aws.ToString(stack.ChangeSetId)),
		DriftStatus:                 stackDriftStatus(stack.DriftInformation),
		EnableTerminationProtection: aws.ToBool(stack.EnableTerminationProtection),
		DisableRollback:             aws.ToBool(stack.DisableRollback),
		ParameterKeys:               parameterKeys(stack.Parameters),
		Outputs:                     stackOutputs(stack.Outputs),
		Tags:                        tagMap(stack.Tags),
		CreationTime:                aws.ToTime(stack.CreationTime),
		LastUpdatedTime:             aws.ToTime(stack.LastUpdatedTime),
		DeletionTime:                aws.ToTime(stack.DeletionTime),
	}
}

// mapDeletedStack converts a recently deleted StackSummary into the scanner
// type. Deleted stacks carry identity, status, and timestamps only.
func mapDeletedStack(summary cfntypes.StackSummary) cfnservice.Stack {
	return cfnservice.Stack{
		ID:              strings.TrimSpace(aws.ToString(summary.StackId)),
		Name:            strings.TrimSpace(aws.ToString(summary.StackName)),
		Status:          string(summary.StackStatus),
		StatusReason:    strings.TrimSpace(aws.ToString(summary.StackStatusReason)),
		ParentID:        strings.TrimSpace(aws.ToString(summary.ParentId)),
		RootID:          strings.TrimSpace(aws.ToString(summary.RootId)),
		DriftStatus:     stackSummaryDriftStatus(summary.DriftInformation),
		Deleted:         true,
		CreationTime:    aws.ToTime(summary.CreationTime),
		LastUpdatedTime: aws.ToTime(summary.LastUpdatedTime),
		DeletionTime:    aws.ToTime(summary.DeletionTime),
	}
}

func mapStackResource(summary cfntypes.StackResourceSummary) cfnservice.StackResource {
	return cfnservice.StackResource{
		LogicalID:    strings.TrimSpace(aws.ToString(summary.LogicalResourceId)),
		PhysicalID:   strings.TrimSpace(aws.ToString(summary.PhysicalResourceId)),
		ResourceType: strings.TrimSpace(aws.ToString(summary.ResourceType)),
		Status:       string(summary.ResourceStatus),
		DriftStatus:  stackResourceSummaryDriftStatus(summary.DriftInformation),
	}
}

// mapStackSet merges a stack-set summary with its DescribeStackSet detail. The
// TemplateBody is intentionally never read from the detail, and only parameter
// keys are retained from the parameter list.
func mapStackSet(summary cfntypes.StackSetSummary, detail *awscfn.DescribeStackSetOutput) cfnservice.StackSet {
	stackSet := cfnservice.StackSet{
		ID:          strings.TrimSpace(aws.ToString(summary.StackSetId)),
		Name:        strings.TrimSpace(aws.ToString(summary.StackSetName)),
		Status:      string(summary.Status),
		Description: strings.TrimSpace(aws.ToString(summary.Description)),
		DriftStatus: string(summary.DriftStatus),
	}
	if detail == nil || detail.StackSet == nil {
		return stackSet
	}
	set := detail.StackSet
	stackSet.ARN = strings.TrimSpace(aws.ToString(set.StackSetARN))
	stackSet.PermissionModel = string(set.PermissionModel)
	stackSet.AdministrationRoleARN = strings.TrimSpace(aws.ToString(set.AdministrationRoleARN))
	stackSet.ExecutionRoleName = strings.TrimSpace(aws.ToString(set.ExecutionRoleName))
	stackSet.Capabilities = capabilityStrings(set.Capabilities)
	stackSet.OrganizationalUnitIDs = cloneStrings(set.OrganizationalUnitIds)
	stackSet.Regions = cloneStrings(set.Regions)
	stackSet.ParameterKeys = parameterKeys(set.Parameters)
	if name := strings.TrimSpace(aws.ToString(set.StackSetName)); name != "" {
		stackSet.Name = name
	}
	if desc := strings.TrimSpace(aws.ToString(set.Description)); desc != "" {
		stackSet.Description = desc
	}
	stackSet.Tags = tagMap(set.Tags)
	return stackSet
}

func mapChangeSet(summary cfntypes.ChangeSetSummary) cfnservice.ChangeSet {
	return cfnservice.ChangeSet{
		ID:              strings.TrimSpace(aws.ToString(summary.ChangeSetId)),
		Name:            strings.TrimSpace(aws.ToString(summary.ChangeSetName)),
		StackID:         strings.TrimSpace(aws.ToString(summary.StackId)),
		StackName:       strings.TrimSpace(aws.ToString(summary.StackName)),
		Status:          string(summary.Status),
		StatusReason:    strings.TrimSpace(aws.ToString(summary.StatusReason)),
		ExecutionStatus: string(summary.ExecutionStatus),
		Description:     strings.TrimSpace(aws.ToString(summary.Description)),
		CreationTime:    aws.ToTime(summary.CreationTime),
	}
}

func mapStackInstance(summary cfntypes.StackInstanceSummary) cfnservice.StackInstance {
	return cfnservice.StackInstance{
		StackSetID:   strings.TrimSpace(aws.ToString(summary.StackSetId)),
		StackID:      strings.TrimSpace(aws.ToString(summary.StackId)),
		Account:      strings.TrimSpace(aws.ToString(summary.Account)),
		Region:       strings.TrimSpace(aws.ToString(summary.Region)),
		Status:       string(summary.Status),
		StatusReason: strings.TrimSpace(aws.ToString(summary.StatusReason)),
		DriftStatus:  string(summary.DriftStatus),
	}
}

func mapRegisteredType(summary cfntypes.TypeSummary) cfnservice.RegisteredType {
	return cfnservice.RegisteredType{
		ARN:              strings.TrimSpace(aws.ToString(summary.TypeArn)),
		TypeName:         strings.TrimSpace(aws.ToString(summary.TypeName)),
		Kind:             string(summary.Type),
		DefaultVersionID: strings.TrimSpace(aws.ToString(summary.DefaultVersionId)),
		PublisherID:      strings.TrimSpace(aws.ToString(summary.PublisherId)),
		PublisherName:    strings.TrimSpace(aws.ToString(summary.PublisherName)),
		IsActivated:      aws.ToBool(summary.IsActivated),
		LastUpdated:      aws.ToTime(summary.LastUpdated),
	}
}

// accumulateDrift folds one resource drift status into the running summary.
// Modified and Deleted resources both count as drifted so the summary reports
// the total drifted resource count an operator cares about.
func accumulateDrift(result *cfnservice.StackDriftResult, status cfntypes.StackResourceDriftStatus) {
	result.TotalChecked++
	switch status {
	case cfntypes.StackResourceDriftStatusModified:
		result.ModifiedCount++
		result.DriftedCount++
	case cfntypes.StackResourceDriftStatusDeleted:
		result.DeletedCount++
		result.DriftedCount++
	case cfntypes.StackResourceDriftStatusInSync:
		result.InSyncCount++
	case cfntypes.StackResourceDriftStatusNotChecked:
		result.NotCheckedCount++
	}
}

// stackOutputs converts SDK outputs into scanner outputs carrying the raw
// value. The scanner applies key-based redaction before emission; the adapter
// performs no classification of its own.
func stackOutputs(outputs []cfntypes.Output) []cfnservice.StackOutput {
	if len(outputs) == 0 {
		return nil
	}
	out := make([]cfnservice.StackOutput, 0, len(outputs))
	for _, output := range outputs {
		out = append(out, cfnservice.StackOutput{
			Key:         strings.TrimSpace(aws.ToString(output.OutputKey)),
			ExportName:  strings.TrimSpace(aws.ToString(output.ExportName)),
			Description: strings.TrimSpace(aws.ToString(output.Description)),
			Value:       aws.ToString(output.OutputValue),
		})
	}
	return out
}

// parameterKeys extracts only the parameter keys. Parameter values, resolved
// SSM values, and NoEcho values are intentionally discarded and never reach the
// scanner type.
func parameterKeys(parameters []cfntypes.Parameter) []string {
	if len(parameters) == 0 {
		return nil
	}
	keys := make([]string, 0, len(parameters))
	for _, parameter := range parameters {
		if key := strings.TrimSpace(aws.ToString(parameter.ParameterKey)); key != "" {
			keys = append(keys, key)
		}
	}
	if len(keys) == 0 {
		return nil
	}
	return keys
}

func capabilityStrings(capabilities []cfntypes.Capability) []string {
	if len(capabilities) == 0 {
		return nil
	}
	out := make([]string, 0, len(capabilities))
	for _, capability := range capabilities {
		if value := strings.TrimSpace(string(capability)); value != "" {
			out = append(out, value)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func tagMap(tags []cfntypes.Tag) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for _, tag := range tags {
		key := strings.TrimSpace(aws.ToString(tag.Key))
		if key == "" {
			continue
		}
		out[key] = aws.ToString(tag.Value)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func stackDriftStatus(info *cfntypes.StackDriftInformation) string {
	if info == nil {
		return ""
	}
	return string(info.StackDriftStatus)
}

func stackSummaryDriftStatus(info *cfntypes.StackDriftInformationSummary) string {
	if info == nil {
		return ""
	}
	return string(info.StackDriftStatus)
}

func stackResourceSummaryDriftStatus(info *cfntypes.StackResourceDriftInformationSummary) string {
	if info == nil {
		return ""
	}
	return string(info.StackResourceDriftStatus)
}

func cloneStrings(input []string) []string {
	if len(input) == 0 {
		return nil
	}
	out := make([]string, 0, len(input))
	for _, value := range input {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}
