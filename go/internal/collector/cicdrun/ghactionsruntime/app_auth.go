// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ghactionsruntime

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"

	"github.com/eshu-hq/eshu/go/internal/github/app"
)

// resolveClaimCredential obtains a fresh bearer for each claimed generation
// when the target uses a host token file or direct App credentials. The local
// copy shares the bearer across run and deployment reads without changing the
// long-lived configured target map.
func resolveClaimCredential(ctx context.Context, target TargetConfig) (TargetConfig, error) {
	if target.AuthMode == AuthModeHostMinted {
		token, err := ResolveHostTokenFile(target.HostTokenFile, target.ScopeID)
		if err != nil {
			return TargetConfig{}, err
		}
		// The provider client validates the claim-local copy again before
		// each bounded read. Present the resolved bearer as token_env there;
		// retain host mode on the configured target for the next claim.
		target.Token = token
		target.AuthMode = AuthModeTokenEnv
		target.HostTokenFile = ""
		return target, nil
	}
	if target.AuthMode != AuthModeGitHubApp {
		return target, nil
	}
	token, err := app.MintInstallationToken(ctx, app.Config{
		BaseURL:        target.APIBaseURL,
		AppID:          target.GitHubAppID,
		InstallationID: target.GitHubAppInstallationID,
		PrivateKey:     target.GitHubAppPrivateKey,
		HTTPClient:     target.AppHTTPClient,
	})
	if err != nil {
		var retryable *app.RetryableError
		if errors.As(err, &retryable) && retryable.Class == app.ClassRateLimited {
			return TargetConfig{}, fmt.Errorf("github app mint rate limited: %w: %w",
				RateLimitError{RetryAfter: retryable.RetryAfter}, err)
		}
		return TargetConfig{}, err
	}
	target.Token = token
	target.AuthMode = AuthModeTokenEnv
	target.GitHubAppID = ""
	target.GitHubAppInstallationID = ""
	target.GitHubAppPrivateKey = ""
	return target, nil
}

// classifyAppReadFailure retries an expired installation token on the next
// fenced claim attempt. Other provider failures retain their original class.
func classifyAppReadFailure(err error, appMode bool) error {
	if !appMode || err == nil {
		return err
	}
	var provider sdk.HTTPError
	if errors.As(err, &provider) && provider.StatusCode == http.StatusUnauthorized {
		return appReadExpiredError{cause: err}
	}
	return err
}

// appReadExpiredError asks the fenced claim runner to retry a GitHub App
// provider-read 401 with a newly minted installation token. The underlying
// bounded HTTP error remains available for status-class telemetry.
type appReadExpiredError struct {
	cause error
}

func (e appReadExpiredError) Error() string   { return "github actions installation token rejected" }
func (e appReadExpiredError) Unwrap() error   { return e.cause }
func (e appReadExpiredError) Retryable() bool { return true }

// classifyReadFailure preserves the terminal host refusal and retryable App
// expiry contracts without altering static-token provider errors.
func classifyReadFailure(err error, appMode, hostMode bool) error {
	if hostMode && err != nil {
		return sdk.ClassifyProviderFailure("github_actions", err, sdk.StatusPolicy{}, sdk.FailureRetryable)
	}
	return classifyAppReadFailure(err, appMode)
}
