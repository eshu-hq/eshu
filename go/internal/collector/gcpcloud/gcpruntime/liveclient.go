// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gcpruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"github.com/eshu-hq/eshu/go/internal/collector/gcpcloud"
)

const (
	// CloudAssetInventoryEndpoint is the default Cloud Asset Inventory API
	// endpoint used by LiveClient.
	CloudAssetInventoryEndpoint = "https://cloudasset.googleapis.com"
	// CloudAssetInventoryOAuthScope is the OAuth scope accepted by assets.list.
	CloudAssetInventoryOAuthScope = "https://www.googleapis.com/auth/cloud-platform"
	// DefaultLivePageSize keeps live assets.list pages bounded when the caller
	// does not specify a smaller page size.
	DefaultLivePageSize = 100
	// MaxLivePageSize is the largest pageSize LiveClient sends to assets.list.
	MaxLivePageSize = 1000
	// DefaultLiveResponseBytes caps one assets.list response body before parsing.
	DefaultLiveResponseBytes = int64(16 << 20)
	// DefaultLiveRequestTimeout bounds one assets.list HTTP attempt.
	DefaultLiveRequestTimeout = 30 * time.Second
	// DefaultLiveMaxAttempts bounds retryable Cloud Asset Inventory attempts.
	DefaultLiveMaxAttempts = 3
)

// ProviderWarning is a bounded PageProvider coverage warning. Source converts
// it into a durable gcp_collection_warning fact instead of treating expected
// provider coverage gaps as silent success.
type ProviderWarning struct {
	WarningKind string
	Outcome     string
	Reason      string
	Retryable   bool
	HiddenCount int
	SourceURI   string
}

// Error returns only bounded warning metadata. It never includes provider
// response bodies, credentials, parent identifiers, or resource names.
func (w ProviderWarning) Error() string {
	return fmt.Sprintf("gcp provider warning kind=%s outcome=%s", w.WarningKind, w.Outcome)
}

// LiveClient fetches Cloud Asset Inventory assets.list pages over REST. It is
// never wired as the default provider; callers must explicitly inject it into a
// Source after supplying a least-privilege credential and bounded runtime
// settings. The OAuth scope is the broad scope assets.list accepts; read-only
// behavior is enforced by IAM and by this client's GET-only CAI path.
type LiveClient struct {
	// CredentialRef names the read-only credential resolved out of band. It is a
	// name only and is never sent to Cloud Asset Inventory or telemetry.
	CredentialRef string
	// TokenSource supplies OAuth tokens for assets.list. It must be set
	// explicitly.
	TokenSource oauth2.TokenSource
	// HTTPClient performs REST calls. Nil uses http.DefaultClient.
	HTTPClient *http.Client
	// Endpoint overrides the Cloud Asset Inventory endpoint for tests or
	// controlled deployments. Empty uses CloudAssetInventoryEndpoint.
	Endpoint string
	// ResourceManagerEndpoint overrides the Cloud Resource Manager endpoint for
	// direct/effective tag API calls. Empty uses CloudResourceManagerEndpoint.
	ResourceManagerEndpoint string
	// PageSize bounds assets.list page size. Values <=0 use DefaultLivePageSize;
	// values above MaxLivePageSize are capped.
	PageSize int
	// TagPageSize bounds Resource Manager tag page size. Values <=0 use
	// DefaultLiveTagPageSize; values above MaxLiveTagPageSize are capped.
	TagPageSize int
	// RequestTimeout bounds one HTTP attempt. Values <=0 use
	// DefaultLiveRequestTimeout.
	RequestTimeout time.Duration
	// MaxAttempts bounds retryable attempts. Values <=0 use
	// DefaultLiveMaxAttempts.
	MaxAttempts int
	// MaxResponseBytes bounds one response body. Values <=0 use
	// DefaultLiveResponseBytes.
	MaxResponseBytes int64
	// RetryBackoff returns the sleep before retrying a failed attempt. Nil uses a
	// small bounded exponential backoff.
	RetryBackoff func(attempt int) time.Duration
	// Sleep waits between retry attempts. Nil uses a context-aware timer.
	Sleep func(context.Context, time.Duration) error
}

// NewADCLiveClient builds a LiveClient backed by Application Default
// Credentials. It does not wire the client into any runtime by itself.
func NewADCLiveClient(ctx context.Context, credentialRef string) (LiveClient, error) {
	if strings.TrimSpace(credentialRef) == "" {
		return LiveClient{}, errors.New("gcp live credential_ref is required")
	}
	ts, err := google.DefaultTokenSource(ctx, CloudAssetInventoryOAuthScope)
	if err != nil {
		return LiveClient{}, errors.New("gcp application default token source unavailable")
	}
	return LiveClient{CredentialRef: strings.TrimSpace(credentialRef), TokenSource: ts}, nil
}

