// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
	"encoding/json"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	sdkcollector "github.com/eshu-hq/eshu/sdk/go/collector"
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
	clock               func() time.Time
	collectorKinds      map[scope.CollectorKind]struct{}
	// liveGrants supplies the current producer-grant set for every emission
	// so revocation during execution fails closed. It defaults to the
	// construction-time Grants snapshot.
	liveGrants func() ([]component.ProducerGrant, error)
	// grantObserver receives grant decisions; nil disables observation.
	grantObserver component.GrantObserver
}
