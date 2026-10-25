// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package snapshot

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"

	statuspkg "github.com/eshu-hq/eshu/go/internal/status"
	"github.com/eshu-hq/eshu/go/internal/status/collector"
	"github.com/eshu-hq/eshu/go/internal/status/shared"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/status/summary"
)

// encodingTag names the stored entry encoding. It is part of the source
// digest, so changing the encoding makes a reader refuse rows an older writer
// stored instead of decoding them.
const encodingTag = "coordinator-snapshot/2"

// The sections of the stored coordinator_snapshot model, in the order Encode
// writes them. The claims section is the model's anchor: a non-empty row always
// has exactly one, and an empty row is the live read's nil snapshot.
const (
	sectionInstance          = "instance"
	sectionRunCount          = "run_count"
	sectionWorkItemCount     = "work_item_count"
	sectionCompletenessCount = "completeness_count"
	sectionBackpressure      = "backpressure"
	sectionTerminalFailure   = "terminal_failure"
	sectionClaims            = "claims"
	sectionRecentFailures    = "recent_failures"
)

type instanceWire struct {
	InstanceID     string  `json:"instance_id"`
	CollectorKind  string  `json:"collector_kind"`
	Mode           string  `json:"mode"`
	Enabled        bool    `json:"enabled"`
	Bootstrap      bool    `json:"bootstrap"`
	ClaimsEnabled  bool    `json:"claims_enabled"`
	DisplayName    string  `json:"display_name"`
	LastObservedAt string  `json:"last_observed_at"`
	UpdatedAt      string  `json:"updated_at"`
	DeactivatedAt  *string `json:"deactivated_at"`
	ScanIntervalNS int64   `json:"scan_interval_ns"`
	// ExtensionHosted records that a component activation hosts the instance;
	// the runtime health bound of its terminal-failure class depends on it.
	ExtensionHosted bool `json:"extension_hosted"`
}

type countWire struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type backpressureWire struct {
	CollectorKind       string      `json:"collector_kind"`
	CollectorInstanceID string      `json:"collector_instance_id"`
	SourceSystem        string      `json:"source_system"`
	Pending             int         `json:"pending"`
	Claimed             int         `json:"claimed"`
	Retrying            int         `json:"retrying"`
	DeadLetter          int         `json:"dead_letter"`
	TerminalFailed      int         `json:"terminal_failed"`
	Expired             int         `json:"expired"`
	ActiveClaims        int         `json:"active_claims"`
	OverdueClaims       int         `json:"overdue_claims"`
	OldestPendingAgeNS  int64       `json:"oldest_pending_age_ns"`
	OldestRetryAgeNS    int64       `json:"oldest_retry_age_ns"`
	OldestClaimAgeNS    int64       `json:"oldest_claim_age_ns"`
	NextRetryDelayNS    int64       `json:"next_retry_delay_ns"`
	FailureClassCounts  []countWire `json:"failure_class_counts"`
}

type terminalFailureWire struct {
	CollectorKind    string  `json:"collector_kind"`
	InstanceID       string  `json:"instance_id"`
	ScopeID          string  `json:"scope_id"`
	WorkItemID       string  `json:"work_item_id"`
	FailureClass     string  `json:"failure_class"`
	FailureOperation string  `json:"failure_operation"`
	ObservedAt       string  `json:"observed_at"`
	Acknowledged     bool    `json:"acknowledged"`
	AcknowledgedAt   *string `json:"acknowledged_at"`
}

type claimsWire struct {
	ActiveClaims       int   `json:"active_claims"`
	OverdueClaims      int   `json:"overdue_claims"`
	OldestPendingAgeNS int64 `json:"oldest_pending_age_ns"`
}

type recentFailuresWire struct {
	WindowNS            int64 `json:"window_ns"`
	FailedRuns          int   `json:"failed_runs"`
	BlockedCompleteness int   `json:"blocked_completeness"`
	TerminalWorkItems   int   `json:"terminal_work_items"`
}

