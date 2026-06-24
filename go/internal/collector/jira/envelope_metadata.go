// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewWorkItemProjectMetadataEnvelope converts one Jira project definition into
// a source fact.
func NewWorkItemProjectMetadataEnvelope(ctx EnvelopeContext, project ProjectMetadata) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	projectID := strings.TrimSpace(project.ID)
	projectKey := strings.TrimSpace(project.Key)
	if projectID == "" && projectKey == "" {
		return facts.Envelope{}, fmt.Errorf("jira project metadata id or key is required")
	}
	stableFactKey := facts.StableID(facts.WorkItemProjectMetadataFactKind, map[string]any{
		"provider":    ProviderJiraCloud,
		"scope_id":    ctx.ScopeID,
		"project_id":  projectID,
		"project_key": projectKey,
	})
	payload := metadataBasePayload(ctx)
	nameFingerprint := textFingerprint(project.Name)
	categoryNameFingerprint := textFingerprint(project.CategoryName)
	payload["project_id"] = projectID
	payload["project_key"] = projectKey
	payload["project_type_key"] = strings.TrimSpace(project.TypeKey)
	payload["project_name"] = ""
	payload["project_name_present"] = strings.TrimSpace(project.Name) != ""
	payload["project_name_fingerprint"] = nameFingerprint
	payload["category_id"] = strings.TrimSpace(project.CategoryID)
	payload["category_name"] = ""
	payload["category_name_present"] = strings.TrimSpace(project.CategoryName) != ""
	payload["category_name_fingerprint"] = categoryNameFingerprint
	payload["style"] = strings.TrimSpace(project.Style)
	payload["visibility_state"] = projectVisibilityState(project)
	payload["self_url"] = ""
	payload["self_url_fingerprint"] = urlFingerprint(sanitizeURL(project.Self))
	payload["last_issue_update_at"] = formatTime(project.LastIssueUpdate)
	payload["issue_count"] = project.IssueCount
	recordID := firstNonBlank(projectID, projectKey)
	return workItemEnvelope(ctx, facts.WorkItemProjectMetadataFactKind, stableFactKey, payload, recordID, ctx.SourceURI), nil
}

// NewWorkItemIssueTypeMetadataEnvelope converts one Jira issue-type definition
// into a source fact.
func NewWorkItemIssueTypeMetadataEnvelope(ctx EnvelopeContext, issueType IssueTypeMetadata) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	issueTypeID := strings.TrimSpace(issueType.ID)
	if issueTypeID == "" {
		return facts.Envelope{}, fmt.Errorf("jira issue type metadata id is required")
	}
	projectID := firstNonBlank(issueType.ProjectID, issueType.Scope.ProjectID)
	stableFactKey := facts.StableID(facts.WorkItemIssueTypeMetadataFactKind, map[string]any{
		"provider":      ProviderJiraCloud,
		"scope_id":      ctx.ScopeID,
		"issue_type_id": issueTypeID,
		"project_id":    projectID,
	})
	payload := metadataBasePayload(ctx)
	payload["issue_type_id"] = issueTypeID
	payload["project_id"] = projectID
	payload["scope_type"] = strings.TrimSpace(issueType.Scope.Type)
	payload["entity_id"] = ""
	payload["entity_id_present"] = strings.TrimSpace(issueType.EntityID) != ""
	payload["entity_id_fingerprint"] = textFingerprint(issueType.EntityID)
	payload["issue_type_name"] = ""
	payload["issue_type_name_present"] = strings.TrimSpace(issueType.Name) != ""
	payload["issue_type_name_fingerprint"] = textFingerprint(issueType.Name)
	payload["description_present"] = strings.TrimSpace(issueType.Description) != ""
	payload["description_fingerprint"] = textFingerprint(issueType.Description)
	payload["hierarchy_level"] = issueType.HierarchyLevel
	payload["subtask"] = issueType.Subtask
	payload["self_url"] = ""
	payload["self_url_fingerprint"] = urlFingerprint(sanitizeURL(issueType.Self))
	return workItemEnvelope(ctx, facts.WorkItemIssueTypeMetadataFactKind, stableFactKey, payload, issueTypeID, ctx.SourceURI), nil
}

