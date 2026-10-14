// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rest

import (
	"context"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"strings"
	"time"
)

// Operation values, the closed set of the operation label on the
// eshu_dp_github_api_* instruments.
const (
	// OperationList is one org repository listing page request.
	OperationList = "list"
	// OperationMint is the GitHub App installation token mint.
	OperationMint = "mint"
)

// Retry constants shared by the org listing and the App token mint.
const (
	// InCallWaitCap is the longest rate-limit wait honoured inside one call. A
	// longer wait ends the call and is slept between cycles, so the process
	// keeps serving its projector.
	InCallWaitCap = 60 * time.Second
	// RetryAfterCap bounds the wait a provider hint may impose.
	RetryAfterCap = time.Hour
	// RateLimitFloor is the wait handed to the cycle when GitHub rate-limits
	// with no retry-after or x-ratelimit-reset header: its guidance is to wait
	// at least one minute.
	RateLimitFloor = time.Minute
	// BaseURLDefault is the production GitHub REST base.
	BaseURLDefault = "https://api.github.com"

	// minimumHintWait keeps a zero or already-elapsed hint from becoming an
	// immediate retry.
	minimumHintWait = time.Second
)

// Config bounds retries of one GitHub REST call.
type Config struct {
	// MaxAttempts is the total requests of one call before it fails,
	// including the first attempt. Values below 1 mean 1.
	MaxAttempts int
	// BaseBackoff is the wait before the first retry; each later wait
	// doubles. Zero or negative means one second.
	BaseBackoff time.Duration
	// Sleep waits out a backoff and reports the context error when the
	// context ends first. Nil means SleepContext. A test injects its clock
	// here so the suite never waits out a real backoff.
	Sleep func(ctx context.Context, d time.Duration) error
	// Jitter spreads a backoff so shards that failed together do not retry
	// together. Nil means no jitter, which keeps a test's wait schedule
	// exact; DefaultConfig sets Jitter.
	Jitter func(time.Duration) time.Duration
	// Now reads the clock for x-ratelimit-reset arithmetic. Nil means
	// time.Now.
	Now func() time.Time
}

// Settings are the per-deployment overrides of the GitHub REST calls. The zero
// value means the production base URL and DefaultConfig. They have no
// environment variable: a test sets them to drive discovery through httptest
// with an injected clock.
type Settings struct {
	// BaseURL is the REST base; empty means BaseURLDefault.
	BaseURL string
	// Retry is the retry budget; a MaxAttempts below 1 means DefaultConfig.
	Retry Config
}

// Resolve returns the effective REST base and retry budget.
func (s Settings) Resolve() (string, Config) {
	cfg := s.Retry
	if cfg.MaxAttempts < 1 {
		cfg = DefaultConfig()
	}
	return BaseURL(s.BaseURL), cfg
}

// DefaultConfig returns the production retry budget: four attempts per call
// with 1 s, 2 s, 4 s waits, each spread over 75% to 125% by Jitter, so a call
// survives a short GitHub outage without stretching a cycle by more than
// about a minute.
func DefaultConfig() Config {
	return Config{MaxAttempts: 4, BaseBackoff: time.Second, Jitter: Jitter}
}

// Jitter spreads a wait over 75% to 125% of it so retries that failed
// together do not return together. It mirrors deadline.jitterBackoff.
func Jitter(base time.Duration) time.Duration {
	// #nosec G404 -- backoff jitter is not security-sensitive
	return time.Duration(float64(base) * (0.75 + 0.5*rand.Float64()))
}

// SleepContext waits d and returns the context error when the context ends
// first, so shutdown never waits out a retry.
func SleepContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err() //nolint:wrapcheck // the context error is the shutdown signal itself
	case <-timer.C:
		return nil
	}
}

// BaseURL returns the REST base for a configured value: it trimmed of
// whitespace and a trailing slash, else BaseURLDefault.
func BaseURL(configured string) string {
	if base := strings.TrimRight(strings.TrimSpace(configured), "/"); base != "" {
		return base
	}
	return BaseURLDefault
}

// Call describes one GitHub REST call Do repeats.
type Call struct {
	// Operation is OperationList or OperationMint.
	Operation string
	// FailurePrefix starts the message of a final request or status failure.
	FailurePrefix string
	// DecodePrefix starts the message of a final body-decode failure.
	DecodePrefix string
	// NewRequest builds the request for one attempt.
	NewRequest func(ctx context.Context) (*http.Request, error)
	// Read consumes a 2xx response body. Its error is classified: a transient
	// transport error retries, anything else is final.
	Read func(resp *http.Response) error
}

