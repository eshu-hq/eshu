// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
)

const (
	workItemEvidenceCapability = "work_item.evidence.list"
	workItemEvidenceMaxLimit   = 200

	// WorkItemEvidenceStateExactProviderFact marks a source-reported Jira fact.
	WorkItemEvidenceStateExactProviderFact = "exact_provider_fact"
	// WorkItemEvidenceStateUnsupportedLinkType marks a Jira remote link whose
	// target provider or link shape is not promoted by Eshu.
	WorkItemEvidenceStateUnsupportedLinkType = "unsupported_link_type"
	// WorkItemEvidenceStateMissingEvidence marks an empty scoped read.
	WorkItemEvidenceStateMissingEvidence = "missing_evidence"
	// WorkItemEvidenceStateStaleEvidence marks source evidence identified as
	// stale by upstream facts or freshness metadata.
	WorkItemEvidenceStateStaleEvidence = "stale_evidence"
	// WorkItemEvidenceStatePermissionHidden marks provider evidence hidden by
	// Jira permissions or issue security.
	WorkItemEvidenceStatePermissionHidden = "permission_hidden"
	// WorkItemEvidenceStateRejectedUnsafePayload marks malformed or unsafe
	// source payloads retained only as bounded warning evidence.
	WorkItemEvidenceStateRejectedUnsafePayload = "rejected_unsafe_payload"
)

var workItemEvidenceFactKinds = []string{
	"work_item.record",
	"work_item.transition",
	"work_item.external_link",
	"work_item.project_metadata",
	"work_item.status_metadata",
	"work_item.workflow_metadata",
	"work_item.field_metadata",
	"work_item.coverage_warning",
}

// WorkItemEvidenceStore reads bounded Jira/work-item source facts.
type WorkItemEvidenceStore interface {
	ListWorkItemEvidence(context.Context, WorkItemEvidenceFilter) ([]WorkItemEvidenceRow, error)
}

// WorkItemEvidenceFilter bounds direct work-item evidence reads to a source,
// work-item identity, project, URL fingerprint, or observation window.
type WorkItemEvidenceFilter struct {
	ScopeID            string
	ProjectKey         string
	WorkItemKey        string
	ProviderWorkItemID string
	ExternalURL        string
	URLFingerprint     string
	ObservedAfter      time.Time
	AfterFactID        string
	Limit              int

	// AllowedRepositoryIDs carries the scoped-token grant set (union of granted
	// repository and ingestion-scope ids) intersected against each work item's
	// durable linked_repository_id. It is empty for shared/admin/local callers,
	// which bypass the grant predicate entirely; a non-empty value bounds a
	// scoped read so a work item is visible only when its durable repository link
	// is granted. A scoped token with an empty grant must short-circuit before
	// the store read rather than pass an empty slice here.
	AllowedRepositoryIDs []string
}

// WorkItemEvidenceRow is one redacted source-fact row from a work-item
// collector. It never exposes raw Jira URLs, remote-link URLs, summaries, user
// identities, or provider response bodies.
type WorkItemEvidenceRow struct {
	FactID                 string `json:"fact_id"`
	FactKind               string `json:"fact_kind"`
	ScopeID                string `json:"scope_id,omitempty"`
	GenerationID           string `json:"generation_id,omitempty"`
	Provider               string `json:"provider,omitempty"`
	SourceConfidence       string `json:"source_confidence,omitempty"`
	ObservedAt             string `json:"observed_at,omitempty"`
	EvidenceState          string `json:"evidence_state"`
	WorkItemKey            string `json:"work_item_key,omitempty"`
	ProviderWorkItemID     string `json:"provider_work_item_id,omitempty"`
	ProjectID              string `json:"project_id,omitempty"`
	ProjectKey             string `json:"project_key,omitempty"`
	IssueTypeID            string `json:"issue_type_id,omitempty"`
	IssueTypeName          string `json:"issue_type_name,omitempty"`
	StatusID               string `json:"status_id,omitempty"`
	StatusName             string `json:"status_name,omitempty"`
	CreatedAt              string `json:"created_at,omitempty"`
	UpdatedAt              string `json:"updated_at,omitempty"`
	ResolvedAt             string `json:"resolved_at,omitempty"`
	ProviderChangelogID    string `json:"provider_changelog_id,omitempty"`
	Field                  string `json:"field,omitempty"`
	From                   string `json:"from,omitempty"`
	To                     string `json:"to,omitempty"`
	ValueRedacted          bool   `json:"value_redacted,omitempty"`
	ProviderRemoteLinkID   string `json:"provider_remote_link_id,omitempty"`
	GlobalID               string `json:"global_id,omitempty"`
	ApplicationName        string `json:"application_name,omitempty"`
	ApplicationType        string `json:"application_type,omitempty"`
	Relationship           string `json:"relationship,omitempty"`
	URLFingerprint         string `json:"url_fingerprint,omitempty"`
	URLPresent             bool   `json:"url_present,omitempty"`
	URLRedacted            bool   `json:"url_redacted,omitempty"`
	TitlePresent           bool   `json:"title_present,omitempty"`
	SummaryPresent         bool   `json:"summary_present,omitempty"`
	AnchorClass            string `json:"correlation_anchor_class,omitempty"`
	ProviderSupportState   string `json:"provider_support_state,omitempty"`
	RedactionPolicyVersion string `json:"redaction_policy_version,omitempty"`
	// LinkedRepositoryID is the durable canonical repository id the Jira
	// collector resolves from a confidently typed GitHub PR or GitLab MR link
	// before redaction (see collector/jira linked_repository.go). It is the same
	// generation-independent id Eshu stores for every repository and carries no
	// raw URL, query parameter, credential, or user identity. It is empty for
	// non-link facts and for links that did not canonicalize. Scoped-token reads
	// authorize a work item only when this id is within the caller's grant set.
	LinkedRepositoryID string `json:"linked_repository_id,omitempty"`
	RawURL             string `json:"-"`
}

