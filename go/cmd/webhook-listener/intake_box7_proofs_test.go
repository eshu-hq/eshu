// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/webhook"
	"github.com/eshu-hq/eshu/go/internal/webhook/intake"
)

// This file holds the #8876 box 7 hermetic proofs for the listener-to-core
// intake path: revoked or rotated authority and request cancellation. It
// runs the real listener, the real submitter, and the real intake API over
// the in-memory coalescing stores in intake_parity_stores_test.go, so it
// needs no database, cluster, or network beyond loopback.

var parityFixedNow = time.Date(2026, time.October, 28, 12, 0, 0, 0, time.UTC)

// setServerToken swaps the intake API for one that requires token. It models
// core rotating or revoking the listener-to-core bearer secret while the
// listener, stores, and httptest server keep running.
func (w *parityWorld) setServerToken(token string) {
	server := intake.NewServer(
		intake.ServerConfig{Token: token, Clock: func() time.Time { return parityFixedNow }},
		intake.Stores{
			Repository: w.repo,
			Incident:   w.incident,
			AWS:        w.aws,
			GCP:        w.gcp,
			Actions:    w.actions,
		},
	)
	w.serverMux.Store(server.Mux())
}

// serveIntake routes one intake request through the optional gate, then the
// current intake API.
func (w *parityWorld) serveIntake(rw http.ResponseWriter, r *http.Request) {
	if gate := w.intakeGate.Load(); gate != nil && (*gate)(rw, r) {
		return
	}
	w.serverMux.Load().ServeHTTP(rw, r)
}

// githubPushDelivery builds one validly signed GitHub push for sha. A
// distinct sha is a distinct refresh key, so it is a distinct durable row.
func githubPushDelivery(sha, deliveryID string) ([]byte, map[string]string) {
	payload := []byte(`{"ref":"refs/heads/main","before":"0000000000000000000000000000000000000000","after":"` + sha +
		`","repository":{"id":42,"full_name":"eshu-hq/eshu","default_branch":"main"}}`)
	return payload, map[string]string{
		"X-GitHub-Event":      "push",
		"X-GitHub-Delivery":   deliveryID,
		"X-Hub-Signature-256": githubSignature("secret", payload),
	}
}

const (
	box7ShaA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	box7ShaB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	box7KeyB = "github|42|main|" + box7ShaB
)

// postIntakeDirect sends a bare bearer request to the intake API, bypassing
// the listener, so the test can read the 401 the listener hides behind 500.
func postIntakeDirect(t *testing.T, world *parityWorld, bearer string) int {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, world.intake.URL+"/api/v1/webhook/intake/repository",
		strings.NewReader(`{"api_version":"v1","provider":"github"}`))
	if err != nil {
		t.Fatalf("build direct intake request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+bearer)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("direct intake request: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	return resp.StatusCode
}

// TestIntakeParityRotatedBearerTokenFailsClosedThenConverges proves revoked
// authority on the intake path (#8876 box 7). Delivery A lands under token
// T1. Core then rotates to T2. Delivery B, validly signed but still sent
// with T1, fails closed: the intake API answers 401, the listener answers
// 500 so the provider redelivers, and no row exists. Once the listener holds
// T2 the redelivery of B succeeds as a fresh row, so exactly two rows exist
// (no lost fact, no duplicate), and the revoked T1 stays rejected.
func TestIntakeParityRotatedBearerTokenFailsClosedThenConverges(t *testing.T) {
	t.Parallel()

	const (
		tokenOne = "listener-token"
		tokenTwo = "rotated-listener-token"
	)
	world := newParityWorld(t, tokenOne)

	payloadA, headersA := githubPushDelivery(box7ShaA, "box7-a")
	if rec := world.post("/webhooks/github", payloadA, headersA); rec.Code != http.StatusAccepted {
		t.Fatalf("delivery A under T1: status = %d, want 202 body=%q", rec.Code, rec.Body.String())
	}
	if got := world.repo.rowCount(); got != 1 {
		t.Fatalf("rows after delivery A = %d, want 1", got)
	}

	world.setServerToken(tokenTwo)
	if got := postIntakeDirect(t, world, tokenOne); got != http.StatusUnauthorized {
		t.Fatalf("intake API with revoked T1: status = %d, want 401", got)
	}

	payloadB, headersB := githubPushDelivery(box7ShaB, "box7-b")
	rec := world.post("/webhooks/github", payloadB, headersB)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("delivery B with revoked T1: status = %d, want 500 (provider must redeliver) body=%q", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), tokenOne) || strings.Contains(rec.Body.String(), tokenTwo) {
		t.Fatalf("listener response leaks a bearer token: %q", rec.Body.String())
	}
	if got := world.repo.rowCount(); got != 1 {
		t.Fatalf("rows after revoked-token delivery B = %d, want 1 (no row for B)", got)
	}
	if got := world.repo.duplicateCount(box7KeyB); got != -1 {
		t.Fatalf("row for B exists after fail-closed delivery, duplicate_count = %d", got)
	}

	world.client.token = tokenTwo
	rec = world.post("/webhooks/github", payloadB, headersB)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("redelivery of B under T2: status = %d, want 202 body=%q", rec.Code, rec.Body.String())
	}
	if got := world.repo.rowCount(); got != 2 {
		t.Fatalf("rows after redelivery under T2 = %d, want exactly 2 (no loss, no duplicate)", got)
	}
	if got := world.repo.duplicateCount(box7KeyB); got != 0 {
		t.Fatalf("duplicate_count for B = %d, want 0 (first durable write)", got)
	}
	if got := postIntakeDirect(t, world, tokenOne); got != http.StatusUnauthorized {
		t.Fatalf("intake API with revoked T1 after recovery: status = %d, want 401", got)
	}
}

