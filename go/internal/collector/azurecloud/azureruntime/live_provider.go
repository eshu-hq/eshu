// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azureruntime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
)

const (
	defaultLiveResourceGraphPageSize = int32(1000)
	maxLiveResourceGraphPageSize     = int32(1000)
	defaultLiveRequestTimeout        = 30 * time.Second
	defaultLiveMaxRetries            = 2
	defaultLiveBackoffCap            = 30 * time.Second
)

const liveResourceGraphResourcesQuery = `
Resources
| project id, name, type, tenantId, subscriptionId, resourceGroup, location, kind, managedBy, apiVersion, sku, identity, tags
| order by id asc
`

// LiveResourceGraphClient is the read-only Resource Graph transport consumed by
// LiveProviderFactory. The interface is intentionally narrower than Azure SDK
// clients so tests can mock provider behavior without live Azure access, while a
// later SDK wrapper can translate this request into the official Resource Graph
// call.
type LiveResourceGraphClient interface {
	// QueryResources executes one bounded Resource Graph Resources query page.
	QueryResources(context.Context, LiveResourceGraphRequest) (LiveResourceGraphResponse, error)
}

// LiveResourceGraphRequest is one bounded Resource Graph Resources query. It
// carries scopes, page size, and continuation state only; credential references,
// query text, and provider locators must never be copied into telemetry labels.
type LiveResourceGraphRequest struct {
	Query              string
	Subscriptions      []string
	ManagementGroups   []string
	SkipToken          string
	PageSize           int32
	AllowPartialScopes bool
}

// LiveResourceGraphResponse is one Resource Graph query page plus optional
// partial-scope evidence reported by the transport.
type LiveResourceGraphResponse struct {
	Page   azurecloud.ResourceGraphPage
	Access azurecloud.ScopeAccess
}

// LiveProviderFactory builds an explicit, injected live Resource Graph provider
// for one Azure scope target. The zero value remains fail-closed: without a
// ResourceGraphClient, PageProvider returns ErrLiveProviderGated and cannot call
// Azure. Command and Helm wiring must keep using the zero value until the live
// credential/security review is complete.
type LiveProviderFactory struct {
	// ResourceGraphClient executes read-only Resource Graph Resources queries.
	// A nil client keeps the factory gated.
	ResourceGraphClient LiveResourceGraphClient
	// ARMFallbackClient executes optional read-only ARM GET fallback calls for
	// explicitly allowlisted resource families. Supplying it without
	// ARMFallbackRules fails closed.
	ARMFallbackClient LiveARMFallbackClient
	// ARMFallbackRules is the code-owned allowlist for optional ARM GET
	// fallback enrichment. Each rule fixes one resource type, API version, and
	// bounded set of extension fields.
	ARMFallbackRules []LiveARMFallbackRule
	// ARMFallbackMaxExtensionBytes bounds one fallback extension wrapper before
	// it can be added to a Resource Graph row. Zero uses the package default.
	ARMFallbackMaxExtensionBytes int
	// Query overrides the owned Resource Graph KQL. Blank uses the package
	// default, which projects only fields consumed by azurecloud.ResourceRow.
	Query string
	// PageSize is sent as the Resource Graph $top option. Values outside
	// 1..1000 are normalized to the default or cap.
	PageSize int32
	// RequestTimeout bounds each provider call. Zero uses a conservative
	// default.
	RequestTimeout time.Duration
	// MaxRetries bounds retry attempts for throttling. Zero uses the default
	// bounded retry budget.
	MaxRetries int
	// BackoffCap caps provider retry delays. Zero uses the default cap.
	BackoffCap time.Duration
	// Sleep is injectable for tests. Nil uses a context-aware timer.
	Sleep func(context.Context, time.Duration) error
}