// NewWorkItemStatusMetadataEnvelope converts one Jira status definition into a
// source fact.
func NewWorkItemStatusMetadataEnvelope(ctx EnvelopeContext, status StatusMetadata) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	statusID := strings.TrimSpace(status.ID)
	if statusID == "" {
		return facts.Envelope{}, fmt.Errorf("jira status metadata id is required")
	}
	projectID := firstNonBlank(status.ProjectID, status.Scope.ProjectID)
	stableFactKey := facts.StableID(facts.WorkItemStatusMetadataFactKind, map[string]any{
		"provider":   ProviderJiraCloud,
		"scope_id":   ctx.ScopeID,
		"status_id":  statusID,
		"project_id": projectID,
	})
	category := strings.TrimSpace(status.StatusCategory)
	payload := metadataBasePayload(ctx)
	payload["status_id"] = statusID
	payload["project_id"] = projectID
	payload["scope_type"] = strings.TrimSpace(status.Scope.Type)
	payload["status_name"] = ""
	payload["status_name_present"] = strings.TrimSpace(status.Name) != ""
	payload["status_name_fingerprint"] = textFingerprint(status.Name)
	payload["description_present"] = strings.TrimSpace(status.Description) != ""
	payload["description_fingerprint"] = textFingerprint(status.Description)
	payload["status_category"] = category
	payload["status_category_key"] = firstNonBlank(status.StatusCategoryKey, strings.ToLower(category))
	payload["self_url"] = ""
	payload["self_url_fingerprint"] = urlFingerprint(sanitizeURL(status.Self))
	return workItemEnvelope(ctx, facts.WorkItemStatusMetadataFactKind, stableFactKey, payload, statusID, ctx.SourceURI), nil
}

// NewWorkItemWorkflowMetadataEnvelope converts one Jira workflow definition
// into a source fact.
func NewWorkItemWorkflowMetadataEnvelope(ctx EnvelopeContext, workflow WorkflowMetadata) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	workflowID := strings.TrimSpace(workflow.ID)
	if workflowID == "" {
		return facts.Envelope{}, fmt.Errorf("jira workflow metadata id is required")
	}
	stableFactKey := facts.StableID(facts.WorkItemWorkflowMetadataFactKind, map[string]any{
		"provider":    ProviderJiraCloud,
		"scope_id":    ctx.ScopeID,
		"workflow_id": workflowID,
		"project_id":  workflow.Scope.ProjectID,
	})
	payload := metadataBasePayload(ctx)
	payload["workflow_id"] = workflowID
	payload["workflow_name"] = ""
	payload["workflow_name_present"] = strings.TrimSpace(workflow.Name) != ""
	payload["workflow_name_fingerprint"] = textFingerprint(workflow.Name)
	payload["description_present"] = strings.TrimSpace(workflow.Description) != ""
	payload["description_fingerprint"] = textFingerprint(workflow.Description)
	payload["scope_type"] = strings.TrimSpace(workflow.Scope.Type)
	payload["project_id"] = strings.TrimSpace(workflow.Scope.ProjectID)
	payload["version_id"] = ""
	payload["version_id_present"] = strings.TrimSpace(workflow.Version.ID) != ""
	payload["version_id_fingerprint"] = textFingerprint(workflow.Version.ID)
	payload["version_number"] = workflow.Version.Number
	payload["statuses"] = workflowStatusPayloads(workflow.Statuses)
	payload["transitions"] = workflowTransitionPayloads(workflow.Transitions)
	return workItemEnvelope(ctx, facts.WorkItemWorkflowMetadataFactKind, stableFactKey, payload, workflowID, ctx.SourceURI), nil
}

