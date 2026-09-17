// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"fmt"
	"sync"
	"time"
)

// Health is the operator-visible collector status. Every collector
// repository exposes these fields (logs, metrics, or status endpoint) so a
// 3 AM operator can answer: is it alive, is its evidence fresh, what is
// failing, and what is it costing.
type Health struct {
	// Alive is true once the process has started and its config validated.
	Alive bool `json:"alive"`
	// LastSuccessAt is the last complete-claim timestamp, zero when none.
	LastSuccessAt time.Time `json:"last_success_at,omitempty"`
	// LastDigest is the last emitted report digest for freshness comparison.
	LastDigest string `json:"last_digest,omitempty"`
	// ConsecutiveFailures counts failures since the last success.
	ConsecutiveFailures int `json:"consecutive_failures"`
	// LastError is the last failure reason without credentials or payloads.
	LastError string `json:"last_error,omitempty"`
	// RetryQueueDepth is the bounded pending-retry count.
	RetryQueueDepth int `json:"retry_queue_depth"`
	// MaxRetryQueueDepth is the configured bound; retries past it go terminal.
	MaxRetryQueueDepth int `json:"max_retry_queue_depth"`
}

// ResourceUse reports bounded cost for operator dashboards.
type ResourceUse struct {
	// MaxRecordsPerClaim bounds one claim's fact emission.
	MaxRecordsPerClaim int `json:"max_records_per_claim"`
	// MaxPayloadBytes bounds one emitted payload.
	MaxPayloadBytes int `json:"max_payload_bytes"`
	// ClaimTimeoutSeconds bounds one claim's wall time.
	ClaimTimeoutSeconds int `json:"claim_timeout_seconds"`
}

// DefaultResourceUse is the template's starting bound. Tune it per source.
func DefaultResourceUse() ResourceUse {
	return ResourceUse{MaxRecordsPerClaim: 5000, MaxPayloadBytes: 65536, ClaimTimeoutSeconds: 300}
}

// Monitor is a goroutine-safe health tracker for one collector process.
type Monitor struct {
	mu       sync.Mutex
	health   Health
	resource ResourceUse
}

// NewMonitor returns a monitor with the default resource bounds.
func NewMonitor() *Monitor {
	return &Monitor{health: Health{Alive: true}, resource: DefaultResourceUse()}
}

// ObserveSuccess records a completed claim.
func (m *Monitor) ObserveSuccess(digest string, at time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.health.LastSuccessAt = at.UTC()
	m.health.LastDigest = digest
	m.health.ConsecutiveFailures = 0
	m.health.LastError = ""
	if m.health.RetryQueueDepth > 0 {
		m.health.RetryQueueDepth--
	}
}

// ObserveFailure records a retryable or terminal failure without secrets.
func (m *Monitor) ObserveFailure(reason string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.health.ConsecutiveFailures++
	m.health.LastError = reason
}

// ObserveRetryNevertheless queues one bounded retry; past the bound it
// reports an error so the caller fails terminal instead of queuing forever.
func (m *Monitor) ObserveRetryNevertheless() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.health.MaxRetryQueueDepth == 0 {
		m.health.MaxRetryQueueDepth = 8
	}
	if m.health.RetryQueueDepth >= m.health.MaxRetryQueueDepth {
		return fmt.Errorf("retry queue bound %d reached: fail terminal", m.health.MaxRetryQueueDepth)
	}
	m.health.RetryQueueDepth++
	return nil
}

// Snapshot returns a copy of current health plus configured bounds.
func (m *Monitor) Snapshot() (Health, ResourceUse) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.health, m.resource
}
