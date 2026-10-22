// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package app

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func mintTestPrivateKeyPEM(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

// mintTestServer answers the installation token endpoint with statuses in
// order, then repeats the last one. A 200 carries a token. It records every
// Authorization header so tests can prove the JWT never leaks into errors.
func mintTestServer(t *testing.T, statuses []int, headers map[string]string) (*httptest.Server, func() int, func() []string) {
	t.Helper()
	var mu sync.Mutex
	calls := 0
	var authorizations []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/app/installations/777/access_tokens" {
			http.Error(w, "unexpected request "+r.Method+" "+r.URL.Path, http.StatusTeapot)
			return
		}
		mu.Lock()
		index := calls
		calls++
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		mu.Unlock()
		if index >= len(statuses) {
			index = len(statuses) - 1
		}
		status := statuses[index]
		if status == http.StatusOK {
			_ = json.NewEncoder(w).Encode(map[string]string{"token": "minted-token"})
			return
		}
		for k, v := range headers {
			w.Header().Set(k, v)
		}
		http.Error(w, "{}", status)
	}))
	t.Cleanup(server.Close)
	count := func() int {
		mu.Lock()
		defer mu.Unlock()
		return calls
	}
	authHeaders := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), authorizations...)
	}
	return server, count, authHeaders
}

type sleepRecorder struct {
	mu    sync.Mutex
	waits []time.Duration
}

func (r *sleepRecorder) record(_ context.Context, d time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.waits = append(r.waits, d)
	return nil
}

func (r *sleepRecorder) recorded() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.waits...)
}

func mintTestConfig(server *httptest.Server, rec *sleepRecorder, keyPEM string) Config {
	return Config{
		BaseURL:        server.URL,
		AppID:          "42",
		InstallationID: "777",
		PrivateKey:     keyPEM,
		MaxAttempts:    4,
		BaseBackoff:    time.Second,
		Sleep:          rec.record,
	}
}

// TestNormalizePrivateKeyPEMRestoresSingleLineKeys pins that a PEM body
// collapsed onto one line (the usual secret-env shape) is re-wrapped at 64
// columns, while already-wrapped and non-PEM values pass through untouched.
func TestNormalizePrivateKeyPEMRestoresSingleLineKeys(t *testing.T) {
	t.Parallel()

	wrapped := mintTestPrivateKeyPEM(t)
	body := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(wrapped,
		"-----BEGIN RSA PRIVATE KEY-----", ""), "-----END RSA PRIVATE KEY-----", ""))
	collapsed := "-----BEGIN RSA PRIVATE KEY-----" + strings.ReplaceAll(body, "\n", "") + "-----END RSA PRIVATE KEY-----"
	restored := NormalizePrivateKeyPEM(collapsed)
	if !strings.HasPrefix(restored, "-----BEGIN RSA PRIVATE KEY-----\n") ||
		!strings.HasSuffix(restored, "\n-----END RSA PRIVATE KEY-----\n") {
		t.Fatalf("restored key missing PEM framing:\n%s", restored)
	}
	for _, line := range strings.Split(strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(
		restored, "-----BEGIN RSA PRIVATE KEY-----", ""), "-----END RSA PRIVATE KEY-----", "")), "\n") {
		if len(line) > 64 {
			t.Fatalf("restored body line has %d chars, want at most 64", len(line))
		}
	}
	if got := NormalizePrivateKeyPEM(wrapped); got != strings.TrimSpace(wrapped) {
		t.Fatalf("wrapped key was reflowed")
	}
	if got := NormalizePrivateKeyPEM("  not-a-pem-value  "); got != "not-a-pem-value" {
		t.Fatalf("non-PEM value = %q, want trimmed passthrough", got)
	}
}

func isRetryableErr(err error) bool {
	var retryable interface{ Retryable() bool }
	return errors.As(err, &retryable) && retryable.Retryable()
}

// TestMintRetriesTransientFaults pins the listing-compatible retry budget: a
// 502 then a 503 then a 200 recovers, with the 1s, 2s wait schedule.
func TestMintRetriesTransientFaults(t *testing.T) {
	t.Parallel()

	keyPEM := mintTestPrivateKeyPEM(t)
	server, calls, _ := mintTestServer(t, []int{http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusOK}, nil)
	rec := &sleepRecorder{}
	token, err := MintInstallationToken(context.Background(), mintTestConfig(server, rec, keyPEM))
	if err != nil || token != "minted-token" {
		t.Fatalf("mint = %q, %v; want minted-token, nil", token, err)
	}
	if got := calls(); got != 3 {
		t.Fatalf("mint calls = %d, want 3", got)
	}
	if got := rec.recorded(); len(got) != 2 || got[0] != time.Second || got[1] != 2*time.Second {
		t.Fatalf("waits = %v, want [1s 2s]", got)
	}
}

