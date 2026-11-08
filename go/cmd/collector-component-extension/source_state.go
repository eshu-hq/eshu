// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// sourceStateRecorder returns the host-owned source-state writer for a
// collector kind, or nil when the kind has none. Only
// vulnerability_intelligence has one (#9420): the retired in-tree collector
// wrote vulnerability_source_states, and the extracted collector has no
// storage handle by design, so the host writes the rows from claim outcomes
// through the same store the in-tree command used. pollInterval is
// ClaimedService's minimum retry delay, so next_retry_at matches the delay
// the claim actually gets.
func sourceStateRecorder(
	kind scope.CollectorKind,
	database db.ExecQueryer,
	instanceID string,
	pollInterval time.Duration,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (extensionhost.ClaimOutcomeRecorder, error) {
	if kind != scope.CollectorVulnerabilityIntelligence {
		return nil, nil
	}
	recorder, err := vulnerability.NewRecorder(vulnerability.Config{
		Store:               vulnerabilitystore.NewVulnerabilitySourceStateStore(database),
		CollectorInstanceID: instanceID,
		RetryDelay:          pollInterval,
		MaxAttempts:         workflow.DefaultClaimMaxAttempts(),
		Instruments:         instruments,
		Logger:              logger,
	})
	if err != nil {
		return nil, err
	}
	return recorder, nil
}

// claimCommitPolicy returns the host commit policy for a collector kind,
// or nil when the kind has none. Only vulnerability_intelligence has one
// (#10037): the GHSA window policy arms the per-stream cursor
// compare-and-advance and write-once from the snapshot's stated
// covered_through, because the extracted collector holds no storage
// handle by design. The policy handles only GHSA window scopes; every
// other claim keeps its existing commit behavior, and GHSA stays off
// because no coordinator configuration can mint a window scope yet.
func claimCommitPolicy(
	kind scope.CollectorKind,
	instanceID string,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (extensionhost.ClaimCommitPolicy, error) {
	if kind != scope.CollectorVulnerabilityIntelligence {
		return nil, nil
	}
	return extensionhost.NewGHSAWindowCommitPolicy(instanceID, instruments, logger)
}
