// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"fmt"
	"time"
)

// ClaimPacingSignal names which depth signal engaged the claim-pacing latch:
// per-domain pending depth or the global pending ceiling.
type ClaimPacingSignal string

const (
	// ClaimPacingSignalDomain paces only claims into the saturated
	// collector family (collector_kind, collector_instance_id).
	ClaimPacingSignalDomain ClaimPacingSignal = "domain"
	// ClaimPacingSignalGlobal paces every claim to protect the database
	// itself: table growth, claim-query cost, and retention lag.
	ClaimPacingSignalGlobal ClaimPacingSignal = "global"
)

// Validate rejects an unknown pacing signal. Unknown signals fail safe at
// the decision layer, which re-evaluates them as disengaged.
func (s ClaimPacingSignal) Validate() error {
	switch s {
	case ClaimPacingSignalDomain, ClaimPacingSignalGlobal:
		return nil
	default:
		return fmt.Errorf("unknown claim pacing signal %q", string(s))
	}
}

// ClaimPacingConfig carries the config-named depth thresholds and delays for
// claim-issuance pacing (#8470). The zero value disables pacing: depth is
// never read and grants are never delayed unless an operator configures
// every watermark and delay.
//
// Per-domain depth is pending work items in the claiming collector family;
// global depth is pending work items system-wide. Each signal engages
// strictly above its high watermark and releases at or below its low
// watermark, so the gap between the two is what stops oscillation.
type ClaimPacingConfig struct {
	DomainHigh int64
	DomainLow  int64
	GlobalHigh int64
	GlobalLow  int64
	// GrantDelay is the backoff a paced grant carries on the existing
	// retry-after path: the runner waits it out instead of treating the
	// paced grant as a failure.
	GrantDelay time.Duration
	// MaxEngagement bounds one latch engagement: past it with depth still
	// above the release watermark, the verdict stays paced and raises Stuck
	// instead of releasing blindly into a saturated system.
	MaxEngagement time.Duration
	// DepthCacheTTL bounds how long one store reuses a depth observation.
	// Zero reads depth on every check (deterministic; the pacing proofs use
	// it). Positive values amortize the pending-count scan, which costs
	// milliseconds at tens of thousands of pending rows against a
	// sub-millisecond claim. The latch row stays authoritative in shared
	// storage and is always read fresh; only the depth numbers may lag by
	// the TTL.
	DepthCacheTTL time.Duration
}

// Enabled reports whether pacing is configured. Every watermark and delay
// must be set: a half-configured throttle is a misconfiguration, not a
// conservative default.
func (c ClaimPacingConfig) Enabled() bool {
	return c.DomainHigh > 0 && c.GlobalHigh > 0 &&
		c.GrantDelay > 0 && c.MaxEngagement > 0
}

// Validate rejects an enabled config whose watermarks cannot latch: each low
// watermark must sit strictly below its high watermark, delays must be
// positive, and the depth cache TTL must not be negative. The disabled zero
// value is valid.
func (c ClaimPacingConfig) Validate() error {
	if !c.Enabled() {
		if c == (ClaimPacingConfig{}) {
			return nil
		}
		return fmt.Errorf("claim pacing is partially configured: set every watermark and delay, or none")
	}
	if c.DomainLow < 0 || c.DomainLow >= c.DomainHigh {
		return fmt.Errorf("claim pacing domain low %d must be below domain high %d", c.DomainLow, c.DomainHigh)
	}
	if c.GlobalLow < 0 || c.GlobalLow >= c.GlobalHigh {
		return fmt.Errorf("claim pacing global low %d must be below global high %d", c.GlobalLow, c.GlobalHigh)
	}
	if c.DepthCacheTTL < 0 {
		return fmt.Errorf("claim pacing depth cache TTL %s must not be negative", c.DepthCacheTTL)
	}
	return nil
}

// ClaimPacingDepths carries one depth observation for a pacing verdict:
// pending work items in the claiming family and pending work items
// system-wide.
type ClaimPacingDepths struct {
	Domain int64
	Global int64
}

// ClaimPacingLatch is the shared hysteresis bit: whether pacing is engaged
// and on which signal. It lives in shared storage alongside the claim path
// so coordinators cannot disagree about whether pacing is on.
type ClaimPacingLatch struct {
	Engaged bool
	Signal  ClaimPacingSignal
}

