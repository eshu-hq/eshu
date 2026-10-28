// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package intake

import (
	"fmt"
	"strings"
	"time"

	awsfreshness "github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
	gcpfreshness "github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/webhook"
)

// APIVersionV1 is the only intake wire version this server accepts.
const APIVersionV1 = "v1"

// wireFieldLimit bounds any single intake string field. Normalized provider
// values are far smaller (delivery IDs are a few dozen bytes); the cap only
// stops a hostile listener from pushing megabytes through one field past
// the body cap in many small keys.
const wireFieldLimit = 4096

// checkAPIVersion validates the wire version without touching storage.
// Absent api_version means the oldest v1 client; any other value fails
// closed.
func checkAPIVersion(version string) error {
	version = strings.TrimSpace(version)
	if version == "" || version == APIVersionV1 {
		return nil
	}
	return fmt.Errorf("%w: %q", errUnsupportedAPIVersion, version)
}

// RepositoryIntakeRequest is the v1 wire form of one normalized repository
// trigger (github, gitlab, or bitbucket) submitted by the extracted
// listener after provider signature verification.
type RepositoryIntakeRequest struct {
	APIVersion           string `json:"api_version"`
	Provider             string `json:"provider"`
	DeliveryID           string `json:"delivery_id"`
	EventKind            string `json:"event_kind"`
	Decision             string `json:"decision"`
	Reason               string `json:"reason"`
	RepositoryExternalID string `json:"repository_external_id"`
	RepositoryFullName   string `json:"repository_full_name"`
	DefaultBranch        string `json:"default_branch"`
	Ref                  string `json:"ref"`
	BeforeSHA            string `json:"before_sha"`
	TargetSHA            string `json:"target_sha"`
	Action               string `json:"action"`
	Sender               string `json:"sender"`
	PullRequestNumber    string `json:"pull_request_number"`
	PullRequestURL       string `json:"pull_request_url"`
	PullRequestTitle     string `json:"pull_request_title"`
}

// toTrigger converts and validates the wire form into the domain trigger
// the core store persists. A missing required field is a classified
// client error, never a silent zero value.
func (r RepositoryIntakeRequest) toTrigger() (webhook.Trigger, error) {
	if err := checkAPIVersion(r.APIVersion); err != nil {
		return webhook.Trigger{}, err
	}
	provider := webhook.Provider(strings.TrimSpace(r.Provider))
	switch provider {
	case webhook.ProviderGitHub, webhook.ProviderGitLab, webhook.ProviderBitbucket:
	default:
		return webhook.Trigger{}, fmt.Errorf("%w: %q", errUnsupportedProvider, r.Provider)
	}
	trigger := webhook.Trigger{
		Provider:             provider,
		EventKind:            webhook.EventKind(strings.TrimSpace(r.EventKind)),
		Decision:             webhook.Decision(strings.TrimSpace(r.Decision)),
		Reason:               webhook.DecisionReason(strings.TrimSpace(r.Reason)),
		DeliveryID:           strings.TrimSpace(r.DeliveryID),
		RepositoryExternalID: strings.TrimSpace(r.RepositoryExternalID),
		RepositoryFullName:   strings.TrimSpace(r.RepositoryFullName),
		DefaultBranch:        strings.TrimSpace(r.DefaultBranch),
		Ref:                  strings.TrimSpace(r.Ref),
		BeforeSHA:            strings.TrimSpace(r.BeforeSHA),
		TargetSHA:            strings.TrimSpace(r.TargetSHA),
		Action:               strings.TrimSpace(r.Action),
		Sender:               strings.TrimSpace(r.Sender),
		PullRequestNumber:    strings.TrimSpace(r.PullRequestNumber),
		PullRequestURL:       strings.TrimSpace(r.PullRequestURL),
		PullRequestTitle:     strings.TrimSpace(r.PullRequestTitle),
	}
	switch trigger.EventKind {
	case webhook.EventKindPush, webhook.EventKindPullRequestMerged, webhook.EventKindMergeRequestMerged:
	default:
		return webhook.Trigger{}, fmt.Errorf("%w: %q", errInvalidTrigger, "event_kind")
	}
	switch trigger.Decision {
	case webhook.DecisionAccepted, webhook.DecisionIgnored:
	default:
		return webhook.Trigger{}, fmt.Errorf("%w: %q", errInvalidTrigger, "decision")
	}
	// Required durable keys: the store derives delivery and refresh keys
	// from these, so an empty one must fail here, not as a zero key.
	for name, value := range map[string]string{
		"delivery_id":            trigger.DeliveryID,
		"repository_external_id": trigger.RepositoryExternalID,
		"repository_full_name":   trigger.RepositoryFullName,
		"default_branch":         trigger.DefaultBranch,
		"target_sha":             trigger.TargetSHA,
	} {
		if value == "" {
			return webhook.Trigger{}, fmt.Errorf("%w: %q", errInvalidTrigger, name)
		}
	}
	if err := checkWireLengths(map[string]string{
		"delivery_id":            trigger.DeliveryID,
		"repository_external_id": trigger.RepositoryExternalID,
		"repository_full_name":   trigger.RepositoryFullName,
		"default_branch":         trigger.DefaultBranch,
		"ref":                    trigger.Ref,
		"before_sha":             trigger.BeforeSHA,
		"target_sha":             trigger.TargetSHA,
		"action":                 trigger.Action,
		"sender":                 trigger.Sender,
		"pull_request_number":    trigger.PullRequestNumber,
		"pull_request_url":       trigger.PullRequestURL,
		"pull_request_title":     trigger.PullRequestTitle,
	}); err != nil {
		return webhook.Trigger{}, err
	}
	return trigger, nil
}

