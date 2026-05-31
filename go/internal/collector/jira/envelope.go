package jira

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// NewWorkItemRecordEnvelope converts one Jira issue into a source fact.
func NewWorkItemRecordEnvelope(ctx EnvelopeContext, issue Issue) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	issueID := strings.TrimSpace(issue.ID)
	issueKey := strings.TrimSpace(issue.Key)
	if issueID == "" || issueKey == "" {
		return facts.Envelope{}, fmt.Errorf("jira issue id and key are required")
	}
	stableFactKey := facts.StableID(facts.WorkItemRecordFactKind, map[string]any{
		"provider":  ProviderJiraCloud,
		"scope_id":  ctx.ScopeID,
		"issue_id":  issueID,
		"issue_key": issueKey,
	})
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              ProviderJiraCloud,
		"provider_work_item_id": issueID,
		"work_item_key":         issueKey,
		"summary":               strings.TrimSpace(issue.Summary),
		"issue_type_id":         strings.TrimSpace(issue.IssueType.ID),
		"issue_type_name":       strings.TrimSpace(issue.IssueType.Name),
		"status_id":             strings.TrimSpace(issue.Status.ID),
		"status_name":           strings.TrimSpace(issue.Status.Name),
		"project_id":            strings.TrimSpace(issue.Project.ID),
		"project_key":           strings.TrimSpace(issue.Project.Key),
		"project_name":          strings.TrimSpace(issue.Project.Name),
		"assignee_account_id":   strings.TrimSpace(issue.Assignee.AccountID),
		"assignee_display_name": strings.TrimSpace(issue.Assignee.DisplayName),
		"reporter_account_id":   strings.TrimSpace(issue.Reporter.AccountID),
		"reporter_display_name": strings.TrimSpace(issue.Reporter.DisplayName),
		"created_at":            formatTime(issue.CreatedAt),
		"updated_at":            formatTime(issue.UpdatedAt),
		"resolved_at":           formatTime(issue.ResolvedAt),
		"self_url":              sanitizeURL(issue.Self),
		"source_url":            sanitizeURL(firstNonBlank(issue.BrowseURL, issue.Self, ctx.SourceURI)),
	}
	return workItemEnvelope(ctx, facts.WorkItemRecordFactKind, stableFactKey, payload, issueID, firstNonBlank(issue.Self, ctx.SourceURI)), nil
}

// NewWorkItemTransitionEnvelope converts one Jira changelog item into a source
// fact.
func NewWorkItemTransitionEnvelope(ctx EnvelopeContext, transition Transition) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	changelogID := strings.TrimSpace(transition.ID)
	if changelogID == "" {
		return facts.Envelope{}, fmt.Errorf("jira changelog id is required")
	}
	stableFactKey := facts.StableID(facts.WorkItemTransitionFactKind, map[string]any{
		"provider":     ProviderJiraCloud,
		"scope_id":     ctx.ScopeID,
		"issue_id":     strings.TrimSpace(transition.IssueID),
		"changelog_id": changelogID,
		"field":        strings.TrimSpace(transition.Field),
	})
	payload := map[string]any{
		"collector_instance_id": ctx.CollectorInstanceID,
		"provider":              ProviderJiraCloud,
		"provider_changelog_id": changelogID,
		"provider_work_item_id": strings.TrimSpace(transition.IssueID),
		"work_item_key":         strings.TrimSpace(transition.IssueKey),
		"field":                 strings.TrimSpace(transition.Field),
		"from":                  strings.TrimSpace(transition.From),
		"to":                    strings.TrimSpace(transition.To),
		"author_account_id":     strings.TrimSpace(transition.Author.AccountID),
		"author_display_name":   strings.TrimSpace(transition.Author.DisplayName),
		"created_at":            formatTime(transition.CreatedAt),
	}
	return workItemEnvelope(ctx, facts.WorkItemTransitionFactKind, stableFactKey, payload, changelogID, ctx.SourceURI), nil
}

