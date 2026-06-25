// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cassette

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// currentSchemaVersion is the only schema version the package reads and writes.
const currentSchemaVersion = "1"

// File is the root document of a cassette JSON file. Each scope entry carries
// the scope identity, the generation metadata, and the pre-recorded fact
// envelopes for one collection run. The file is collector-agnostic: any
// credentialed collector can write and replay one.
type File struct {
	// Collector names the collector kind that produced this cassette (e.g.
	// "kubernetes_live"). Informational; not validated at replay time so the
	// cassette can be replayed by any collector that accepts the fact kinds it
	// contains.
	Collector string `json:"collector"`
	// SchemaVersion must equal "1".
	SchemaVersion string `json:"schema_version"`
	// Scopes is the ordered list of pre-recorded scope+generation batches. Each
	// batch is replayed as one CollectedGeneration by Source.Next.
	Scopes []Scope `json:"scopes"`
}

// Scope is one pre-recorded scope+generation worth of facts.
type Scope struct {
	// ScopeID is the durable source-local scope identity (required).
	ScopeID string `json:"scope_id"`
	// SourceSystem names the collector family (e.g. "kubernetes_live").
	SourceSystem string `json:"source_system"`
	// ScopeKind is the scope classification (e.g. "cluster", "region").
	ScopeKind string `json:"scope_kind"`
	// CollectorKind is the collector family label (e.g. "kubernetes_live").
	CollectorKind string `json:"collector_kind"`
	// PartitionKey is the partition routing key. Defaults to ScopeID when empty.
	PartitionKey string `json:"partition_key,omitempty"`
	// Metadata is an optional map of key-value pairs describing the scope.
	Metadata map[string]string `json:"metadata,omitempty"`
	// GenerationID is the durable generation identity (required).
	GenerationID string `json:"generation_id"`
	// ObservedAt is the observation timestamp for the generation (required).
	ObservedAt time.Time `json:"observed_at"`
	// TriggerKind is how the generation was produced. Defaults to "snapshot".
	TriggerKind string `json:"trigger_kind,omitempty"`
	// Facts is the ordered list of pre-recorded fact envelopes.
	Facts []Fact `json:"facts"`
}

// partitionKey returns the effective partition key for the scope.
func (s Scope) partitionKey() string {
	if pk := strings.TrimSpace(s.PartitionKey); pk != "" {
		return pk
	}
	return s.ScopeID
}

// triggerKind returns the effective trigger kind for the scope.
func (s Scope) triggerKind() string {
	if tk := strings.TrimSpace(s.TriggerKind); tk != "" {
		return tk
	}
	return "snapshot"
}

// Fact is one pre-recorded fact envelope. FactID is derived deterministically
// from FactKind and Payload at replay time if left empty.
type Fact struct {
	// FactKind is the durable fact type identifier (required).
	FactKind string `json:"fact_kind"`
	// StableFactKey is the durable deduplication key (required).
	StableFactKey string `json:"stable_fact_key"`
	// SchemaVersion is the payload schema version (required).
	SchemaVersion string `json:"schema_version"`
	// CollectorKind is the collector that produced the fact. Falls back to the
	// parent scope's CollectorKind when empty.
	CollectorKind string `json:"collector_kind,omitempty"`
	// FencingToken is the generation fencing token. Defaults to 1.
	FencingToken int64 `json:"fencing_token,omitempty"`
	// SourceConfidence is the evidence quality label (e.g. "observed").
	SourceConfidence string `json:"source_confidence,omitempty"`
	// Payload is the fact-kind-specific data map (required).
	Payload map[string]any `json:"payload"`
	// IsTombstone marks this fact as a retraction.
	IsTombstone bool `json:"is_tombstone,omitempty"`
	// SourceURI is an optional provenance URI for the fact.
	SourceURI string `json:"source_uri,omitempty"`
}

// fencingToken returns the effective fencing token.
func (f Fact) fencingToken() int64 {
	if f.FencingToken > 0 {
		return f.FencingToken
	}
	return 1
}

// collectorKind returns the effective collector kind, falling back to the
// scope-level kind when the fact does not carry its own.
func (f Fact) collectorKind(scopeCollectorKind string) string {
	if ck := strings.TrimSpace(f.CollectorKind); ck != "" {
		return ck
	}
	return scopeCollectorKind
}

// sourceConfidence returns the effective source confidence label.
func (f Fact) sourceConfidence() string {
	if sc := strings.TrimSpace(f.SourceConfidence); sc != "" {
		return sc
	}
	return "observed"
}

// LoadFile reads and validates a cassette from the given file path.
func LoadFile(path string) (File, error) {
	// #nosec G304 -- path is an operator-supplied cassette location (the
	// -cassette-file flag / repo-shipped testdata), not user- or request-derived
	// input.
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read cassette file %q: %w", path, err)
	}
	var f File
	if err := json.Unmarshal(data, &f); err != nil {
		return File{}, fmt.Errorf("parse cassette file %q: %w", path, err)
	}
	if err := f.validate(); err != nil {
		return File{}, fmt.Errorf("invalid cassette file %q: %w", path, err)
	}
	return f, nil
}

func (f File) validate() error {
	if f.SchemaVersion != currentSchemaVersion {
		return fmt.Errorf("unsupported schema_version %q (want %q)", f.SchemaVersion, currentSchemaVersion)
	}
	if len(f.Scopes) == 0 {
		return errors.New("cassette must contain at least one scope")
	}
	for i, s := range f.Scopes {
		if err := s.validate(); err != nil {
			return fmt.Errorf("scope[%d]: %w", i, err)
		}
	}
	return nil
}

func (s Scope) validate() error {
	if strings.TrimSpace(s.ScopeID) == "" {
		return errors.New("scope_id is required")
	}
	if strings.TrimSpace(s.SourceSystem) == "" {
		return errors.New("source_system is required")
	}
	if strings.TrimSpace(s.ScopeKind) == "" {
		return errors.New("scope_kind is required")
	}
	if strings.TrimSpace(s.CollectorKind) == "" {
		return errors.New("collector_kind is required")
	}
	if strings.TrimSpace(s.GenerationID) == "" {
		return errors.New("generation_id is required")
	}
	if s.ObservedAt.IsZero() {
		return errors.New("observed_at is required and must be non-zero")
	}
	for i, fct := range s.Facts {
		if err := fct.validate(); err != nil {
			return fmt.Errorf("fact[%d]: %w", i, err)
		}
	}
	return nil
}

func (f Fact) validate() error {
	if strings.TrimSpace(f.FactKind) == "" {
		return errors.New("fact_kind is required")
	}
	if strings.TrimSpace(f.StableFactKey) == "" {
		return errors.New("stable_fact_key is required")
	}
	if strings.TrimSpace(f.SchemaVersion) == "" {
		return errors.New("schema_version is required")
	}
	if f.Payload == nil {
		return errors.New("payload is required (use {} for an empty payload)")
	}
	return nil
}
