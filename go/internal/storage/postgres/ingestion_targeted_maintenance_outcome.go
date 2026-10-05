// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"errors"
	"sort"
)

// OwedPartition names one (scope_id, generation_id) whose
// backward_evidence_committed phase an activation obligation owes (#7584).
type OwedPartition struct {
	ScopeID      string
	GenerationID string
}

// TargetedMaintenanceError is a typed refusal or per-owed result of the
// partition-scoped maintenance pass. Each value has a stable Reason, used as a
// bounded telemetry label, and is matched with errors.Is against the exported
// sentinels below.
type TargetedMaintenanceError struct {
	reason  string
	message string
}

// Error implements error.
func (e *TargetedMaintenanceError) Error() string { return e.message }

// Reason returns the stable, closed-set label of the error.
func (e *TargetedMaintenanceError) Reason() string { return e.reason }

var (
	// ErrTargetedMaintenanceCatalogChanged refuses a pass because an active
	// partition's memo row records another repository-catalog fingerprint. A
	// catalog change can alter evidence between repositories the owed
	// partitions never touch, so the pass cannot bound what it changes. The
	// epoch whole pass the same ingestion commit triggers rewrites every memo
	// and completes the obligation; the consumer holds and retries.
	ErrTargetedMaintenanceCatalogChanged = &TargetedMaintenanceError{
		reason:  "catalog_changed",
		message: "partition-scoped maintenance refused: repository catalog changed since active backward evidence was committed",
	}
	// ErrTargetedMaintenanceNoMemoBaseline refuses a pass because no active
	// partition holds a memo row (a fresh install before its first whole pass,
	// or an install whose every partition is ArgoCD-bearing). Without one the
	// catalog-change guard cannot tell whether the catalog changed, so the pass
	// waits for a whole pass to write the baseline.
	ErrTargetedMaintenanceNoMemoBaseline = &TargetedMaintenanceError{
		reason:  "no_memo_baseline",
		message: "partition-scoped maintenance refused: no active partition holds a memo row to compare the catalog against",
	}
	// ErrTargetedMaintenanceClosureTooDeep reports that promoting unprocessed
	// dependent partitions did not settle within the round bound.
	ErrTargetedMaintenanceClosureTooDeep = &TargetedMaintenanceError{
		reason:  "closure_too_deep",
		message: "partition-scoped maintenance closure did not settle within its round bound",
	}
	// ErrTargetedMaintenanceInapplicable is the per-owed result for a
	// partition that is the scope's active generation but that no pass can
	// ever publish a phase for: it maps to no repository in the shipped
	// active-repository read (a scope without a repository fact, or the scope
	// that loses a repo_id DISTINCT ON collision).
	ErrTargetedMaintenanceInapplicable = &TargetedMaintenanceError{
		reason:  "inapplicable",
		message: "owed partition can never carry a backward_evidence phase",
	}
	// ErrTargetedMaintenanceNotActive is the per-owed result for a partition
	// whose scope has moved to another generation.
	ErrTargetedMaintenanceNotActive = &TargetedMaintenanceError{
		reason:  "not_active",
		message: "owed partition is no longer the scope's active generation",
	}
	// ErrTargetedMaintenanceRetry is the per-owed result for an applicable,
	// active partition whose phase this pass did not publish (its generation
	// advanced under the pass, or the batch or fan-in guard skipped it).
	ErrTargetedMaintenanceRetry = &TargetedMaintenanceError{
		reason:  "retry",
		message: "owed partition phase was not published this pass",
	}
)

// targetedMaintenanceErrors is the closed sentinel set.
var targetedMaintenanceErrors = []*TargetedMaintenanceError{
	ErrTargetedMaintenanceCatalogChanged,
	ErrTargetedMaintenanceNoMemoBaseline,
	ErrTargetedMaintenanceClosureTooDeep,
	ErrTargetedMaintenanceInapplicable,
	ErrTargetedMaintenanceNotActive,
	ErrTargetedMaintenanceRetry,
}

// TargetedMaintenanceReasons returns the sorted closed set of reasons a
// TargetedMaintenanceError can carry.
func TargetedMaintenanceReasons() []string {
	reasons := make([]string, 0, len(targetedMaintenanceErrors))
	for _, sentinel := range targetedMaintenanceErrors {
		reasons = append(reasons, sentinel.reason)
	}
	sort.Strings(reasons)
	return reasons
}

// TargetedMaintenanceReason returns the stable reason of err: "" for nil, the
// TargetedMaintenanceError reason when err wraps one, and "error" otherwise.
func TargetedMaintenanceReason(err error) string {
	if err == nil {
		return ""
	}
	var typed *TargetedMaintenanceError
	if errors.As(err, &typed) {
		return typed.reason
	}
	return "error"
}

// TargetedMaintenanceOutcomeKind is the per-owed result of one pass.
type TargetedMaintenanceOutcomeKind string

const (
	// TargetedMaintenancePublished means the owed partition carries its
	// backward_evidence_committed phase after the pass.
	TargetedMaintenancePublished TargetedMaintenanceOutcomeKind = "published"
	// TargetedMaintenanceNotActive means the scope moved to another generation.
	TargetedMaintenanceNotActive TargetedMaintenanceOutcomeKind = "not_active"
	// TargetedMaintenanceInapplicable means no pass can publish a phase for it.
	TargetedMaintenanceInapplicable TargetedMaintenanceOutcomeKind = "inapplicable"
	// TargetedMaintenanceRetry means the phase was not published this pass.
	TargetedMaintenanceRetry TargetedMaintenanceOutcomeKind = "retry"
)

// TargetedMaintenanceOutcome is the result for one owed partition.
type TargetedMaintenanceOutcome struct {
	Partition OwedPartition
	Kind      TargetedMaintenanceOutcomeKind
}

// Err returns nil for a published phase and the matching sentinel otherwise.
func (o TargetedMaintenanceOutcome) Err() error {
	switch o.Kind {
	case TargetedMaintenancePublished:
		return nil
	case TargetedMaintenanceNotActive:
		return ErrTargetedMaintenanceNotActive
	case TargetedMaintenanceInapplicable:
		return ErrTargetedMaintenanceInapplicable
	default:
		return ErrTargetedMaintenanceRetry
	}
}

// buildTargetedMaintenanceSnapshot returns the scope -> generation snapshot
// the batch and fan-in guards compare against: one generation per scope over
// the loaded and affected partitions. A scope the two sets name with
// different generations advanced between the reads; it is left out (so both
// guards skip its repositories, exactly as the whole pass skips a scope
// missing from its snapshot) and returned in conflicts, sorted.
func buildTargetedMaintenanceSnapshot(
	load map[scopeGenerationPartition]struct{},
	affected map[scopeGenerationPartition]struct{},
) (map[string]string, []string) {
	snapshot := make(map[string]string, len(load)+len(affected))
	conflicted := make(map[string]struct{})
	for _, set := range []map[scopeGenerationPartition]struct{}{load, affected} {
		for partition := range set {
			if generation, seen := snapshot[partition.ScopeID]; seen && generation != partition.GenerationID {
				conflicted[partition.ScopeID] = struct{}{}
				continue
			}
			snapshot[partition.ScopeID] = partition.GenerationID
		}
	}
	conflicts := make([]string, 0, len(conflicted))
	for scopeID := range conflicted {
		delete(snapshot, scopeID)
		conflicts = append(conflicts, scopeID)
	}
	sort.Strings(conflicts)
	return snapshot, conflicts
}
