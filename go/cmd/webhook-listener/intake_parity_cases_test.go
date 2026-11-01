// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// This file is the #9377 seven-provider signature/dedup/ordering parity
// harness through the extracted path: provider-signed HTTP → listener
// (verify + normalize, submitter mode, zero SQL) → core intake API
// (auth + version + bounds) → coalescing stores. It covers valid and
// invalid signatures, duplicates, bounded bodies, and partial failures for
// all seven mounted providers, plus FIFO claim ordering and redelivery
// convergence.

type parityWorld struct {
	listener *http.ServeMux
	repo     *parityRepositoryStore
	incident *parityIncidentStore
	aws      *parityAWSStore
	gcp      *parityGCPStore
	actions  *parityActionsStore
	intake   *httptest.Server
	client   *intakeClient
	// serverMux holds the intake API handler; setServerToken swaps it.
	serverMux atomic.Pointer[http.ServeMux]
	// intakeGate, when set, may intercept an intake request before the API
	// sees it (return true when handled); box 7 proofs use it to stall core.
	intakeGate atomic.Pointer[func(http.ResponseWriter, *http.Request) bool]
}

func newParityWorld(t *testing.T, listenerToken string) *parityWorld {
	t.Helper()
	world := &parityWorld{
		repo:     &parityRepositoryStore{},
		incident: &parityIncidentStore{},
		aws:      &parityAWSStore{},
		gcp:      &parityGCPStore{},
		actions:  &parityActionsStore{},
	}
	fixedNow := parityFixedNow
	world.setServerToken("listener-token")
	world.intake = httptest.NewServer(http.HandlerFunc(world.serveIntake))
	t.Cleanup(world.intake.Close)

	client, err := newIntakeClient(world.intake.URL, listenerToken)
	if err != nil {
		t.Fatalf("newIntakeClient() error = %v, want nil", err)
	}
	world.client = client
	cfg := webhookListenerConfig{
		GitHubSecret:        "secret",
		GitHubPath:          "/webhooks/github",
		GitLabToken:         "secret",
		GitLabPath:          "/webhooks/gitlab",
		BitbucketSecret:     "secret",
		BitbucketPath:       "/webhooks/bitbucket",
		PagerDutySecret:     "secret",
		PagerDutyPath:       "/webhooks/pagerduty",
		PagerDutyScopeID:    "pagerduty:account:example",
		JiraSecret:          "secret",
		JiraPath:            "/webhooks/jira",
		JiraScopeID:         "jira:site:example",
		AWSFreshnessToken:   "secret",
		AWSFreshnessPath:    "/webhooks/aws/eventbridge",
		GCPFreshnessToken:   "secret",
		GCPFreshnessPath:    "/webhook/gcp-freshness",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
		IntakeURL:           world.intake.URL,
		IntakeToken:         listenerToken,
	}
	mux, err := newWebhookMux(webhookHandler{
		Config:                 cfg,
		Store:                  &intakeRepositorySubmitter{client: client},
		ActionsStore:           &intakeActionsSubmitter{client: client},
		IncidentFreshnessStore: &intakeIncidentSubmitter{client: client},
		AWSFreshnessStore:      &intakeAWSFreshnessSubmitter{client: client},
		GCPFreshnessStore:      &intakeGCPFreshnessSubmitter{client: client},
		Clock:                  func() time.Time { return fixedNow },
	})
	if err != nil {
		t.Fatalf("newWebhookMux() error = %v, want nil", err)
	}
	world.listener = mux
	return world
}

func (w *parityWorld) post(path string, payload []byte, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(payload))
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	w.listener.ServeHTTP(rec, req)
	return rec
}

type parityProviderCase struct {
	name    string
	path    string
	payload []byte
	headers map[string]string
}