// FetchPage fetches and parses one Cloud Asset Inventory assets.list page.
func (c LiveClient) FetchPage(ctx context.Context, req PageRequest) (gcpcloud.AssetsListPage, error) {
	token, err := c.token()
	if err != nil {
		return gcpcloud.AssetsListPage{}, err
	}
	endpoint, err := c.requestURL(req)
	if err != nil {
		return gcpcloud.AssetsListPage{}, err
	}

	attempts := c.maxAttempts()
	for attempt := 1; attempt <= attempts; attempt++ {
		page, retryable, err := c.fetchAttempt(ctx, endpoint, token)
		if err == nil {
			return page, nil
		}
		if !retryable || attempt == attempts {
			return gcpcloud.AssetsListPage{}, err
		}
		if sleepErr := c.sleep(ctx, c.backoff(attempt)); sleepErr != nil {
			return gcpcloud.AssetsListPage{}, sleepErr
		}
	}
	return gcpcloud.AssetsListPage{}, ProviderWarning{
		WarningKind: gcpcloud.WarningKindUnavailable,
		Outcome:     gcpcloud.OutcomeUnavailable,
		Reason:      "cloud asset inventory retry attempts exhausted",
		Retryable:   true,
	}
}

func (c LiveClient) fetchAttempt(
	ctx context.Context,
	endpoint string,
	token *oauth2.Token,
) (gcpcloud.AssetsListPage, bool, error) {
	attemptCtx, cancel := context.WithTimeout(ctx, c.requestTimeout())
	defer cancel()
	httpReq, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, endpoint, nil)
	if err != nil {
		return gcpcloud.AssetsListPage{}, false, errors.New("build gcp live assets.list request")
	}
	token.SetAuthHeader(httpReq)

	resp, err := c.httpClient().Do(httpReq)
	if err != nil {
		if attemptCtx.Err() != nil {
			return gcpcloud.AssetsListPage{}, false, attemptCtx.Err()
		}
		return gcpcloud.AssetsListPage{}, true, ProviderWarning{
			WarningKind: gcpcloud.WarningKindUnavailable,
			Outcome:     gcpcloud.OutcomeUnavailable,
			Reason:      "cloud asset inventory transport unavailable",
			Retryable:   true,
		}
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := readBounded(resp.Body, c.maxResponseBytes())
	if err != nil {
		return gcpcloud.AssetsListPage{}, false, ProviderWarning{
			WarningKind: gcpcloud.WarningKindRedaction,
			Outcome:     gcpcloud.OutcomeUnavailable,
			Reason:      "cloud asset inventory response exceeded size limit",
		}
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		warning := classifyLiveStatus(resp.StatusCode)
		return gcpcloud.AssetsListPage{}, warning.Retryable, warning
	}
	page, err := gcpcloud.ParseAssetsListPage(body)
	if err != nil {
		return gcpcloud.AssetsListPage{}, false, fmt.Errorf("parse gcp live assets.list response: %w", err)
	}
	return page, false, nil
}

func (c LiveClient) token() (*oauth2.Token, error) {
	if c.TokenSource == nil {
		return nil, errors.New("gcp live token source is required")
	}
	token, err := c.TokenSource.Token()
	if err != nil {
		return nil, ProviderWarning{
			WarningKind: gcpcloud.WarningKindPartialPermission,
			Outcome:     gcpcloud.OutcomeUnavailable,
			Reason:      "credential token unavailable",
			Retryable:   true,
		}
	}
	if token == nil || strings.TrimSpace(token.AccessToken) == "" {
		return nil, ProviderWarning{
			WarningKind: gcpcloud.WarningKindPartialPermission,
			Outcome:     gcpcloud.OutcomeUnavailable,
			Reason:      "credential token empty",
			Retryable:   true,
		}
	}
	return token, nil
}

func (c LiveClient) requestURL(req PageRequest) (string, error) {
	parentPath, err := liveParentPath(req.Scope)
	if err != nil {
		return "", err
	}
	contentType, err := liveContentType(req.Scope.ContentFamily)
	if err != nil {
		return "", err
	}
	base := strings.TrimRight(firstNonEmpty(c.Endpoint, CloudAssetInventoryEndpoint), "/")
	values := url.Values{}
	values.Set("contentType", contentType)
	values.Set("pageSize", strconv.Itoa(c.pageSize()))
	if token := strings.TrimSpace(req.PageToken); token != "" {
		values.Set("pageToken", token)
	}
	assetTypes, err := liveAssetTypes(req.Scope.AssetTypeFamily)
	if err != nil {
		return "", err
	}
	for _, assetType := range assetTypes {
		values.Add("assetTypes", assetType)
	}
	return base + "/v1/" + parentPath + "/assets?" + values.Encode(), nil
}

func liveParentPath(scopeCfg ScopeConfig) (string, error) {
	id := strings.TrimSpace(scopeCfg.ParentScopeID)
	if id == "" {
		return "", errors.New("gcp live parent scope id is required")
	}
	if strings.ContainsAny(id, "/?#") {
		return "", errors.New("gcp live parent scope id contains unsupported path characters")
	}
	switch scopeCfg.ParentScopeKind {
	case gcpcloud.ParentScopeProject:
		return "projects/" + url.PathEscape(id), nil
	case gcpcloud.ParentScopeFolder:
		return "folders/" + url.PathEscape(id), nil
	case gcpcloud.ParentScopeOrganization:
		return "organizations/" + url.PathEscape(id), nil
	default:
		return "", fmt.Errorf("gcp live unsupported parent scope kind %q", scopeCfg.ParentScopeKind)
	}
}