// SourceSHA256 returns the hex SHA-256 identifying what a binary would store:
// every statement text the snapshot is computed from, in order, and the entry
// encoding. The writer stores it with the row and the reader compares it with
// its own, so a row computed by other statements or encoded another way is
// never decoded.
func SourceSHA256(statements ...string) string {
	digest := sha256.New()
	for _, part := range append([]string{encodingTag}, statements...) {
		_, _ = digest.Write([]byte(part))
		_, _ = digest.Write([]byte{0})
	}
	return hex.EncodeToString(digest.Sum(nil))
}

// Encode returns the live coordinator snapshot as stored entries, one per
// instance, count, backpressure row, and terminal failure, plus one claims
// entry and, when the live read computed a window, one recent_failures entry.
// A nil snapshot, the live read's answer for a deployment with no coordinator
// state, encodes as no entries.
func Encode(snap *statuspkg.CoordinatorSnapshot) ([]summary.Entry, error) {
	if snap == nil {
		return []summary.Entry{}, nil
	}
	var entries []summary.Entry
	add := func(section string, ordinal int, value any) error {
		encoded, err := json.Marshal(value)
		if err != nil {
			return fmt.Errorf("encode coordinator snapshot %s: %w", section, err)
		}
		entries = append(entries, summary.Entry{Section: section, Ordinal: int64(ordinal), JSON: string(encoded)})
		return nil
	}
	for i, in := range snap.CollectorInstances {
		if err := add(sectionInstance, i+1, instanceWire{
			InstanceID: in.InstanceID, CollectorKind: in.CollectorKind, Mode: in.Mode, Enabled: in.Enabled,
			Bootstrap: in.Bootstrap, ClaimsEnabled: in.ClaimsEnabled, DisplayName: in.DisplayName,
			LastObservedAt: wireTime(in.LastObservedAt), UpdatedAt: wireTime(in.UpdatedAt),
			DeactivatedAt: optionalWireTime(in.DeactivatedAt), ScanIntervalNS: int64(in.ScanInterval), ExtensionHosted: in.ExtensionHosted,
		}); err != nil {
			return nil, err
		}
	}
	for _, group := range []struct {
		section string
		counts  []shared.NamedCount
	}{
		{sectionRunCount, snap.RunStatusCounts},
		{sectionWorkItemCount, snap.WorkItemStatusCounts},
		{sectionCompletenessCount, snap.CompletenessCounts},
	} {
		for i, count := range group.counts {
			if err := add(group.section, i+1, countWire{Name: count.Name, Count: count.Count}); err != nil {
				return nil, err
			}
		}
	}
	for i, row := range snap.CollectorBackpressure {
		if err := add(sectionBackpressure, i+1, backpressureToWire(row)); err != nil {
			return nil, err
		}
	}
	for i, failure := range snap.CollectorTerminalFailures {
		if err := add(sectionTerminalFailure, i+1, terminalFailureWire{
			CollectorKind: failure.CollectorKind, InstanceID: failure.InstanceID, ScopeID: failure.ScopeID,
			WorkItemID: failure.WorkItemID, FailureClass: failure.FailureClass, FailureOperation: failure.FailureOperation,
			ObservedAt: wireTime(failure.ObservedAt), Acknowledged: failure.Acknowledged,
			AcknowledgedAt: optionalWireTime(failure.AcknowledgedAt),
		}); err != nil {
			return nil, err
		}
	}
	if err := add(sectionClaims, 1, claimsWire{
		ActiveClaims: snap.ActiveClaims, OverdueClaims: snap.OverdueClaims, OldestPendingAgeNS: int64(snap.OldestPendingAge),
	}); err != nil {
		return nil, err
	}
	if recent := snap.RecentFailures; recent != nil {
		if err := add(sectionRecentFailures, 1, recentFailuresWire{
			WindowNS: int64(recent.Window), FailedRuns: recent.FailedRuns,
			BlockedCompleteness: recent.BlockedCompleteness, TerminalWorkItems: recent.TerminalWorkItems,
		}); err != nil {
			return nil, err
		}
	}
	return entries, nil
}

