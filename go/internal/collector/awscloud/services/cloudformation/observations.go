// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloudformation

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/redact"
)

// resourceTypeIAMRole is the relationship target type CloudFormation emits for
// stack IAM service-role references, matching the shared awscloud convention.
const resourceTypeIAMRole = "aws_iam_role"

func stackObservation(boundary awscloud.Boundary, stack Stack, redactionKey redact.Key) awscloud.ResourceObservation {
	stackID := strings.TrimSpace(stack.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          stackID,
		ResourceID:   firstNonEmpty(stackID, stack.Name),
		ResourceType: awscloud.ResourceTypeCloudFormationStack,
		Name:         strings.TrimSpace(stack.Name),
		State:        strings.TrimSpace(stack.Status),
		Tags:         cloneStringMap(stack.Tags),
		Attributes: map[string]any{
			"status":                        strings.TrimSpace(stack.Status),
			"status_reason":                 strings.TrimSpace(stack.StatusReason),
			"description":                   strings.TrimSpace(stack.Description),
			"role_arn":                      strings.TrimSpace(stack.RoleARN),
			"template_url":                  strings.TrimSpace(stack.TemplateURL),
			"capabilities":                  cloneStrings(stack.Capabilities),
			"notification_arns":             cloneStrings(stack.NotificationARNs),
			"parent_id":                     strings.TrimSpace(stack.ParentID),
			"root_id":                       strings.TrimSpace(stack.RootID),
			"change_set_id":                 strings.TrimSpace(stack.ChangeSetID),
			"drift_status":                  strings.TrimSpace(stack.DriftStatus),
			"enable_termination_protection": stack.EnableTerminationProtection,
			"disable_rollback":              stack.DisableRollback,
			"deleted":                       stack.Deleted,
			"parameter_keys":                cloneStrings(stack.ParameterKeys),
			"outputs":                       stackOutputs(stack.Outputs, redactionKey),
			"creation_time":                 timeOrEmpty(stack.CreationTime),
			"last_updated_time":             timeOrEmpty(stack.LastUpdatedTime),
			"deletion_time":                 timeOrEmpty(stack.DeletionTime),
		},
		CorrelationAnchors: []string{stackID, stack.Name},
		SourceRecordID:     firstNonEmpty(stackID, stack.Name),
	}
}

// stackOutputs converts scanner outputs into payload entries. Output keys whose
// names match the shared AWS sensitive-key policy carry a redaction marker and
// no value; ordinary outputs carry their cleartext value. This keeps inventory
// evidence while never persisting a secret-shaped output value.
func stackOutputs(outputs []StackOutput, redactionKey redact.Key) []map[string]any {
	if len(outputs) == 0 {
		return nil
	}
	out := make([]map[string]any, 0, len(outputs))
	for _, output := range outputs {
		key := strings.TrimSpace(output.Key)
		entry := map[string]any{
			"key":         key,
			"export_name": strings.TrimSpace(output.ExportName),
			"description": strings.TrimSpace(output.Description),
		}
		if redacted, marker := awscloud.ClassifyStackOutput(key, output.Value, redactionKey); redacted {
			entry["redacted"] = marker
		} else {
			entry["value"] = output.Value
		}
		out = append(out, entry)
	}
	return out
}

func stackSetObservation(boundary awscloud.Boundary, stackSet StackSet) awscloud.ResourceObservation {
	stackSetARN := strings.TrimSpace(stackSet.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          stackSetARN,
		ResourceID:   firstNonEmpty(stackSetARN, stackSet.ID, stackSet.Name),
		ResourceType: awscloud.ResourceTypeCloudFormationStackSet,
		Name:         strings.TrimSpace(stackSet.Name),
		State:        strings.TrimSpace(stackSet.Status),
		Tags:         cloneStringMap(stackSet.Tags),
		Attributes: map[string]any{
			"stack_set_id":            strings.TrimSpace(stackSet.ID),
			"status":                  strings.TrimSpace(stackSet.Status),
			"description":             strings.TrimSpace(stackSet.Description),
			"permission_model":        strings.TrimSpace(stackSet.PermissionModel),
			"administration_role_arn": strings.TrimSpace(stackSet.AdministrationRoleARN),
			"execution_role_name":     strings.TrimSpace(stackSet.ExecutionRoleName),
			"drift_status":            strings.TrimSpace(stackSet.DriftStatus),
			"capabilities":            cloneStrings(stackSet.Capabilities),
			"organizational_unit_ids": cloneStrings(stackSet.OrganizationalUnitIDs),
			"regions":                 cloneStrings(stackSet.Regions),
			"parameter_keys":          cloneStrings(stackSet.ParameterKeys),
		},
		CorrelationAnchors: []string{stackSetARN, stackSet.ID, stackSet.Name},
		SourceRecordID:     firstNonEmpty(stackSetARN, stackSet.ID, stackSet.Name),
	}
}

