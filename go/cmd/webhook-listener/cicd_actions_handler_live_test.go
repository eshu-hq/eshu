// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

//eshu:readiness live-postgres-readiness
//eshu:readiness:reason #9146 signed GitHub workflow_run and workflow_job routing, rejected invalid signatures, and durable listener-to-PostgreSQL Actions inbox proof in an isolated schema; focused local tests passed in 0.06s
//eshu:readiness:env ESHU_POSTGRES_DSN

import (
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	actionsstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/webhook/actions"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestWebhookHandlerAcceptsSignedGitHubWorkflowRunWithoutGitRefresh(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"action":"completed","repository":{"id":42,"full_name":"example/repo","default_branch":"main"},"workflow_run":{"id":123,"run_attempt":1,"updated_at":"2026-10-26T03:00:00Z"}}`)
	gitStore := &recordingTriggerStore{}
	actionsStore := &recordingActionsStore{}
	mux, err := newWebhookMux(webhookHandler{Config: webhookListenerConfig{
		GitHubSecret: "secret", GitHubPath: "/webhooks/github", MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, Store: gitStore, ActionsStore: actionsStore, Clock: func() time.Time { return time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	req.Header.Set("X-GitHub-Event", "workflow_run")
	req.Header.Set("X-GitHub-Delivery", "workflow-delivery-1")
	req.Header.Set("X-Hub-Signature-256", githubSignature("secret", payload))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("signed workflow_run status = %d, want 202; body=%q", rec.Code, rec.Body.String())
	}
	if len(actionsStore.triggers) != 1 || actionsStore.triggers[0].RunID != "123" {
		t.Fatalf("stored actions = %+v, want one run 123", actionsStore.triggers)
	}
	if len(gitStore.triggers) != 0 {
		t.Fatalf("workflow_run sent %d Git refresh triggers, want 0", len(gitStore.triggers))
	}
}

// recordingActionsStore captures the production handler's verified-delivery handoff.
type recordingActionsStore struct{ triggers []webhook.ActionsTrigger }

func (s *recordingActionsStore) Store(_ context.Context, trigger webhook.ActionsTrigger, at time.Time) (webhook.StoredActionsTrigger, error) {
	s.triggers = append(s.triggers, trigger)
	return webhook.StoredActionsTrigger{ActionsTrigger: trigger, Status: webhook.TriggerStatusQueued, ReceivedAt: at}, nil
}

func TestSignedActionsDeliveryPersistsInRealInboxLive(t *testing.T) {
	dsn := os.Getenv("ESHU_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ESHU_POSTGRES_DSN is not set")
	}
	boot, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = boot.Close() })
	schema := fmt.Sprintf("cicd_listener_test_%d", time.Now().UnixNano())
	if _, err = boot.ExecContext(t.Context(), "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if _, cleanupErr := boot.ExecContext(cleanupCtx, "DROP SCHEMA "+schema+" CASCADE"); cleanupErr != nil {
			t.Errorf("drop isolated actions schema: %v", cleanupErr)
		}
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	database, err := sql.Open("pgx", parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	actions := actionsstore.NewStore(postgres.SQLDB{DB: database})
	if err := actions.EnsureSchema(t.Context()); err != nil {
		t.Fatal(err)
	}
	mux, err := newWebhookMux(webhookHandler{
		Config: webhookListenerConfig{GitHubSecret: "secret", GitHubPath: "/webhooks/github", MaxRequestBodyBytes: defaultMaxWebhookBodyBytes},
		Store:  &recordingTriggerStore{}, ActionsStore: actions,
		Clock: func() time.Time { return time.Date(2026, time.October, 26, 4, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"action":"completed","repository":{"id":42,"full_name":"example/repo"},"workflow_run":{"id":123}}`)
	request := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	request.Header.Set("X-GitHub-Event", "workflow_run")
	request.Header.Set("X-GitHub-Delivery", "durable-signed-delivery")
	request.Header.Set("X-Hub-Signature-256", githubSignature("secret", payload))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("response=%d body=%q", response.Code, response.Body.String())
	}
	duplicate := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	duplicate.Header = request.Header.Clone()
	duplicateResponse := httptest.NewRecorder()
	mux.ServeHTTP(duplicateResponse, duplicate)
	if duplicateResponse.Code != http.StatusAccepted {
		t.Fatalf("exact duplicate status=%d body=%q, want 202", duplicateResponse.Code, duplicateResponse.Body.String())
	}
	changedPayload := []byte(`{"action":"completed","repository":{"id":42,"full_name":"example/repo"},"workflow_run":{"id":124}}`)
	conflict := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(changedPayload))
	conflict.Header.Set("X-GitHub-Event", "workflow_run")
	conflict.Header.Set("X-GitHub-Delivery", "durable-signed-delivery")
	conflict.Header.Set("X-Hub-Signature-256", githubSignature("secret", changedPayload))
	conflictResponse := httptest.NewRecorder()
	mux.ServeHTTP(conflictResponse, conflict)
	if conflictResponse.Code != http.StatusConflict {
		t.Fatalf("conflicting delivery status=%d body=%q, want 409", conflictResponse.Code, conflictResponse.Body.String())
	}
	if bytes.Contains(conflictResponse.Body.Bytes(), []byte(`"decision":"accepted"`)) {
		t.Fatalf("conflict response claims accepted storage: %q", conflictResponse.Body.String())
	}
	var deliveryCount int
	if err := database.QueryRowContext(t.Context(), `SELECT count(*) FROM cicd_actions_deliveries WHERE delivery_id='durable-signed-delivery'`).Scan(&deliveryCount); err != nil {
		t.Fatal(err)
	}
	if deliveryCount != 1 {
		t.Fatalf("stored delivery count=%d, want 1", deliveryCount)
	}
	var repository, status, runID string
	if err := database.QueryRowContext(t.Context(), `SELECT repository,status,run_id FROM cicd_actions_deliveries WHERE delivery_id='durable-signed-delivery'`).Scan(&repository, &status, &runID); err != nil {
		t.Fatal(err)
	}
	if repository != "example/repo" || status != "queued" || runID != "123" {
		t.Fatalf("stored repository=%q status=%q run_id=%q", repository, status, runID)
	}
}

func TestWebhookHandlerAcceptsSignedWorkflowJob(t *testing.T) {
	payload := []byte(`{"action":"completed","repository":{"id":42,"full_name":"example/repo"},"workflow_job":{"id":456,"run_id":123}}`)
	actions := &recordingActionsStore{}
	mux, err := newWebhookMux(webhookHandler{Config: webhookListenerConfig{GitHubSecret: "secret", GitHubPath: "/webhooks/github", MaxRequestBodyBytes: defaultMaxWebhookBodyBytes}, Store: &recordingTriggerStore{}, ActionsStore: actions})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload))
	request.Header.Set("X-GitHub-Event", "workflow_job")
	request.Header.Set("X-GitHub-Delivery", "job-delivery")
	request.Header.Set("X-Hub-Signature-256", githubSignature("secret", payload))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || len(actions.triggers) != 1 || actions.triggers[0].RunID != "123" || actions.triggers[0].JobID != "456" {
		t.Fatalf("job response=%d triggers=%+v", response.Code, actions.triggers)
	}
}

func TestWebhookHandlerRejectsBadActionsSignatureBeforeStorage(t *testing.T) {
	for _, event := range []string{"workflow_run", "workflow_job"} {
		t.Run(event, func(t *testing.T) {
			actions := &recordingActionsStore{}
			mux, err := newWebhookMux(webhookHandler{Config: webhookListenerConfig{GitHubSecret: "secret", GitHubPath: "/webhooks/github", MaxRequestBodyBytes: defaultMaxWebhookBodyBytes}, Store: &recordingTriggerStore{}, ActionsStore: actions})
			if err != nil {
				t.Fatal(err)
			}
			request := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader([]byte(`{}`)))
			request.Header.Set("X-GitHub-Event", event)
			request.Header.Set("X-GitHub-Delivery", "invalid-signature")
			request.Header.Set("X-Hub-Signature-256", "sha256=bad")
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, request)
			if response.Code != http.StatusUnauthorized || len(actions.triggers) != 0 {
				t.Fatalf("status=%d triggers=%+v", response.Code, actions.triggers)
			}
		})
	}
}