// TestMintFinalStatusesDoNotRetry pins that 401, 403 and 404 from the mint
// are final: one call, no wait, not retryable.
func TestMintFinalStatusesDoNotRetry(t *testing.T) {
	t.Parallel()

	keyPEM := mintTestPrivateKeyPEM(t)
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		server, calls, _ := mintTestServer(t, []int{status}, nil)
		rec := &sleepRecorder{}
		_, err := MintInstallationToken(context.Background(), mintTestConfig(server, rec, keyPEM))
		if err == nil || isRetryableErr(err) {
			t.Fatalf("status %d: error = %v, want a final error", status, err)
		}
		if got := calls(); got != 1 {
			t.Fatalf("status %d: mint calls = %d, want 1", status, got)
		}
		if got := rec.recorded(); len(got) != 0 {
			t.Fatalf("status %d: waits = %v, want none", status, got)
		}
	}
}

// TestMintExhaustionReturnsRetryable pins one retryable error shape for an
// outlasted transient: server_error class, 4 attempts.
func TestMintExhaustionReturnsRetryable(t *testing.T) {
	t.Parallel()

	keyPEM := mintTestPrivateKeyPEM(t)
	server, calls, _ := mintTestServer(t, []int{http.StatusBadGateway}, nil)
	rec := &sleepRecorder{}
	_, err := MintInstallationToken(context.Background(), mintTestConfig(server, rec, keyPEM))
	if err == nil || !isRetryableErr(err) {
		t.Fatalf("error = %v, want a retryable error", err)
	}
	var typed *RetryableError
	if !errors.As(err, &typed) {
		t.Fatalf("error %v is not a *RetryableError", err)
	}
	if typed.Class != "server_error" || typed.Attempts != 4 {
		t.Fatalf("typed = %+v, want server_error 4 attempts", typed)
	}
	if got := calls(); got != 4 {
		t.Fatalf("mint calls = %d, want 4", got)
	}
}

// TestMintRateLimitShortHintSleepsAndRecovers pins that a short Retry-After
// hint is slept in-call and the mint recovers.
func TestMintRateLimitShortHintSleepsAndRecovers(t *testing.T) {
	t.Parallel()

	keyPEM := mintTestPrivateKeyPEM(t)
	server, _, _ := mintTestServer(t, []int{http.StatusTooManyRequests, http.StatusOK}, map[string]string{"Retry-After": "2"})
	rec := &sleepRecorder{}
	token, err := MintInstallationToken(context.Background(), mintTestConfig(server, rec, keyPEM))
	if err != nil || token == "" {
		t.Fatalf("mint = %q, %v; want recovery", token, err)
	}
	if got := rec.recorded(); len(got) != 1 || got[0] != 2*time.Second {
		t.Fatalf("waits = %v, want [2s]", got)
	}
}

// TestMintRateLimitLongHintDefers pins that a hint longer than the in-call
// cap ends the mint at once with the capped RetryAfter: one call, no wait.
func TestMintRateLimitLongHintDefers(t *testing.T) {
	t.Parallel()

	keyPEM := mintTestPrivateKeyPEM(t)
	server, calls, _ := mintTestServer(t, []int{http.StatusForbidden}, map[string]string{"Retry-After": "3600"})
	rec := &sleepRecorder{}
	_, err := MintInstallationToken(context.Background(), mintTestConfig(server, rec, keyPEM))
	var typed *RetryableError
	if !errors.As(err, &typed) {
		t.Fatalf("error %v is not a *RetryableError", err)
	}
	if typed.RetryAfter != time.Hour || calls() != 1 || len(rec.recorded()) != 0 {
		t.Fatalf("typed %+v calls %d waits %v, want 1h, one call, no wait", typed, calls(), rec.recorded())
	}
}

// TestMintSignsRS256JWTWithSkewedClaims pins the JWT shape against a
// generated RSA key: RS256, iss=app ID, iat=now-60s, exp=now+540s.
func TestMintSignsRS256JWTWithSkewedClaims(t *testing.T) {
	t.Parallel()

	keyPEM := mintTestPrivateKeyPEM(t)
	block, _ := pem.Decode([]byte(keyPEM))
	if block == nil {
		t.Fatal("decode generated PEM")
	}
	publicKey, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		t.Fatalf("parse generated key: %v", err)
	}
	fixedNow := time.Date(2026, time.October, 22, 12, 0, 0, 0, time.UTC)
	server, _, authHeaders := mintTestServer(t, []int{http.StatusOK}, nil)
	rec := &sleepRecorder{}
	cfg := mintTestConfig(server, rec, keyPEM)
	cfg.Now = func() time.Time { return fixedNow }
	if _, err := MintInstallationToken(context.Background(), cfg); err != nil {
		t.Fatalf("mint: %v", err)
	}
	headers := authHeaders()
	if len(headers) != 1 {
		t.Fatalf("authorization headers = %d, want 1", len(headers))
	}
	raw, ok := strings.CutPrefix(headers[0], "Bearer ")
	if !ok || raw == "" {
		t.Fatalf("authorization header = %q, want Bearer JWT", headers[0])
	}
	parser := jwt.NewParser(jwt.WithTimeFunc(func() time.Time { return fixedNow }))
	parsed, err := parser.Parse(raw, func(*jwt.Token) (any, error) { return &publicKey.PublicKey, nil })
	if err != nil || !parsed.Valid {
		t.Fatalf("parse JWT: %v (valid=%v)", err, parsed != nil && parsed.Valid)
	}
	if parsed.Method != jwt.SigningMethodRS256 {
		t.Fatalf("JWT alg = %v, want RS256", parsed.Method.Alg())
	}
	claims, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		t.Fatalf("claims type = %T, want MapClaims", parsed.Claims)
	}
	iss, _ := claims.GetIssuer()
	if iss != "42" {
		t.Fatalf("iss = %q, want 42", iss)
	}
	issuedAt, _ := claims.GetIssuedAt()
	if issuedAt == nil || issuedAt.Unix() != fixedNow.Unix()-60 {
		t.Fatalf("iat = %v, want now-60s", issuedAt)
	}
	expiresAt, _ := claims.GetExpirationTime()
	if expiresAt == nil || expiresAt.Unix() != fixedNow.Unix()+540 {
		t.Fatalf("exp = %v, want now+540s", expiresAt)
	}
}

