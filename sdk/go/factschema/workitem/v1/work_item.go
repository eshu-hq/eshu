// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package v1

// WorkItemRecord is the schema-version-1 typed payload for the
// "work_item.record" fact kind: one provider-reported work item (a Jira
// issue).
//
// The required set matches the collector emitter
// (jira.NewWorkItemRecordEnvelope), which rejects a blank issue id or key and
// always stamps the provider token. Provider, ProviderWorkItemID, and
// WorkItemKey are the durable identity the query read model anchors on: a
// missing anchor would produce an empty-string work-item identity, the exact
// accuracy hole Contract System v1 exists to close. Every other field is
// optional: the emitter always stamps the key but the value may legitimately
// be an empty string (a redacted text field, an unset reference, or a blank
// timestamp), so a required non-pointer there would dead-letter valid,
// redacted-but-real facts.
type WorkItemRecord struct {
	// Provider is the work-item provider token (for example "jira_cloud").
	// Required — the emitter always stamps it.
	Provider string `json:"provider"`

	// ProviderWorkItemID is the provider-assigned issue id. Required — the
	// emitter rejects a blank issue id, and this is the durable identity the
	// work-item node is keyed on.
	ProviderWorkItemID string `json:"provider_work_item_id"`

	// WorkItemKey is the provider-facing issue key (for example "OPS-123").
	// Required — the emitter rejects a blank issue key.
	WorkItemKey string `json:"work_item_key"`

	// RedactionPolicyVersion is the redaction policy token stamped on every
	// work-item payload. Optional: boundary metadata, not graph identity.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`

	// Summary is always redacted to the empty string by the collector.
	// Optional and kept for round-trip parity with the emitted shape; see
	// SummaryPresent for the presence signal.
	Summary *string `json:"summary,omitempty"`

	// SummaryPresent reports whether the source issue carried a non-blank
	// summary before redaction. Optional.
	SummaryPresent *bool `json:"summary_present,omitempty"`

	// IssueTypeID is the provider issue-type id. Optional: may be blank for an
	// issue with no resolved issue type.
	IssueTypeID *string `json:"issue_type_id,omitempty"`

	// IssueTypeName is the provider issue-type display name. Optional.
	IssueTypeName *string `json:"issue_type_name,omitempty"`

	// StatusID is the provider status id. Optional.
	StatusID *string `json:"status_id,omitempty"`

	// StatusName is the provider status display name. Optional.
	StatusName *string `json:"status_name,omitempty"`

	// ProjectID is the provider project id. Optional.
	ProjectID *string `json:"project_id,omitempty"`

	// ProjectKey is the provider project key. Optional.
	ProjectKey *string `json:"project_key,omitempty"`

	// ProjectName is always redacted to the empty string by the collector.
	// Optional; see ProjectNamePresent for the presence signal.
	ProjectName *string `json:"project_name,omitempty"`

	// ProjectNamePresent reports whether the source project carried a
	// non-blank name before redaction. Optional.
	ProjectNamePresent *bool `json:"project_name_present,omitempty"`

	// AssigneeAccountID is always redacted to the empty string by the
	// collector. Optional; see AssigneePresent for the presence signal.
	AssigneeAccountID *string `json:"assignee_account_id,omitempty"`

	// AssigneeDisplayName is always redacted to the empty string by the
	// collector. Optional.
	AssigneeDisplayName *string `json:"assignee_display_name,omitempty"`

	// AssigneePresent reports whether the source issue carried an assignee
	// reference before redaction. Optional.
	AssigneePresent *bool `json:"assignee_present,omitempty"`

	// ReporterAccountID is always redacted to the empty string by the
	// collector. Optional.
	ReporterAccountID *string `json:"reporter_account_id,omitempty"`

	// ReporterDisplayName is always redacted to the empty string by the
	// collector. Optional.
	ReporterDisplayName *string `json:"reporter_display_name,omitempty"`

	// ReporterPresent reports whether the source issue carried a reporter
	// reference before redaction. Optional.
	ReporterPresent *bool `json:"reporter_present,omitempty"`

	// CreatedAt is the issue creation timestamp (RFC 3339). Optional.
	CreatedAt *string `json:"created_at,omitempty"`

	// UpdatedAt is the issue last-update timestamp (RFC 3339). Optional.
	UpdatedAt *string `json:"updated_at,omitempty"`

	// ResolvedAt is the issue resolution timestamp (RFC 3339). Optional.
	ResolvedAt *string `json:"resolved_at,omitempty"`

	// SelfURL is always redacted to the empty string by the collector.
	// Optional; see SelfURLFingerprint for the derived fingerprint.
	SelfURL *string `json:"self_url,omitempty"`

	// SelfURLFingerprint is a normalized sha256 fingerprint of the (redacted)
	// self URL. Optional: empty when the source self URL was blank.
	SelfURLFingerprint *string `json:"self_url_fingerprint,omitempty"`

	// SourceURL is always redacted to the empty string by the collector.
	// Optional; see SourceURLFingerprint for the derived fingerprint.
	SourceURL *string `json:"source_url,omitempty"`

	// SourceURLFingerprint is a normalized sha256 fingerprint of the
	// (redacted) source URL. Optional: empty when the source URL was blank.
	SourceURLFingerprint *string `json:"source_url_fingerprint,omitempty"`

	// CollectorInstanceID is the collector boundary token the emitter stamps
	// on every payload. Optional: boundary metadata, not graph identity.
	CollectorInstanceID *string `json:"collector_instance_id,omitempty"`
}