func liveContentType(family string) (string, error) {
	contentType := strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(firstNonEmpty(family, "resource")), "-", "_"))
	switch contentType {
	case "RESOURCE", "IAM_POLICY", "ORG_POLICY", "ACCESS_POLICY", "OS_INVENTORY", "RELATIONSHIP":
		return contentType, nil
	default:
		return "", ProviderWarning{
			WarningKind: gcpcloud.WarningKindUnsupported,
			Outcome:     gcpcloud.OutcomeUnsupported,
			Reason:      "cloud asset inventory content family unsupported",
		}
	}
}

func liveAssetTypes(family string) ([]string, error) {
	trimmed := strings.TrimSpace(family)
	if trimmed == "" || trimmed == "mixed" {
		return nil, nil
	}
	parts := strings.Split(trimmed, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		value := strings.TrimSpace(part)
		switch {
		case value == "":
			continue
		case strings.Contains(value, ".googleapis.com/"):
			out = append(out, value)
		case safeAssetFamily(value):
			out = append(out, value+".googleapis.com.*")
		default:
			return nil, ProviderWarning{
				WarningKind: gcpcloud.WarningKindUnsupported,
				Outcome:     gcpcloud.OutcomeUnsupported,
				Reason:      "cloud asset inventory asset family unsupported",
			}
		}
	}
	if len(out) == 0 {
		return nil, ProviderWarning{
			WarningKind: gcpcloud.WarningKindUnsupported,
			Outcome:     gcpcloud.OutcomeUnsupported,
			Reason:      "cloud asset inventory asset family unsupported",
		}
	}
	return out, nil
}

func safeAssetFamily(value string) bool {
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

func classifyLiveStatus(status int) ProviderWarning {
	switch status {
	case http.StatusUnauthorized:
		return ProviderWarning{
			WarningKind: gcpcloud.WarningKindPartialPermission,
			Outcome:     gcpcloud.OutcomeUnavailable,
			Reason:      "credential token rejected by provider",
			Retryable:   true,
		}
	case http.StatusForbidden:
		return ProviderWarning{
			WarningKind: gcpcloud.WarningKindPartialPermission,
			Outcome:     gcpcloud.OutcomePartial,
			Reason:      "cloud asset inventory permission denied",
		}
	case http.StatusBadRequest, http.StatusNotFound, http.StatusNotImplemented:
		return ProviderWarning{
			WarningKind: gcpcloud.WarningKindUnsupported,
			Outcome:     gcpcloud.OutcomeUnsupported,
			Reason:      "cloud asset inventory request unsupported",
		}
	case http.StatusTooManyRequests:
		return ProviderWarning{
			WarningKind: gcpcloud.WarningKindQuota,
			Outcome:     gcpcloud.OutcomeUnavailable,
			Reason:      "cloud asset inventory throttle exhausted",
			Retryable:   true,
		}
	default:
		retryable := status >= http.StatusInternalServerError
		return ProviderWarning{
			WarningKind: gcpcloud.WarningKindUnavailable,
			Outcome:     gcpcloud.OutcomeUnavailable,
			Reason:      "cloud asset inventory source unavailable",
			Retryable:   retryable,
		}
	}
}

func readBounded(reader io.Reader, maxBytes int64) ([]byte, error) {
	limited := io.LimitReader(reader, maxBytes+1)
	body, err := io.ReadAll(limited)
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > maxBytes {
		return nil, errors.New("response body exceeds limit")
	}
	return body, nil
}

func (c LiveClient) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

func (c LiveClient) pageSize() int {
	switch {
	case c.PageSize <= 0:
		return DefaultLivePageSize
	case c.PageSize > MaxLivePageSize:
		return MaxLivePageSize
	default:
		return c.PageSize
	}
}

func (c LiveClient) requestTimeout() time.Duration {
	if c.RequestTimeout <= 0 {
		return DefaultLiveRequestTimeout
	}
	return c.RequestTimeout
}

func (c LiveClient) maxAttempts() int {
	if c.MaxAttempts <= 0 {
		return DefaultLiveMaxAttempts
	}
	return c.MaxAttempts
}

func (c LiveClient) maxResponseBytes() int64 {
	if c.MaxResponseBytes <= 0 {
		return DefaultLiveResponseBytes
	}
	return c.MaxResponseBytes
}

func (c LiveClient) backoff(attempt int) time.Duration {
	if c.RetryBackoff != nil {
		return c.RetryBackoff(attempt)
	}
	delay := time.Duration(100*(1<<(attempt-1))) * time.Millisecond
	if delay > time.Second {
		return time.Second
	}
	return delay
}

func (c LiveClient) sleep(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	if c.Sleep != nil {
		return c.Sleep(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

var _ PageProvider = LiveClient{}
