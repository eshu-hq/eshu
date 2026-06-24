// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"time"
)

const defaultHTTPTimeout = 30 * time.Second

// ErrStatusHandled lets a caller mark a non-2xx response as provider-specific
// partial coverage instead of a request failure.
var ErrStatusHandled = errors.New("collector status handled")

// HTTPDoer is the subset of http.Client used by SDK request helpers.
type HTTPDoer interface {
	Do(*http.Request) (*http.Response, error)
}

// HTTPError carries bounded HTTP provider failure details.
type HTTPError struct {
	Provider        string
	StatusCode      int
	Message         string
	RetryAfter      time.Duration
	RateLimitReason string
	Cause           error
}

// Error returns a bounded HTTP failure string without provider response bodies.
func (e HTTPError) Error() string {
	provider := strings.TrimSpace(e.Provider)
	if provider == "" {
		provider = "collector"
	}
	if e.StatusCode != 0 {
		return fmt.Sprintf("%s request failed with status %d", provider, e.StatusCode)
	}
	message := strings.TrimSpace(e.Message)
	if message == "" {
		message = "request failed"
	}
	return provider + " " + message
}

// Unwrap returns the underlying transport, encode, or decode cause.
func (e HTTPError) Unwrap() error {
	return e.Cause
}

// RetryAfterDelay returns provider retry guidance parsed from Retry-After.
func (e HTTPError) RetryAfterDelay() time.Duration {
	return e.RetryAfter
}

// JSONRequest configures one bounded JSON HTTP request.
type JSONRequest struct {
	Provider    string
	Method      string
	BaseURL     *url.URL
	PathPrefix  string
	Endpoint    string
	Query       url.Values
	Body        any
	Out         any
	Client      HTTPDoer
	Headers     func(*http.Request)
	MaxRetries  int
	RetryStatus func(int) bool
	OnRetry     func(*http.Response, int)
	StatusError func(*http.Response) error
	Decode      func(io.Reader) error
}

// ParseBaseURL validates a provider base URL without checking credentials.
func ParseBaseURL(provider string, rawURL string) (*url.URL, error) {
	base, err := url.Parse(strings.TrimRight(strings.TrimSpace(rawURL), "/"))
	if err != nil {
		return nil, fmt.Errorf("parse %s base_url: %w", strings.TrimSpace(provider), err)
	}
	if base.Scheme == "" || base.Host == "" {
		return nil, fmt.Errorf("%s base_url must include scheme and host", strings.TrimSpace(provider))
	}
	if base.User != nil {
		return nil, fmt.Errorf("%s base_url must not include credentials", strings.TrimSpace(provider))
	}
	return base, nil
}

// DefaultHTTPClient returns the default bounded collector HTTP client.
func DefaultHTTPClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	return &http.Client{Timeout: timeout}
}

// ParseRetryAfter parses a Retry-After seconds or HTTP-date header value.
func ParseRetryAfter(raw string, now time.Time) time.Duration {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0
	}
	if seconds, err := strconv.Atoi(trimmed); err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	if now.IsZero() {
		now = time.Now()
	}
	if value, err := http.ParseTime(trimmed); err == nil {
		delay := value.Sub(now)
		if delay > 0 {
			return delay
		}
	}
	return 0
}

// ParseRetryAfterHeader parses a Retry-After header against the current time.
func ParseRetryAfterHeader(raw string) time.Duration {
	return ParseRetryAfter(raw, time.Now())
}

// ShouldRetryStatus reports whether a provider HTTP status is retryable.
func ShouldRetryStatus(statusCode int) bool {
	return statusCode == http.StatusTooManyRequests || statusCode >= http.StatusInternalServerError
}

// DoJSON executes a bounded JSON request with optional retry handling.
func DoJSON(ctx context.Context, request JSONRequest) error {
	method := strings.TrimSpace(request.Method)
	if method == "" {
		method = http.MethodGet
	}
	retryStatus := request.RetryStatus
	if retryStatus == nil {
		retryStatus = ShouldRetryStatus
	}
	client := request.Client
	if client == nil {
		client = http.DefaultClient
	}
	for attempt := 0; ; attempt++ {
		httpRequest, err := buildRequest(ctx, request, method)
		if err != nil {
			return HTTPError{Provider: request.Provider, Message: "build request", Cause: err}
		}
		response, err := client.Do(httpRequest)
		if err != nil {
			return HTTPError{Provider: request.Provider, Message: "request failed", Cause: err}
		}
		if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
			if retryStatus(response.StatusCode) && attempt < request.MaxRetries {
				if request.OnRetry != nil {
					request.OnRetry(response, attempt)
				}
				closeBody(response.Body)
				continue
			}
			statusErr := statusError(request, response)
			closeBody(response.Body)
			return statusErr
		}
		err = decodeBody(request, response.Body)
		closeBody(response.Body)
		return err
	}
}

func buildRequest(ctx context.Context, request JSONRequest, method string) (*http.Request, error) {
	if request.BaseURL == nil {
		return nil, fmt.Errorf("base_url is required")
	}
	var body io.Reader
	if request.Body != nil {
		encoded, err := json.Marshal(request.Body)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}
	reqURL := *request.BaseURL
	reqURL.Path = joinURLPath(reqURL.Path, request.PathPrefix, request.Endpoint)
	reqURL.RawQuery = request.Query.Encode()
	httpRequest, err := http.NewRequestWithContext(ctx, method, reqURL.String(), body)
	if err != nil {
		return nil, err
	}
	httpRequest.Header.Set("Accept", "application/json")
	if request.Body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	if request.Headers != nil {
		request.Headers(httpRequest)
	}
	return httpRequest, nil
}

func joinURLPath(parts ...string) string {
	clean := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.Trim(part, "/"); trimmed != "" {
			clean = append(clean, trimmed)
		}
	}
	if len(clean) == 0 {
		return ""
	}
	return "/" + path.Join(clean...)
}

func statusError(request JSONRequest, response *http.Response) error {
	if request.StatusError != nil {
		if err := request.StatusError(response); err != nil {
			return err
		}
	}
	return HTTPError{
		Provider:        request.Provider,
		StatusCode:      response.StatusCode,
		Message:         http.StatusText(response.StatusCode),
		RetryAfter:      ParseRetryAfterHeader(response.Header.Get("Retry-After")),
		RateLimitReason: strings.TrimSpace(response.Header.Get("RateLimit-Reason")),
	}
}

func decodeBody(request JSONRequest, body io.Reader) error {
	if request.Decode != nil {
		if err := request.Decode(body); err != nil {
			return HTTPError{Provider: request.Provider, Message: "decode response", Cause: err}
		}
		return nil
	}
	if request.Out == nil {
		return nil
	}
	if err := json.NewDecoder(body).Decode(request.Out); err != nil {
		return HTTPError{Provider: request.Provider, Message: "decode response", Cause: err}
	}
	return nil
}

func closeBody(body io.Closer) {
	if body != nil {
		_ = body.Close()
	}
}
