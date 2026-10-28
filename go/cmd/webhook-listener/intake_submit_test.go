// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	awsfreshness "github.com/eshu-hq/eshu/go/internal/collector/awscloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/catalog"
	gcpfreshness "github.com/eshu-hq/eshu/go/internal/collector/gcpcloud/freshness"
	"github.com/eshu-hq/eshu/go/internal/webhook"
	"github.com/eshu-hq/eshu/go/internal/webhook/intake"
)

// TestIntakeSubmitterPostsRepositoryTrigger is the #9377 RED test: the
// extracted listener's submitter must POST the normalized trigger to the
// core intake API with bearer auth and return the durable receipt, without
// touching any core table itself.
func TestIntakeSubmitterPostsRepositoryTrigger(t *testing.T) {
	t.Parallel()

	intake, requests := intakeTestServer(t, intake.Stores{
		Repository: &intakeStubRepositoryStore{},
	})
	submitter, err := newIntakeSubmitter(intake.URL, "listener-token")
	if err != nil {
		t.Fatalf("newIntakeSubmitter() error = %v, want nil", err)
	}

	stored, err := submitter.StoreTrigger(context.Background(), webhook.Trigger{
		Provider:             webhook.ProviderGitHub,
		EventKind:            webhook.EventKindPush,
		Decision:             webhook.DecisionAccepted,
		DeliveryID:           "delivery-1",
		RepositoryExternalID: "123",
		RepositoryFullName:   "octo/repo",
		DefaultBranch:        "main",
		TargetSHA:            "abc123",
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("StoreTrigger() error = %v, want nil", err)
	}
	if stored.TriggerID == "" {
		t.Fatalf("stored TriggerID is empty, want the intake receipt id")
	}
	if got := len(requests.calls); got != 1 {
		t.Fatalf("intake calls = %d, want 1", got)
	}
	call := requests.calls[0]
	if call.path != "/api/v1/webhook/intake/repository" {
		t.Fatalf("intake path = %q, want the repository route", call.path)
	}
	if call.authorization != "Bearer listener-token" {
		t.Fatalf("authorization = %q, want the listener bearer token", call.authorization)
	}
}

// TestIntakeSubmitterMapsDeliveryConflict is the #9377 RED test: a 409
// delivery-conflict from the intake API must surface as
// webhook.ErrActionsDeliveryConflict so the listener keeps its 409 contract.
func TestIntakeSubmitterMapsDeliveryConflict(t *testing.T) {
	t.Parallel()

	intake, _ := intakeTestServer(t, intake.Stores{
		Actions: &intakeStubActionsStore{err: webhook.ErrActionsDeliveryConflict},
	})
	client, err := newIntakeClient(intake.URL, "listener-token")
	if err != nil {
		t.Fatalf("newIntakeClient() error = %v, want nil", err)
	}
	submitter := &intakeActionsSubmitter{client: client}

	_, err = submitter.Store(context.Background(), webhook.ActionsTrigger{
		DeliveryID:   "delivery-1",
		EventKind:    "workflow_run",
		Action:       "completed",
		RepositoryID: "123",
		Repository:   "octo/repo",
		RunID:        "42",
	}, time.Now().UTC())
	if !errors.Is(err, webhook.ErrActionsDeliveryConflict) {
		t.Fatalf("Store() error = %v, want ErrActionsDeliveryConflict", err)
	}
}

// TestIntakeSubmitterFailsClosed is the #9377 RED test: an empty intake URL
// or token must fail at construction, never as a silent direct-store
// fallback.
func TestIntakeSubmitterFailsClosed(t *testing.T) {
	t.Parallel()

	if _, err := newIntakeSubmitter("", "listener-token"); err == nil {
		t.Fatal("newIntakeSubmitter() with empty URL = nil, want error")
	}
	if _, err := newIntakeSubmitter("http://intake:8080", ""); err == nil {
		t.Fatal("newIntakeSubmitter() with empty token = nil, want error")
	}
}

type intakeTestCall struct {
	path          string
	authorization string
}

type intakeTestRequests struct {
	calls []intakeTestCall
}

// intakeTestServer runs the real core intake API over stub stores so the
// submitter tests prove the wire contract, not a canned HTTP fixture.
func intakeTestServer(t *testing.T, stores intake.Stores) (*httptest.Server, *intakeTestRequests) {
	t.Helper()
	requests := &intakeTestRequests{}
	server := intake.NewServer(intake.ServerConfig{Token: "listener-token"}, stores)
	mux := server.Mux()
	wrapped := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.calls = append(requests.calls, intakeTestCall{path: r.URL.Path, authorization: r.Header.Get("Authorization")})
		mux.ServeHTTP(w, r)
	})
	intake := httptest.NewServer(wrapped)
	t.Cleanup(intake.Close)
	return intake, requests
}

type intakeStubRepositoryStore struct{}

func (s *intakeStubRepositoryStore) StoreTrigger(_ context.Context, trigger webhook.Trigger, _ time.Time) (webhook.StoredTrigger, error) {
	return webhook.StoredTrigger{
		Trigger:   trigger,
		TriggerID: "intake-trigger-1",
		Status:    webhook.TriggerStatusQueued,
	}, nil
}

type intakeStubActionsStore struct {
	err error
}

func (s *intakeStubActionsStore) Store(_ context.Context, _ webhook.ActionsTrigger, _ time.Time) (webhook.StoredActionsTrigger, error) {
	if s.err != nil {
		return webhook.StoredActionsTrigger{}, s.err
	}
	return webhook.StoredActionsTrigger{Status: webhook.TriggerStatusQueued}, nil
}

var (
	_ = awsfreshness.TriggerStatusQueued
	_ = catalog.TriggerStatusQueued
	_ = gcpfreshness.Trigger{}
)