// WorkItemTransition is the schema-version-1 typed payload for the
// "work_item.transition" fact kind: one provider-reported changelog entry.
//
// The required set matches the collector emitter
// (jira.NewWorkItemTransitionEnvelope), which rejects a blank changelog id and
// always stamps the provider token. Provider and ProviderChangelogID are the
// durable identity. WorkItemKey/ProviderWorkItemID come from the source
// transition's IssueKey/IssueID, which the emitter does not itself validate as
// non-blank, so they stay optional rather than risk dead-lettering a
// genuinely emitted fact whose issue linkage was incomplete upstream.
type WorkItemTransition struct {
	// Provider is the work-item provider token. Required.
	Provider string `json:"provider"`

	// ProviderChangelogID is the provider-assigned changelog entry id.
	// Required — the emitter rejects a blank changelog id.
	ProviderChangelogID string `json:"provider_changelog_id"`

	// RedactionPolicyVersion is the redaction policy token. Optional.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`

	// ProviderWorkItemID is the issue id the transition belongs to. Optional:
	// the emitter does not guard this field as non-blank.
	ProviderWorkItemID *string `json:"provider_work_item_id,omitempty"`

	// WorkItemKey is the issue key the transition belongs to. Optional.
	WorkItemKey *string `json:"work_item_key,omitempty"`

	// Field is the provider field name that changed. Optional.
	Field *string `json:"field,omitempty"`

	// From is the redacted prior field value. Optional: empty when the value
	// was redacted or absent.
	From *string `json:"from,omitempty"`

	// To is the redacted new field value. Optional.
	To *string `json:"to,omitempty"`

	// ValueRedacted reports whether From/To were redacted before emission.
	// Optional.
	ValueRedacted *bool `json:"value_redacted,omitempty"`

	// AuthorAccountID is always redacted to the empty string by the
	// collector. Optional.
	AuthorAccountID *string `json:"author_account_id,omitempty"`

	// AuthorDisplayName is always redacted to the empty string by the
	// collector. Optional.
	AuthorDisplayName *string `json:"author_display_name,omitempty"`

	// AuthorPresent reports whether the source transition carried an author
	// reference before redaction. Optional.
	AuthorPresent *bool `json:"author_present,omitempty"`

	// AuthorRedacted reports whether the author reference was redacted.
	// Optional.
	AuthorRedacted *bool `json:"author_redacted,omitempty"`

	// CreatedAt is the transition timestamp (RFC 3339). Optional.
	CreatedAt *string `json:"created_at,omitempty"`
}

