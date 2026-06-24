// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import (
	"context"
	"net/http"
	"net/url"
	"strings"
)

const (
	jiraProjectSearchEndpoint = "/rest/api/3/project/search"
	jiraIssueTypesEndpoint    = "/rest/api/3/issuetype/project"
	jiraStatusesEndpoint      = "/rest/api/3/statuses/search"
	jiraFieldsEndpoint        = "/rest/api/3/field/search"
	jiraWorkflowsEndpoint     = "/rest/api/3/workflows/search"
)

type metadataPageInfo struct {
	StartAt    int  `json:"startAt"`
	MaxResults int  `json:"maxResults"`
	Total      int  `json:"total"`
	IsLast     bool `json:"isLast"`
}

type projectSearchResponse struct {
	metadataPageInfo
	Values []rawProjectMetadata `json:"values"`
}

type rawProjectMetadata struct {
	ID              string `json:"id"`
	Key             string `json:"key"`
	Name            string `json:"name"`
	Self            string `json:"self"`
	ProjectTypeKey  string `json:"projectTypeKey"`
	Style           string `json:"style"`
	Archived        bool   `json:"archived"`
	Deleted         bool   `json:"deleted"`
	ProjectCategory struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"projectCategory"`
	Insight struct {
		LastIssueUpdateTime string `json:"lastIssueUpdateTime"`
		TotalIssueCount     int    `json:"totalIssueCount"`
	} `json:"insight"`
}

type rawMetadataScope struct {
	Type    string `json:"type"`
	Project struct {
		ID string `json:"id"`
	} `json:"project"`
}

type rawIssueTypeMetadata struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Description    string           `json:"description"`
	EntityID       string           `json:"entityId"`
	HierarchyLevel int              `json:"hierarchyLevel"`
	Subtask        bool             `json:"subtask"`
	Self           string           `json:"self"`
	Scope          rawMetadataScope `json:"scope"`
}

type statusSearchResponse struct {
	metadataPageInfo
	Values []rawStatusMetadata `json:"values"`
}

type rawStatusMetadata struct {
	ID             string           `json:"id"`
	Name           string           `json:"name"`
	Description    string           `json:"description"`
	StatusCategory string           `json:"statusCategory"`
	Self           string           `json:"self"`
	Scope          rawMetadataScope `json:"scope"`
}

type fieldSearchResponse struct {
	metadataPageInfo
	Values []rawFieldMetadata `json:"values"`
}

type rawFieldMetadata struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Self        string         `json:"self"`
	Schema      rawFieldSchema `json:"schema"`
}

type rawFieldSchema struct {
	Type     string `json:"type"`
	Items    string `json:"items"`
	System   string `json:"system"`
	Custom   string `json:"custom"`
	CustomID any    `json:"customId"`
}

type workflowSearchResponse struct {
	metadataPageInfo
	Values []rawWorkflowMetadata `json:"values"`
}

type rawWorkflowMetadata struct {
	ID          string           `json:"id"`
	Name        string           `json:"name"`
	Description string           `json:"description"`
	Scope       rawMetadataScope `json:"scope"`
	Version     struct {
		ID            string `json:"id"`
		VersionNumber int    `json:"versionNumber"`
	} `json:"version"`
	Statuses    []rawWorkflowStatus     `json:"statuses"`
	Transitions []rawWorkflowTransition `json:"transitions"`
}

type rawWorkflowStatus struct {
	StatusReference string `json:"statusReference"`
	StatusID        string `json:"statusId"`
	Deprecated      bool   `json:"deprecated"`
}

type rawWorkflowTransition struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Type              string `json:"type"`
	ToStatusReference string `json:"toStatusReference"`
	Links             []struct {
		FromStatusReference string `json:"fromStatusReference"`
	} `json:"links"`
	Validators []any `json:"validators"`
	Triggers   []any `json:"triggers"`
	Actions    []any `json:"actions"`
}

func (c HTTPClient) collectMetadata(
	ctx context.Context,
	target TargetConfig,
	result *CollectionResult,
) error {
	limit := normalizedLimit(target.MetadataLimit, defaultMetadataLimit, maxMetadataLimit)
	if err := c.collectProjects(ctx, limit, result); err != nil {
		return err
	}
	if err := c.collectIssueTypes(ctx, limit, result); err != nil {
		return err
	}
	if err := c.collectStatuses(ctx, limit, result); err != nil {
		return err
	}
	if err := c.collectFields(ctx, limit, result); err != nil {
		return err
	}
	if err := c.collectWorkflows(ctx, limit, result); err != nil {
		return err
	}
	return nil
}

func (c HTTPClient) collectProjects(ctx context.Context, limit int, result *CollectionResult) error {
	startAt := 0
	for len(result.Projects) < limit {
		query := metadataPageQuery(startAt, limit-len(result.Projects))
		var response projectSearchResponse
		if err := c.doJSON(ctx, http.MethodGet, jiraProjectSearchEndpoint, query, nil, &response); err != nil {
			return c.recordMetadataEndpointError(result, "project", "", err)
		}
		result.Stats.MetadataPages++
		for _, raw := range response.Values {
			if len(result.Projects) >= limit {
				break
			}
			result.Stats.MetadataObjectsScanned++
			result.Stats.MetadataRedactions += projectRedactionCount(raw)
			result.Projects = append(result.Projects, ProjectMetadata{
				ID:              strings.TrimSpace(raw.ID),
				Key:             strings.TrimSpace(raw.Key),
				Name:            strings.TrimSpace(raw.Name),
				TypeKey:         strings.TrimSpace(raw.ProjectTypeKey),
				CategoryID:      strings.TrimSpace(raw.ProjectCategory.ID),
				CategoryName:    strings.TrimSpace(raw.ProjectCategory.Name),
				Style:           strings.TrimSpace(raw.Style),
				Archived:        raw.Archived,
				Deleted:         raw.Deleted,
				Self:            strings.TrimSpace(raw.Self),
				LastIssueUpdate: parseJiraTime(raw.Insight.LastIssueUpdateTime),
				IssueCount:      raw.Insight.TotalIssueCount,
			})
			result.Stats.MetadataObjectsEmitted++
		}
		if !metadataHasMore(response.metadataPageInfo, len(response.Values)) {
			break
		}
		startAt = metadataNextStart(response.metadataPageInfo, len(response.Values))
	}
	return nil
}

func (c HTTPClient) collectIssueTypes(ctx context.Context, limit int, result *CollectionResult) error {
	for _, project := range result.Projects {
		if len(result.IssueTypes) >= limit {
			break
		}
		projectID := strings.TrimSpace(project.ID)
		if projectID == "" {
			continue
		}
		query := url.Values{}
		query.Set("projectId", projectID)
		var response []rawIssueTypeMetadata
		if err := c.doJSON(ctx, http.MethodGet, jiraIssueTypesEndpoint, query, nil, &response); err != nil {
			if warningErr := c.recordMetadataEndpointError(result, "issue_type", projectID, err); warningErr != nil {
				return warningErr
			}
			continue
		}
		result.Stats.MetadataPages++
		for _, raw := range response {
			if len(result.IssueTypes) >= limit {
				break
			}
			result.Stats.MetadataObjectsScanned++
			result.Stats.MetadataRedactions += issueTypeRedactionCount(raw)
			scope := metadataScope(raw.Scope)
			if scope.ProjectID == "" {
				scope.ProjectID = projectID
			}
			result.IssueTypes = append(result.IssueTypes, IssueTypeMetadata{
				ID:             strings.TrimSpace(raw.ID),
				Name:           strings.TrimSpace(raw.Name),
				Description:    strings.TrimSpace(raw.Description),
				EntityID:       strings.TrimSpace(raw.EntityID),
				ProjectID:      projectID,
				Scope:          scope,
				HierarchyLevel: raw.HierarchyLevel,
				Subtask:        raw.Subtask,
				Self:           strings.TrimSpace(raw.Self),
			})
			result.Stats.MetadataObjectsEmitted++
		}
	}
	return nil
}

func (c HTTPClient) collectStatuses(ctx context.Context, limit int, result *CollectionResult) error {
	startAt := 0
	for len(result.Statuses) < limit {
		query := metadataPageQuery(startAt, limit-len(result.Statuses))
		var response statusSearchResponse
		if err := c.doJSON(ctx, http.MethodGet, jiraStatusesEndpoint, query, nil, &response); err != nil {
			return c.recordMetadataEndpointError(result, "status", "", err)
		}
		result.Stats.MetadataPages++
		for _, raw := range response.Values {
			if len(result.Statuses) >= limit {
				break
			}
			result.Stats.MetadataObjectsScanned++
			result.Stats.MetadataRedactions += statusRedactionCount(raw)
			scope := metadataScope(raw.Scope)
			result.Statuses = append(result.Statuses, StatusMetadata{
				ID:                strings.TrimSpace(raw.ID),
				Name:              strings.TrimSpace(raw.Name),
				Description:       strings.TrimSpace(raw.Description),
				StatusCategory:    strings.TrimSpace(raw.StatusCategory),
				StatusCategoryKey: strings.ToLower(strings.TrimSpace(raw.StatusCategory)),
				ProjectID:         scope.ProjectID,
				Scope:             scope,
				Self:              strings.TrimSpace(raw.Self),
			})
			result.Stats.MetadataObjectsEmitted++
		}
		if !metadataHasMore(response.metadataPageInfo, len(response.Values)) {
			break
		}
		startAt = metadataNextStart(response.metadataPageInfo, len(response.Values))
	}
	return nil
}

func (c HTTPClient) collectFields(ctx context.Context, limit int, result *CollectionResult) error {
	startAt := 0
	for len(result.Fields) < limit {
		query := metadataPageQuery(startAt, limit-len(result.Fields))
		var response fieldSearchResponse
		if err := c.doJSON(ctx, http.MethodGet, jiraFieldsEndpoint, query, nil, &response); err != nil {
			return c.recordMetadataEndpointError(result, "field", "", err)
		}
		result.Stats.MetadataPages++
		for _, raw := range response.Values {
			if len(result.Fields) >= limit {
				break
			}
			result.Stats.MetadataObjectsScanned++
			result.Stats.MetadataRedactions += fieldRedactionCount(raw)
			result.Fields = append(result.Fields, FieldMetadata{
				ID:          strings.TrimSpace(raw.ID),
				Name:        strings.TrimSpace(raw.Name),
				Description: strings.TrimSpace(raw.Description),
				Self:        strings.TrimSpace(raw.Self),
				Schema: FieldSchema{
					Type:     strings.TrimSpace(raw.Schema.Type),
					Items:    strings.TrimSpace(raw.Schema.Items),
					System:   strings.TrimSpace(raw.Schema.System),
					Custom:   strings.TrimSpace(raw.Schema.Custom),
					CustomID: anyString(raw.Schema.CustomID),
				},
			})
			result.Stats.MetadataObjectsEmitted++
		}
		if !metadataHasMore(response.metadataPageInfo, len(response.Values)) {
			break
		}
		startAt = metadataNextStart(response.metadataPageInfo, len(response.Values))
	}
	return nil
}

func (c HTTPClient) collectWorkflows(ctx context.Context, limit int, result *CollectionResult) error {
	startAt := 0
	for len(result.Workflows) < limit {
		query := metadataPageQuery(startAt, limit-len(result.Workflows))
		var response workflowSearchResponse
		if err := c.doJSON(ctx, http.MethodGet, jiraWorkflowsEndpoint, query, nil, &response); err != nil {
			if warningErr := c.recordMetadataEndpointError(result, "workflow", "", err); warningErr != nil {
				return warningErr
			}
			break
		}
		result.Stats.MetadataPages++
		for _, raw := range response.Values {
			if len(result.Workflows) >= limit {
				break
			}
			result.Stats.MetadataObjectsScanned++
			result.Stats.MetadataRedactions += workflowRedactionCount(raw)
			result.Workflows = append(result.Workflows, workflowMetadata(raw))
			result.Stats.MetadataObjectsEmitted++
		}
		if !metadataHasMore(response.metadataPageInfo, len(response.Values)) {
			break
		}
		startAt = metadataNextStart(response.metadataPageInfo, len(response.Values))
	}
	return nil
}