// NewWorkItemFieldMetadataEnvelope converts one Jira field definition into a
// source fact.
func NewWorkItemFieldMetadataEnvelope(ctx EnvelopeContext, field FieldMetadata) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	fieldID := strings.TrimSpace(field.ID)
	if fieldID == "" {
		return facts.Envelope{}, fmt.Errorf("jira field metadata id is required")
	}
	fieldFingerprint := textFingerprint(fieldID)
	stableFactKey := facts.StableID(facts.WorkItemFieldMetadataFactKind, map[string]any{
		"provider": ProviderJiraCloud,
		"scope_id": ctx.ScopeID,
		"field_id": fieldID,
	})
	payload := metadataBasePayload(ctx)
	payload["field_id"] = ""
	payload["field_id_present"] = true
	payload["field_id_fingerprint"] = fieldFingerprint
	payload["field_name"] = ""
	payload["field_name_present"] = strings.TrimSpace(field.Name) != ""
	payload["field_name_fingerprint"] = textFingerprint(field.Name)
	payload["description_present"] = strings.TrimSpace(field.Description) != ""
	payload["description_fingerprint"] = textFingerprint(field.Description)
	payload["schema_type"] = strings.TrimSpace(field.Schema.Type)
	payload["schema_items"] = strings.TrimSpace(field.Schema.Items)
	payload["schema_system"] = strings.TrimSpace(field.Schema.System)
	payload["schema_custom"] = strings.TrimSpace(field.Schema.Custom)
	payload["custom_id_present"] = strings.TrimSpace(field.Schema.CustomID) != ""
	payload["self_url"] = ""
	payload["self_url_fingerprint"] = urlFingerprint(sanitizeURL(field.Self))
	return workItemEnvelope(ctx, facts.WorkItemFieldMetadataFactKind, stableFactKey, payload, fieldFingerprint, ctx.SourceURI), nil
}

// NewWorkItemMetadataWarningEnvelope converts one Jira metadata warning into a
// source fact.
func NewWorkItemMetadataWarningEnvelope(ctx EnvelopeContext, warning MetadataWarning) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	metadataType := strings.TrimSpace(warning.MetadataType)
	reason := strings.TrimSpace(warning.Reason)
	if metadataType == "" || reason == "" {
		return facts.Envelope{}, fmt.Errorf("jira metadata warning type and reason are required")
	}
	providerIDFingerprint := textFingerprint(warning.ProviderID)
	stableFactKey := facts.StableID(facts.WorkItemMetadataWarningFactKind, map[string]any{
		"provider":      ProviderJiraCloud,
		"scope_id":      ctx.ScopeID,
		"metadata_type": metadataType,
		"reason":        reason,
		"provider_id":   providerIDFingerprint,
	})
	payload := metadataBasePayload(ctx)
	payload["metadata_type"] = metadataType
	payload["reason"] = reason
	payload["failure_class"] = strings.TrimSpace(warning.FailureClass)
	payload["provider_id"] = ""
	payload["provider_id_present"] = strings.TrimSpace(warning.ProviderID) != ""
	payload["provider_id_fingerprint"] = providerIDFingerprint
	return workItemEnvelope(ctx, facts.WorkItemMetadataWarningFactKind, stableFactKey, payload, stableFactKey, ctx.SourceURI), nil
}

func metadataBasePayload(ctx EnvelopeContext) map[string]any {
	return map[string]any{
		"collector_instance_id":    ctx.CollectorInstanceID,
		"provider":                 ProviderJiraCloud,
		"redaction_policy_version": redactionPolicyVersion,
	}
}

func projectVisibilityState(project ProjectMetadata) string {
	switch {
	case project.Deleted:
		return "deleted"
	case project.Archived:
		return "archived"
	default:
		return "active"
	}
}

func workflowStatusPayloads(statuses []WorkflowStatusMetadata) []map[string]any {
	out := make([]map[string]any, 0, len(statuses))
	for _, status := range statuses {
		out = append(out, map[string]any{
			"status_reference": strings.TrimSpace(status.StatusReference),
			"status_id":        strings.TrimSpace(status.StatusID),
			"deprecated":       status.Deprecated,
		})
	}
	return out
}

func workflowTransitionPayloads(transitions []WorkflowTransitionMetadata) []map[string]any {
	out := make([]map[string]any, 0, len(transitions))
	for _, transition := range transitions {
		from := append([]string(nil), transition.FromStatusReferences...)
		for i := range from {
			from[i] = strings.TrimSpace(from[i])
		}
		sort.Strings(from)
		out = append(out, map[string]any{
			"transition_id":               strings.TrimSpace(transition.ID),
			"transition_name":             "",
			"transition_name_present":     strings.TrimSpace(transition.Name) != "",
			"transition_name_fingerprint": textFingerprint(transition.Name),
			"transition_type":             strings.TrimSpace(transition.Type),
			"from_status_references":      from,
			"to_status_reference":         strings.TrimSpace(transition.ToStatusReference),
			"has_validators":              transition.HasValidators,
			"has_triggers":                transition.HasTriggers,
			"has_actions":                 transition.HasActions,
		})
	}
	return out
}

func textFingerprint(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return "sha256:" + hex.EncodeToString(sum[:])
}
