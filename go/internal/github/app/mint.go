// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package app

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// OperationMint names the installation-token mint in observer callbacks, so
// runtimes keep counting it under the same operation label as before the
// extraction.
const OperationMint = "mint"

// Outcome labels for Observer.MintAttempt, matching the bounded vocabulary
// of the repository-discovery REST signals.
const (
	OutcomeSuccess = "success"
	OutcomeRetried = "retried"
	OutcomeFailed  = "failed"
)

// Observer receives one event per mint attempt and per scheduled retry. A nil
// Observer disables callbacks. Runtimes wire their own instruments behind it;
// this package defines no metric family.
type Observer interface {
	MintAttempt(operation, class, outcome string)
	MintRetry(operation, class string, attempt, status int, wait time.Duration)
}

// Config configures one GitHub App installation-token mint. AppID,
// InstallationID, and PrivateKey are required; every other field has a
// production default. The zero value is not usable without credentials.
type Config struct {
	// BaseURL is the GitHub REST base; empty means defaultBaseURL.
	BaseURL string
	// AppID is the GitHub App ID (the JWT iss claim).
	AppID string
	// InstallationID is the installation to mint a token for.
	InstallationID string
	// PrivateKey is the App PEM; single-line secret-env bodies are
	// re-wrapped by NormalizePrivateKeyPEM.
	PrivateKey string
	// HTTPClient performs the mint request; nil means a 15-second client.
	HTTPClient *http.Client
	// MaxAttempts bounds the requests of one mint, including the first.
	// Values below 1 mean 1.
	MaxAttempts int
	// BaseBackoff is the wait before the first retry; each later
	// non-rate-limit wait doubles. Zero or negative means one second.
	BaseBackoff time.Duration
	// Jitter spreads a backoff; nil means no jitter.
	Jitter func(time.Duration) time.Duration
	// Sleep waits out a backoff and reports the context error when the
	// context ends first. Nil means a timer sleep. Tests inject a recorder.
	Sleep func(ctx context.Context, d time.Duration) error
	// Now reads the clock for JWT claims and rate-limit arithmetic.
	// Nil means time.Now.
	Now func() time.Time
	// Observe receives attempt and retry callbacks; nil disables them.
	Observe Observer
}

// MintInstallationToken exchanges App credentials for one installation token.
// It signs a fresh RS256 JWT per call (iat=now-60s covers 60 seconds of clock
// skew; larger skew fails closed as a terminal denial), posts it to
// {base}/app/installations/{id}/access_tokens, and retries transient faults
// under the configured budget.
//
// A retryable fault that outlasts the budget returns *RetryableError; 401
// and un-signalled 403 return *AuthDeniedError; 404 returns *NotFoundError;
// validation and decode failures return *ConfigError or a plain error. No
// returned error carries credentials, keys, tokens, or JWTs.
func MintInstallationToken(ctx context.Context, cfg Config) (string, error) {
	appID := strings.TrimSpace(cfg.AppID)
	installationID := strings.TrimSpace(cfg.InstallationID)
	if appID == "" || installationID == "" || strings.TrimSpace(cfg.PrivateKey) == "" {
		return "", &ConfigError{message: "github app auth requires an app id, an installation id, and a private key"}
	}
	parsedKey, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(NormalizePrivateKeyPEM(cfg.PrivateKey)))
	if err != nil {
		return "", &ConfigError{message: "parse github app private key"}
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	moment := now().Unix()
	claims := jwt.MapClaims{
		"iat": moment - 60,
		"exp": moment + 540,
		"iss": appID,
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodRS256, claims).SignedString(parsedKey)
	if err != nil {
		return "", &ConfigError{message: "sign github app JWT"}
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = defaultBaseURL
	}
	mintURL := base + "/app/installations/" + url.PathEscape(installationID) + "/access_tokens"
	client := cfg.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = sleepContext
	}
	clock := cfg.Now
	if clock == nil {
		clock = time.Now
	}
	attempts := max(cfg.MaxAttempts, 1)
	wait := cfg.BaseBackoff
	if wait <= 0 {
		wait = time.Second
	}
	observe := cfg.Observe
	for attemptNumber := 1; ; attemptNumber++ {
		token, verdict, buildErr := attempt(ctx, client, mintURL, signed, clock)
		if buildErr != nil {
			return "", buildErr
		}
		if verdict.cause == nil {
			if observe != nil {
				observe.MintAttempt(OperationMint, "none", OutcomeSuccess)
			}
			return token, nil
		}
		if !verdict.retryable {
			if observe != nil {
				observe.MintAttempt(OperationMint, verdict.class, OutcomeFailed)
			}
			return "", verdict.cause
		}
		sleepFor, retryAfter, deferToCycle := verdict.schedule(wait, cfg.Jitter)
		if attemptNumber >= attempts || deferToCycle {
			if observe != nil {
				observe.MintAttempt(OperationMint, verdict.class, OutcomeFailed)
			}
			return "", &RetryableError{
				Class:      verdict.class,
				Attempts:   attemptNumber,
				RetryAfter: retryAfter,
				cause:      verdict.cause,
			}
		}
		if observe != nil {
			observe.MintAttempt(OperationMint, verdict.class, OutcomeRetried)
		}
		if sleepErr := sleep(ctx, sleepFor); sleepErr != nil {
			return "", fmt.Errorf("%w (last attempt: %v)", sleepErr, verdict.cause)
		}
		if observe != nil {
			observe.MintRetry(OperationMint, verdict.class, attemptNumber, verdict.status, sleepFor)
		}
		if !verdict.limited {
			wait *= 2
		}
	}
}