// verdict is the classification of one finished attempt. A zero verdict
// (cause nil) is success.
type verdict struct {
	retryable bool
	class     string
	status    int
	// cause is the attempt's error: the final error when !retryable, the
	// wrapped cause of a retryable one.
	cause error
	// hint is the provider's rate-limit wait; hintKnown reports whether the
	// headers named one.
	hint      time.Duration
	hintKnown bool
	// limited marks a rate-limit response, whose wait rules differ from a 5xx.
	limited bool
}

// Do performs call under cfg and returns nil on a 2xx whose body Read
// accepted. A retryable fault that outlasts the budget returns
// *RetryableError; every other failure is a plain error and stays fatal.
//
// A rate-limit hint of at most InCallWaitCap is slept in-call, replaces the
// backoff, and consumes one attempt. A longer hint, or no hint on a 429, ends
// the call at once with RetryAfter set (the hint capped at RetryAfterCap, or
// RateLimitFloor) so the wait is slept between cycles. A cancelled context
// ends a wait with the context error, never *RetryableError.
func Do(ctx context.Context, client *http.Client, cfg Config, call Call) error {
	sleep := cfg.Sleep
	if sleep == nil {
		sleep = SleepContext
	}
	attempts := max(cfg.MaxAttempts, 1)
	wait := cfg.BaseBackoff
	if wait <= 0 {
		wait = time.Second
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	for attemptNumber := 1; ; attemptNumber++ {
		v, err := attempt(ctx, client, call, now)
		if err != nil {
			return err
		}
		if v.cause == nil {
			recordAttempt(ctx, call.Operation, classNone, outcomeSuccess)
			return nil
		}
		if !v.retryable {
			recordAttempt(ctx, call.Operation, v.class, outcomeFailed)
			return v.cause
		}

		var sleepFor, retryAfter time.Duration
		deferToCycle := false
		switch {
		case v.limited && v.hintKnown:
			hint := max(v.hint, minimumHintWait)
			retryAfter = min(hint, RetryAfterCap)
			if hint > InCallWaitCap {
				deferToCycle = true
			} else {
				sleepFor = hint
			}
		case v.limited:
			retryAfter, deferToCycle = RateLimitFloor, true
		default:
			sleepFor = wait
			if cfg.Jitter != nil {
				sleepFor = cfg.Jitter(wait)
			}
		}
		if attemptNumber >= attempts || deferToCycle {
			recordAttempt(ctx, call.Operation, v.class, outcomeFailed)
			return &RetryableError{
				Operation:  call.Operation,
				Class:      v.class,
				Attempts:   attemptNumber,
				RetryAfter: retryAfter,
				cause:      v.cause,
			}
		}
		recordAttempt(ctx, call.Operation, v.class, outcomeRetried)
		if sleepErr := sleep(ctx, sleepFor); sleepErr != nil {
			return fmt.Errorf("%w (last attempt: %v)", sleepErr, v.cause)
		}
		recordRetry(ctx, call.Operation, v.class, attemptNumber, v.status, sleepFor)
		if !v.limited {
			wait *= 2
		}
	}
}

// attempt issues one request and classifies the result. A non-nil error is a
// fatal request-build failure Do returns as is.
func attempt(ctx context.Context, client *http.Client, call Call, now func() time.Time) (verdict, error) {
	request, err := call.NewRequest(ctx)
	if err != nil {
		return verdict{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		cause := fmt.Errorf("%s: %w", call.FailurePrefix, err)
		return verdict{retryable: IsTransient(ctx, err), class: TransportClass(err), cause: cause}, nil
	}
	status := response.StatusCode
	var readErr error
	if status < 300 {
		readErr = call.Read(response)
	}
	headers := response.Header
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()

	switch {
	case status < 300:
		if readErr == nil {
			return verdict{}, nil
		}
		cause := fmt.Errorf("%s: %w", call.DecodePrefix, readErr)
		if IsTransient(ctx, readErr) {
			return verdict{retryable: true, class: TransportClass(readErr), status: status, cause: cause}, nil
		}
		return verdict{class: "decode", status: status, cause: cause}, nil
	case status >= 500:
		return verdict{
			retryable: true, class: "server_error", status: status,
			cause: fmt.Errorf("%s: status %d", call.FailurePrefix, status),
		}, nil
	case status == http.StatusTooManyRequests || status == http.StatusForbidden:
		hint, hintKnown, signalled := RateLimitHint(headers, now())
		if status == http.StatusTooManyRequests || signalled {
			return verdict{
				retryable: true, class: "rate_limited", status: status, limited: true,
				hint: hint, hintKnown: hintKnown,
				cause: fmt.Errorf("%s: status %d", call.FailurePrefix, status),
			}, nil
		}
	}
	return verdict{
		class: "client_error", status: status,
		cause: fmt.Errorf("%s: status %d", call.FailurePrefix, status),
	}, nil
}
