// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"log/slog"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
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

// workItemEvidenceFactKinds bounds the evidence read to exactly the work_item
// family the fact-kind registry maps to GET /api/v0/work-items/evidence.
// facts.WorkItemFactKinds() is the single source of truth for that family, so
// deriving the read set from it (rather than a hand-maintained copy) keeps the
// SQL kind list, the decode switch in decodeWorkItemEvidenceRow, and the
// registry in lockstep and drops any kind that is not a real registered fact.
var workItemEvidenceFactKinds = facts.WorkItemFactKinds()

// WorkItemEvidenceStore reads bounded Jira/work-item source facts.
type WorkItemEvidenceStore interface {
	ListWorkItemEvidence(context.Context, WorkItemEvidenceFilter) (WorkItemEvidencePage, error)
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
	SchemaVersion    string
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

// buildWorkItemEvidenceRows decodes each fact through the typed
// sdk/go/factschema/workitem/v1 seam and shapes it into a WorkItemEvidenceRow.
// A fact whose payload is missing a required identity anchor (per its kind's
// typed struct, see workitem/v1/README.md) is classified input_invalid by the
// decode seam and DROPPED from the result — logged at debug level for
// operator visibility — rather than producing a row with an empty-string
// identity. This is the accuracy guarantee Contract System v1 exists to
// protect: a malformed fact must be a visible absence, not a silent wrong
// answer. Every other decode error (an unsupported schema major) is treated
// the same way today because this is a best-effort list read, not a durable
// write path with its own dead-letter queue; a future schema-major rollout
// would need to widen this behavior deliberately.
func buildWorkItemEvidenceRows(facts []workItemEvidenceFactRow) []WorkItemEvidenceRow {
	rows := make([]WorkItemEvidenceRow, 0, len(facts))
	for _, fact := range facts {
		row, ok := decodeWorkItemEvidenceRow(fact)
		if !ok {
			continue
		}
		rows = append(rows, row)
	}
	return rows
}

// decodeWorkItemEvidenceRow decodes one fact row into a WorkItemEvidenceRow
// through the typed decode seam matching its fact kind. ok is false when the
// fact failed decode (a *queryDecodeError, logged at debug level); the caller
// drops the fact from the result set rather than emitting an empty-identity
// row. An unrecognized fact kind also returns ok=false rather than a
// zero-value row, matching the historical behavior of the raw-map lookups
// (which would have returned all-empty fields for an unknown kind, never
// surfaced as an evidence row in practice since workItemEvidenceFactKinds
// bounds the SQL read).
func decodeWorkItemEvidenceRow(fact workItemEvidenceFactRow) (WorkItemEvidenceRow, bool) {
	base := WorkItemEvidenceRow{
		FactID:           fact.FactID,
		FactKind:         fact.FactKind,
		ScopeID:          fact.ScopeID,
		GenerationID:     fact.GenerationID,
		SourceConfidence: fact.SourceConfidence,
		ObservedAt:       fact.ObservedAt,
	}

	switch fact.FactKind {
	case "work_item.record":
		record, err := decodeWorkItemRecord(workItemDecodeInput{FactID: fact.FactID, SchemaVersion: fact.SchemaVersion, Payload: fact.Payload})
		if err != nil {
			logWorkItemEvidenceDecodeDrop(err)
			return WorkItemEvidenceRow{}, false
		}
		base.Provider = record.Provider
		base.WorkItemKey = record.WorkItemKey
		base.ProviderWorkItemID = record.ProviderWorkItemID
		base.ProjectID = workItemDerefString(record.ProjectID)
		base.ProjectKey = workItemDerefString(record.ProjectKey)
		base.IssueTypeID = workItemDerefString(record.IssueTypeID)
		base.IssueTypeName = workItemDerefString(record.IssueTypeName)
		base.StatusID = workItemDerefString(record.StatusID)
		base.StatusName = workItemDerefString(record.StatusName)
		base.CreatedAt = workItemDerefString(record.CreatedAt)
		base.UpdatedAt = workItemDerefString(record.UpdatedAt)
		base.ResolvedAt = workItemDerefString(record.ResolvedAt)
		base.RedactionPolicyVersion = workItemDerefString(record.RedactionPolicyVersion)

	case "work_item.transition":
		transition, err := decodeWorkItemTransition(workItemDecodeInput{FactID: fact.FactID, SchemaVersion: fact.SchemaVersion, Payload: fact.Payload})
		if err != nil {
			logWorkItemEvidenceDecodeDrop(err)
			return WorkItemEvidenceRow{}, false
		}
		base.Provider = transition.Provider
		base.WorkItemKey = workItemDerefString(transition.WorkItemKey)
		base.ProviderWorkItemID = workItemDerefString(transition.ProviderWorkItemID)
		base.ProviderChangelogID = transition.ProviderChangelogID
		base.Field = workItemDerefString(transition.Field)
		base.From = workItemDerefString(transition.From)
		base.To = workItemDerefString(transition.To)
		base.ValueRedacted = workItemDerefBool(transition.ValueRedacted)
		base.RedactionPolicyVersion = workItemDerefString(transition.RedactionPolicyVersion)

	case "work_item.external_link":
		link, err := decodeWorkItemExternalLink(workItemDecodeInput{FactID: fact.FactID, SchemaVersion: fact.SchemaVersion, Payload: fact.Payload})
		if err != nil {
			logWorkItemEvidenceDecodeDrop(err)
			return WorkItemEvidenceRow{}, false
		}
		base.Provider = link.Provider
		base.WorkItemKey = workItemDerefString(link.WorkItemKey)
		base.ProviderWorkItemID = workItemDerefString(link.ProviderWorkItemID)
		base.ProviderRemoteLinkID = workItemDerefString(link.ProviderRemoteLinkID)
		base.GlobalID = workItemDerefString(link.GlobalID)
		base.ApplicationName = workItemDerefString(link.ApplicationName)
		base.ApplicationType = workItemDerefString(link.ApplicationType)
		base.Relationship = workItemDerefString(link.Relationship)
		base.URLFingerprint = workItemDerefString(link.URLFingerprint)
		base.URLPresent = workItemDerefBool(link.URLPresent)
		base.URLRedacted = workItemDerefBool(link.URLRedacted)
		base.TitlePresent = workItemDerefBool(link.TitlePresent)
		base.SummaryPresent = workItemDerefBool(link.SummaryPresent)
		base.AnchorClass = workItemDerefString(link.AnchorClass)
		base.ProviderSupportState = workItemDerefString(link.ProviderSupportState)
		base.RedactionPolicyVersion = workItemDerefString(link.RedactionPolicyVersion)
		base.LinkedRepositoryID = workItemDerefString(link.LinkedRepositoryID)

	case "work_item.project_metadata":
		metadata, err := decodeWorkItemProjectMetadata(workItemDecodeInput{FactID: fact.FactID, SchemaVersion: fact.SchemaVersion, Payload: fact.Payload})
		if err != nil {
			logWorkItemEvidenceDecodeDrop(err)
			return WorkItemEvidenceRow{}, false
		}
		base.Provider = metadata.Provider
		base.ProjectID = workItemDerefString(metadata.ProjectID)
		base.ProjectKey = workItemDerefString(metadata.ProjectKey)
		base.RedactionPolicyVersion = workItemDerefString(metadata.RedactionPolicyVersion)

	case "work_item.issue_type_metadata":
		metadata, err := decodeWorkItemIssueTypeMetadata(workItemDecodeInput{FactID: fact.FactID, SchemaVersion: fact.SchemaVersion, Payload: fact.Payload})
		if err != nil {
			logWorkItemEvidenceDecodeDrop(err)
			return WorkItemEvidenceRow{}, false
		}
		base.Provider = metadata.Provider
		base.ProjectID = workItemDerefString(metadata.ProjectID)
		base.IssueTypeID = metadata.IssueTypeID
		base.RedactionPolicyVersion = workItemDerefString(metadata.RedactionPolicyVersion)

	case "work_item.status_metadata":
		metadata, err := decodeWorkItemStatusMetadata(workItemDecodeInput{FactID: fact.FactID, SchemaVersion: fact.SchemaVersion, Payload: fact.Payload})
		if err != nil {
			logWorkItemEvidenceDecodeDrop(err)
			return WorkItemEvidenceRow{}, false
		}
		base.Provider = metadata.Provider
		base.ProjectID = workItemDerefString(metadata.ProjectID)
		base.StatusID = metadata.StatusID
		base.StatusName = workItemDerefString(metadata.StatusName)
		base.RedactionPolicyVersion = workItemDerefString(metadata.RedactionPolicyVersion)

	case "work_item.workflow_metadata":
		metadata, err := decodeWorkItemWorkflowMetadata(workItemDecodeInput{FactID: fact.FactID, SchemaVersion: fact.SchemaVersion, Payload: fact.Payload})
		if err != nil {
			logWorkItemEvidenceDecodeDrop(err)
			return WorkItemEvidenceRow{}, false
		}
		base.Provider = metadata.Provider
		base.ProjectID = workItemDerefString(metadata.ProjectID)
		base.RedactionPolicyVersion = workItemDerefString(metadata.RedactionPolicyVersion)

	case "work_item.field_metadata":
		metadata, err := decodeWorkItemFieldMetadata(workItemDecodeInput{FactID: fact.FactID, SchemaVersion: fact.SchemaVersion, Payload: fact.Payload})
		if err != nil {
			logWorkItemEvidenceDecodeDrop(err)
			return WorkItemEvidenceRow{}, false
		}
		base.Provider = metadata.Provider
		base.RedactionPolicyVersion = workItemDerefString(metadata.RedactionPolicyVersion)

	case "work_item.metadata_warning":
		warning, err := decodeWorkItemMetadataWarning(workItemDecodeInput{FactID: fact.FactID, SchemaVersion: fact.SchemaVersion, Payload: fact.Payload})
		if err != nil {
			logWorkItemEvidenceDecodeDrop(err)
			return WorkItemEvidenceRow{}, false
		}
		base.Provider = warning.Provider
		base.RedactionPolicyVersion = workItemDerefString(warning.RedactionPolicyVersion)

	default:
		// workItemEvidenceFactKinds bounds the SQL read to the kinds this
		// switch handles; an unrecognized kind here would mean the SQL kind
		// list and this switch drifted apart. Drop rather than emit a
		// zero-identity row.
		return WorkItemEvidenceRow{}, false
	}

	base.EvidenceState = workItemEvidenceState(fact)
	return base, true
}

// logWorkItemEvidenceDecodeDrop emits an operator-diagnosable debug log for a
// work-item evidence fact dropped from a list response because its payload
// failed typed decode. This is a read-path best-effort drop, not a durable
// dead-letter queue entry (there is no queue on this path), so a debug-level
// structured log is the visibility contract: an operator can search fact_id,
// fact_kind, and classification to find exactly which malformed fact was
// excluded and why. EVERY decode drop is a *queryDecodeError, so fact_id and
// fact_kind are logged for all of them (a missing/null required field via
// input_invalid AND an unsupported schema major alike); missing_field is added
// only when the failure is attributable to one field.
func logWorkItemEvidenceDecodeDrop(err error) {
	var decodeErr *queryDecodeError
	if !errors.As(err, &decodeErr) {
		slog.Debug("work-item evidence fact dropped from list: decode error", slog.String("error", err.Error()))
		return
	}
	attrs := []any{
		slog.String("fact_id", decodeErr.FactID),
		slog.String("fact_kind", decodeErr.FactKind),
		slog.String("classification", decodeErr.Classification),
	}
	if decodeErr.Field != "" {
		attrs = append(attrs, slog.String("missing_field", decodeErr.Field))
	}
	slog.Debug("work-item evidence fact dropped from list", attrs...)
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