// Decode rebuilds the snapshot the live read returns from stored entries; no
// entries is the live read's nil snapshot. The empty sections the live read
// returns as empty slices decode to empty non-nil slices, and the terminal
// failures stay nil when none are stored. It is strict: an unknown section or
// field, a wrong type, a bad timestamp, trailing data, a missing or repeated
// claims entry, or a repeated recent_failures entry is an error, so a reader
// falls back instead of serving a row it only half understands.
func Decode(entries []summary.Entry) (*statuspkg.CoordinatorSnapshot, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	snap := &statuspkg.CoordinatorSnapshot{
		CollectorInstances:    []collector.InstanceSummary{},
		RunStatusCounts:       []shared.NamedCount{},
		WorkItemStatusCounts:  []shared.NamedCount{},
		CompletenessCounts:    []shared.NamedCount{},
		CollectorBackpressure: []collector.BackpressureSnapshot{},
	}
	claims, recent := 0, 0
	for _, entry := range entries {
		var err error
		switch entry.Section {
		case sectionInstance:
			err = decodeInstance(entry, snap)
		case sectionRunCount:
			err = decodeCount(entry, &snap.RunStatusCounts)
		case sectionWorkItemCount:
			err = decodeCount(entry, &snap.WorkItemStatusCounts)
		case sectionCompletenessCount:
			err = decodeCount(entry, &snap.CompletenessCounts)
		case sectionBackpressure:
			err = decodeBackpressure(entry, snap)
		case sectionTerminalFailure:
			err = decodeTerminalFailure(entry, snap)
		case sectionClaims:
			claims++
			var wire claimsWire
			if err = decodeStrict(entry.JSON, &wire); err == nil {
				snap.ActiveClaims, snap.OverdueClaims = wire.ActiveClaims, wire.OverdueClaims
				snap.OldestPendingAge = time.Duration(wire.OldestPendingAgeNS)
			}
		case sectionRecentFailures:
			recent++
			var wire recentFailuresWire
			if err = decodeStrict(entry.JSON, &wire); err == nil {
				snap.RecentFailures = &statuspkg.CoordinatorRecentFailures{
					Window: time.Duration(wire.WindowNS), FailedRuns: wire.FailedRuns,
					BlockedCompleteness: wire.BlockedCompleteness, TerminalWorkItems: wire.TerminalWorkItems,
				}
			}
		default:
			err = fmt.Errorf("unknown section %q", entry.Section)
		}
		if err != nil {
			return nil, fmt.Errorf("decode coordinator snapshot %s %d: %w", entry.Section, entry.Ordinal, err)
		}
	}
	if claims != 1 {
		return nil, fmt.Errorf("decode coordinator snapshot: %d claims entries, want 1", claims)
	}
	if recent > 1 {
		return nil, fmt.Errorf("decode coordinator snapshot: %d recent_failures entries, want at most 1", recent)
	}
	return snap, nil
}

func backpressureToWire(row collector.BackpressureSnapshot) backpressureWire {
	wire := backpressureWire{
		CollectorKind: row.CollectorKind, CollectorInstanceID: row.CollectorInstanceID, SourceSystem: row.SourceSystem,
		Pending: row.Pending, Claimed: row.Claimed, Retrying: row.Retrying, DeadLetter: row.DeadLetter,
		TerminalFailed: row.TerminalFailed, Expired: row.Expired, ActiveClaims: row.ActiveClaims,
		OverdueClaims: row.OverdueClaims, OldestPendingAgeNS: int64(row.OldestPendingAge),
		OldestRetryAgeNS: int64(row.OldestRetryAge), OldestClaimAgeNS: int64(row.OldestClaimAge),
		NextRetryDelayNS: int64(row.NextRetryDelay),
	}
	for _, count := range row.FailureClassCounts {
		wire.FailureClassCounts = append(wire.FailureClassCounts, countWire{Name: count.Name, Count: count.Count})
	}
	return wire
}