// verdict is the classification of one finished mint attempt. A verdict with
// a nil cause is success.
type verdict struct {
	retryable bool
	class     string
	status    int
	cause     error
	hint      time.Duration
	hintKnown bool
	limited   bool
}

// schedule resolves the wait before the next attempt. A known rate-limit hint
// replaces the backoff; a hint past inCallWaitCap, or no hint on a limit,
// defers the wait to the caller instead of sleeping in-call.
func (v verdict) schedule(backoff time.Duration, jitter func(time.Duration) time.Duration) (sleepFor, retryAfter time.Duration, deferToCycle bool) {
	if v.limited && v.hintKnown {
		hint := max(v.hint, minimumHintWait)
		retryAfter = min(hint, retryAfterCap)
		if hint > inCallWaitCap {
			return 0, retryAfter, true
		}
		return hint, retryAfter, false
	}
	if v.limited {
		return 0, rateLimitFloor, true
	}
	sleepFor = backoff
	if jitter != nil {
		sleepFor = jitter(backoff)
	}
	return sleepFor, 0, false
}

// attempt issues one mint request and classifies the result. A non-nil error
// is a fatal request-build failure returned as is.
func attempt(
	ctx context.Context,
	client *http.Client,
	mintURL, signed string,
	now func() time.Time,
) (string, verdict, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, mintURL, bytes.NewReader([]byte("{}")))
	if err != nil {
		return "", verdict{}, fmt.Errorf("build github app token request: %w", err)
	}
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("Authorization", "Bearer "+signed)
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "eshu-go")
	response, err := client.Do(request)
	if err != nil {
		cause := fmt.Errorf("mint github app token: %w", err)
		if isTransient(ctx, err) {
			return "", verdict{retryable: true, class: transportClass(err), cause: cause}, nil
		}
		return "", verdict{class: transportClass(err), cause: cause}, nil
	}
	status := response.StatusCode
	var token string
	var readErr error
	if status < 300 {
		readErr = decodeToken(response.Body, &token)
	}
	headers := response.Header
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	switch {
	case status < 300:
		if readErr == nil {
			if strings.TrimSpace(token) == "" {
				return "", verdict{class: "client_error", cause: &ConfigError{message: "github app token response omitted token"}}, nil
			}
			return token, verdict{}, nil
		}
		cause := fmt.Errorf("decode github app token response: %w", readErr)
		if isTransient(ctx, readErr) {
			return "", verdict{retryable: true, class: transportClass(readErr), status: status, cause: cause}, nil
		}
		return "", verdict{class: "decode", status: status, cause: cause}, nil
	case status >= 500:
		return "", verdict{
			retryable: true, class: ClassServerError, status: status,
			cause: fmt.Errorf("mint github app token: status %d", status),
		}, nil
	case status == http.StatusTooManyRequests || status == http.StatusForbidden:
		hint, hintKnown, signalled := rateLimitHint(headers, now())
		if status == http.StatusTooManyRequests || signalled {
			return "", verdict{
				retryable: true, class: ClassRateLimited, status: status, limited: true,
				hint: hint, hintKnown: hintKnown,
				cause: fmt.Errorf("mint github app token: status %d", status),
			}, nil
		}
	}
	switch status {
	case http.StatusUnauthorized:
		return "", verdict{class: "client_error", cause: &AuthDeniedError{StatusCode: status}}, nil
	case http.StatusForbidden:
		return "", verdict{class: "client_error", cause: &AuthDeniedError{StatusCode: status}}, nil
	case http.StatusNotFound:
		return "", verdict{class: "client_error", cause: &NotFoundError{}}, nil
	default:
		return "", verdict{
			class: "client_error", status: status,
			cause: fmt.Errorf("mint github app token: status %d", status),
		}, nil
	}
}