// IncidentIntakeRequest is the v1 wire form of one normalized incident
// freshness trigger (pagerduty or jira).
type IncidentIntakeRequest struct {
	APIVersion string    `json:"api_version"`
	Provider   string    `json:"provider"`
	EventKind  string    `json:"event_kind"`
	EventID    string    `json:"event_id"`
	ScopeID    string    `json:"scope_id"`
	ResourceID string    `json:"resource_id"`
	ObservedAt time.Time `json:"observed_at"`
}

// toTrigger converts and validates the wire form. ObservedAt may be zero:
// the store defaults it to the server-stamped received time.
func (r IncidentIntakeRequest) toTrigger() (webhook.IncidentFreshnessTrigger, error) {
	if err := checkAPIVersion(r.APIVersion); err != nil {
		return webhook.IncidentFreshnessTrigger{}, err
	}
	provider := webhook.Provider(strings.TrimSpace(r.Provider))
	switch provider {
	case webhook.ProviderPagerDuty, webhook.ProviderJira:
	default:
		return webhook.IncidentFreshnessTrigger{}, fmt.Errorf("%w: %q", errUnsupportedProvider, r.Provider)
	}
	trigger := webhook.IncidentFreshnessTrigger{
		Provider:   provider,
		EventKind:  strings.TrimSpace(r.EventKind),
		EventID:    strings.TrimSpace(r.EventID),
		ScopeID:    strings.TrimSpace(r.ScopeID),
		ResourceID: strings.TrimSpace(r.ResourceID),
		ObservedAt: r.ObservedAt.UTC(),
	}
	if trigger.EventKind == "" || trigger.EventID == "" || trigger.ScopeID == "" {
		return webhook.IncidentFreshnessTrigger{}, fmt.Errorf("%w: event_kind, event_id, and scope_id are required", errInvalidTrigger)
	}
	if err := checkWireLengths(map[string]string{
		"event_kind":  trigger.EventKind,
		"event_id":    trigger.EventID,
		"scope_id":    trigger.ScopeID,
		"resource_id": trigger.ResourceID,
	}); err != nil {
		return webhook.IncidentFreshnessTrigger{}, err
	}
	return trigger, nil
}