// TestIntakeParityRevokedTokenFailsClosedForAllProviders proves the revoked
// authority boundary is not a GitHub special case: every one of the seven
// mounted providers, validly signed, is refused with a provider-facing 500
// and stores nothing once the core token no longer matches.
func TestIntakeParityRevokedTokenFailsClosedForAllProviders(t *testing.T) {
	t.Parallel()

	world := newParityWorld(t, "listener-token")
	world.setServerToken("rotated-listener-token")
	for _, provider := range parityValidProviders() {
		rec := world.post(provider.path, provider.payload, provider.headers)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s: status = %d, want 500 body=%q", provider.name, rec.Code, rec.Body.String())
		}
	}
	if got := world.repo.rowCount() + world.incident.rowCount() + world.aws.rowCount() + world.gcp.rowCount() + world.actions.rowCount(); got != 0 {
		t.Fatalf("rows after revoked-token deliveries = %d, want 0", got)
	}
}

// stallIntake installs a gate that signals entered and then holds each
// intake request until its context ends (the listener hung up) or release
// closes. It returns entered and a release func that is safe to call twice.
func stallIntake(world *parityWorld) (entered chan struct{}, release func()) {
	entered = make(chan struct{}, 8)
	released := make(chan struct{})
	gate := func(_ http.ResponseWriter, r *http.Request) bool {
		entered <- struct{}{}
		select {
		case <-r.Context().Done():
		case <-released:
		}
		return true
	}
	world.intakeGate.Store(&gate)
	closed := false
	return entered, func() {
		if !closed {
			closed = true
			close(released)
		}
	}
}

func waitFor(t *testing.T, what string, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

// TestIntakeParityProviderCancellationStoresNothingThenRetryStoresOne proves
// cancellation on the provider-facing edge (#8876 box 7). A provider that
// hangs up while core is still working must not leave a row, the listener
// must stop promptly instead of riding out the 10s intake timeout, and the
// provider's redelivery must store exactly one row, then coalesce as a
// duplicate on a further replay.
func TestIntakeParityProviderCancellationStoresNothingThenRetryStoresOne(t *testing.T) {
	t.Parallel()

	world := newParityWorld(t, "listener-token")
	payload, headers := githubPushDelivery(box7ShaA, "box7-cancel")
	entered, release := stallIntake(world)
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader(payload)).WithContext(ctx)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	handled := make(chan struct{})
	go func() {
		defer close(handled)
		world.listener.ServeHTTP(rec, req)
	}()

	waitFor(t, "intake request to reach core", entered)
	cancel()
	select {
	case <-handled:
	case <-time.After(3 * time.Second):
		t.Fatal("listener handler still blocked 3s after provider cancel; submit client ignores context")
	}
	if rec.Code == http.StatusAccepted {
		t.Fatalf("cancelled delivery answered 202, want a non-2xx so it is redelivered")
	}
	if got := world.repo.rowCount(); got != 0 {
		t.Fatalf("rows after cancelled delivery = %d, want 0", got)
	}

	world.intakeGate.Store(nil)
	const key = "github|42|main|" + box7ShaA
	if rec := world.post("/webhooks/github", payload, headers); rec.Code != http.StatusAccepted {
		t.Fatalf("redelivery after cancel: status = %d, want 202 body=%q", rec.Code, rec.Body.String())
	}
	if got := world.repo.rowCount(); got != 1 {
		t.Fatalf("rows after redelivery = %d, want exactly 1", got)
	}
	if got := world.repo.duplicateCount(key); got != 0 {
		t.Fatalf("duplicate_count after first durable write = %d, want 0", got)
	}
	if rec := world.post("/webhooks/github", payload, headers); rec.Code != http.StatusAccepted {
		t.Fatalf("second replay: status = %d, want 202", rec.Code)
	}
	if got, dup := world.repo.rowCount(), world.repo.duplicateCount(key); got != 1 || dup != 1 {
		t.Fatalf("after replay rows = %d duplicate_count = %d, want 1 and 1", got, dup)
	}
}

