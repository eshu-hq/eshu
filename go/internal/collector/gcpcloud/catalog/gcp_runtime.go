// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "time"

// Live-client, tag-provider, poll, and freshness runtime constants for the
// GCP cloud collector (#6696, slice L). Consolidated verbatim out of
// gcpruntime/liveclient.go, gcpruntime/liveclient_tags.go,
// gcpruntime/tagprovider.go, gcpruntime/config.go, and
// freshness/types.go.
//
// A move must never change a value: gcp_l_test.go pins every constant
// byte-identical to its pre-consolidation gcpcloud value.

// Live Cloud Asset Inventory client bounds (from gcpruntime/liveclient.go).
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

// Live Resource Manager tag bounds (from gcpruntime/liveclient_tags.go).
const (
	// CloudResourceManagerEndpoint is the default endpoint for Resource Manager
	// direct and effective tag APIs.
	CloudResourceManagerEndpoint = "https://cloudresourcemanager.googleapis.com"
	// DefaultLiveTagPageSize keeps Resource Manager tag pages bounded.
	DefaultLiveTagPageSize = 100
	// MaxLiveTagPageSize is the largest pageSize LiveClient sends to tag APIs.
	MaxLiveTagPageSize = 300

	// TagBindingsSourceURI is the bounded source URI for tagBindings.list calls.
	TagBindingsSourceURI = "resourcemanager://tagBindings.list"
	// EffectiveTagsSourceURI is the bounded source URI for effectiveTags.list
	// calls.
	EffectiveTagsSourceURI = "resourcemanager://effectiveTags.list"
)

// Tag source kinds and inheritance states (from gcpruntime/tagprovider.go).
// The inheritance states are stored on tag observations and emitted as
// bounded evidence, so they are wire values, not implementation details.
const (
	// TagSourceKindDirect identifies directly attached Resource Manager tags.
	TagSourceKindDirect = "direct"
	// TagSourceKindEffective identifies Resource Manager effective tags.
	TagSourceKindEffective = "effective"

	// TagInheritanceDirect marks a tag attached directly to the resource.
	TagInheritanceDirect = "direct"
	// TagInheritanceInherited marks a tag inherited from an ancestor.
	TagInheritanceInherited = "inherited"
)

// DefaultPollInterval is the source poll cadence used when a config supplies no
// explicit interval (from gcpruntime/config.go). It keeps idle scaffolding
// polls bounded.
const DefaultPollInterval = 30 * time.Minute

// EventKind identifies the normalized GCP Cloud Asset Inventory (CAI) signal
// family (from freshness/types.go).
type EventKind string

const (
	// EventKindAssetChange represents a CAI asset create or update signal.
	EventKindAssetChange EventKind = "asset_change"
	// EventKindAssetDeleted represents a CAI asset deletion signal.
	EventKindAssetDeleted EventKind = "asset_deleted"
)

// TriggerStatus describes durable GCP freshness trigger handoff state (from
// freshness/types.go).
type TriggerStatus string

const (
	// TriggerStatusQueued means the trigger is waiting for workflow handoff.
	TriggerStatusQueued TriggerStatus = "queued"
	// TriggerStatusClaimed means a handoff actor owns the trigger row.
	TriggerStatusClaimed TriggerStatus = "claimed"
	// TriggerStatusHandedOff means workflow work items were created.
	TriggerStatusHandedOff TriggerStatus = "handed_off"
	// TriggerStatusFailed means handoff failed and needs operator attention.
	TriggerStatusFailed TriggerStatus = "failed"
)
