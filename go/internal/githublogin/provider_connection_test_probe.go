// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package githublogin

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/secretcrypto"
)

// ConnectionTestResult reports a bounded, safe test-connection outcome.
// Detail never carries a secret or plaintext credential.
type ConnectionTestResult struct {
	OK     bool
	Detail string
}

// reachFunc abstracts a GitHub API reachability probe so TestConnection is
// testable without a live network call. Production callers pass nil to
// probe the real REST API root (GET {apiBaseURL}/ — a real, unauthenticated
// GitHub endpoint that returns 200 for both github.com and a reachable
// GitHub Enterprise Server instance).
type reachFunc func(ctx context.Context, apiBaseURL string) error

// TestConnection validates a GitHub provider's API-host reachability and the
// decrypted client secret's basic shape.
//
// This is the ONLY (*secretcrypto.Keyring).Open call site in this package
// besides ResolveSealedProviderConfig: it decrypts sealedSecret transiently,
// in-process, uses it only to confirm it decodes to a well-formed, non-empty
// client_secret, and discards it immediately — never logged, returned, or
// serialized.
//
// Explicit scope note (matching oidclogin.TestConnection's own documented
// scope): this does NOT perform a live OAuth2 authorization-code round
// trip — that requires an interactive user/browser session with GitHub and
// cannot be safely automated from an admin API call. What it proves: (1)
// apiBaseURL is reachable (the same host FetchIdentity calls during real
// login), and (2) the stored secret decrypts to a non-empty client_secret.
func TestConnection(
	ctx context.Context,
	keyring *secretcrypto.Keyring,
	providerConfigID, revisionID, apiBaseURL, sealedSecret string,
) (ConnectionTestResult, error) {
	return testConnection(ctx, keyring, providerConfigID, revisionID, apiBaseURL, sealedSecret, nil)
}

func testConnection(
	ctx context.Context,
	keyring *secretcrypto.Keyring,
	providerConfigID, revisionID, apiBaseURL, sealedSecret string,
	reach reachFunc,
) (ConnectionTestResult, error) {
	if keyring == nil {
		return ConnectionTestResult{}, fmt.Errorf("githublogin: connection test requires a configured keyring")
	}
	apiBaseURL = strings.TrimSpace(apiBaseURL)
	if apiBaseURL == "" {
		apiBaseURL = defaultAPIBaseURL
	}
	if reach == nil {
		reach = probeAPIRoot
	}
	if err := reach(ctx, apiBaseURL); err != nil {
		return ConnectionTestResult{OK: false, Detail: "github api host unreachable"}, nil
	}

	plaintext, err := keyring.Open(sealedSecret, []byte(ProviderSecretAAD(providerConfigID, revisionID)))
	if err != nil {
		return ConnectionTestResult{OK: false, Detail: "stored secret failed to decrypt"}, nil
	}
	var secret dbProviderSecret
	if err := json.Unmarshal(plaintext, &secret); err != nil || strings.TrimSpace(secret.ClientSecret) == "" {
		return ConnectionTestResult{OK: false, Detail: "stored secret is not a valid client_secret payload"}, nil
	}

	return ConnectionTestResult{OK: true, Detail: "github api host reachable; client_secret present"}, nil
}

func probeAPIRoot(ctx context.Context, apiBaseURL string) error {
	client := &http.Client{Timeout: githubHTTPTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(apiBaseURL, "/")+"/", nil)
	if err != nil {
		return fmt.Errorf("build github api root probe request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", githubAPIVersion)
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("call github api root probe: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 500 {
		return fmt.Errorf("github api root returned status %d", resp.StatusCode)
	}
	return nil
}
