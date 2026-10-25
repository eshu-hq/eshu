// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"time"

	sdkcollector "github.com/eshu-hq/eshu-sdk/collector"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// Request is the bounded JSON document passed to an extension process.
type Request struct {
	ProtocolVersion string                `json:"protocol_version"`
	Claim           sdkcollector.Claim    `json:"claim"`
	Contract        sdkcollector.Contract `json:"contract"`
	Config          map[string]any        `json:"config,omitempty"`
}

// Runner executes one extension invocation for an already-claimed work item.
type Runner interface {
	RunCollector(context.Context, Request) (sdkcollector.Result, error)
}

// StatusRecorder records validated SDK status records at the host boundary.
type StatusRecorder interface {
	RecordExtensionStatus(context.Context, StatusRecord) error
}

// StatusRecord is a bounded host-owned copy of one SDK status record.
type StatusRecord struct {
	ComponentID       string
	InstanceID        string
	CollectorKind     string
	SourceSystem      string
	ScopeID           string
	WorkItemID        string
	GenerationID      string
	State             sdkcollector.ResultState
	Class             sdkcollector.StatusClass
	FailureClass      string
	RetryAfterSeconds int
	Partial           bool
	WarningCount      int
	FactCount         int
	SourceLatencyMS   int
	ObservedAt        time.Time
}

// ResultRecorder is an optional extension of StatusRecorder. When the
// configured StatusRecorder also implements it, the Source reports exactly one
// ResultRecord per NextClaimed call, for every final state: SDK results
// (complete, unchanged, partial, retryable, terminal) and host verdicts
// (invalid claim, launch failure, invalid result, identity mismatch, status
// record failure). It cannot fail the claim.
type ResultRecorder interface {
	RecordExtensionResult(context.Context, ResultRecord)
}

// ResultOrigin says who decided a claim result's outcome.
type ResultOrigin string

const (
	// ResultOriginSDK marks a result the SDK validator accepted; its state
	// and status counts come from the extension.
	ResultOriginSDK ResultOrigin = "sdk"
	// ResultOriginHost marks a host verdict on the claim or result. Its
	// failure class is a FailureClass* constant, and it carries no status
	// counts, because an unvalidated result's statuses are not trusted.
	ResultOriginHost ResultOrigin = "host"
)

// ResultRecord is the bounded per-claim summary the Source reports. It carries
// no scope, instance, work item, generation, URL, or payload values by
// construction, so a recorder cannot leak them into labels or logs.
type ResultRecord struct {
	// CollectorKind is the claim's manifest-declared kind, or "unknown" when
	// the host rejected a claim whose kind the manifest does not declare.
	CollectorKind string
	Origin        ResultOrigin
	// State is the SDK result state, or for a host verdict the state
	// ClaimedService applies (terminal or retryable).
	State        sdkcollector.ResultState
	FailureClass string
	StatusCount  int
	FactCount    int
	WarningCount int
	// SourceLatencyMS is the largest source_latency_ms across the statuses.
	SourceLatencyMS int
	Partial         bool
	// RetryAfter is the retry delay the extension requested, after the
	// MaxRetryAfterDelay cap; zero unless the result is retryable. It is
	// recorded before ClaimedService decides: ClaimedService applies
	// max(PollInterval, RetryAfter) while attempts remain and no delay once
	// the attempt budget is spent, so this is the request, not the applied
	// delay.
	RetryAfter time.Duration
	// RetryAfterRequestedSeconds is the largest retry_after_seconds the
	// extension requested, before the cap.
	RetryAfterRequestedSeconds int
}

// Config configures a Source for one component instance.
type Config struct {
	Manifest            component.Manifest
	CollectorInstanceID string
	ScopeKind           scope.ScopeKind
	ConfigHandle        string
	Config              map[string]any
	Runner              Runner
	StatusRecorder      StatusRecorder
	Clock               func() time.Time
	// Grants carries core-issued producer authorizations consulted when
	// the manifest declares core-owned fact kinds. Nil grants deny: a
	// core-owned declaration without a live grant fails Source construction
	// exactly as manifest validation without grant context rejects it.
	Grants []component.ProducerGrant
	// LiveGrants, when set, supplies the current grant set for every
	// emitted result so revocation during execution fails closed. When nil,
	// emission checks fall back to the construction-time Grants snapshot. A
	// non-nil error means the grant set could not be read: core-owned kinds
	// are denied fail-closed (reason grants_unreadable) rather than emitted
	// under unknown authorization.
	LiveGrants func() ([]component.ProducerGrant, error)
	// GrantObserver, when set, receives one producer-grant decision per
	// core-owned fact kind at activation (Source construction) and at every
	// emission recheck. Nil disables observation and costs one nil check.
	GrantObserver component.GrantObserver
}

// Source implements collector.ClaimedSource for collector SDK extensions.
type Source struct {
	manifest            component.Manifest
	collectorInstanceID string
	scopeKind           scope.ScopeKind
	configHandle        string
	config              map[string]any
	contract            sdkcollector.Contract
	validator           sdkcollector.Validator
	payloadSchemas      map[string]json.RawMessage
	runner              Runner
	statusRecorder      StatusRecorder
	// resultRecorder is statusRecorder when it also implements
	// ResultRecorder, resolved once at construction; nil otherwise.
	resultRecorder ResultRecorder
	clock          func() time.Time
	collectorKinds map[scope.CollectorKind]struct{}
	// liveGrants supplies the current producer-grant set for every emission
	// so revocation during execution fails closed. It defaults to the
	// construction-time Grants snapshot.
	liveGrants func() ([]component.ProducerGrant, error)
	// grantObserver receives grant decisions; nil disables observation.
	grantObserver component.GrantObserver
}