// PageProvider returns a read-only Resource Graph page provider when explicitly
// configured with a client. The default zero-value factory returns
// ErrLiveProviderGated so production wiring cannot accidentally issue a live
// Azure request.
func (f LiveProviderFactory) PageProvider(
	_ context.Context,
	_ azurecloud.Boundary,
	target TargetConfig,
) (azurecloud.PageProvider, error) {
	if f.ResourceGraphClient == nil {
		return nil, ErrLiveProviderGated
	}
	if strings.TrimSpace(target.CredentialRef) == "" {
		return nil, fmt.Errorf("azure live provider requires a credential reference")
	}
	lane := strings.TrimSpace(target.SourceLane)
	if lane == "" {
		lane = azurecloud.SourceLaneResourceGraph
	}
	if lane != azurecloud.SourceLaneResourceGraph {
		return nil, fmt.Errorf("azure live provider supports source lane %q only", azurecloud.SourceLaneResourceGraph)
	}
	armRules, err := normalizeLiveARMFallbackRules(f.ARMFallbackRules)
	if err != nil {
		return nil, err
	}
	if len(armRules) > 0 && f.ARMFallbackClient == nil {
		return nil, fmt.Errorf("azure live ARM fallback requires an injected read-only client")
	}
	if f.ARMFallbackClient != nil && len(armRules) == 0 {
		return nil, fmt.Errorf("azure live ARM fallback requires at least one allowlist rule")
	}
	return &liveResourceGraphProvider{
		client:               f.ResourceGraphClient,
		armClient:            f.ARMFallbackClient,
		armRules:             armRules,
		armMaxExtensionBytes: normalizedLiveARMFallbackMaxExtensionBytes(f.ARMFallbackMaxExtensionBytes),
		target:               target,
		query:                liveQuery(f.Query),
		pageSize:             normalizedLivePageSize(f.PageSize),
		requestTimeout:       normalizedLiveTimeout(f.RequestTimeout),
		maxRetries:           normalizedLiveRetries(f.MaxRetries),
		backoffCap:           normalizedLiveBackoffCap(f.BackoffCap),
		sleep:                normalizedLiveSleep(f.Sleep),
	}, nil
}

type liveResourceGraphProvider struct {
	client               LiveResourceGraphClient
	armClient            LiveARMFallbackClient
	armRules             map[string]liveARMFallbackRule
	armMaxExtensionBytes int
	target               TargetConfig
	query                string
	pageSize             int32
	requestTimeout       time.Duration
	maxRetries           int
	backoffCap           time.Duration
	sleep                func(context.Context, time.Duration) error

	mu     sync.Mutex
	access azurecloud.ScopeAccess
}

func (p *liveResourceGraphProvider) NextPage(
	ctx context.Context,
	skipToken string,
) (azurecloud.ResourceGraphPage, error) {
	request := p.request(skipToken)
	for attempt := 0; ; attempt++ {
		response, err := p.queryResources(ctx, request)
		if err == nil {
			p.mergeAccess(response.Access)
			page := response.Page
			if err := p.applyARMFallbacks(ctx, &page); err != nil {
				return azurecloud.ResourceGraphPage{}, err
			}
			return page, nil
		}
		liveErr, ok := classifyLiveProviderError(err)
		if !ok {
			return azurecloud.ResourceGraphPage{}, fmt.Errorf("query azure resource graph: %w", err)
		}
		switch liveErr.kind {
		case liveProviderErrorThrottled:
			p.mergeAccess(azurecloud.ScopeAccess{
				Partial: true,
				Reason:  azurecloud.WarningThrottled,
				Message: "resource graph throttled; scan results may be partial",
			})
			if attempt < p.maxRetries {
				if err := p.sleep(ctx, p.boundedBackoff(liveErr.retryAfter)); err != nil {
					return azurecloud.ResourceGraphPage{}, err
				}
				continue
			}
			return azurecloud.ResourceGraphPage{}, nil
		case liveProviderErrorSkipTokenExpired:
			p.mergeAccess(azurecloud.ScopeAccess{
				Partial: true,
				Reason:  azurecloud.WarningStale,
				Message: "resource graph continuation token expired; rerun the scan",
			})
			return azurecloud.ResourceGraphPage{}, nil
		case liveProviderErrorTokenExpired:
			p.mergeAccess(azurecloud.ScopeAccess{
				Partial: true,
				Reason:  azurecloud.WarningStale,
				Message: "resource graph token expired; rerun the scan",
			})
			return azurecloud.ResourceGraphPage{}, nil
		case liveProviderErrorPermissionHidden:
			p.mergeAccess(azurecloud.ScopeAccess{
				Partial:             true,
				HiddenResourceCount: liveErr.hiddenResourceCount,
				Reason:              azurecloud.WarningPermissionHidden,
				Message:             "configured resources were hidden from the read-only identity",
			})
			return azurecloud.ResourceGraphPage{}, nil
		case liveProviderErrorUnsupported:
			p.mergeAccess(azurecloud.ScopeAccess{
				Partial: true,
				Reason:  azurecloud.WarningUnsupported,
				Message: "resource graph does not expose the requested resource family",
			})
			return azurecloud.ResourceGraphPage{}, nil
		default:
			return azurecloud.ResourceGraphPage{}, fmt.Errorf("query azure resource graph: %w", err)
		}
	}
}

