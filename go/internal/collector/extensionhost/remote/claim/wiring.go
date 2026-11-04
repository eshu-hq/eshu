// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package claim

import (
	"context"
	"errors"
	"time"

	workflowv1 "github.com/eshu-hq/eshu-workflow/workflowv1"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// ErrCutoverDisabled reports a plane operation on a cutover gate that is
// off. The caller keeps the in-tree path: collector.ClaimedService stays
// the sole claim-mutation owner and the registry grant read stays the
// live-grant source. The sentinel never touches the wire, reads no grants,
// and observes no decisions, so a wired binary with the flag off behaves
// byte-identically to a binary without the gate.
var ErrCutoverDisabled = errors.New("claim: remote cutover is disabled")

// CutoverConfig binds one remote Client behind an explicit cutover flag.
// The zero value is the default-off gate: NewCutover succeeds without a
// client and every plane operation reports ErrCutoverDisabled.
type CutoverConfig struct {
	// Enabled routes claim, commit, and live-grant re-read through Client.
	// False (the default) keeps the in-tree path.
	Enabled bool
	// Client is the remote claimed-service seam. Required when Enabled;
	// ignored when disabled.
	Client *Client
}

// Cutover is the Box 1 cutover gate for the extension-host extraction
// (eshu-hq/eshu#9381): claim via ClaimNext, commit via
// ValidateAndPinClaimForCommit plus the claim-lifecycle operations, and
// live-grant re-read via the bound Client, all built only on the pinned
// eshu-workflow v0.1.0 table. It adds no lock, goroutine, channel, or
// mutable state: the flag is read-only after construction and Client is
// safe for concurrent use, so the single-writer model stays in-tree and
// the gate introduces no new lock or wait edge.
type Cutover struct {
	enabled bool
	client  *Client
}

// NewCutover validates the gate fail-closed: an enabled gate without a
// client is never built. A disabled gate needs no client.
func NewCutover(cfg CutoverConfig) (*Cutover, error) {
	if !cfg.Enabled {
		return &Cutover{}, nil
	}
	if cfg.Client == nil {
		return nil, errors.New("claim: cutover client is required when enabled")
	}
	return &Cutover{enabled: true, client: cfg.Client}, nil
}

// Enabled reports whether plane operations route through the Client. A nil
// gate reports disabled.
func (c *Cutover) Enabled() bool {
	return c != nil && c.enabled && c.client != nil
}

// clientFor returns the bound Client or the disabled sentinel.
func (c *Cutover) clientFor() (*Client, error) {
	if !c.Enabled() {
		return nil, ErrCutoverDisabled
	}
	return c.client, nil
}

// ClaimNext asks the plane for the next eligible work item when enabled.
func (c *Cutover) ClaimNext(
	ctx context.Context,
	selector workflowv1.ClaimSelector,
	observedAt time.Time,
	leaseTTL time.Duration,
) (workflowv1.ClaimedWorkItem, bool, error) {
	client, err := c.clientFor()
	if err != nil {
		return workflowv1.ClaimedWorkItem{}, false, err
	}
	return client.ClaimNext(ctx, selector, observedAt, leaseTTL)
}

// Heartbeat renews the lease on a held claim epoch when enabled.
func (c *Cutover) Heartbeat(ctx context.Context, mutation workflowv1.ClaimMutation) error {
	client, err := c.clientFor()
	if err != nil {
		return err
	}
	return client.Heartbeat(ctx, mutation)
}

// Complete finishes a held claim after its generation committed when enabled.
func (c *Cutover) Complete(ctx context.Context, mutation workflowv1.ClaimMutation) error {
	client, err := c.clientFor()
	if err != nil {
		return err
	}
	return client.Complete(ctx, mutation)
}

// Release returns a held claim unworked when enabled.
func (c *Cutover) Release(ctx context.Context, mutation workflowv1.ClaimMutation) error {
	client, err := c.clientFor()
	if err != nil {
		return err
	}
	return client.Release(ctx, mutation)
}

// FailRetryable records a retryable claim failure when enabled.
func (c *Cutover) FailRetryable(ctx context.Context, mutation workflowv1.ClaimMutation) error {
	client, err := c.clientFor()
	if err != nil {
		return err
	}
	return client.FailRetryable(ctx, mutation)
}

// FailTerminal records a terminal claim failure when enabled.
func (c *Cutover) FailTerminal(ctx context.Context, mutation workflowv1.ClaimMutation) error {
	client, err := c.clientFor()
	if err != nil {
		return err
	}
	return client.FailTerminal(ctx, mutation)
}

// PinForCommit asks core to pin a claim server-side for a fact commit when
// enabled: the protocol replacement for the in-transaction heartbeat the
// in-tree path relied on.
func (c *Cutover) PinForCommit(
	ctx context.Context,
	req workflowv1.CommitPinRequest,
) (workflowv1.CommitPinResponse, error) {
	client, err := c.clientFor()
	if err != nil {
		return workflowv1.CommitPinResponse{}, err
	}
	return client.PinForCommit(ctx, req)
}

// VerifyWiring is the seam activation gate through the bound Client when
// enabled. It reuses component.EvaluateEmission and
// component.ClassifyEmission, never a reimplementation.
func (c *Cutover) VerifyWiring(ctx context.Context, emissions []DeclaredEmission) error {
	client, err := c.clientFor()
	if err != nil {
		return err
	}
	return client.VerifyWiring(ctx, emissions)
}

// AuthorizeEmission rechecks core-issued producer authorization against a
// fresh live-grant read through the bound Client when enabled.
func (c *Cutover) AuthorizeEmission(ctx context.Context, kind, schemaVersion string) error {
	client, err := c.clientFor()
	if err != nil {
		return err
	}
	return client.AuthorizeEmission(ctx, kind, schemaVersion)
}

// ReadLiveGrants returns the current grant set from the bound Client when
// enabled.
func (c *Cutover) ReadLiveGrants(ctx context.Context) ([]component.ProducerGrant, error) {
	client, err := c.clientFor()
	if err != nil {
		return nil, err
	}
	return client.ReadLiveGrants(ctx)
}
