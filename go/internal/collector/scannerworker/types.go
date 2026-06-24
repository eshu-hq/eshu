// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package scannerworker

import (
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const safeLocatorHashPrefix = "sha256:"

// TargetKind identifies the bounded target type passed to a scanner worker.
type TargetKind string

const (
	// TargetRepository scopes scanner work to one repository generation.
	TargetRepository TargetKind = "repository"
	// TargetImage scopes scanner work to one container image digest or image
	// artifact whose subject identity is carried by the analyzer source.
	TargetImage TargetKind = "image"
	// TargetArtifact scopes scanner work to one build, release, or package
	// artifact whose subject identity is carried by the analyzer source.
	TargetArtifact TargetKind = "artifact"
)

// TargetScope is the durable, bounded scanner target identity copied from a
// workflow work item.
type TargetScope struct {
	Kind             TargetKind
	ScopeID          string
	AcceptanceUnitID string
	SourceRunID      string
	GenerationID     string
	LocatorHash      string
}

// ResourceLimits describes the resource envelope a scanner worker must enforce.
type ResourceLimits struct {
	CPUMillis     int64
	MemoryBytes   int64
	Timeout       time.Duration
	MaxInputBytes int64
	MaxFiles      int64
	MaxFacts      int
}

// ClaimInput is the immutable scanner-worker input copied from one active
// workflow claim.
type ClaimInput struct {
	WorkItemID     string
	ClaimID        string
	FencingToken   int64
	OwnerID        string
	Analyzer       AnalyzerKind
	Target         TargetScope
	Limits         ResourceLimits
	GenerationID   string
	Attempt        int
	ClaimedAt      time.Time
	LeaseExpiresAt time.Time
	ObservedAt     time.Time
}

// FactOutput is the bounded source-fact result returned by a scanner worker.
type FactOutput struct {
	TargetCount int
	ResultCount int
	Facts       []facts.Envelope
}

// ResourceUsage captures measured resource usage for retry and dead-letter
// diagnostics.
type ResourceUsage struct {
	CPUSeconds      float64
	PeakMemoryBytes int64
}

func (target TargetScope) validateFor(item workflow.WorkItem) error {
	if strings.TrimSpace(string(target.Kind)) == "" {
		return fmt.Errorf("target kind must not be blank")
	}
	if !isAllowedTargetKind(target.Kind) {
		return fmt.Errorf("unsupported target kind %q", target.Kind)
	}
	if strings.TrimSpace(target.ScopeID) == "" {
		return fmt.Errorf("target scope_id must not be blank")
	}
	if strings.TrimSpace(target.AcceptanceUnitID) == "" {
		return fmt.Errorf("target acceptance_unit_id must not be blank")
	}
	if strings.TrimSpace(target.SourceRunID) == "" {
		return fmt.Errorf("target source_run_id must not be blank")
	}
	if strings.TrimSpace(target.GenerationID) == "" {
		return fmt.Errorf("target generation_id must not be blank")
	}
	if strings.TrimSpace(target.LocatorHash) == "" {
		return fmt.Errorf("target locator_hash must not be blank")
	}
	if err := validateSafeLocatorHash(target.LocatorHash); err != nil {
		return err
	}
	if target.ScopeID != item.ScopeID {
		return fmt.Errorf("target scope_id %q does not match work item scope_id %q", target.ScopeID, item.ScopeID)
	}
	if target.AcceptanceUnitID != item.AcceptanceUnitID {
		return fmt.Errorf("target acceptance_unit_id %q does not match work item acceptance_unit_id %q", target.AcceptanceUnitID, item.AcceptanceUnitID)
	}
	if target.SourceRunID != item.SourceRunID {
		return fmt.Errorf("target source_run_id %q does not match work item source_run_id %q", target.SourceRunID, item.SourceRunID)
	}
	if target.GenerationID != item.GenerationID {
		return fmt.Errorf("target generation_id %q does not match work item generation_id %q", target.GenerationID, item.GenerationID)
	}
	return nil
}

func isAllowedTargetKind(kind TargetKind) bool {
	switch kind {
	case TargetRepository, TargetImage, TargetArtifact:
		return true
	default:
		return false
	}
}

func validateSafeLocatorHash(value string) error {
	if !strings.HasPrefix(value, safeLocatorHashPrefix) {
		return fmt.Errorf("target locator_hash must use %s<64 hex>", safeLocatorHashPrefix)
	}
	hash := strings.TrimPrefix(value, safeLocatorHashPrefix)
	if len(hash) != 64 {
		return fmt.Errorf("target locator_hash must use %s<64 hex>", safeLocatorHashPrefix)
	}
	for _, char := range hash {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') && (char < 'A' || char > 'F') {
			return fmt.Errorf("target locator_hash must use %s<64 hex>", safeLocatorHashPrefix)
		}
	}
	return nil
}

func (limits ResourceLimits) validate() error {
	if limits.CPUMillis <= 0 {
		return fmt.Errorf("cpu_millis must be positive")
	}
	if limits.MemoryBytes <= 0 {
		return fmt.Errorf("memory_bytes must be positive")
	}
	if limits.Timeout <= 0 {
		return fmt.Errorf("timeout must be positive")
	}
	if limits.MaxInputBytes <= 0 {
		return fmt.Errorf("max_input_bytes must be positive")
	}
	if limits.MaxFiles <= 0 {
		return fmt.Errorf("max_files must be positive")
	}
	if limits.MaxFacts <= 0 {
		return fmt.Errorf("max_facts must be positive")
	}
	return nil
}