func parityValidProviders() []parityProviderCase {
	githubPayload := []byte(`{
		"ref":"refs/heads/main",
		"before":"1111111111111111111111111111111111111111",
		"after":"2222222222222222222222222222222222222222",
		"repository":{"id":42,"full_name":"eshu-hq/eshu","default_branch":"main"}
	}`)
	gitlabPayload := []byte(`{
		"object_kind":"push",
		"ref":"refs/heads/main",
		"after":"2222222222222222222222222222222222222222",
		"project":{"id":77,"path_with_namespace":"eshu-hq/eshu","default_branch":"main"}
	}`)
	bitbucketPayload := []byte(`{
		"repository":{"uuid":"{repo-uuid}","full_name":"eshu-hq/eshu","mainbranch":{"name":"main"}},
		"push":{"changes":[{"new":{"type":"branch","name":"main","target":{"hash":"2222222222222222222222222222222222222222"}}}]}
	}`)
	pagerDutyPayload := []byte(`{
		"event": {
			"id": "evt-123",
			"event_type": "incident.triggered",
			"occurred_at": "2026-05-31T18:00:00Z",
			"data": {"id": "PINC123", "type": "incident"}
		}
	}`)
	jiraPayload := []byte(`{
		"webhookEvent": "jira:issue_updated",
		"timestamp": 1780250400000,
		"issue": {
			"id": "10001",
			"key": "OPS-123",
			"self": "https://example.atlassian.net/rest/api/3/issue/10001"
		}
	}`)
	awsPayload := []byte(`{
		"id": "config-event-1",
		"detail-type": "Config Configuration Item Change",
		"source": "aws.config",
		"account": "123456789012",
		"region": "us-east-1",
		"time": "2026-05-15T10:11:12Z",
		"detail": {
			"configurationItem": {
				"awsAccountId": "123456789012",
				"awsRegion": "us-east-1",
				"resourceType": "AWS::Lambda::Function",
				"resourceId": "orders-api",
				"configurationItemCaptureTime": "2026-05-15T10:10:59Z"
			}
		}
	}`)
	return []parityProviderCase{
		{
			name:    "github",
			path:    "/webhooks/github",
			payload: githubPayload,
			headers: map[string]string{
				"X-GitHub-Event":      "push",
				"X-GitHub-Delivery":   "parity-github-1",
				"X-Hub-Signature-256": githubSignature("secret", githubPayload),
			},
		},
		{
			name:    "gitlab",
			path:    "/webhooks/gitlab",
			payload: gitlabPayload,
			headers: map[string]string{
				"X-Gitlab-Event":  "Push Hook",
				"X-Gitlab-Token":  "secret",
				"Idempotency-Key": "parity-gitlab-1",
			},
		},
		{
			name:    "bitbucket",
			path:    "/webhooks/bitbucket",
			payload: bitbucketPayload,
			headers: map[string]string{
				"X-Event-Key":     "repo:push",
				"X-Request-UUID":  "parity-bitbucket-1",
				"X-Hub-Signature": githubSignature("secret", bitbucketPayload),
			},
		},
		{
			name:    "pagerduty",
			path:    "/webhooks/pagerduty",
			payload: pagerDutyPayload,
			headers: map[string]string{
				"X-PagerDuty-Signature": pagerDutyWebhookSignature("secret", pagerDutyPayload),
				"X-Webhook-Id":          "parity-pagerduty-1",
			},
		},
		{
			name:    "jira",
			path:    "/webhooks/jira",
			payload: jiraPayload,
			headers: map[string]string{
				"X-Atlassian-Webhook-Identifier": "parity-jira-1",
				"X-Hub-Signature":                githubSignature("secret", jiraPayload),
			},
		},
		{
			name:    "aws",
			path:    "/webhooks/aws/eventbridge",
			payload: awsPayload,
			headers: map[string]string{
				"Authorization": "Bearer secret",
			},
		},
		{
			name:    "gcp",
			path:    "/webhook/gcp-freshness",
			payload: gcpPushEnvelopeForParity(),
			headers: map[string]string{
				"X-Eshu-GCP-Freshness-Token": "secret",
			},
		},
	}
}

func gcpPushEnvelopeForParity() []byte {
	envelope := map[string]any{
		"message": map[string]any{
			"data":      base64.StdEncoding.EncodeToString([]byte(gcpFreshnessValidTemporalAsset)),
			"messageId": "parity-gcp-1",
		},
		"subscription": "projects/demo/subscriptions/gcp-freshness",
	}
	encoded, err := json.Marshal(envelope)
	if err != nil {
		panic(err)
	}
	return encoded
}

// TestIntakeParitySevenProvidersAcceptValidDeliveries proves all seven
// mounted providers flow through the extracted path: valid signatures end
// at 202 with exactly one durable row per provider family.
func TestIntakeParitySevenProvidersAcceptValidDeliveries(t *testing.T) {
	t.Parallel()

	world := newParityWorld(t, "listener-token")
	for _, provider := range parityValidProviders() {
		rec := world.post(provider.path, provider.payload, provider.headers)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("%s: status = %d, want 202 body=%q", provider.name, rec.Code, rec.Body.String())
		}
	}
	if got := world.repo.rowCount(); got != 3 {
		t.Fatalf("repository rows = %d, want 3 (github, gitlab, bitbucket)", got)
	}
	if got := world.incident.rowCount(); got != 2 {
		t.Fatalf("incident rows = %d, want 2 (pagerduty, jira)", got)
	}
	if got := world.aws.rowCount(); got != 1 {
		t.Fatalf("aws rows = %d, want 1", got)
	}
	if got := world.gcp.rowCount(); got != 1 {
		t.Fatalf("gcp rows = %d, want 1", got)
	}
}