type workItemEvidenceFactRow struct {
	FactID           string
	FactKind         string
	ScopeID          string
	GenerationID     string
	SourceConfidence string
	ObservedAt       string
	Payload          map[string]any
}

func normalizeWorkItemEvidenceFilter(filter WorkItemEvidenceFilter) WorkItemEvidenceFilter {
	filter.ScopeID = strings.TrimSpace(filter.ScopeID)
	filter.ProjectKey = strings.ToUpper(strings.TrimSpace(filter.ProjectKey))
	filter.WorkItemKey = strings.ToUpper(strings.TrimSpace(filter.WorkItemKey))
	filter.ProviderWorkItemID = strings.TrimSpace(filter.ProviderWorkItemID)
	filter.ExternalURL = strings.TrimSpace(filter.ExternalURL)
	filter.URLFingerprint = strings.TrimSpace(filter.URLFingerprint)
	if filter.URLFingerprint == "" {
		filter.URLFingerprint = workItemURLFingerprint(filter.ExternalURL)
	}
	filter.ExternalURL = ""
	filter.AfterFactID = strings.TrimSpace(filter.AfterFactID)
	if !filter.ObservedAfter.IsZero() {
		filter.ObservedAfter = filter.ObservedAfter.UTC()
	}
	return filter
}

func (f WorkItemEvidenceFilter) hasScope() bool {
	return strings.TrimSpace(f.ScopeID) != "" ||
		strings.TrimSpace(f.ProjectKey) != "" ||
		strings.TrimSpace(f.WorkItemKey) != "" ||
		strings.TrimSpace(f.ProviderWorkItemID) != "" ||
		strings.TrimSpace(f.URLFingerprint) != "" ||
		!f.ObservedAfter.IsZero()
}

func buildWorkItemEvidenceRows(facts []workItemEvidenceFactRow) []WorkItemEvidenceRow {
	rows := make([]WorkItemEvidenceRow, 0, len(facts))
	for _, fact := range facts {
		payload := fact.Payload
		row := WorkItemEvidenceRow{
			FactID:                 fact.FactID,
			FactKind:               fact.FactKind,
			ScopeID:                fact.ScopeID,
			GenerationID:           fact.GenerationID,
			Provider:               StringVal(payload, "provider"),
			SourceConfidence:       fact.SourceConfidence,
			ObservedAt:             fact.ObservedAt,
			EvidenceState:          workItemEvidenceState(fact),
			WorkItemKey:            StringVal(payload, "work_item_key"),
			ProviderWorkItemID:     StringVal(payload, "provider_work_item_id"),
			ProjectID:              StringVal(payload, "project_id"),
			ProjectKey:             StringVal(payload, "project_key"),
			IssueTypeID:            StringVal(payload, "issue_type_id"),
			IssueTypeName:          StringVal(payload, "issue_type_name"),
			StatusID:               StringVal(payload, "status_id"),
			StatusName:             StringVal(payload, "status_name"),
			CreatedAt:              StringVal(payload, "created_at"),
			UpdatedAt:              StringVal(payload, "updated_at"),
			ResolvedAt:             StringVal(payload, "resolved_at"),
			ProviderChangelogID:    StringVal(payload, "provider_changelog_id"),
			Field:                  StringVal(payload, "field"),
			From:                   StringVal(payload, "from"),
			To:                     StringVal(payload, "to"),
			ValueRedacted:          BoolVal(payload, "value_redacted"),
			ProviderRemoteLinkID:   StringVal(payload, "provider_remote_link_id"),
			GlobalID:               StringVal(payload, "global_id"),
			ApplicationName:        StringVal(payload, "application_name"),
			ApplicationType:        StringVal(payload, "application_type"),
			Relationship:           StringVal(payload, "relationship"),
			URLFingerprint:         StringVal(payload, "url_fingerprint"),
			URLPresent:             BoolVal(payload, "url_present"),
			URLRedacted:            BoolVal(payload, "url_redacted"),
			TitlePresent:           BoolVal(payload, "title_present"),
			SummaryPresent:         BoolVal(payload, "summary_present"),
			AnchorClass:            StringVal(payload, "correlation_anchor_class"),
			ProviderSupportState:   StringVal(payload, "provider_support_state"),
			RedactionPolicyVersion: StringVal(payload, "redaction_policy_version"),
			LinkedRepositoryID:     StringVal(payload, "linked_repository_id"),
		}
		rows = append(rows, row)
	}
	return rows
}