func decodeToken(body io.Reader, token *string) error {
	var payload struct {
		Token string `json:"token"`
	}
	//nolint:wrapcheck // the decode error is wrapped one frame up in the bounded mint failure
	if err := json.NewDecoder(body).Decode(&payload); err != nil {
		return err
	}
	*token = payload.Token
	return nil
}

// sleepContext waits d and returns the context error when the context ends
// first, so shutdown never waits out a retry.
func sleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // the context error is the shutdown signal itself
	case <-timer.C:
		return nil
	}
}

// NormalizePrivateKeyPEM re-wraps a single-line PEM body (the usual
// secret-environment shape) at 64 columns. Already-wrapped and non-PEM
// values pass through trimmed.
func NormalizePrivateKeyPEM(privateKey string) string {
	stripped := strings.TrimSpace(privateKey)
	if strings.Contains(stripped, "\n") || !strings.HasPrefix(stripped, "-----BEGIN ") {
		return stripped
	}
	stripped = strings.ReplaceAll(stripped, "-----BEGIN RSA PRIVATE KEY-----", "")
	stripped = strings.ReplaceAll(stripped, "-----END RSA PRIVATE KEY-----", "")
	stripped = strings.TrimSpace(stripped)
	body := make([]string, 0, len(stripped)/64+1)
	for len(stripped) > 64 {
		body = append(body, stripped[:64])
		stripped = stripped[64:]
	}
	if stripped != "" {
		body = append(body, stripped)
	}
	return "-----BEGIN RSA PRIVATE KEY-----\n" + strings.Join(body, "\n") + "\n-----END RSA PRIVATE KEY-----\n"
}

// streamErrorTypes are the reflect type names of an HTTP/2 stream reset, the
// fault net/http's bundled HTTP/2 client returns as an unexported type. It
// wraps no io.ErrUnexpectedEOF and is not a net.Error, so
// sdk.IsTransientTransportError does not accept it. The names mirror the
// repository-discovery REST transport classifier; a rename there must be
// mirrored here.
var streamErrorTypes = map[string]struct{}{
	"http.http2StreamError": {},
	"http2.StreamError":     {},
}

func isStreamError(err error) bool {
	for ; err != nil; err = errors.Unwrap(err) {
		if _, ok := streamErrorTypes[reflect.TypeOf(err).String()]; ok {
			return true
		}
	}
	return false
}

// isTransient reports whether err is a connection-level fault a mint retries.
// A cancelled context is never transient, so shutdown is never retried.
func isTransient(ctx context.Context, err error) bool {
	if sdk.IsTransientTransportError(ctx, err) {
		return true
	}
	if err == nil || (ctx != nil && ctx.Err() != nil) || errors.Is(err, context.Canceled) {
		return false
	}
	return isStreamError(err)
}

// transportClass names the failure class of a transport error as a bounded
// metric label. An HTTP/2 stream reset is a "reset".
func transportClass(err error) string {
	if isStreamError(err) {
		return "reset"
	}
	return sdk.TransportFailureClass(err)
}

// rateLimitHint reads a rate-limit wait from response headers: the wait and
// whether one was named, and whether the headers signal a rate limit at all.
// Retry-after (seconds or an HTTP date) wins over x-ratelimit-reset (a Unix
// timestamp, used only when remaining is 0).
func rateLimitHint(headers http.Header, now time.Time) (wait time.Duration, known bool, signalled bool) {
	retryAfter := strings.TrimSpace(headers.Get("Retry-After"))
	exhausted := strings.TrimSpace(headers.Get("X-RateLimit-Remaining")) == "0"
	signalled = retryAfter != "" || exhausted
	if retryAfter != "" {
		if seconds, err := strconv.ParseInt(retryAfter, 10, 64); err == nil && seconds >= 0 {
			// Clamp before multiplying: a huge value would overflow the
			// duration and become a one-second retry instead of a deferral.
			seconds = min(seconds, int64(retryAfterCap/time.Second))
			return time.Duration(seconds) * time.Second, true, signalled
		}
		if at, err := http.ParseTime(retryAfter); err == nil {
			return max(at.Sub(now), 0), true, signalled
		}
	}
	if exhausted {
		if reset, err := strconv.ParseInt(strings.TrimSpace(headers.Get("X-RateLimit-Reset")), 10, 64); err == nil {
			return max(time.Unix(reset, 0).Sub(now), 0), true, signalled
		}
	}
	return 0, false, signalled
}