// TestIntakeParityRejectsInvalidSignatures proves all seven providers fail
// closed on bad auth through the extracted path, storing nothing.
func TestIntakeParityRejectsInvalidSignatures(t *testing.T) {
	t.Parallel()

	world := newParityWorld(t, "listener-token")
	payload := []byte(`{"ref":"refs/heads/main"}`)
	cases := []parityProviderCase{
		{name: "github", path: "/webhooks/github", payload: payload, headers: map[string]string{
			"X-GitHub-Event": "push", "X-GitHub-Delivery": "bad-1", "X-Hub-Signature-256": "sha256=deadbeef",
		}},
		{name: "gitlab", path: "/webhooks/gitlab", payload: payload, headers: map[string]string{
			"X-Gitlab-Event": "Push Hook", "X-Gitlab-Token": "wrong", "Idempotency-Key": "bad-2",
		}},
		{name: "bitbucket", path: "/webhooks/bitbucket", payload: payload, headers: map[string]string{
			"X-Event-Key": "repo:push", "X-Request-UUID": "bad-3", "X-Hub-Signature": "sha256=deadbeef",
		}},
		{name: "pagerduty", path: "/webhooks/pagerduty", payload: payload, headers: map[string]string{
			"X-PagerDuty-Signature": "v1=deadbeef", "X-Webhook-Id": "bad-4",
		}},
		{name: "jira", path: "/webhooks/jira", payload: payload, headers: map[string]string{
			"X-Atlassian-Webhook-Identifier": "bad-5", "X-Hub-Signature": "sha256=deadbeef",
		}},
		{name: "aws", path: "/webhooks/aws/eventbridge", payload: payload, headers: map[string]string{
			"Authorization": "Bearer wrong",
		}},
		{name: "gcp", path: "/webhook/gcp-freshness", payload: payload, headers: map[string]string{
			"X-Eshu-GCP-Freshness-Token": "wrong",
		}},
	}
	for _, provider := range cases {
		rec := world.post(provider.path, provider.payload, provider.headers)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401 body=%q", provider.name, rec.Code, rec.Body.String())
		}
	}
	if got := world.repo.rowCount() + world.incident.rowCount() + world.aws.rowCount() + world.gcp.rowCount(); got != 0 {
		t.Fatalf("stored rows after invalid signatures = %d, want 0", got)
	}
}

// TestIntakeParityDuplicateDeliveriesCoalesce proves provider redelivery
// converges: the same delivery twice stores one row with duplicate_count 1,
// still answering 202 so the provider backs off.
func TestIntakeParityDuplicateDeliveriesCoalesce(t *testing.T) {
	t.Parallel()

	world := newParityWorld(t, "listener-token")
	providers := parityValidProviders()
	github := providers[0]
	for i := 0; i < 2; i++ {
		rec := world.post(github.path, github.payload, github.headers)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("delivery %d: status = %d, want 202", i, rec.Code)
		}
	}
	if got := world.repo.rowCount(); got != 1 {
		t.Fatalf("repository rows after redelivery = %d, want 1", got)
	}
	const key = "github|42|main|2222222222222222222222222222222222222222"
	if got := world.repo.duplicateCount(key); got != 1 {
		t.Fatalf("duplicate_count = %d, want 1", got)
	}
}

// TestIntakeParityBoundedBody proves an oversized provider body fails with
// 413 at the listener edge, storing nothing on either side.
func TestIntakeParityBoundedBody(t *testing.T) {
	t.Parallel()

	world := newParityWorld(t, "listener-token")
	payload := []byte(`{"ref":"refs/heads/main","padding":"` + string(make([]byte, 2<<20)) + `"}`)
	rec := world.post("/webhooks/github", payload, map[string]string{
		"X-GitHub-Event":      "push",
		"X-GitHub-Delivery":   "big-1",
		"X-Hub-Signature-256": githubSignature("secret", payload),
	})
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized body: status = %d, want 413", rec.Code)
	}
	if got := world.repo.rowCount(); got != 0 {
		t.Fatalf("repository rows after oversized body = %d, want 0", got)
	}
}

// TestIntakeParityPartialFailure proves durable retry: a core store outage
// surfaces 500 to the provider (which redelivers), and the healed retry
// converges to exactly one row.
func TestIntakeParityPartialFailure(t *testing.T) {
	t.Parallel()

	world := newParityWorld(t, "listener-token")
	github := parityValidProviders()[0]
	world.repo.failErr = errors.New("core postgres unavailable")
	rec := world.post(github.path, github.payload, github.headers)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("store outage: status = %d, want 500", rec.Code)
	}
	world.repo.failErr = nil
	rec = world.post(github.path, github.payload, github.headers)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("healed retry: status = %d, want 202 body=%q", rec.Code, rec.Body.String())
	}
	if got := world.repo.rowCount(); got != 1 {
		t.Fatalf("repository rows after healed retry = %d, want 1 (no dual rows)", got)
	}
}

