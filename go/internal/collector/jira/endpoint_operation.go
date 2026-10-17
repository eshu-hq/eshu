// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package jira

import "strings"

// jiraOperationForEndpoint maps a Jira REST endpoint to its bounded failure
// operation token (#8258). Per-issue paths carry the issue key, so the token
// is matched by prefix and suffix and never derived from the raw path: an
// unknown endpoint keeps a blank token and claim handling falls back to its
// stage operation.
func jiraOperationForEndpoint(endpoint string) string {
	switch endpoint {
	case jiraSearchEndpoint:
		return "search_issues"
	case jiraProjectSearchEndpoint:
		return "list_projects"
	case jiraIssueTypesEndpoint:
		return "list_issue_types"
	case jiraStatusesEndpoint:
		return "list_statuses"
	case jiraFieldsEndpoint:
		return "list_fields"
	case jiraWorkflowsEndpoint:
		return "list_workflows"
	}
	if strings.HasPrefix(endpoint, jiraIssueEndpointPrefix+"/") {
		switch {
		case strings.HasSuffix(endpoint, "/changelog"):
			return "read_changelog"
		case strings.HasSuffix(endpoint, "/remotelink"):
			return "read_remote_links"
		}
	}
	return ""
}