// TestMintRejectsMissingCredentials pins fail-closed validation before any
// HTTP request: each missing field is a final error with zero calls.
func TestMintRejectsMissingCredentials(t *testing.T) {
	t.Parallel()

	keyPEM := mintTestPrivateKeyPEM(t)
	server, calls, _ := mintTestServer(t, []int{http.StatusOK}, nil)
	rec := &sleepRecorder{}
	base := mintTestConfig(server, rec, keyPEM)
	tests := []struct {
		name   string
		mutate func(*Config)
	}{
		{name: "app id", mutate: func(c *Config) { c.AppID = " " }},
		{name: "installation id", mutate: func(c *Config) { c.InstallationID = "" }},
		{name: "private key", mutate: func(c *Config) { c.PrivateKey = "" }},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cfg := base
			tt.mutate(&cfg)
			_, err := MintInstallationToken(context.Background(), cfg)
			if err == nil || isRetryableErr(err) {
				t.Fatalf("error = %v, want a final validation error", err)
			}
		})
	}
	if got := calls(); got != 0 {
		t.Fatalf("mint calls = %d, want 0", got)
	}
}

// TestMintRejectsBadPrivateKey pins that an unparseable key is final with
// zero calls and never echoes the key.
func TestMintRejectsBadPrivateKey(t *testing.T) {
	t.Parallel()

	server, calls, _ := mintTestServer(t, []int{http.StatusOK}, nil)
	rec := &sleepRecorder{}
	cfg := mintTestConfig(server, rec, "NOT-A-KEY-ffff1234")
	_, err := MintInstallationToken(context.Background(), cfg)
	if err == nil || isRetryableErr(err) {
		t.Fatalf("error = %v, want a final error", err)
	}
	if got := calls(); got != 0 {
		t.Fatalf("mint calls = %d, want 0", got)
	}
	if strings.Contains(fmt.Sprintf("%+v", err), "NOT-A-KEY-ffff1234") {
		t.Fatalf("error echoes the key material: %v", err)
	}
}

// TestMintErrorsCarryNoCredentials is the credential canary: across a denial
// and a success, no error string (including the wrapped chain) may contain a
// PEM fragment, a captured JWT, or the minted token.
func TestMintErrorsCarryNoCredentials(t *testing.T) {
	t.Parallel()

	keyPEM := mintTestPrivateKeyPEM(t)
	pemFragment := strings.TrimSpace(keyPEM)
	if len(pemFragment) > 200 {
		pemFragment = pemFragment[100:200]
	}
	denied, _, deniedAuth := mintTestServer(t, []int{http.StatusUnauthorized}, nil)
	rec := &sleepRecorder{}
	_, deniedErr := MintInstallationToken(context.Background(), mintTestConfig(denied, rec, keyPEM))
	if deniedErr == nil {
		t.Fatal("denied mint: want an error")
	}
	deniedText := fmt.Sprintf("%+v", deniedErr)

	okServer, _, okAuth := mintTestServer(t, []int{http.StatusOK}, nil)
	token, okErr := MintInstallationToken(context.Background(), mintTestConfig(okServer, rec, keyPEM))
	if okErr != nil {
		t.Fatalf("mint: %v", okErr)
	}
	for _, secret := range []string{pemFragment, "PRIVATE KEY", token} {
		if secret != "" && strings.Contains(deniedText, secret) {
			t.Fatalf("denied error carries credential material %q: %v", secret, deniedErr)
		}
	}
	for _, header := range append(deniedAuth(), okAuth()...) {
		jwtValue, ok := strings.CutPrefix(header, "Bearer ")
		if !ok || jwtValue == "" {
			t.Fatalf("authorization header = %q, want Bearer JWT", header)
		}
		if strings.Contains(deniedText, jwtValue) {
			t.Fatal("denied error carries the signed JWT")
		}
	}
}