// TestIntakeParityClaimOrderIsFIFO proves ordering through the API path:
// three deliveries claim in received order, a redelivery does not jump the
// queue, and claimed rows never redeliver from the same claim pass.
func TestIntakeParityClaimOrderIsFIFO(t *testing.T) {
	t.Parallel()

	world := newParityWorld(t, "listener-token")
	shas := []string{
		"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"cccccccccccccccccccccccccccccccccccccccc",
	}
	for i, sha := range shas {
		payload := []byte(`{"ref":"refs/heads/main","before":"0000000000000000000000000000000000000000","after":"` + sha + `","repository":{"id":42,"full_name":"eshu-hq/eshu","default_branch":"main"}}`)
		// Distinct deliveries per SHA: the same delivery ID would dedupe
		// into one row instead of three ordered rows.
		headers := map[string]string{
			"X-GitHub-Event":      "push",
			"X-GitHub-Delivery":   fmt.Sprintf("fifo-delivery-%d", i),
			"X-Hub-Signature-256": githubSignature("secret", payload),
		}
		rec := world.post("/webhooks/github", payload, headers)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("delivery %d: status = %d, want 202 body=%q", i, rec.Code, rec.Body.String())
		}
	}
	claimed := world.repo.claim(10)
	if len(claimed) != 3 {
		t.Fatalf("claimed rows = %d, want 3", len(claimed))
	}
	for i, sha := range shas {
		if claimed[i].TargetSHA != sha {
			t.Fatalf("claimed[%d].TargetSHA = %q, want %q (FIFO)", i, claimed[i].TargetSHA, sha)
		}
	}
	if second := world.repo.claim(10); len(second) != 0 {
		t.Fatalf("second claim pass = %d rows, want 0 (claimed rows never redeliver)", len(second))
	}
}

// TestIntakeParityIntakeAuthFailureSurfaces500 proves the auth boundary
// direction: a valid provider signature with a bad listener-to-core token
// fails at the intake API, and the listener answers 500 (not 401), so the
// provider redelivers instead of dropping a correctly signed delivery.
func TestIntakeParityIntakeAuthFailureSurfaces500(t *testing.T) {
	t.Parallel()

	world := newParityWorld(t, "wrong-listener-token")
	github := parityValidProviders()[0]
	rec := world.post(github.path, github.payload, github.headers)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("bad intake token: status = %d, want 500 body=%q", rec.Code, rec.Body.String())
	}
	if got := world.repo.rowCount(); got != 0 {
		t.Fatalf("repository rows after intake auth failure = %d, want 0", got)
	}
}

// TestIntakeParityActionsDuplicateAndConflict proves the Actions inbox
// contract through the extracted path: an exact-identity replay answers
// 202 with one stored row, while a conflicting identity for the same
// delivery answers 409 with the original row intact.
func TestIntakeParityActionsDuplicateAndConflict(t *testing.T) {
	t.Parallel()

	world := newParityWorld(t, "listener-token")
	payload := []byte(`{"action":"completed","repository":{"id":42,"full_name":"Example/Repo"},"workflow_run":{"id":123}}`)
	headers := map[string]string{
		"X-GitHub-Event":      "workflow_run",
		"X-GitHub-Delivery":   "actions-delivery-1",
		"X-Hub-Signature-256": githubSignature("secret", payload),
	}
	for i := 0; i < 2; i++ {
		rec := world.post("/webhooks/github", payload, headers)
		if rec.Code != http.StatusAccepted {
			t.Fatalf("replay %d: status = %d, want 202 body=%q", i, rec.Code, rec.Body.String())
		}
	}
	if got := world.actions.rowCount(); got != 1 {
		t.Fatalf("actions rows after exact replay = %d, want 1", got)
	}
	conflictPayload := []byte(`{"action":"completed","repository":{"id":42,"full_name":"Example/Repo"},"workflow_run":{"id":999}}`)
	conflictHeaders := map[string]string{
		"X-GitHub-Event":      "workflow_run",
		"X-GitHub-Delivery":   "actions-delivery-1",
		"X-Hub-Signature-256": githubSignature("secret", conflictPayload),
	}
	rec := world.post("/webhooks/github", conflictPayload, conflictHeaders)
	if rec.Code != http.StatusConflict {
		t.Fatalf("conflicting identity: status = %d, want 409 body=%q", rec.Code, rec.Body.String())
	}
	if got := world.actions.rowCount(); got != 1 {
		t.Fatalf("actions rows after conflict = %d, want 1 (original intact)", got)
	}
}
