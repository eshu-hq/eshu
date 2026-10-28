// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	awsfreshness "github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	gcpfreshness "github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"github.com/eshu-hq/eshu/go/internal/webhook/intake"
)

// intakeRequestTimeout bounds one core intake submission. A slow core must
// surface as a 500 to the provider (which redelivers), never as an
// unbounded listener handler block.
const intakeRequestTimeout = 10 * time.Second

// intakeClient posts normalized triggers to core's authenticated versioned
// intake API (#9377). It performs zero SQL: on the intake path the listener
// is a verifier/normalizer only, and core retains durable trigger storage
// and lifecycle. One submitter type per trigger family below embeds this
// client so each satisfies exactly one of the listener's store interfaces.
type intakeClient struct {
	baseURL string
	token   string
	client  *http.Client
}

func newIntakeClient(baseURL, token string) (*intakeClient, error) {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil, errors.New("webhook intake URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return nil, fmt.Errorf("webhook intake URL must be an http(s) base URL: %q", baseURL)
	}
	if strings.TrimSpace(token) == "" {
		return nil, errors.New("webhook intake token is required")
	}
	return &intakeClient{
		baseURL: baseURL,
		token:   token,
		client:  &http.Client{Timeout: intakeRequestTimeout},
	}, nil
}

// newIntakeSubmitter builds the repository-route submitter for the core
// intake API base URL (scheme + host, no route suffix). An empty URL or
// token fails closed: the listener must never silently fall back to direct
// table writes.
func newIntakeSubmitter(baseURL, token string) (*intakeRepositorySubmitter, error) {
	client, err := newIntakeClient(baseURL, token)
	if err != nil {
		return nil, err
	}
	return &intakeRepositorySubmitter{client: client}, nil
}

// intakeMode reports whether the listener submits through the core intake
// API instead of writing trigger tables directly.
func intakeMode(config webhookListenerConfig) bool {
	return strings.TrimSpace(config.IntakeURL) != ""
}

// intakeReceipt is the durable receipt the core intake API returns for one
// accepted submission.
type intakeReceipt struct {
	APIVersion     string `json:"api_version"`
	TriggerID      string `json:"trigger_id"`
	Status         string `json:"status"`
	Decision       string `json:"decision"`
	Reason         string `json:"reason"`
	Kind           string `json:"kind"`
	Duplicate      bool   `json:"duplicate"`
	DuplicateCount int    `json:"duplicate_count"`
	Code           string `json:"code"`
}

// post submits one wire request and decodes the receipt. Non-2xx responses
// map to errors the handlers already translate: 409 with the
// delivery-conflict code becomes webhook.ErrActionsDeliveryConflict, and
// everything else is a submission error (the handler answers 500/503 so
// the provider redelivers).
func (s *intakeClient) post(ctx context.Context, route string, request any) (intakeReceipt, error) {
	var receipt intakeReceipt
	body, err := json.Marshal(request)
	if err != nil {
		return receipt, fmt.Errorf("encode intake submission: %w", err)
	}
	// #nosec G704 -- baseURL is the operator-configured core intake endpoint
	// (ESHU_WEBHOOK_INTAKE_URL, scheme and host validated in
	// newIntakeClient) and route is a per-method constant; no
	// provider-derived value can change the target.
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, s.baseURL+route, bytes.NewReader(body))
	if err != nil {
		return receipt, fmt.Errorf("build intake submission: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Authorization", "Bearer "+s.token)
	// #nosec G704 -- same operator-configured intake endpoint as above.
	response, err := s.client.Do(httpRequest)
	if err != nil {
		return receipt, fmt.Errorf("submit intake %s: %w", route, err)
	}
	defer func() { _ = response.Body.Close() }()
	responseBody, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return receipt, fmt.Errorf("read intake %s response: %w", route, err)
	}
	if response.StatusCode == http.StatusConflict {
		var conflict intakeReceipt
		if err := json.Unmarshal(responseBody, &conflict); err == nil && conflict.Code == "delivery_conflict" {
			return receipt, webhook.ErrActionsDeliveryConflict
		}
		return receipt, fmt.Errorf("submit intake %s: unexpected status %d", route, response.StatusCode)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return receipt, fmt.Errorf("submit intake %s: unexpected status %d", route, response.StatusCode)
	}
	if err := json.Unmarshal(responseBody, &receipt); err != nil {
		return receipt, fmt.Errorf("decode intake %s receipt: %w", route, err)
	}
	if receipt.TriggerID == "" && route != "/api/v1/webhook/intake/actions" {
		return receipt, fmt.Errorf("decode intake %s receipt: missing trigger_id", route)
	}
	return receipt, nil
}

// duplicateCount normalizes the receipt's duplicate signal: a duplicate
// submission reports at least 1 so handler duplicate telemetry stays exact.
func duplicateCount(receipt intakeReceipt) int {
	if !receipt.Duplicate {
		return 0
	}
	return max(receipt.DuplicateCount, 1)
}

