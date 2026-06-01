package jira

import (
	"net/http"
	"strconv"
	"strings"
	"time"
)

func jiraSearchFields() []string {
	return []string{
		"summary",
		"created",
		"updated",
		"resolutiondate",
		"issuetype",
		"status",
		"project",
		"assignee",
		"reporter",
	}
}

func changelogValues(item changelogItem) (string, string, bool) {
	field := strings.ToLower(strings.TrimSpace(firstNonBlank(item.FieldID, item.Field)))
	if redactedChangelogField(field) {
		return "", "", true
	}
	return firstNonBlank(item.From, item.FromID), firstNonBlank(item.To, item.ToID), false
}

func redactedChangelogField(field string) bool {
	switch field {
	case "assignee", "reporter", "creator", "comment", "description", "attachment":
		return true
	default:
		return strings.HasPrefix(field, "customfield_")
	}
}

func changelogPaginationPresent(response changelogResponse) bool {
	return response.IsLast || response.Total > 0 || response.MaxResults > 0 || response.StartAt > 0
}

func remoteLinkProviderSupportState(application LinkApplication, rawURL string) string {
	value := strings.ToLower(firstNonBlank(application.Type, application.Name, rawURL))
	switch {
	case strings.Contains(value, "github") && strings.Contains(strings.ToLower(rawURL), "/pull/"):
		return "supported_provider"
	case strings.Contains(value, "gitlab") && strings.Contains(strings.ToLower(rawURL), "/merge_requests/"):
		return "supported_provider"
	default:
		return "unsupported_provider"
	}
}

func parseRetryAfter(raw string) time.Duration {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(trimmed); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if value, err := http.ParseTime(trimmed); err == nil {
		delay := time.Until(value)
		if delay > 0 {
			return delay
		}
	}
	return 0
}
