// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

func (c HTTPClient) recordMetadataEndpointError(
	result *CollectionResult,
	metadataType string,
	providerID string,
	err error,
) error {
	warning, ok := metadataWarningFromError(metadataType, providerID, err)
	if !ok {
		return err
	}
	result.MetadataWarnings = append(result.MetadataWarnings, warning)
	result.Stats.MetadataObjectsEmitted++
	switch warning.Reason {
	case "permission_hidden":
		result.Stats.PermissionHiddenMetadata++
	case "unsupported":
		result.Stats.UnsupportedMetadata++
	}
	return nil
}

func metadataWarningFromError(metadataType string, providerID string, err error) (MetadataWarning, bool) {
	if errors.Is(err, ErrArchivedIssue) {
		return MetadataWarning{
			MetadataType: metadataType,
			Reason:       "archived",
			FailureClass: string(FailureArchived),
			ProviderID:   providerID,
		}, true
	}
	var jiraErr JiraError
	if !errors.As(err, &jiraErr) {
		return MetadataWarning{}, false
	}
	switch jiraErr.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return MetadataWarning{
			MetadataType: metadataType,
			Reason:       "permission_hidden",
			FailureClass: string(FailurePermissionHidden),
			ProviderID:   providerID,
		}, true
	case http.StatusNotFound:
		return MetadataWarning{
			MetadataType: metadataType,
			Reason:       "unsupported",
			FailureClass: string(FailureDeleted),
			ProviderID:   providerID,
		}, true
	default:
		return MetadataWarning{}, false
	}
}

func metadataPageQuery(startAt int, remaining int) url.Values {
	query := url.Values{}
	query.Set("startAt", strconv.Itoa(startAt))
	query.Set("maxResults", strconv.Itoa(min(remaining, maxMetadataLimit)))
	return query
}

func metadataHasMore(page metadataPageInfo, valueCount int) bool {
	if page.IsLast || valueCount == 0 {
		return false
	}
	if page.Total > 0 {
		return metadataNextStart(page, valueCount) < page.Total
	}
	return page.MaxResults > 0
}

func metadataNextStart(page metadataPageInfo, valueCount int) int {
	pageSize := page.MaxResults
	if pageSize <= 0 {
		pageSize = valueCount
	}
	return page.StartAt + pageSize
}

func metadataScope(raw rawMetadataScope) MetadataScope {
	return MetadataScope{
		Type:      strings.TrimSpace(raw.Type),
		ProjectID: strings.TrimSpace(raw.Project.ID),
	}
}

func workflowMetadata(raw rawWorkflowMetadata) WorkflowMetadata {
	workflow := WorkflowMetadata{
		ID:          strings.TrimSpace(raw.ID),
		Name:        strings.TrimSpace(raw.Name),
		Description: strings.TrimSpace(raw.Description),
		Scope:       metadataScope(raw.Scope),
		Version: WorkflowVersion{
			ID:     strings.TrimSpace(raw.Version.ID),
			Number: raw.Version.VersionNumber,
		},
		Statuses:    make([]WorkflowStatusMetadata, 0, len(raw.Statuses)),
		Transitions: make([]WorkflowTransitionMetadata, 0, len(raw.Transitions)),
	}
	for _, rawStatus := range raw.Statuses {
		workflow.Statuses = append(workflow.Statuses, WorkflowStatusMetadata{
			StatusReference: strings.TrimSpace(rawStatus.StatusReference),
			StatusID:        strings.TrimSpace(rawStatus.StatusID),
			Deprecated:      rawStatus.Deprecated,
		})
	}
	for _, rawTransition := range raw.Transitions {
		from := make([]string, 0, len(rawTransition.Links))
		for _, link := range rawTransition.Links {
			if value := strings.TrimSpace(link.FromStatusReference); value != "" {
				from = append(from, value)
			}
		}
		workflow.Transitions = append(workflow.Transitions, WorkflowTransitionMetadata{
			ID:                   strings.TrimSpace(rawTransition.ID),
			Name:                 strings.TrimSpace(rawTransition.Name),
			Type:                 strings.TrimSpace(rawTransition.Type),
			FromStatusReferences: from,
			ToStatusReference:    strings.TrimSpace(rawTransition.ToStatusReference),
			HasValidators:        len(rawTransition.Validators) > 0,
			HasTriggers:          len(rawTransition.Triggers) > 0,
			HasActions:           len(rawTransition.Actions) > 0,
		})
	}
	return workflow
}

func projectRedactionCount(raw rawProjectMetadata) int {
	return metadataRedactionCount(raw.Name, raw.ProjectCategory.Name, raw.Self)
}

func issueTypeRedactionCount(raw rawIssueTypeMetadata) int {
	return metadataRedactionCount(raw.Name, raw.Description, raw.EntityID, raw.Self)
}

func statusRedactionCount(raw rawStatusMetadata) int {
	return metadataRedactionCount(raw.Name, raw.Description, raw.Self)
}

func fieldRedactionCount(raw rawFieldMetadata) int {
	return metadataRedactionCount(raw.ID, raw.Name, raw.Description, raw.Self, anyString(raw.Schema.CustomID))
}

func workflowRedactionCount(raw rawWorkflowMetadata) int {
	count := metadataRedactionCount(raw.Name, raw.Description, raw.Version.ID)
	for _, transition := range raw.Transitions {
		count += metadataRedactionCount(transition.Name)
	}
	return count
}

func metadataRedactionCount(values ...string) int {
	count := 0
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			count++
		}
	}
	return count
}