// intakeRepositorySubmitter submits normalized repository triggers through
// the core intake API. It satisfies the listener's triggerStore interface.
type intakeRepositorySubmitter struct {
	client *intakeClient
}

// StoreTrigger submits one normalized repository trigger.
func (s *intakeRepositorySubmitter) StoreTrigger(ctx context.Context, trigger webhook.Trigger, _ time.Time) (webhook.StoredTrigger, error) {
	receipt, err := s.client.post(ctx, "/api/v1/webhook/intake/repository", intake.RepositoryIntakeRequest{
		APIVersion:           intake.APIVersionV1,
		Provider:             string(trigger.Provider),
		DeliveryID:           trigger.DeliveryID,
		EventKind:            string(trigger.EventKind),
		Decision:             string(trigger.Decision),
		Reason:               string(trigger.Reason),
		RepositoryExternalID: trigger.RepositoryExternalID,
		RepositoryFullName:   trigger.RepositoryFullName,
		DefaultBranch:        trigger.DefaultBranch,
		Ref:                  trigger.Ref,
		BeforeSHA:            trigger.BeforeSHA,
		TargetSHA:            trigger.TargetSHA,
		Action:               trigger.Action,
		Sender:               trigger.Sender,
		PullRequestNumber:    trigger.PullRequestNumber,
		PullRequestURL:       trigger.PullRequestURL,
		PullRequestTitle:     trigger.PullRequestTitle,
	})
	if err != nil {
		return webhook.StoredTrigger{}, err
	}
	return webhook.StoredTrigger{
		Trigger:        trigger,
		TriggerID:      receipt.TriggerID,
		Status:         webhook.TriggerStatus(receipt.Status),
		DuplicateCount: duplicateCount(receipt),
	}, nil
}

// intakeActionsSubmitter submits verified Actions wake-up identities through
// the core intake API. It satisfies the listener's actionsTriggerStore
// interface.
type intakeActionsSubmitter struct {
	client *intakeClient
}

// Store submits one verified Actions wake-up identity.
func (s *intakeActionsSubmitter) Store(ctx context.Context, trigger webhook.ActionsTrigger, _ time.Time) (webhook.StoredActionsTrigger, error) {
	receipt, err := s.client.post(ctx, "/api/v1/webhook/intake/actions", intake.ActionsIntakeRequest{
		APIVersion:   intake.APIVersionV1,
		Provider:     string(webhook.ProviderGitHub),
		DeliveryID:   trigger.DeliveryID,
		EventKind:    trigger.EventKind,
		Action:       trigger.Action,
		RepositoryID: trigger.RepositoryID,
		Repository:   trigger.Repository,
		RunID:        trigger.RunID,
		JobID:        trigger.JobID,
	})
	if err != nil {
		return webhook.StoredActionsTrigger{}, err
	}
	return webhook.StoredActionsTrigger{
		ActionsTrigger: trigger,
		Status:         webhook.TriggerStatus(receipt.Status),
	}, nil
}

// intakeIncidentSubmitter submits normalized incident freshness triggers
// through the core intake API. It satisfies the listener's
// incidentFreshnessStore interface.
type intakeIncidentSubmitter struct {
	client *intakeClient
}

// StoreIncidentFreshnessTrigger submits one normalized incident freshness
// trigger.
func (s *intakeIncidentSubmitter) StoreIncidentFreshnessTrigger(ctx context.Context, trigger webhook.IncidentFreshnessTrigger, _ time.Time) (webhook.StoredIncidentFreshnessTrigger, error) {
	receipt, err := s.client.post(ctx, "/api/v1/webhook/intake/incident", intake.IncidentIntakeRequest{
		APIVersion: intake.APIVersionV1,
		Provider:   string(trigger.Provider),
		EventKind:  trigger.EventKind,
		EventID:    trigger.EventID,
		ScopeID:    trigger.ScopeID,
		ResourceID: trigger.ResourceID,
		ObservedAt: trigger.ObservedAt,
	})
	if err != nil {
		return webhook.StoredIncidentFreshnessTrigger{}, err
	}
	return webhook.StoredIncidentFreshnessTrigger{
		IncidentFreshnessTrigger: trigger,
		TriggerID:                receipt.TriggerID,
		Status:                   webhook.TriggerStatus(receipt.Status),
		DuplicateCount:           duplicateCount(receipt),
	}, nil
}

// intakeAWSFreshnessSubmitter submits normalized AWS freshness triggers
// through the core intake API. It satisfies the listener's
// awsFreshnessStore interface.
type intakeAWSFreshnessSubmitter struct {
	client *intakeClient
}