// settledGoroutines returns the goroutine count once it stops falling,
// closing idle keep-alive connections first so pooled transport goroutines
// do not count as leaks.
func settledGoroutines(client *http.Client) int {
	last := -1
	for i := 0; i < 50; i++ {
		client.CloseIdleConnections()
		runtime.Gosched()
		time.Sleep(20 * time.Millisecond)
		now := runtime.NumGoroutine()
		if now == last {
			return now
		}
		last = now
	}
	return last
}

// TestIntakeSubmitterHonorsContextCancellationWithoutLeak proves the submit
// client stops on caller cancellation (#8876 box 7). A cancelled context
// returns context.Canceled well inside the 10s client timeout, never
// reports a receipt, and leaves no goroutine behind once core releases the
// stalled request. Not parallel: it counts process goroutines.
func TestIntakeSubmitterHonorsContextCancellationWithoutLeak(t *testing.T) {
	world := newParityWorld(t, "listener-token")
	submitter := &intakeRepositorySubmitter{client: world.client}
	trigger := webhook.Trigger{
		Provider:             webhook.ProviderGitHub,
		EventKind:            webhook.EventKindPush,
		Decision:             webhook.DecisionAccepted,
		DeliveryID:           "box7-leak",
		RepositoryExternalID: "42",
		RepositoryFullName:   "eshu-hq/eshu",
		DefaultBranch:        "main",
		TargetSHA:            box7ShaA,
	}
	// Warm the connection pool so the baseline includes steady-state
	// goroutines, then settle.
	if _, err := submitter.StoreTrigger(context.Background(), trigger, parityFixedNow); err != nil {
		t.Fatalf("warm-up StoreTrigger() error = %v, want nil", err)
	}
	baseline := settledGoroutines(world.client.client)

	entered, release := stallIntake(world)
	defer release()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	type result struct {
		err     error
		elapsed time.Duration
	}
	done := make(chan result, 1)
	go func() {
		start := time.Now()
		_, err := submitter.StoreTrigger(ctx, trigger, parityFixedNow)
		done <- result{err: err, elapsed: time.Since(start)}
	}()
	waitFor(t, "submission to reach core", entered)
	cancel()
	var got result
	select {
	case got = <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StoreTrigger still blocked 3s after cancel; context is not honored")
	}
	if !errors.Is(got.err, context.Canceled) {
		t.Fatalf("StoreTrigger() error = %v, want context.Canceled", got.err)
	}
	if got.elapsed >= intakeRequestTimeout/2 {
		t.Fatalf("StoreTrigger returned after %s, want prompt return well under the %s client timeout", got.elapsed, intakeRequestTimeout)
	}
	release()
	world.intakeGate.Store(nil)

	deadline := time.Now().Add(5 * time.Second)
	for {
		if now := settledGoroutines(world.client.client); now <= baseline {
			break
		} else if time.Now().After(deadline) {
			t.Fatalf("goroutines = %d after cancel, baseline %d: submit path leaks", now, baseline)
		}
	}
	if got := world.repo.rowCount(); got != 1 {
		t.Fatalf("rows = %d, want 1 (only the warm-up write)", got)
	}
}