func stackInstanceObservation(
	boundary awscloud.Boundary,
	stackSet StackSet,
	instance StackInstance,
) awscloud.ResourceObservation {
	resourceID := stackInstanceResourceID(stackSet, instance)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          strings.TrimSpace(instance.StackID),
		ResourceID:   resourceID,
		ResourceType: awscloud.ResourceTypeCloudFormationStackInstance,
		Name:         resourceID,
		State:        strings.TrimSpace(instance.Status),
		Attributes: map[string]any{
			"stack_set_id":   firstNonEmpty(instance.StackSetID, stackSet.ID),
			"stack_set_name": firstNonEmpty(instance.StackSetName, stackSet.Name),
			"stack_id":       strings.TrimSpace(instance.StackID),
			"account":        strings.TrimSpace(instance.Account),
			"region":         strings.TrimSpace(instance.Region),
			"status":         strings.TrimSpace(instance.Status),
			"status_reason":  strings.TrimSpace(instance.StatusReason),
			"drift_status":   strings.TrimSpace(instance.DriftStatus),
		},
		CorrelationAnchors: []string{instance.StackID, resourceID},
		SourceRecordID:     resourceID,
	}
}

func changeSetObservation(boundary awscloud.Boundary, changeSet ChangeSet) awscloud.ResourceObservation {
	changeSetID := strings.TrimSpace(changeSet.ID)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          changeSetID,
		ResourceID:   firstNonEmpty(changeSetID, changeSet.Name),
		ResourceType: awscloud.ResourceTypeCloudFormationChangeSet,
		Name:         strings.TrimSpace(changeSet.Name),
		State:        strings.TrimSpace(changeSet.Status),
		Attributes: map[string]any{
			"change_set_id":    changeSetID,
			"stack_id":         strings.TrimSpace(changeSet.StackID),
			"stack_name":       strings.TrimSpace(changeSet.StackName),
			"status":           strings.TrimSpace(changeSet.Status),
			"status_reason":    strings.TrimSpace(changeSet.StatusReason),
			"execution_status": strings.TrimSpace(changeSet.ExecutionStatus),
			"description":      strings.TrimSpace(changeSet.Description),
			"creation_time":    timeOrEmpty(changeSet.CreationTime),
		},
		CorrelationAnchors: []string{changeSetID, changeSet.Name},
		SourceRecordID:     firstNonEmpty(changeSetID, changeSet.Name),
	}
}

func driftObservation(
	boundary awscloud.Boundary,
	stack Stack,
	drift StackDriftResult,
) awscloud.ResourceObservation {
	stackID := firstNonEmpty(stack.ID, drift.StackID)
	driftID := stackID + "#drift"
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ResourceID:   driftID,
		ResourceType: awscloud.ResourceTypeCloudFormationStackDrift,
		Name:         strings.TrimSpace(stack.Name) + " drift",
		State:        strings.TrimSpace(stack.DriftStatus),
		Attributes: map[string]any{
			"stack_id":           stackID,
			"stack_drift_status": strings.TrimSpace(stack.DriftStatus),
			"total_checked":      drift.TotalChecked,
			"drifted_count":      drift.DriftedCount,
			"in_sync_count":      drift.InSyncCount,
			"not_checked_count":  drift.NotCheckedCount,
			"deleted_count":      drift.DeletedCount,
			"modified_count":     drift.ModifiedCount,
		},
		CorrelationAnchors: []string{driftID, stackID},
		SourceRecordID:     driftID,
	}
}

func typeObservation(boundary awscloud.Boundary, registeredType RegisteredType) awscloud.ResourceObservation {
	typeARN := strings.TrimSpace(registeredType.ARN)
	return awscloud.ResourceObservation{
		Boundary:     boundary,
		ARN:          typeARN,
		ResourceID:   firstNonEmpty(typeARN, registeredType.TypeName),
		ResourceType: awscloud.ResourceTypeCloudFormationType,
		Name:         strings.TrimSpace(registeredType.TypeName),
		Attributes: map[string]any{
			"type_name":          strings.TrimSpace(registeredType.TypeName),
			"kind":               strings.TrimSpace(registeredType.Kind),
			"default_version_id": strings.TrimSpace(registeredType.DefaultVersionID),
			"publisher_id":       strings.TrimSpace(registeredType.PublisherID),
			"publisher_name":     strings.TrimSpace(registeredType.PublisherName),
			"is_activated":       registeredType.IsActivated,
			"last_updated":       timeOrEmpty(registeredType.LastUpdated),
		},
		CorrelationAnchors: []string{typeARN, registeredType.TypeName},
		SourceRecordID:     firstNonEmpty(typeARN, registeredType.TypeName),
	}
}

