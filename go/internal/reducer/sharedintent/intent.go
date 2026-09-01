// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sharedintent

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer/payloadcore"
)

// Row is one durable shared-domain projection intent.
type Row struct {
	IntentID         string
	ProjectionDomain string
	PartitionKey     string
	ScopeID          string
	AcceptanceUnitID string
	RepositoryID     string
	SourceRunID      string
	GenerationID     string
	Payload          map[string]any
	CreatedAt        time.Time
	CompletedAt      *time.Time
}

// Input holds the parameters for building one deterministic shared projection
// intent row.
type Input struct {
	ProjectionDomain string
	PartitionKey     string
	// IdentityKey overrides the partition key only for deterministic intent ID
	// construction when several rows must share one stored partition key.
	IdentityKey      string
	ScopeID          string
	AcceptanceUnitID string
	RepositoryID     string
	SourceRunID      string
	GenerationID     string
	Payload          map[string]any
	CreatedAt        time.Time
}

// Build builds one deterministic shared projection intent row. The intent ID is
// a SHA256 of the identity fields, matching the Python implementation exactly.
func Build(input Input) Row {
	acceptanceUnitID := strings.TrimSpace(input.AcceptanceUnitID)
	if acceptanceUnitID == "" {
		acceptanceUnitID = strings.TrimSpace(input.RepositoryID)
	}
	identityPartitionKey := input.PartitionKey
	if strings.TrimSpace(input.IdentityKey) != "" {
		identityPartitionKey = strings.TrimSpace(input.IdentityKey)
	}

	intentID := StableIntentID(map[string]string{
		"acceptance_unit_id": acceptanceUnitID,
		"generation_id":      input.GenerationID,
		"partition_key":      identityPartitionKey,
		"projection_domain":  input.ProjectionDomain,
		"repository_id":      input.RepositoryID,
		"scope_id":           strings.TrimSpace(input.ScopeID),
		"source_run_id":      input.SourceRunID,
	})

	return Row{
		IntentID:         intentID,
		ProjectionDomain: input.ProjectionDomain,
		PartitionKey:     input.PartitionKey,
		ScopeID:          strings.TrimSpace(input.ScopeID),
		AcceptanceUnitID: acceptanceUnitID,
		RepositoryID:     input.RepositoryID,
		SourceRunID:      input.SourceRunID,
		GenerationID:     input.GenerationID,
		Payload:          input.Payload,
		CreatedAt:        input.CreatedAt,
		CompletedAt:      nil,
	}
}

// StableIntentID computes a deterministic intent identifier matching the Python
// _stable_intent_id function. It serializes the identity dict as compact JSON
// with sorted keys: {"identity":{...sorted fields...}}
func StableIntentID(identity map[string]string) string {
	// Build the identity object with sorted keys. Since json.Marshal sorts
	// map keys by default in Go, this produces the same output as Python's
	// json.dumps(sort_keys=True, separators=(",", ":")).
	wrapper := map[string]any{
		"identity": identity,
	}

	payload, err := json.Marshal(wrapper)
	if err != nil {
		// Identity fields are plain strings; marshal cannot fail.
		panic(fmt.Sprintf("marshal identity: %v", err))
	}

	digest := sha256.Sum256(payload)
	return fmt.Sprintf("%x", digest)
}

// AcceptanceKey identifies one authoritative freshness slice.
type AcceptanceKey struct {
	ScopeID          string
	AcceptanceUnitID string
	SourceRunID      string
}

// AcceptanceKey returns the bounded-unit freshness key for the row. The second
// result is false when the row does not carry enough identity to name a slice.
func (row Row) AcceptanceKey() (AcceptanceKey, bool) {
	scopeID := strings.TrimSpace(row.ScopeID)
	if scopeID == "" && row.Payload != nil {
		scopeID = strings.TrimSpace(payloadcore.AnyToString(row.Payload["scope_id"]))
	}

	acceptanceUnitID := strings.TrimSpace(row.AcceptanceUnitID)
	if acceptanceUnitID == "" && row.Payload != nil {
		acceptanceUnitID = strings.TrimSpace(payloadcore.AnyToString(row.Payload["acceptance_unit_id"]))
	}
	if acceptanceUnitID == "" {
		acceptanceUnitID = strings.TrimSpace(row.RepositoryID)
	}

	sourceRunID := strings.TrimSpace(row.SourceRunID)
	if scopeID == "" || acceptanceUnitID == "" || sourceRunID == "" {
		return AcceptanceKey{}, false
	}

	return AcceptanceKey{
		ScopeID:          scopeID,
		AcceptanceUnitID: acceptanceUnitID,
		SourceRunID:      sourceRunID,
	}, true
}
