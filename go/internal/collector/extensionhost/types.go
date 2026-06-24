// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"context"
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
	runner              Runner
	statusRecorder      StatusRecorder
	clock               func() time.Time
	collectorKinds      map[scope.CollectorKind]struct{}
}