// AWSIntakeRequest is the v1 wire form of one normalized AWS freshness
// trigger.
type AWSIntakeRequest struct {
	APIVersion   string    `json:"api_version"`
	EventID      string    `json:"event_id"`
	Kind         string    `json:"kind"`
	AccountID    string    `json:"account_id"`
	Region       string    `json:"region"`
	ServiceKind  string    `json:"service_kind"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	ObservedAt   time.Time `json:"observed_at"`
}

// toTrigger converts the wire form; the domain Validate runs server-side
// before storage so an invalid tuple fails with 400, not 500.
func (r AWSIntakeRequest) toTrigger() (awsfreshness.Trigger, error) {
	if err := checkAPIVersion(r.APIVersion); err != nil {
		return awsfreshness.Trigger{}, err
	}
	trigger := awsfreshness.Trigger{
		EventID:      strings.TrimSpace(r.EventID),
		Kind:         awsfreshness.EventKind(strings.TrimSpace(r.Kind)),
		AccountID:    strings.TrimSpace(r.AccountID),
		Region:       strings.TrimSpace(r.Region),
		ServiceKind:  strings.TrimSpace(r.ServiceKind),
		ResourceType: strings.TrimSpace(r.ResourceType),
		ResourceID:   strings.TrimSpace(r.ResourceID),
		ObservedAt:   r.ObservedAt.UTC(),
	}
	if err := checkWireLengths(map[string]string{
		"event_id":      trigger.EventID,
		"kind":          string(trigger.Kind),
		"account_id":    trigger.AccountID,
		"region":        trigger.Region,
		"service_kind":  trigger.ServiceKind,
		"resource_type": trigger.ResourceType,
		"resource_id":   trigger.ResourceID,
	}); err != nil {
		return awsfreshness.Trigger{}, err
	}
	if err := trigger.Validate(); err != nil {
		return awsfreshness.Trigger{}, fmt.Errorf("%w: %v", errInvalidTrigger, err)
	}
	return trigger, nil
}

// GCPIntakeRequest is the v1 wire form of one normalized GCP freshness
// trigger.
type GCPIntakeRequest struct {
	APIVersion      string    `json:"api_version"`
	EventID         string    `json:"event_id"`
	Kind            string    `json:"kind"`
	ParentScopeKind string    `json:"parent_scope_kind"`
	ParentScopeID   string    `json:"parent_scope_id"`
	AssetType       string    `json:"asset_type"`
	Location        string    `json:"location"`
	ObservedAt      time.Time `json:"observed_at"`
}

// toTrigger converts the wire form; the domain Validate runs server-side
// before storage so an invalid tuple fails with 400, not 500.
func (r GCPIntakeRequest) toTrigger() (gcpfreshness.Trigger, error) {
	if err := checkAPIVersion(r.APIVersion); err != nil {
		return gcpfreshness.Trigger{}, err
	}
	trigger := gcpfreshness.Trigger{
		EventID:         strings.TrimSpace(r.EventID),
		Kind:            gcpfreshness.EventKind(strings.TrimSpace(r.Kind)),
		ParentScopeKind: gcpcloud.ParentScopeKind(strings.TrimSpace(r.ParentScopeKind)),
		ParentScopeID:   strings.TrimSpace(r.ParentScopeID),
		AssetType:       strings.TrimSpace(r.AssetType),
		Location:        strings.TrimSpace(r.Location),
		ObservedAt:      r.ObservedAt.UTC(),
	}
	if err := checkWireLengths(map[string]string{
		"event_id":          trigger.EventID,
		"kind":              string(trigger.Kind),
		"parent_scope_kind": string(trigger.ParentScopeKind),
		"parent_scope_id":   trigger.ParentScopeID,
		"asset_type":        trigger.AssetType,
		"location":          trigger.Location,
	}); err != nil {
		return gcpfreshness.Trigger{}, err
	}
	if err := trigger.Validate(); err != nil {
		return gcpfreshness.Trigger{}, fmt.Errorf("%w: %v", errInvalidTrigger, err)
	}
	return trigger, nil
}

// ActionsIntakeRequest is the v1 wire form of one verified GitHub Actions
// wake-up identity.
type ActionsIntakeRequest struct {
	APIVersion   string `json:"api_version"`
	Provider     string `json:"provider"`
	DeliveryID   string `json:"delivery_id"`
	EventKind    string `json:"event_kind"`
	Action       string `json:"action"`
	RepositoryID string `json:"repository_id"`
	Repository   string `json:"repository"`
	RunID        string `json:"run_id"`
	JobID        string `json:"job_id"`
}

// toTrigger converts and validates the wire form. RepositoryID is the
// verified numeric repository identity the listener normalizer extracted
// from the signed payload; the intake API only accepts it from the
// authenticated listener, never from provider-facing input.
func (r ActionsIntakeRequest) toTrigger() (webhook.ActionsTrigger, error) {
	if err := checkAPIVersion(r.APIVersion); err != nil {
		return webhook.ActionsTrigger{}, err
	}
	if provider := webhook.Provider(strings.TrimSpace(r.Provider)); provider != webhook.ProviderGitHub {
		return webhook.ActionsTrigger{}, fmt.Errorf("%w: %q", errUnsupportedProvider, r.Provider)
	}
	eventKind := strings.TrimSpace(r.EventKind)
	if eventKind != "workflow_run" && eventKind != "workflow_job" {
		return webhook.ActionsTrigger{}, fmt.Errorf("%w: %q", errInvalidTrigger, "event_kind")
	}
	repository := strings.ToLower(strings.Trim(strings.TrimSpace(r.Repository), "/"))
	parts := strings.Split(repository, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" || len(repository) > 255 {
		return webhook.ActionsTrigger{}, fmt.Errorf("%w: %q", errInvalidTrigger, "repository")
	}
	deliveryID := strings.TrimSpace(r.DeliveryID)
	if deliveryID == "" || len(deliveryID) > 256 {
		return webhook.ActionsTrigger{}, fmt.Errorf("%w: %q", errInvalidTrigger, "delivery_id")
	}
	action := strings.TrimSpace(r.Action)
	if action == "" || len(action) > 64 {
		return webhook.ActionsTrigger{}, fmt.Errorf("%w: %q", errInvalidTrigger, "action")
	}
	repositoryID := strings.TrimSpace(r.RepositoryID)
	if repositoryID == "" || len(repositoryID) > 32 || !isDigits(repositoryID) {
		return webhook.ActionsTrigger{}, fmt.Errorf("%w: %q", errInvalidTrigger, "repository_id")
	}
	trigger := webhook.ActionsTrigger{
		DeliveryID:   deliveryID,
		EventKind:    eventKind,
		Action:       action,
		RepositoryID: repositoryID,
		Repository:   repository,
		RunID:        strings.TrimSpace(r.RunID),
		JobID:        strings.TrimSpace(r.JobID),
	}
	if trigger.RunID == "" {
		return webhook.ActionsTrigger{}, fmt.Errorf("%w: %q", errInvalidTrigger, "run_id")
	}
	if eventKind == "workflow_job" && trigger.JobID == "" {
		return webhook.ActionsTrigger{}, fmt.Errorf("%w: %q", errInvalidTrigger, "job_id")
	}
	if err := checkWireLengths(map[string]string{
		"delivery_id":   trigger.DeliveryID,
		"event_kind":    trigger.EventKind,
		"action":        trigger.Action,
		"repository_id": trigger.RepositoryID,
		"repository":    trigger.Repository,
		"run_id":        trigger.RunID,
		"job_id":        trigger.JobID,
	}); err != nil {
		return webhook.ActionsTrigger{}, err
	}
	return trigger, nil
}

// isDigits reports whether every byte is an ASCII digit.
func isDigits(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return true
}

// checkWireLengths bounds every intake string field so one hostile field
// cannot push megabytes past the body cap in many small keys.
func checkWireLengths(fields map[string]string) error {
	for name, value := range fields {
		if len(value) > wireFieldLimit {
			return fmt.Errorf("%w: %q exceeds %d bytes", errInvalidTrigger, name, wireFieldLimit)
		}
	}
	return nil
}