// NewWorkItemExternalLinkEnvelope converts one Jira remote link into a source
// fact.
func NewWorkItemExternalLinkEnvelope(ctx EnvelopeContext, link ExternalLink) (facts.Envelope, error) {
	if err := validateEnvelopeContext(ctx); err != nil {
		return facts.Envelope{}, err
	}
	recordID := firstNonBlank(link.ID, link.GlobalID, sanitizeURL(link.Object.URL))
	if recordID == "" {
		return facts.Envelope{}, fmt.Errorf("jira remote link id, global_id, or url is required")
	}
	stableFactKey := facts.StableID(facts.WorkItemExternalLinkFactKind, map[string]any{
		"provider":       ProviderJiraCloud,
		"scope_id":       ctx.ScopeID,
		"issue_id":       strings.TrimSpace(link.IssueID),
		"remote_link_id": recordID,
	})
	payload := map[string]any{
		"collector_instance_id":    ctx.CollectorInstanceID,
		"provider":                 ProviderJiraCloud,
		"provider_remote_link_id":  strings.TrimSpace(link.ID),
		"provider_work_item_id":    strings.TrimSpace(link.IssueID),
		"work_item_key":            strings.TrimSpace(link.IssueKey),
		"global_id":                strings.TrimSpace(link.GlobalID),
		"application_name":         strings.TrimSpace(link.Application.Name),
		"application_type":         strings.TrimSpace(link.Application.Type),
		"relationship":             strings.TrimSpace(link.Relationship),
		"url":                      sanitizeURL(link.Object.URL),
		"title":                    strings.TrimSpace(link.Object.Title),
		"summary":                  strings.TrimSpace(link.Object.Summary),
		"correlation_anchor_class": externalLinkAnchorClass(link),
	}
	return workItemEnvelope(ctx, facts.WorkItemExternalLinkFactKind, stableFactKey, payload, recordID, firstNonBlank(link.Object.URL, ctx.SourceURI)), nil
}

func workItemEnvelope(
	ctx EnvelopeContext,
	factKind string,
	stableFactKey string,
	payload map[string]any,
	sourceRecordID string,
	sourceURI string,
) facts.Envelope {
	return facts.Envelope{
		FactID:           workItemFactID(factKind, stableFactKey, ctx.ScopeID, ctx.GenerationID),
		ScopeID:          ctx.ScopeID,
		GenerationID:     ctx.GenerationID,
		FactKind:         factKind,
		StableFactKey:    stableFactKey,
		SchemaVersion:    facts.WorkItemSchemaVersionV1,
		CollectorKind:    CollectorKind,
		FencingToken:     ctx.FencingToken,
		SourceConfidence: facts.SourceConfidenceReported,
		ObservedAt:       normalizedObservedAt(ctx.ObservedAt),
		Payload:          payload,
		SourceRef: facts.Ref{
			SourceSystem:   CollectorKind,
			ScopeID:        ctx.ScopeID,
			GenerationID:   ctx.GenerationID,
			FactKey:        stableFactKey,
			SourceURI:      sanitizeURL(sourceURI),
			SourceRecordID: strings.TrimSpace(sourceRecordID),
		},
	}
}

func validateEnvelopeContext(ctx EnvelopeContext) error {
	if strings.TrimSpace(ctx.ScopeID) == "" {
		return fmt.Errorf("jira envelope scope_id must not be blank")
	}
	if strings.TrimSpace(ctx.GenerationID) == "" {
		return fmt.Errorf("jira envelope generation_id must not be blank")
	}
	if strings.TrimSpace(ctx.CollectorInstanceID) == "" {
		return fmt.Errorf("jira envelope collector_instance_id must not be blank")
	}
	return nil
}

func workItemFactID(factKind, stableFactKey, scopeID, generationID string) string {
	return facts.StableID("WorkItemFact", map[string]any{
		"fact_kind":       factKind,
		"generation_id":   generationID,
		"scope_id":        scopeID,
		"stable_fact_key": stableFactKey,
	})
}

func externalLinkAnchorClass(link ExternalLink) string {
	value := strings.ToLower(firstNonBlank(link.Application.Type, link.Application.Name, link.Object.URL))
	switch {
	case strings.Contains(value, "github") && strings.Contains(strings.ToLower(link.Object.URL), "/pull/"):
		return "github_pull_request"
	case strings.Contains(value, "gitlab") && strings.Contains(strings.ToLower(link.Object.URL), "/merge_requests/"):
		return "gitlab_merge_request"
	default:
		return "remote_link"
	}
}

func sanitizeURL(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	parsed, err := url.Parse(trimmed)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return ""
	}
	parsed.User = nil
	query := parsed.Query()
	for key := range query {
		if sensitiveQueryKey(key) {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

func sensitiveQueryKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "access_token", "api_key", "apikey", "auth", "authorization", "jwt",
		"key", "password", "passwd", "secret", "sig", "signature", "token":
		return true
	default:
		return false
	}
}

func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func normalizedObservedAt(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value.UTC()
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func anyString(value any) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		return strconv.FormatInt(int64(typed), 10)
	case int:
		return strconv.Itoa(typed)
	case int64:
		return strconv.FormatInt(typed, 10)
	default:
		return ""
	}
}