func stackRelationships(boundary awscloud.Boundary, stack Stack) []awscloud.RelationshipObservation {
	stackID := strings.TrimSpace(stack.ID)
	sourceID := firstNonEmpty(stackID, stack.Name)
	if sourceID == "" {
		return nil
	}
	var out []awscloud.RelationshipObservation
	if role := strings.TrimSpace(stack.RoleARN); role != "" {
		out = append(out, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCloudFormationStackUsesIAMRole,
			SourceResourceID: sourceID,
			SourceARN:        stackID,
			TargetResourceID: role,
			TargetARN:        role,
			TargetType:       resourceTypeIAMRole,
			SourceRecordID:   sourceID + "->iam:" + role,
		})
	}
	if templateURL := strings.TrimSpace(stack.TemplateURL); templateURL != "" {
		out = append(out, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCloudFormationStackUsesS3TemplateURL,
			SourceResourceID: sourceID,
			SourceARN:        stackID,
			TargetResourceID: templateURL,
			TargetType:       awscloud.ResourceTypeS3Bucket,
			SourceRecordID:   sourceID + "->template:" + templateURL,
		})
	}
	return out
}

func stackResourceTypeRelationships(
	boundary awscloud.Boundary,
	stack Stack,
	resources []StackResource,
) []awscloud.RelationshipObservation {
	stackID := strings.TrimSpace(stack.ID)
	sourceID := firstNonEmpty(stackID, stack.Name)
	if sourceID == "" {
		return nil
	}
	var out []awscloud.RelationshipObservation
	for _, resource := range resources {
		resourceType := strings.TrimSpace(resource.ResourceType)
		if resourceType == "" {
			continue
		}
		physical := strings.TrimSpace(resource.PhysicalID)
		target := firstNonEmpty(physical, strings.TrimSpace(resource.LogicalID))
		out = append(out, awscloud.RelationshipObservation{
			Boundary:         boundary,
			RelationshipType: awscloud.RelationshipCloudFormationStackUsesResourceType,
			SourceResourceID: sourceID,
			SourceARN:        stackID,
			TargetResourceID: firstNonEmpty(target, resourceType),
			TargetARN:        arnTarget(physical),
			TargetType:       resourceType,
			Attributes: map[string]any{
				"logical_resource_id": strings.TrimSpace(resource.LogicalID),
				"resource_status":     strings.TrimSpace(resource.Status),
				"drift_status":        strings.TrimSpace(resource.DriftStatus),
			},
			SourceRecordID: sourceID + "->" + resourceType + ":" + firstNonEmpty(target, resource.LogicalID),
		})
	}
	return out
}

func stackSetInstanceRelationship(
	boundary awscloud.Boundary,
	stackSet StackSet,
	instance StackInstance,
) (awscloud.RelationshipObservation, bool) {
	stackSetID := firstNonEmpty(stackSet.ARN, stackSet.ID, stackSet.Name)
	resourceID := stackInstanceResourceID(stackSet, instance)
	if stackSetID == "" || resourceID == "" {
		return awscloud.RelationshipObservation{}, false
	}
	return awscloud.RelationshipObservation{
		Boundary:         boundary,
		RelationshipType: awscloud.RelationshipCloudFormationStackSetContainsStackInstance,
		SourceResourceID: stackSetID,
		SourceARN:        strings.TrimSpace(stackSet.ARN),
		TargetResourceID: resourceID,
		TargetARN:        strings.TrimSpace(instance.StackID),
		TargetType:       awscloud.ResourceTypeCloudFormationStackInstance,
		Attributes: map[string]any{
			"account": strings.TrimSpace(instance.Account),
			"region":  strings.TrimSpace(instance.Region),
			"status":  strings.TrimSpace(instance.Status),
		},
		SourceRecordID: stackSetID + "->instance:" + resourceID,
	}, true
}

// stackInstanceResourceID derives a stable identity for one stack-set instance
// from its stack set, account, and region. A stack instance has no ARN of its
// own beyond the per-target stack ID, which may be empty before deployment.
func stackInstanceResourceID(stackSet StackSet, instance StackInstance) string {
	name := firstNonEmpty(instance.StackSetName, stackSet.Name, instance.StackSetID, stackSet.ID)
	account := strings.TrimSpace(instance.Account)
	region := strings.TrimSpace(instance.Region)
	if name == "" || account == "" || region == "" {
		return strings.TrimSpace(instance.StackID)
	}
	return name + ":" + account + ":" + region
}

func arnTarget(value string) string {
	if strings.HasPrefix(value, "arn:") {
		return value
	}
	return ""
}
