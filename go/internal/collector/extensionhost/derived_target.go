// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/github/app"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// HostDerivedTargetKey is the stdio Request.Config key carrying the target the
// coordinator derived for one claim, as an object. For a Go reachability claim it
// holds scope_id, repository_id, source_commit_sha, and clone_url. The host owns
// the key: static activation config must not define it while a resolver is set,
// so config cannot forge a commit or a remote. A scope id names no URL, so this
// per-claim config is how the component learns what to fetch.
const HostDerivedTargetKey = "host_derived_target"

// DerivedTargetResolver returns the derived target for one claimed work item, or
// nil for a scope that has none. An error wrapping
// collector.ErrSlotScopeTransport fails the claim terminal before the component
// launches; any other error fails it retryable.
type DerivedTargetResolver func(ctx context.Context, item workflow.WorkItem) (map[string]any, error)

// InstallationKeyResolver returns the installation-map key for a claim whose
// scope id cannot name its installation, or "" when the claim has no such key.
type InstallationKeyResolver func(ctx context.Context, item workflow.WorkItem) (string, error)

// RunScopeSetReader returns the persisted requested_scope_set JSON for one
// workflow run. The postgres WorkflowControlStore serves it.
type RunScopeSetReader interface {
	GetRunRequestedScopeSet(ctx context.Context, runID string) (string, error)
}

// GoReachabilityTargetResolver delivers a Go reachability claim's target from
// the run row the coordinator persisted at plan time. Every other scope returns
// nil with no store read, so its claim stays byte-identical.
func GoReachabilityTargetResolver(store RunScopeSetReader) DerivedTargetResolver {
	return func(ctx context.Context, item workflow.WorkItem) (map[string]any, error) {
		target, ok, err := goReachabilityTarget(ctx, store, item)
		if err != nil || !ok {
			return nil, err
		}
		return map[string]any{
			"scope_id":          target.ScopeID,
			"repository_id":     target.RepositoryID,
			"source_commit_sha": target.SourceCommitSHA,
			"clone_url":         target.CloneURL,
		}, nil
	}
}

// GoReachabilityInstallationKey selects a Go reachability claim's installation
// from the host and owner of its planned remote. Every other scope returns "",
// so an owner key can never serve an unrelated claim.
func GoReachabilityInstallationKey(store RunScopeSetReader) InstallationKeyResolver {
	return func(ctx context.Context, item workflow.WorkItem) (string, error) {
		target, ok, err := goReachabilityTarget(ctx, store, item)
		if err != nil || !ok {
			return "", err
		}
		return target.InstallationKey(), nil
	}
}

// goReachabilityTarget reads and validates one claim's target. It reports false
// for a scope that is not a Go reachability scope. A missing run or an unusable
// entry wraps collector.ErrSlotScopeTransport because no retry can repair it; a
// store fault stays unwrapped so the claim plane retries.
func goReachabilityTarget(
	ctx context.Context,
	store RunScopeSetReader,
	item workflow.WorkItem,
) (scannercomponent.GoReachabilityTarget, bool, error) {
	scopeID := strings.TrimSpace(item.ScopeID)
	if _, ok := scannercomponent.ParseGoReachabilityScopeID(scopeID); !ok {
		return scannercomponent.GoReachabilityTarget{}, false, nil
	}
	if store == nil {
		return scannercomponent.GoReachabilityTarget{}, false, fmt.Errorf(
			"%w: no run reader for go reachability scope", collector.ErrSlotScopeTransport)
	}
	raw, err := store.GetRunRequestedScopeSet(ctx, strings.TrimSpace(item.RunID))
	if err != nil {
		if errors.Is(err, workflow.ErrRunNotFound) {
			return scannercomponent.GoReachabilityTarget{}, false, fmt.Errorf("%w: %w", collector.ErrSlotScopeTransport, err)
		}
		return scannercomponent.GoReachabilityTarget{}, false, err
	}
	target, err := scannercomponent.ResolveGoReachabilityTarget(raw, scopeID)
	if err != nil {
		return scannercomponent.GoReachabilityTarget{}, false, fmt.Errorf("%w: %w", collector.ErrSlotScopeTransport, err)
	}
	return target, true, nil
}

// installationKeyError keeps a key-read failure inside the provider's bounded
// vocabulary. classifyTokenProviderError would turn an unknown error into a
// terminal denial, which would burn a claim on a transient store fault, so a
// store fault becomes a retryable mint failure with its text dropped.
func installationKeyError(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	if errors.Is(err, collector.ErrSlotScopeTransport) {
		return app.NewConfigError("no usable derived target for this claim scope")
	}
	return app.NewRetryableError("", 1, 0, errors.New("derived target read failed"))
}

// reservedOwnerKey normalizes an owner key to the lowercase form normalized
// remotes produce, so `Eshu-HQ` in an operator's map still matches. Any other key
// is an exact scope id and passes through unchanged.
func reservedOwnerKey(key string) string {
	if strings.HasPrefix(key, scannercomponent.InstallationOwnerKeyPrefix) {
		return strings.ToLower(key)
	}
	return key
}
