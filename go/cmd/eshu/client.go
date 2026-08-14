// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const eshuEnvelopeMIMEType = "application/eshu.envelope+json"

// APIClient wraps HTTP calls to the Go API.
type APIClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
}

type apiHTTPError struct {
	StatusCode int
	Body       string
}

func (e *apiHTTPError) Error() string {
	return fmt.Sprintf("API error %d: %s", e.StatusCode, e.Body)
}

// NewAPIClient creates a client from environment/config.
// Resolution order: flags -> env -> config file.
func NewAPIClient(serviceURL, apiKey, profile string) *APIClient {
	base := serviceURL
	if base == "" {
		base = resolveConfigValue("ESHU_SERVICE_URL", profile)
	}
	if base == "" {
		base = os.Getenv("ESHU_SERVICE_URL")
	}
	if base == "" {
		base = "http://localhost:8080"
	}
	base = strings.TrimRight(base, "/")

	key := apiKey
	if key == "" {
		key = resolveConfigValue("ESHU_API_KEY", profile)
	}
	if key == "" {
		key = os.Getenv("ESHU_API_KEY")
	}

	timeoutStr := os.Getenv("ESHU_REMOTE_TIMEOUT_SECONDS")
	timeout := 30 * time.Second
	if timeoutStr != "" {
		if d, err := time.ParseDuration(timeoutStr + "s"); err == nil {
			timeout = d
		}
	}

	return &APIClient{
		BaseURL: base,
		APIKey:  key,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// Get performs a GET request and decodes JSON response.
func (c *APIClient) Get(path string, result any) error {
	return c.do("GET", path, nil, result, "")
}

// GetEnvelope performs a GET request that asks for Eshu's canonical envelope.
func (c *APIClient) GetEnvelope(path string, result any) error {
	return c.do("GET", path, nil, result, eshuEnvelopeMIMEType)
}

// Post performs a POST request with JSON body and decodes JSON response.
func (c *APIClient) Post(path string, body, result any) error {
	return c.do("POST", path, body, result, "")
}

// PostEnvelope performs a POST request that asks for Eshu's canonical envelope.
func (c *APIClient) PostEnvelope(path string, body, result any) error {
	return c.do("POST", path, body, result, eshuEnvelopeMIMEType)
}

func (c *APIClient) do(method, path string, body, result any, accept string) error {
	url := c.BaseURL + path

	// The request URL is the one string here that concentrates secrets: the
	// configured base URL may carry userinfo credentials, and callers append a
	// query string built from caller-supplied parameters (api keys, tokens).
	// net/http embeds that whole URL in the errors below, so every transport
	// failure has to be redacted before it reaches a terminal or a CI log.
	//
	// safePath is the value substituted in its place. Everything a secret can
	// ride on is dropped: the base URL (and with it any userinfo) and the query
	// string. What remains is the bare endpoint path, which is also what a
	// reader needs to act on the failure.
	//
	// Residual, deliberately not covered: the path SEGMENTS are echoed
	// verbatim. A caller that puts a secret in the path itself rather than in
	// the query or a header still prints it. That boundary is the same one the
	// rest of the CLI assumes, and narrowing further would leave an error no
	// operator could act on.
	safePath, _, _ := strings.Cut(path, "?")

	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		// This is the worse of the two leaking sites. http.NewRequest returns
		// url.Parse's *url.Error unchanged, and that one carries the RAW URL --
		// net/http's stripPassword only runs inside Client.Do, so a URL that
		// fails to parse prints the userinfo PASSWORD in cleartext.
		return fmt.Errorf("create request: %w", requestErrorWithoutURL(err, safePath))
	}
	req.Header.Set("Content-Type", "application/json")
	if accept != "" {
		req.Header.Set("Accept", accept)
	}
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		// Client.Do wraps the failure in a *url.Error whose URL has been through
		// stripPassword. That masks the userinfo password only -- the username
		// and the ENTIRE query string survive, so this still leaks without the
		// redaction below.
		return fmt.Errorf("request failed: %w", requestErrorWithoutURL(err, safePath))
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return &apiHTTPError{StatusCode: resp.StatusCode, Body: string(respBody)}
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
