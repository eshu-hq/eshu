// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
	"github.com/eshu-hq/eshu/go/internal/github/app"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// HostMintedGitHubTokenKey is the stdio Request.Config key carrying a
// host-minted GitHub App installation token for one claim. The host sets it
// per claim when Config.TokenProvider is configured; the satellite spends it
// as its Bearer [REDACTED] never reads the App private key. Static activation
// config must not define this key when a provider is set: NewSource rejects
// that combination fail-closed. Without a provider the key is never set and
// the static config (including token_env) passes through untouched.
//
// Wire shape per arbiter ruling on #9182: an object {token, scope_id} under
// the top-level config object, not a bare string, so optional fields can be
// added later without a breaking change. The satellite must reject a missing
// key, an empty token, or a scope_id mismatch terminal.
const HostMintedGitHubTokenKey = "host_github_app_token"

// TokenProvider mints one GitHub App installation token for a claimed work
// item. The host owns the App credentials: implementations close over the App
// ID, installation mapping, and private key host-side (for example by calling
// app.MintInstallationToken) and return only the minted token, so the key
// never crosses the stdio boundary. It receives the claimed item so one
// provider can select the installation per target (per scope ID) instead of
// needing one Source per installation.
//
// A returned error is classified by classifyTokenProviderError: bounded
// github/app faults keep their chain, while any other provider error is
// replaced by a bounded terminal denial so unbounded third-party text never
// reaches logs or status records. Implementations must not return the token,
// key, or credential in the error.
type TokenProvider func(ctx context.Context, item workflow.WorkItem) (string, error)

// AppTokenProviderConfig configures NewAppTokenProvider. AppID and PrivateKey
// are the host-owned GitHub App credentials; they never cross the stdio
// boundary. Installations maps a claimed scope ID to the installation that
// serves it, so one provider covers every planned caller without one Source
// per installation. BaseURL overrides the GitHub REST base (empty means the
// production default); HTTPClient overrides the mint transport (nil means a
// 15-second client) and exists as a test seam.
type AppTokenProviderConfig struct {
	AppID         string
	PrivateKey    string
	Installations map[string]string
	BaseURL       string
	HTTPClient    *http.Client
}

// NewAppTokenProvider builds the production TokenProvider: per claim it
// selects the installation for the claimed scope ID and mints one short-lived
// installation token with app.MintInstallationToken. An unknown scope fails
// closed with a bounded *app.ConfigError (terminal auth_denied downstream);
// the App key stays host-side and only the minted token crosses to the
// satellite.
func NewAppTokenProvider(cfg AppTokenProviderConfig) (TokenProvider, error) {
	appID := strings.TrimSpace(cfg.AppID)
	privateKey := strings.TrimSpace(cfg.PrivateKey)
	if appID == "" || privateKey == "" {
		return nil, app.NewConfigError("github app token provider requires an app id and a private key")
	}
	if len(cfg.Installations) == 0 {
		return nil, app.NewConfigError("github app token provider requires at least one installation")
	}
	installations := make(map[string]string, len(cfg.Installations))
	for scopeID, installationID := range cfg.Installations {
		scopeID = strings.TrimSpace(scopeID)
		installationID = strings.TrimSpace(installationID)
		if scopeID == "" || installationID == "" {
			return nil, app.NewConfigError("github app token provider requires non-blank scope and installation ids")
		}
		installations[scopeID] = installationID
	}
	return func(ctx context.Context, item workflow.WorkItem) (string, error) {
		installationID, ok := installations[strings.TrimSpace(item.ScopeID)]
		if !ok {
			return "", app.NewConfigError("no github app installation for this claim scope")
		}
		return app.MintInstallationToken(ctx, app.Config{
			BaseURL:        cfg.BaseURL,
			AppID:          appID,
			InstallationID: installationID,
			PrivateKey:     privateKey,
			HTTPClient:     cfg.HTTPClient,
		})
	}, nil
}

// mintHostToken runs the Source's provider for one claim. A nil provider
// returns "" with no error, leaving the static config untouched.
func (s *Source) mintHostToken(ctx context.Context, item workflow.WorkItem) (string, error) {
	if s.tokenProvider == nil {
		return "", nil
	}
	token, err := s.tokenProvider(ctx, item)
	if err != nil {
		return "", classifyTokenProviderError(err)
	}
	if strings.TrimSpace(token) == "" {
		return "", extensionFailure{
			class:    string(sdk.FailureAuthDenied),
			terminal: true,
			cause:    errors.New("github app token provider returned no token"),
		}
	}
	return token, nil
}

// injectHostToken sets the minted token on a per-claim request config clone.
// A present key fails closed: construction already rejects the combination,
// so this only fires on a mutated clone. The value is the arbiter-ruled
// object {token, scope_id}; the key is host-owned and reserved.
func injectHostToken(config map[string]any, token, scopeID string) (map[string]any, error) {
	if _, reserved := config[HostMintedGitHubTokenKey]; reserved {
		return nil, extensionFailure{
			class:    string(sdk.FailureTerminal),
			terminal: true,
			cause:    errors.New("extension config must not define the host-minted token key"),
		}
	}
	if config == nil {
		config = make(map[string]any, 1)
	}
	config[HostMintedGitHubTokenKey] = map[string]any{
		"token":    token,
		"scope_id": scopeID,
	}
	return config, nil
}

// classifyTokenProviderError maps a mint failure onto the bounded claim
// failure vocabulary without carrying credentials: an unknown installation,
// revoked credentials, or misconfiguration is a terminal denial (no retry
// storm); an outlasted transient or rate limit stays retryable with the
// provider's backoff; a cancelled context stays retryable. It mirrors the
// in-process mapping in securityalerts.classifyMintError so both runtimes
// classify the same fault the same way.
func classifyTokenProviderError(err error) extensionFailure {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return extensionFailure{
			class: string(sdk.FailureRetryable),
			cause: err,
		}
	}
	var retryable *app.RetryableError
	if errors.As(err, &retryable) {
		class := retryable.Class
		if strings.TrimSpace(class) == "" {
			class = string(sdk.FailureRetryable)
		}
		return extensionFailure{
			class:      class,
			retryAfter: retryable.RetryAfter,
			cause:      err,
		}
	}
	var denied *app.AuthDeniedError
	if errors.As(err, &denied) {
		return extensionFailure{
			class:    string(sdk.FailureAuthDenied),
			terminal: true,
			cause:    err,
		}
	}
	var notFound *app.NotFoundError
	if errors.As(err, &notFound) {
		return extensionFailure{
			class:    string(sdk.FailureAuthDenied),
			terminal: true,
			cause:    err,
		}
	}
	var configErr *app.ConfigError
	if errors.As(err, &configErr) {
		return extensionFailure{
			class:    string(sdk.FailureAuthDenied),
			terminal: true,
			cause:    err,
		}
	}
	return extensionFailure{
		class:    string(sdk.FailureAuthDenied),
		terminal: true,
		cause:    errors.New("github app token mint denied"),
	}
}