func (p *liveResourceGraphProvider) ScopeAccess(context.Context) azurecloud.ScopeAccess {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.access
}

func (p *liveResourceGraphProvider) request(skipToken string) LiveResourceGraphRequest {
	request := LiveResourceGraphRequest{
		Query:              p.query,
		SkipToken:          strings.TrimSpace(skipToken),
		PageSize:           p.pageSize,
		AllowPartialScopes: true,
	}
	switch p.target.ScopeKind {
	case azurecloud.ScopeKindSubscription:
		request.Subscriptions = []string{p.target.ProviderScopeID}
	case azurecloud.ScopeKindManagementGroup:
		request.ManagementGroups = []string{p.target.ProviderScopeID}
	}
	return request
}

func (p *liveResourceGraphProvider) queryResources(
	ctx context.Context,
	request LiveResourceGraphRequest,
) (LiveResourceGraphResponse, error) {
	callCtx, cancel := context.WithTimeout(ctx, p.requestTimeout)
	defer cancel()
	return p.client.QueryResources(callCtx, request)
}

func (p *liveResourceGraphProvider) boundedBackoff(retryAfter time.Duration) time.Duration {
	if retryAfter <= 0 {
		return p.backoffCap
	}
	if retryAfter > p.backoffCap {
		return p.backoffCap
	}
	return retryAfter
}

func (p *liveResourceGraphProvider) mergeAccess(access azurecloud.ScopeAccess) {
	if !access.Partial {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.access.HiddenResourceCount < access.HiddenResourceCount {
		p.access.HiddenResourceCount = access.HiddenResourceCount
	}
	if p.access.Reason == "" || accessWarningPriority(access.Reason) > accessWarningPriority(p.access.Reason) {
		p.access.Reason = access.Reason
		p.access.Message = access.Message
	}
	if p.access.Message == "" {
		p.access.Message = access.Message
	}
	p.access.Partial = true
}

func accessWarningPriority(reason string) int {
	if reason == azurecloud.WarningFallbackSkipped {
		return 0
	}
	return 1
}

type liveProviderErrorKind string

const (
	liveProviderErrorThrottled        liveProviderErrorKind = "throttled"
	liveProviderErrorSkipTokenExpired liveProviderErrorKind = "skip_token_expired" // #nosec G101 -- error-kind label for a skipped expired-token case, not a credential value
	liveProviderErrorTokenExpired     liveProviderErrorKind = "token_expired"      // #nosec G101 -- error-kind label for an expired auth token, not a credential value
	liveProviderErrorPermissionHidden liveProviderErrorKind = "permission_hidden"
	liveProviderErrorUnsupported      liveProviderErrorKind = "unsupported"
)

type liveProviderError struct {
	kind                liveProviderErrorKind
	retryAfter          time.Duration
	hiddenResourceCount int
	err                 error
}

func (e liveProviderError) Error() string {
	if e.err != nil {
		return string(e.kind) + ": " + e.err.Error()
	}
	return string(e.kind)
}

func (e liveProviderError) Unwrap() error {
	return e.err
}

func classifyLiveProviderError(err error) (liveProviderError, bool) {
	var liveErr liveProviderError
	if errors.As(err, &liveErr) {
		return liveErr, true
	}
	if sdkErr, ok := classifyAzureSDKError(err); ok {
		return sdkErr, true
	}
	return liveProviderError{}, false
}

func liveQuery(query string) string {
	if trimmed := strings.TrimSpace(query); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(liveResourceGraphResourcesQuery)
}

func normalizedLivePageSize(value int32) int32 {
	switch {
	case value <= 0:
		return defaultLiveResourceGraphPageSize
	case value > maxLiveResourceGraphPageSize:
		return maxLiveResourceGraphPageSize
	default:
		return value
	}
}

func normalizedLiveTimeout(value time.Duration) time.Duration {
	if value <= 0 {
		return defaultLiveRequestTimeout
	}
	return value
}

func normalizedLiveRetries(value int) int {
	if value <= 0 {
		return defaultLiveMaxRetries
	}
	return value
}

func normalizedLiveBackoffCap(value time.Duration) time.Duration {
	if value <= 0 {
		return defaultLiveBackoffCap
	}
	return value
}

func normalizedLiveSleep(
	sleep func(context.Context, time.Duration) error,
) func(context.Context, time.Duration) error {
	if sleep != nil {
		return sleep
	}
	return func(ctx context.Context, d time.Duration) error {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return nil
		}
	}
}
