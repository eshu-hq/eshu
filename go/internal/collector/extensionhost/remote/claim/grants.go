// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package claim

import (
	"context"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// GrantReader re-reads the live producer-grant set. The host calls it on
// every emission decision, so a grant revoked during execution fails the
// next result closed without rebuilding the client. A read failure denies
// core-owned kinds (reason grants_unreadable) rather than emitting under
// unknown authorization.
type GrantReader func(ctx context.Context) ([]component.ProducerGrant, error)

// RegistryGrantReader backs the seam with the component registry on disk,
// mirroring the in-tree LiveGrants closure. It takes only the registry home
// path: no database, graph, or workflow-store handle.
func RegistryGrantReader(home string) GrantReader {
	return func(ctx context.Context) ([]component.ProducerGrant, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return component.NewRegistry(home).ProducerGrants()
	}
}

// DecisionStage names the lifecycle point of a seam grant decision. The set
// is closed so operator signals stay low-cardinality.
type DecisionStage string

const (
	// StageWiring is the VerifyWiring activation-gate decision: the
	// seam-side counterpart of the in-tree activation/readback gate.
	StageWiring DecisionStage = "wiring"
	// StageEmission is the per-emission live-grant recheck decision.
	StageEmission DecisionStage = "emission"
)

// Decision is one producer-grant allow or deny. It carries identifiers
// only: never grant contents beyond the producer, version, and kind, and
// never payloads or credentials.
type Decision struct {
	Stage      DecisionStage
	Allowed    bool
	Reason     component.GrantReason
	ProducerID string
	Version    string
	Kind       string
}

// Observer receives seam grant decisions at the owning decision site.
// Implementations must be safe for concurrent use and must not retain or
// mutate the decision. A nil observer disables observation, which never
// changes an allow/deny outcome.
type Observer interface {
	ObserveDecision(ctx context.Context, decision Decision)
}

// DeclaredEmission names one core-owned kind and schema version the holder
// intends to emit. VerifyWiring checks each against the live grant set,
// mirroring the manifest-declared core-kind check of the in-tree
// activation gate.
type DeclaredEmission struct {
	Kind          string
	SchemaVersion string
}

// DenyError is one fail-closed grant denial. It names the holder identity
// and the kind only: the single-holder proof is that the identity always
// comes from construction, never from the wire.
type DenyError struct {
	Stage      DecisionStage
	Reason     component.GrantReason
	ProducerID string
	Version    string
	Kind       string
}

func (e *DenyError) Error() string {
	return fmt.Sprintf(
		"producer grant denied at %s for %q version %q kind %q: %s",
		e.Stage, e.ProducerID, e.Version, e.Kind, e.Reason,
	)
}

// ReadLiveGrants returns the current grant set from the configured reader.
// Callers treat a read error as fail-closed via AuthorizeEmission or
// VerifyWiring; this method surfaces the raw error only for diagnostics.
func (c *Client) ReadLiveGrants(ctx context.Context) ([]component.ProducerGrant, error) {
	return c.grants(ctx)
}

// VerifyWiring is the seam activation gate: it re-reads the live grants and
// checks every declared core-owned emission under the construction-time
// holder identity. One decision is observed per declared core-owned kind;
// the first denial fails the wiring closed. An unreadable set denies every
// core-owned declaration.
func (c *Client) VerifyWiring(ctx context.Context, emissions []DeclaredEmission) error {
	grants, readErr := c.grants(ctx)
	if readErr != nil {
		grants = nil
	}
	now := c.clock()
	var first *DenyError
	for _, emission := range emissions {
		_, governed := component.EvaluateEmission(
			grants, c.producerID, c.version, emission.Kind, emission.SchemaVersion, c.scopes, now,
		)
		if !governed {
			continue
		}
		reason := component.GrantReasonGrantsUnreadable
		if readErr == nil {
			reason = component.ClassifyEmission(
				grants, c.producerID, c.version, emission.Kind, emission.SchemaVersion, c.scopes, now,
			)
		}
		allowed := reason == component.GrantReasonGranted
		c.observe(ctx, Decision{
			Stage:      StageWiring,
			Allowed:    allowed,
			Reason:     reason,
			ProducerID: c.producerID,
			Version:    c.version,
			Kind:       emission.Kind,
		})
		if !allowed && first == nil {
			first = &DenyError{
				Stage:      StageWiring,
				Reason:     reason,
				ProducerID: c.producerID,
				Version:    c.version,
				Kind:       emission.Kind,
			}
		}
	}
	if first != nil {
		return first
	}
	return nil
}

// AuthorizeEmission rechecks core-issued producer authorization for one
// emission against a fresh live-grant read, so a grant revoked after wiring
// fails the next result closed. Kinds that are not core-owned need no grant
// and always pass without observation. It returns nil on allow and
// *DenyError on deny, including the unreadable-set fail-closed deny.
func (c *Client) AuthorizeEmission(ctx context.Context, kind, schemaVersion string) error {
	grants, readErr := c.grants(ctx)
	if readErr != nil {
		grants = nil
	}
	now := c.clock()
	allowed, governed := component.EvaluateEmission(
		grants, c.producerID, c.version, kind, schemaVersion, c.scopes, now,
	)
	if allowed {
		if governed {
			c.observe(ctx, Decision{
				Stage:      StageEmission,
				Allowed:    true,
				Reason:     component.GrantReasonGranted,
				ProducerID: c.producerID,
				Version:    c.version,
				Kind:       kind,
			})
		}
		return nil
	}
	reason := component.GrantReasonGrantsUnreadable
	if readErr == nil {
		reason = component.ClassifyEmission(
			grants, c.producerID, c.version, kind, schemaVersion, c.scopes, now,
		)
	}
	c.observe(ctx, Decision{
		Stage:      StageEmission,
		Allowed:    false,
		Reason:     reason,
		ProducerID: c.producerID,
		Version:    c.version,
		Kind:       kind,
	})
	return &DenyError{
		Stage:      StageEmission,
		Reason:     reason,
		ProducerID: c.producerID,
		Version:    c.version,
		Kind:       kind,
	}
}

// observe reports one decision unless observation is disabled.
func (c *Client) observe(ctx context.Context, decision Decision) {
	if c.observer == nil {
		return
	}
	c.observer.ObserveDecision(ctx, decision)
}
