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

// APIClient wraps HTTP calls to the Go API.
type APIClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
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
	return c.do("GET", path, nil, result)
}

// Post performs a POST request with JSON body and decodes JSON response.
func (c *APIClient) Post(path string, body, result any) error {
	return c.do("POST", path, body, result)
}

func (c *APIClient) do(method, path string, body, result any) error {
	url := c.BaseURL + path

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
		return fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return fmt.Errorf("API error %d: %s", resp.StatusCode, string(respBody))
	}

	if result != nil {
		if err := json.Unmarshal(respBody, result); err != nil {
			return fmt.Errorf("decode response: %w", err)
		}
	}
	return nil
}