// StoreTrigger submits one normalized AWS freshness trigger.
func (s *intakeAWSFreshnessSubmitter) StoreTrigger(ctx context.Context, trigger awsfreshness.Trigger, _ time.Time) (awsfreshness.StoredTrigger, error) {
	receipt, err := s.client.post(ctx, "/api/v1/webhook/intake/aws", intake.AWSIntakeRequest{
		APIVersion:   intake.APIVersionV1,
		EventID:      trigger.EventID,
		Kind:         string(trigger.Kind),
		AccountID:    trigger.AccountID,
		Region:       trigger.Region,
		ServiceKind:  trigger.ServiceKind,
		ResourceType: trigger.ResourceType,
		ResourceID:   trigger.ResourceID,
		ObservedAt:   trigger.ObservedAt,
	})
	if err != nil {
		return awsfreshness.StoredTrigger{}, err
	}
	return awsfreshness.StoredTrigger{
		Trigger:        trigger,
		TriggerID:      receipt.TriggerID,
		Status:         awsfreshness.TriggerStatus(receipt.Status),
		DuplicateCount: duplicateCount(receipt),
	}, nil
}

// intakeGCPFreshnessSubmitter submits normalized GCP freshness triggers
// through the core intake API. It satisfies the listener's
// gcpFreshnessStore interface.
type intakeGCPFreshnessSubmitter struct {
	client *intakeClient
}

// StoreTrigger submits one normalized GCP freshness trigger.
func (s *intakeGCPFreshnessSubmitter) StoreTrigger(ctx context.Context, trigger gcpfreshness.Trigger, _ time.Time) (gcpfreshness.StoredTrigger, error) {
	receipt, err := s.client.post(ctx, "/api/v1/webhook/intake/gcp", intake.GCPIntakeRequest{
		APIVersion:      intake.APIVersionV1,
		EventID:         trigger.EventID,
		Kind:            string(trigger.Kind),
		ParentScopeKind: string(trigger.ParentScopeKind),
		ParentScopeID:   trigger.ParentScopeID,
		AssetType:       trigger.AssetType,
		Location:        trigger.Location,
		ObservedAt:      trigger.ObservedAt,
	})
	if err != nil {
		return gcpfreshness.StoredTrigger{}, err
	}
	return gcpfreshness.StoredTrigger{
		Trigger:        trigger,
		TriggerID:      receipt.TriggerID,
		Status:         gcpfreshness.TriggerStatus(receipt.Status),
		DuplicateCount: duplicateCount(receipt),
	}, nil
}

// intakeOIDCValidatorForConfig mirrors run()'s direct-mode OIDC wiring:
// the validator is only installed when the audience + allowlist pair is
// configured, so intake mode keeps the same fail-closed GCP auth paths.
func intakeOIDCValidatorForConfig(cfg webhookListenerConfig) gcpPushOIDCValidator {
	if cfg.GCPFreshnessOIDCAudience != "" && cfg.GCPFreshnessOIDCAllowedSA != "" {
		return googleOIDCValidator{}
	}
	return nil
}

// intakeStatusReader is the listener's status surface in intake mode: there
// is no local Postgres, so the operator snapshot carries only the intake
// uplink state and readiness probes core's /healthz. A listener that
// cannot reach core is not ready: it could accept provider deliveries it
// cannot hand off, so traffic must stay on a replica that can.
type intakeStatusReader struct {
	baseURL string
	client  *http.Client
	clock   func() time.Time
}

// CheckStatusReadiness probes the core intake /healthz.
func (r *intakeStatusReader) CheckStatusReadiness(ctx context.Context) error {
	if r == nil || strings.TrimSpace(r.baseURL) == "" {
		return errors.New("webhook intake URL is required")
	}
	client := r.client
	if client == nil {
		client = &http.Client{Timeout: intakeRequestTimeout}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, r.baseURL+"/healthz", nil)
	if err != nil {
		return fmt.Errorf("build intake health probe: %w", err)
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("probe webhook intake health: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("webhook intake health returned status %d", response.StatusCode)
	}
	return nil
}

// ReadStatusSnapshot returns the intake-mode operator snapshot: the uplink
// state only, since durable trigger truth lives in core.
func (r *intakeStatusReader) ReadStatusSnapshot(_ context.Context, _ time.Time) (statuspkg.RawSnapshot, error) {
	return statuspkg.RawSnapshot{AsOf: r.now()}, nil
}

// ReadStatusSnapshotFiltered returns the same uplink-only snapshot: intake
// mode has no local sections to filter.
func (r *intakeStatusReader) ReadStatusSnapshotFiltered(_ context.Context, _ time.Time, _ statuspkg.SnapshotSelection) (statuspkg.RawSnapshot, error) {
	return statuspkg.RawSnapshot{AsOf: r.now()}, nil
}

func (r *intakeStatusReader) now() time.Time {
	if r != nil && r.clock != nil {
		return r.clock()
	}
	return time.Now().UTC()
}