// WorkItemExternalLink is the schema-version-1 typed payload for the
// "work_item.external_link" fact kind: one provider-reported remote link
// attached to a work item.
//
// The required set matches the collector emitter
// (jira.NewWorkItemExternalLinkEnvelope), which always stamps the provider
// token. The emitter's identity guard accepts ANY of link.ID, link.GlobalID,
// or a URL-derived fingerprint as the source record id, so no single payload
// field among ProviderRemoteLinkID/GlobalID/URLFingerprint is unconditionally
// present — each stays optional; only Provider is guaranteed.
type WorkItemExternalLink struct {
	// Provider is the work-item provider token. Required.
	Provider string `json:"provider"`

	// RedactionPolicyVersion is the redaction policy token. Optional.
	RedactionPolicyVersion *string `json:"redaction_policy_version,omitempty"`

	// ProviderRemoteLinkID is the provider-assigned remote-link id. Optional:
	// the emitter accepts GlobalID or a URL fingerprint as an alternate
	// identity anchor, so this field alone may be blank.
	ProviderRemoteLinkID *string `json:"provider_remote_link_id,omitempty"`

	// ProviderWorkItemID is the issue id the link is attached to. Optional.
	ProviderWorkItemID *string `json:"provider_work_item_id,omitempty"`

	// WorkItemKey is the issue key the link is attached to. Optional.
	WorkItemKey *string `json:"work_item_key,omitempty"`

	// GlobalID is the provider global link id. Optional: an alternate
	// identity anchor to ProviderRemoteLinkID.
	GlobalID *string `json:"global_id,omitempty"`

	// ApplicationName is the linked external application's name. Optional.
	ApplicationName *string `json:"application_name,omitempty"`

	// ApplicationType is the linked external application's type token.
	// Optional.
	ApplicationType *string `json:"application_type,omitempty"`

	// Relationship is the provider-reported link relationship. Optional.
	Relationship *string `json:"relationship,omitempty"`

	// URL is always redacted to the empty string by the collector. Optional;
	// see URLFingerprint for the derived fingerprint.
	URL *string `json:"url,omitempty"`

	// URLPresent reports whether the source link carried a non-blank URL
	// before redaction. Optional.
	URLPresent *bool `json:"url_present,omitempty"`

	// URLFingerprint is a normalized sha256 fingerprint of the (redacted)
	// link URL. Optional: an alternate identity anchor to
	// ProviderRemoteLinkID/GlobalID.
	URLFingerprint *string `json:"url_fingerprint,omitempty"`

	// URLRedacted reports whether the link URL was redacted. Optional.
	URLRedacted *bool `json:"url_redacted,omitempty"`

	// Title is always redacted to the empty string by the collector.
	// Optional.
	Title *string `json:"title,omitempty"`

	// TitlePresent reports whether the source link carried a non-blank title
	// before redaction. Optional.
	TitlePresent *bool `json:"title_present,omitempty"`

	// Summary is always redacted to the empty string by the collector.
	// Optional.
	Summary *string `json:"summary,omitempty"`

	// SummaryPresent reports whether the source link carried a non-blank
	// summary before redaction. Optional.
	SummaryPresent *bool `json:"summary_present,omitempty"`

	// AnchorClass classifies the link's typed correlation shape (for example
	// "github_pull_request", "gitlab_merge_request", or "remote_link").
	// Optional.
	AnchorClass *string `json:"correlation_anchor_class,omitempty"`

	// ProviderSupportState reports whether Eshu promotes this link's provider
	// type to typed correlation. Optional.
	ProviderSupportState *string `json:"provider_support_state,omitempty"`

	// LinkedRepositoryID is the durable canonical repository id the collector
	// resolves from a confidently typed GitHub PR or GitLab MR link before
	// redaction. Optional: empty for non-link facts and for links that did
	// not canonicalize to a repository.
	LinkedRepositoryID *string `json:"linked_repository_id,omitempty"`
}