func workItemEvidenceState(fact workItemEvidenceFactRow) string {
	payload := fact.Payload
	if state := strings.TrimSpace(StringVal(payload, "evidence_state")); knownWorkItemEvidenceState(state) {
		return state
	}
	if BoolVal(payload, "permission_hidden") ||
		StringVal(payload, "failure_class") == "permission_hidden" ||
		StringVal(payload, "visibility_state") == "permission_hidden" {
		return WorkItemEvidenceStatePermissionHidden
	}
	if StringVal(payload, "source_freshness") == "stale" ||
		StringVal(payload, "freshness_state") == "stale" {
		return WorkItemEvidenceStateStaleEvidence
	}
	if fact.FactKind == "work_item.external_link" {
		state := strings.TrimSpace(StringVal(payload, "provider_support_state"))
		switch {
		case strings.Contains(state, "unsupported"):
			return WorkItemEvidenceStateUnsupportedLinkType
		case strings.Contains(state, "rejected"):
			return WorkItemEvidenceStateRejectedUnsafePayload
		}
	}
	if StringVal(payload, "warning_reason") == "rejected_unsafe_payload" {
		return WorkItemEvidenceStateRejectedUnsafePayload
	}
	return WorkItemEvidenceStateExactProviderFact
}

func knownWorkItemEvidenceState(state string) bool {
	return slices.Contains([]string{
		WorkItemEvidenceStateExactProviderFact,
		WorkItemEvidenceStateUnsupportedLinkType,
		WorkItemEvidenceStateMissingEvidence,
		WorkItemEvidenceStateStaleEvidence,
		WorkItemEvidenceStatePermissionHidden,
		WorkItemEvidenceStateRejectedUnsafePayload,
	}, state)
}

func summarizeWorkItemEvidenceStates(rows []WorkItemEvidenceRow) []string {
	if len(rows) == 0 {
		return []string{WorkItemEvidenceStateMissingEvidence}
	}
	seen := map[string]struct{}{}
	for _, row := range rows {
		state := strings.TrimSpace(row.EvidenceState)
		if state == "" {
			state = WorkItemEvidenceStateExactProviderFact
		}
		seen[state] = struct{}{}
	}
	return setToSortedSlice(seen)
}

func workItemEvidenceSpanAttributes(rows []WorkItemEvidenceRow, truncated bool) []attribute.KeyValue {
	counts := map[string]int{
		WorkItemEvidenceStateStaleEvidence:         0,
		WorkItemEvidenceStatePermissionHidden:      0,
		WorkItemEvidenceStateRejectedUnsafePayload: 0,
		WorkItemEvidenceStateUnsupportedLinkType:   0,
	}
	for _, row := range rows {
		state := strings.TrimSpace(row.EvidenceState)
		if state == "" {
			state = WorkItemEvidenceStateExactProviderFact
		}
		if _, ok := counts[state]; ok {
			counts[state]++
		}
	}
	missingCount := 0
	if len(rows) == 0 {
		missingCount = 1
	}
	return []attribute.KeyValue{
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceQueryCount, 1),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceResultCount, len(rows)),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceStaleCount, counts[WorkItemEvidenceStateStaleEvidence]),
		attribute.Int(telemetry.SpanAttrWorkItemEvidencePermissionHiddenCount, counts[WorkItemEvidenceStatePermissionHidden]),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceRejectedUnsafePayloadCount, counts[WorkItemEvidenceStateRejectedUnsafePayload]),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceUnsupportedLinkTypeCount, counts[WorkItemEvidenceStateUnsupportedLinkType]),
		attribute.Int(telemetry.SpanAttrWorkItemEvidenceMissingCount, missingCount),
		attribute.Bool(telemetry.SpanAttrWorkItemEvidenceTruncated, truncated),
	}
}

func workItemURLFingerprint(raw string) string {
	sanitized := sanitizeWorkItemURL(raw)
	if sanitized == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(sanitized))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func sanitizeWorkItemURL(raw string) string {
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
		if sensitiveWorkItemQueryKey(key) {
			query.Del(key)
		}
	}
	parsed.RawQuery = query.Encode()
	parsed.Fragment = ""
	return parsed.String()
}

func sensitiveWorkItemQueryKey(key string) bool {
	switch strings.ToLower(strings.TrimSpace(key)) {
	case "access_token", "api_key", "apikey", "auth", "authorization", "jwt",
		"key", "password", "passwd", "secret", "sig", "signature", "token":
		return true
	default:
		return false
	}
}