func decodeInstance(entry summary.Entry, snap *statuspkg.CoordinatorSnapshot) error {
	var wire instanceWire
	if err := decodeStrict(entry.JSON, &wire); err != nil {
		return err
	}
	observed, err := parseWireTime(wire.LastObservedAt)
	if err != nil {
		return fmt.Errorf("last_observed_at: %w", err)
	}
	updated, err := parseWireTime(wire.UpdatedAt)
	if err != nil {
		return fmt.Errorf("updated_at: %w", err)
	}
	deactivated, err := parseOptionalWireTime(wire.DeactivatedAt)
	if err != nil {
		return fmt.Errorf("deactivated_at: %w", err)
	}
	snap.CollectorInstances = append(snap.CollectorInstances, collector.InstanceSummary{
		InstanceID: wire.InstanceID, CollectorKind: wire.CollectorKind, Mode: wire.Mode, Enabled: wire.Enabled,
		Bootstrap: wire.Bootstrap, ClaimsEnabled: wire.ClaimsEnabled, DisplayName: wire.DisplayName,
		LastObservedAt: observed, UpdatedAt: updated, DeactivatedAt: deactivated,
		ScanInterval: time.Duration(wire.ScanIntervalNS), ExtensionHosted: wire.ExtensionHosted,
	})
	return nil
}

func decodeCount(entry summary.Entry, target *[]shared.NamedCount) error {
	var wire countWire
	if err := decodeStrict(entry.JSON, &wire); err != nil {
		return err
	}
	*target = append(*target, shared.NamedCount{Name: wire.Name, Count: wire.Count})
	return nil
}

func decodeBackpressure(entry summary.Entry, snap *statuspkg.CoordinatorSnapshot) error {
	var wire backpressureWire
	if err := decodeStrict(entry.JSON, &wire); err != nil {
		return err
	}
	row := collector.BackpressureSnapshot{
		CollectorKind: wire.CollectorKind, CollectorInstanceID: wire.CollectorInstanceID, SourceSystem: wire.SourceSystem,
		Pending: wire.Pending, Claimed: wire.Claimed, Retrying: wire.Retrying, DeadLetter: wire.DeadLetter,
		TerminalFailed: wire.TerminalFailed, Expired: wire.Expired, ActiveClaims: wire.ActiveClaims,
		OverdueClaims: wire.OverdueClaims, OldestPendingAge: time.Duration(wire.OldestPendingAgeNS),
		OldestRetryAge: time.Duration(wire.OldestRetryAgeNS), OldestClaimAge: time.Duration(wire.OldestClaimAgeNS),
		NextRetryDelay: time.Duration(wire.NextRetryDelayNS),
	}
	for _, count := range wire.FailureClassCounts {
		row.FailureClassCounts = append(row.FailureClassCounts, shared.NamedCount{Name: count.Name, Count: count.Count})
	}
	snap.CollectorBackpressure = append(snap.CollectorBackpressure, row)
	return nil
}

func decodeTerminalFailure(entry summary.Entry, snap *statuspkg.CoordinatorSnapshot) error {
	var wire terminalFailureWire
	if err := decodeStrict(entry.JSON, &wire); err != nil {
		return err
	}
	observed, err := parseWireTime(wire.ObservedAt)
	if err != nil {
		return fmt.Errorf("observed_at: %w", err)
	}
	acknowledged, err := parseOptionalWireTime(wire.AcknowledgedAt)
	if err != nil {
		return fmt.Errorf("acknowledged_at: %w", err)
	}
	snap.CollectorTerminalFailures = append(snap.CollectorTerminalFailures, collector.TerminalFailure{
		CollectorKind: wire.CollectorKind, InstanceID: wire.InstanceID, ScopeID: wire.ScopeID, WorkItemID: wire.WorkItemID,
		FailureClass: wire.FailureClass, FailureOperation: wire.FailureOperation, ObservedAt: observed,
		Acknowledged: wire.Acknowledged, AcknowledgedAt: acknowledged,
	})
	return nil
}

func wireTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

func optionalWireTime(t time.Time) *string {
	if t.IsZero() {
		return nil
	}
	text := wireTime(t)
	return &text
}

func parseWireTime(text string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, text)
	if err != nil {
		return time.Time{}, err
	}
	return parsed.UTC(), nil
}

func parseOptionalWireTime(text *string) (time.Time, error) {
	if text == nil {
		return time.Time{}, nil
	}
	return parseWireTime(*text)
}

func decodeStrict(text string, target any) error {
	decoder := json.NewDecoder(bytes.NewReader([]byte(text)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("trailing data after the entry")
	}
	return nil
}
