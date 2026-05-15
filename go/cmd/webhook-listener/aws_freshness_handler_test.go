package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
)

func TestWebhookHandlerAcceptsAWSFreshnessEventBridgePayload(t *testing.T) {
	t.Parallel()

	payload := []byte(`{
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
	store := &recordingAWSFreshnessStore{}
	mux := mustWebhookMuxWithAWSFreshness(t, webhookListenerConfig{
		AWSFreshnessToken:   "secret",
		AWSFreshnessPath:    "/webhooks/aws/eventbridge",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/aws/eventbridge", bytes.NewReader(payload))
	req.Header.Set("Authorization", "Bearer secret")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d body=%q", rec.Code, http.StatusAccepted, rec.Body.String())
	}
	if got, want := len(store.triggers), 1; got != want {
		t.Fatalf("stored triggers = %d, want %d", got, want)
	}
	trigger := store.triggers[0]
	if trigger.Kind != freshness.EventKindConfigChange {
		t.Fatalf("Kind = %q, want %q", trigger.Kind, freshness.EventKindConfigChange)
	}
	if trigger.ServiceKind != awscloud.ServiceLambda {
		t.Fatalf("ServiceKind = %q, want %q", trigger.ServiceKind, awscloud.ServiceLambda)
	}
}

func TestWebhookHandlerRejectsAWSFreshnessBadToken(t *testing.T) {
	t.Parallel()

	store := &recordingAWSFreshnessStore{}
	mux := mustWebhookMuxWithAWSFreshness(t, webhookListenerConfig{
		AWSFreshnessToken:   "secret",
		AWSFreshnessPath:    "/webhooks/aws/eventbridge",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/aws/eventbridge", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerRejectsAWSFreshnessBadTokenBeforeBodyRead(t *testing.T) {
	t.Parallel()

	store := &recordingAWSFreshnessStore{}
	mux := mustWebhookMuxWithAWSFreshness(t, webhookListenerConfig{
		AWSFreshnessToken:   "secret",
		AWSFreshnessPath:    "/webhooks/aws/eventbridge",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	body := &readCountingCloser{}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/aws/eventbridge", body)
	req.Header.Set("Authorization", "Bearer wrong")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if body.reads != 0 {
		t.Fatalf("body reads = %d, want 0", body.reads)
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored triggers = %d, want 0", len(store.triggers))
	}
}

func TestWebhookHandlerRejectsAWSFreshnessBareAuthorizationToken(t *testing.T) {
	t.Parallel()

	store := &recordingAWSFreshnessStore{}
	mux := mustWebhookMuxWithAWSFreshness(t, webhookListenerConfig{
		AWSFreshnessToken:   "secret",
		AWSFreshnessPath:    "/webhooks/aws/eventbridge",
		MaxRequestBodyBytes: defaultMaxWebhookBodyBytes,
	}, store)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/aws/eventbridge", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Authorization", "secret")
	rec := httptest.NewRecorder()

	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
	if len(store.triggers) != 0 {
		t.Fatalf("stored triggers = %d, want 0", len(store.triggers))
	}
}

func TestValidAWSFreshnessTokenAcceptsCaseInsensitiveBearerScheme(t *testing.T) {
	t.Parallel()

	for _, scheme := range []string{"bearer", "BEARER", "Bearer"} {
		req := httptest.NewRequest(http.MethodPost, "/webhooks/aws/eventbridge", nil)
		req.Header.Set("Authorization", scheme+" secret")
		if !validAWSFreshnessToken(req, "secret") {
			t.Fatalf("validAWSFreshnessToken(%q) = false, want true", scheme)
		}
	}
}

func TestLoadWebhookListenerConfigAllowsAWSFreshnessOnly(t *testing.T) {
	t.Parallel()

	cfg, err := loadWebhookListenerConfig(func(key string) string {
		values := map[string]string{
			"ESHU_AWS_FRESHNESS_TOKEN": "secret",
		}
		return values[key]
	})
	if err != nil {
		t.Fatalf("loadWebhookListenerConfig() error = %v, want nil", err)
	}
	if cfg.AWSFreshnessPath != "/webhooks/aws/eventbridge" {
		t.Fatalf("AWSFreshnessPath = %q, want /webhooks/aws/eventbridge", cfg.AWSFreshnessPath)
	}
}

func mustWebhookMuxWithAWSFreshness(
	t *testing.T,
	cfg webhookListenerConfig,
	store *recordingAWSFreshnessStore,
) *http.ServeMux {
	t.Helper()
	mux, err := newWebhookMux(webhookHandler{
		Config:            cfg,
		AWSFreshnessStore: store,
		Clock: func() time.Time {
			return time.Date(2026, 5, 15, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("newWebhookMux() error = %v", err)
	}
	return mux
}

type recordingAWSFreshnessStore struct {
	triggers []freshness.Trigger
}

func (s *recordingAWSFreshnessStore) StoreTrigger(
	_ context.Context,
	trigger freshness.Trigger,
	receivedAt time.Time,
) (freshness.StoredTrigger, error) {
	s.triggers = append(s.triggers, trigger)
	return freshness.NewStoredTrigger(trigger, receivedAt)
}

type readCountingCloser struct {
	reads int
}

func (r *readCountingCloser) Read([]byte) (int, error) {
	r.reads++
	return 0, io.ErrUnexpectedEOF
}

func (*readCountingCloser) Close() error {
	return nil
}