// ClaimPacingDecision is one pacing verdict for one claim attempt.
type ClaimPacingDecision struct {
	Paced bool
	// Signal names the engaging depth signal and stays set while the latch
	// holds, so the operator can answer "is pacing on, and why".
	Signal ClaimPacingSignal
	// Domain carries the saturated family as "kind/instance" for the domain
	// signal and stays empty for the global ceiling.
	Domain string
	// RetryAfter is the grant delay, set exactly when Paced.
	RetryAfter time.Duration
	// Stuck reports an engagement older than MaxEngagement with depth still
	// above the release watermark: still paced, and loud.
	Stuck bool
}

// DecideClaimPacing is the pure pacing verdict over one depth observation
// and the shared latch. It never touches storage: the store reads depths and
// the latch, calls this, and advances the latch on transitions.
//
// Per-domain depth is consulted first with the global depth as the ceiling.
// Engagement is strictly above the high watermark; release is at or below
// the low watermark, so depths between the two follow the latch. A latch
// engaged on one signal while the other signal trips re-engages on the newly
// hot signal rather than releasing into a saturated system.
func DecideClaimPacing(
	cfg ClaimPacingConfig,
	depths ClaimPacingDepths,
	latch ClaimPacingLatch,
	engagedFor time.Duration,
) ClaimPacingDecision {
	if !cfg.Enabled() {
		return ClaimPacingDecision{}
	}
	domainHot := depths.Domain > cfg.DomainHigh
	globalHot := depths.Global > cfg.GlobalHigh
	if !latch.Engaged || latch.Signal.Validate() != nil {
		switch {
		case domainHot:
			return paced(cfg, ClaimPacingSignalDomain, 0)
		case globalHot:
			return paced(cfg, ClaimPacingSignalGlobal, 0)
		default:
			return ClaimPacingDecision{}
		}
	}
	switch latch.Signal {
	case ClaimPacingSignalDomain:
		if depths.Domain <= cfg.DomainLow {
			if globalHot {
				return paced(cfg, ClaimPacingSignalGlobal, 0)
			}
			return ClaimPacingDecision{}
		}
	case ClaimPacingSignalGlobal:
		if depths.Global <= cfg.GlobalLow {
			if domainHot {
				return paced(cfg, ClaimPacingSignalDomain, 0)
			}
			return ClaimPacingDecision{}
		}
	}
	decision := paced(cfg, latch.Signal, engagedFor)
	return decision
}

// paced builds an engaged verdict. Stuck trips only on a positive overdue
// engagement with depth still above the release watermark: the caller reaches
// here only when the engaged signal has not released.
func paced(cfg ClaimPacingConfig, signal ClaimPacingSignal, engagedFor time.Duration) ClaimPacingDecision {
	return ClaimPacingDecision{
		Paced:      true,
		Signal:     signal,
		RetryAfter: cfg.GrantDelay,
		Stuck:      engagedFor > cfg.MaxEngagement,
	}
}

// ClaimPacingDeferral is the grant-delay channel: a paced ClaimNext returns
// it as the error with no claim issued, so the runner backs off on the
// existing retry-after path instead of treating the paced grant as a
// failure. It implements RetryAfterDelay so errors.As matches the collector
// retry-after convention.
type ClaimPacingDeferral struct {
	Signal ClaimPacingSignal
	Domain string
	// RetryAfter is the grant delay the runner must wait before claiming
	// again. It is always positive on a deferral the dispatcher returns.
	RetryAfter time.Duration
	Stuck      bool
}

// Error describes the paced grant for operator logs.
func (e ClaimPacingDeferral) Error() string {
	if e.Stuck {
		return fmt.Sprintf("claim pacing engaged on %s depth (domain %q): grant delayed %s, engagement stuck past its bound",
			e.Signal, e.Domain, e.RetryAfter)
	}
	return fmt.Sprintf("claim pacing engaged on %s depth (domain %q): grant delayed %s",
		e.Signal, e.Domain, e.RetryAfter)
}

// RetryAfterDelay exposes the grant delay on the existing retry-after path.
func (e ClaimPacingDeferral) RetryAfterDelay() time.Duration {
	return e.RetryAfter
}
