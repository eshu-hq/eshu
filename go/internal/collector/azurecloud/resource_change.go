// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package azurecloud

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/sdk/go/factschema"
	azurev1 "github.com/eshu-hq/eshu/sdk/go/factschema/azure/v1"
)

// Azure resource change types classify one Resource Graph change record. They
// are a bounded enum so a fabricated change type never reaches durable facts.
const (
	// ChangeTypeCreated is a resource create change.
	ChangeTypeCreated = "created"
	// ChangeTypeUpdated is a resource update change.
	ChangeTypeUpdated = "updated"
	// ChangeTypeDeleted is a resource delete change (a tombstone candidate, not
	// proof of final state).
	ChangeTypeDeleted = "deleted"
)

// maxChangedPropertyPaths bounds the number of changed property paths carried on
// one azure_resource_change fact so a pathological change record cannot emit an
// unbounded payload; excess paths are dropped (sorted) and truncation recorded.
const maxChangedPropertyPaths = 100

// ResourceChangeObservation is one Resource Graph change record. The collector
// carries the bounded change type, operation, client type, actor class, and the
// changed property PATHS (never the previous/new values) as freshness evidence;
// it fingerprints the actor identity and never proves final resource state.
type ResourceChangeObservation struct {
	// Boundary carries the scope and generation contract fields.
	Boundary Boundary
	// TargetARMResourceID is the raw ARM identity the change applied to.
	TargetARMResourceID string
	// ChangeType is the bounded change type.
	ChangeType string
	// ChangeTime is the change timestamp; it must be set.
	ChangeTime time.Time
	// Operation is the bounded provider operation label.
	Operation string
	// ClientType is the bounded client type label.
	ClientType string
	// ActorID is the raw actor identity; it is fingerprinted, never stored raw.
	ActorID string
	// ActorClass is the bounded actor class (e.g. user, service_principal).
	ActorClass string
	// ChangedPropertyPaths lists changed property paths only, never values.
	ChangedPropertyPaths []string
	// SourceRecordID overrides the default record id.
	SourceRecordID string
	// SourceURI is the bounded source URI.
	SourceURI string
}

// NewResourceChangeEnvelope builds the durable azure_resource_change fact for one
// change record. It carries changed property paths and truncation flags only —
// never previous/new values — and fingerprints the actor with the redaction key.
// A delete change is a tombstone candidate, not proof of final state; the reducer
// confirms via inventory. The stable fact key is derived from the target
// identity, change type, change time, and operation, so each change event keys a
// distinct row.
//
// It fails closed on a missing target, an unknown change type, a zero change
// time, or a zero redaction key.
func NewResourceChangeEnvelope(observation ResourceChangeObservation, key redact.Key) (facts.Envelope, error) {
	if err := validateBoundary(observation.Boundary); err != nil {
		return facts.Envelope{}, err
	}
	if key.IsZero() {
		return facts.Envelope{}, fmt.Errorf("azure resource change observation requires a redaction key")
	}
	targetID := strings.TrimSpace(observation.TargetARMResourceID)
	if targetID == "" {
		return facts.Envelope{}, fmt.Errorf("azure resource change observation requires target_arm_resource_id")
	}
	changeType, err := normalizeChangeType(observation.ChangeType)
	if err != nil {
		return facts.Envelope{}, err
	}
	if observation.ChangeTime.IsZero() {
		return facts.Envelope{}, fmt.Errorf("azure resource change observation requires a change time")
	}

	identity, err := ParseARMIdentity(targetID)
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("normalize target arm identity: %w", err)
	}
	changedPaths, truncated := boundedChangedPropertyPaths(observation.ChangedPropertyPaths)

	stableKey := facts.StableID(facts.AzureResourceChangeFactKind, map[string]any{
		"normalized_id": identity.Normalized,
		"change_type":   changeType,
		"change_time":   observation.ChangeTime.UTC().Format(time.RFC3339Nano),
		"operation":     strings.TrimSpace(observation.Operation),
		"source_lane":   observation.Boundary.SourceLane,
	})

	operation := strings.TrimSpace(observation.Operation)
	clientType := strings.TrimSpace(observation.ClientType)
	actorClass := strings.TrimSpace(observation.ActorClass)
	changedCount := len(changedPaths)
	isTombstone := changeType == ChangeTypeDeleted
	payload, err := factschema.EncodeAzureResourceChange(azurev1.ResourceChange{
		TargetARMResourceID:      targetID,
		TargetNormalizedID:       identity.Normalized,
		TargetResourceType:       identity.ResourceType,
		ChangeType:               changeType,
		ChangeTime:               observation.ChangeTime.UTC().Format(time.RFC3339Nano),
		Operation:                &operation,
		ClientType:               &clientType,
		ActorClass:               &actorClass,
		ChangedPropertyPaths:     changedPaths,
		ChangedPropertyCount:     &changedCount,
		ChangedPropertyTruncated: &truncated,
		IsTombstoneCandidate:     &isTombstone,
		RedactionPolicyVersion:   stringPtr(RedactionPolicyVersion),
	})
	if err != nil {
		return facts.Envelope{}, fmt.Errorf("encode azure_resource_change payload: %w", err)
	}
	addAzureBoundaryPayload(payload, observation.Boundary)
	payload["tenant_id"] = observation.Boundary.TenantID
	payload["scope_kind"] = observation.Boundary.ScopeKind
	payload["provider_scope_id"] = observation.Boundary.ProviderScopeID
	payload["source_lane"] = observation.Boundary.SourceLane
	if actor := strings.TrimSpace(observation.ActorID); actor != "" {
		payload["actor_fingerprint"] = redact.String(actor, "azure_change_actor", "azure_change_actor:"+strings.TrimSpace(observation.ActorClass), key).Marker
	}

	return newEnvelope(
		observation.Boundary,
		facts.AzureResourceChangeFactKind,
		facts.AzureResourceChangeSchemaVersion,
		stableKey,
		sourceRecordID(observation.SourceRecordID, identity.Normalized+"|"+changeType),
		observation.SourceURI,
		payload,
	), nil
}

// boundedChangedPropertyPaths trims, de-duplicates, sorts, and caps the changed
// property paths, returning the bounded paths and whether the input was
// truncated.
func boundedChangedPropertyPaths(paths []string) ([]string, bool) {
	seen := make(map[string]struct{}, len(paths))
	for _, p := range paths {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			seen[trimmed] = struct{}{}
		}
	}
	if len(seen) == 0 {
		return nil, false
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	truncated := false
	if len(out) > maxChangedPropertyPaths {
		out = out[:maxChangedPropertyPaths]
		truncated = true
	}
	return out, truncated
}

// normalizeChangeType validates the change type against the bounded set.
func normalizeChangeType(changeType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(changeType)) {
	case ChangeTypeCreated, "create":
		return ChangeTypeCreated, nil
	case ChangeTypeUpdated, "update":
		return ChangeTypeUpdated, nil
	case ChangeTypeDeleted, "delete":
		return ChangeTypeDeleted, nil
	default:
		return "", fmt.Errorf("azure resource change observation has unknown change_type %q", changeType)
	}
}
