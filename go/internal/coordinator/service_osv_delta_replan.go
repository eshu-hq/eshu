// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// osvDeltaWatermarkStaleReason is the warning reason the coordinator logs
// when a replan watermark's last successful poll is older than the
// provisional stale bound. It matches the wording the freshness signals use,
// so one alert query covers both.
const osvDeltaWatermarkStaleReason = "osv_delta_watermark_stale"

// OSVDeltaChangedNameReader loads the delta feed's changed_name facts for
// one replan pass. The production implementation is FactStore.
// ListVulnerabilityChangedNames, wired in cmd at enablement; tests and the
// flag-off path never call it.
type OSVDeltaChangedNameReader interface {
	ListVulnerabilityChangedNames(context.Context) ([]vulnerability.OSVDeltaChangedRecord, error)
}

// OSVDeltaStoredNameReader loads the packages Eshu already stores per
// advisory id, so the intersection unions the record's current names with
// the stored ones. The production implementation is FactStore.
// ListStoredAdvisoryPackageNames, wired in cmd at enablement.
type OSVDeltaStoredNameReader interface {
	ListStoredAdvisoryPackageNames(context.Context, []string) (map[string][]vulnerability.OSVDeltaStoredPackage, error)
}

// OSVDeltaOwnedNameResolver maps intersected (ecosystem, name) lookups onto
// the owned dependency inventory. The production implementation is FactStore.
// ResolveOwnedPackageNames, wired in cmd at enablement.
type OSVDeltaOwnedNameResolver interface {
	ResolveOwnedPackageNames(context.Context, []vulnerability.OSVDeltaLookup) ([]vulnerability.OSVDeltaResolvedTarget, error)
}

// OSVDeltaReplanStore persists the per-instance change-driven replan queue
// and its commit watermark. The production implementation is the
// vulnerabilitystore.OwnedTargetReplanStore, wired in cmd at enablement.
type OSVDeltaReplanStore interface {
	AdmitChangedTargets(context.Context, string, []vulnerability.OSVDeltaValidatedChange) (int, int, error)
	ReadPendingReplan(context.Context, string) ([]vulnerability.OSVDeltaQueuedRow, error)
	CommitReplanDrain(context.Context, string, []int64, time.Time, time.Time) (int64, error)
	ReadCommitWatermark(context.Context, string) (vulnerability.OSVDeltaWatermark, error)
	RecordPollSucceeded(context.Context, string, time.Time) error
}

// osvDeltaPass carries one tick's replan state from preparation (before the
// plan) to commit (after admission).
type osvDeltaPass struct {
	store       OSVDeltaReplanStore
	instanceID  string
	request     *vulnerability.OSVDeltaDrainRequest
	pending     []vulnerability.OSVDeltaValidatedChange
	admitted    int
	duplicates  int
	parseFailed int
}

// osvDeltaReplanEnabled reports whether one instance runs the change-driven
// re-query drain: the owned-package derivation enables it, sources it from
// OSV, and opts in per instance. Off plans no _replan scopes and touches no
// replan store — the admission, the readers, and the queue stay idle.
func osvDeltaReplanEnabled(derivation vulnerability.DerivationConfiguration) bool {
	if !derivation.Enabled || !derivation.OSVDeltaReplan {
		return false
	}
	for _, source := range derivation.Sources {
		if strings.TrimSpace(source) == "osv" {
			return true
		}
	}
	return false
}

// prepareOSVDeltaReplan runs the pre-plan half of one replan pass: read the
// changed facts and the queued rows, union each with the stored names,
// admit the new tuples, resolve the whole hit list once, and build the
// drain request. It returns nil without touching any replan store when the
// flag is off. A missing reader or store with the flag on fails loudly
// instead of silently planning no replan targets.
func (s Service) prepareOSVDeltaReplan(
	ctx context.Context,
	instance workflow.CollectorInstance,
	derivation vulnerability.DerivationConfiguration,
) (*osvDeltaPass, error) {
	if !osvDeltaReplanEnabled(derivation) {
		return nil, nil
	}
	if s.OSVDeltaChangedNameReader == nil {
		return nil, fmt.Errorf("changed name reader is required for osv delta replan targets")
	}
	if s.OSVDeltaStoredNameReader == nil {
		return nil, fmt.Errorf("stored advisory name reader is required for osv delta replan targets")
	}
	if s.OSVDeltaOwnedNameResolver == nil {
		return nil, fmt.Errorf("owned package name resolver is required for osv delta replan targets")
	}
	if s.OSVDeltaReplanStore == nil {
		return nil, fmt.Errorf("owned target replan store is required for osv delta replan targets")
	}
	ctx, span := otel.Tracer(telemetry.DefaultSignalName).Start(ctx, contract.SpanOSVDeltaReplanDrain)
	defer span.End()

	changedRecords, err := s.OSVDeltaChangedNameReader.ListVulnerabilityChangedNames(ctx)
	if err != nil {
		return nil, fmt.Errorf("list vulnerability changed names: %w", err)
	}
	// The first pending read feeds the stored-name lookup: the union must
	// cover queued rows whose advisories report no change this pass (late
	// stored names arrive after the change did).
	pending, err := s.OSVDeltaReplanStore.ReadPendingReplan(ctx, instance.InstanceID)
	if err != nil {
		return nil, fmt.Errorf("read pending owned target replan: %w", err)
	}
	pendingRecords := make([]vulnerability.OSVDeltaChangedRecord, 0, len(pending))
	for _, row := range pending {
		pendingRecords = append(pendingRecords, vulnerability.OSVDeltaChangedRecord{
			AdvisoryID:    row.AdvisoryID,
			Ecosystem:     row.Ecosystem,
			PackageName:   row.PackageName,
			Withdrawn:     row.Withdrawn,
			FeedTimestamp: row.FeedTimestamp.UTC().Format(time.RFC3339),
			RowID:         row.RowID,
		})
	}
	advisorySet := make(map[string]struct{})
	for _, record := range changedRecords {
		if id := strings.TrimSpace(record.AdvisoryID); id != "" {
			advisorySet[id] = struct{}{}
		}
	}
	for _, record := range pendingRecords {
		if id := strings.TrimSpace(record.AdvisoryID); id != "" {
			advisorySet[id] = struct{}{}
		}
	}
	advisoryIDs := make([]string, 0, len(advisorySet))
	for id := range advisorySet {
		advisoryIDs = append(advisoryIDs, id)
	}
	stored, err := s.OSVDeltaStoredNameReader.ListStoredAdvisoryPackageNames(ctx, advisoryIDs)
	if err != nil {
		return nil, fmt.Errorf("list stored advisory package names: %w", err)
	}
	if stored == nil {
		stored = make(map[string][]vulnerability.OSVDeltaStoredPackage)
	}
	changedIntersection := vulnerability.IntersectOSVDeltaChangedNames(changedRecords, stored)
	admitted, duplicates, err := s.OSVDeltaReplanStore.AdmitChangedTargets(ctx, instance.InstanceID, changedIntersection.Changes)
	if err != nil {
		return nil, fmt.Errorf("admit owned target replan: %w", err)
	}
	if admitted > 0 {
		// Re-read after the admission, so tuples this pass admitted drain
		// in the same pass instead of waiting a whole poll behind. The
		// stored map already covers their advisories: new rows only carry
		// advisories the changed set named.
		pending, err = s.OSVDeltaReplanStore.ReadPendingReplan(ctx, instance.InstanceID)
		if err != nil {
			return nil, fmt.Errorf("read pending owned target replan: %w", err)
		}
		pendingRecords = pendingRecords[:0]
		for _, row := range pending {
			pendingRecords = append(pendingRecords, vulnerability.OSVDeltaChangedRecord{
				AdvisoryID:    row.AdvisoryID,
				Ecosystem:     row.Ecosystem,
				PackageName:   row.PackageName,
				Withdrawn:     row.Withdrawn,
				FeedTimestamp: row.FeedTimestamp.UTC().Format(time.RFC3339),
				RowID:         row.RowID,
			})
		}
	}
	drainIntersection := vulnerability.IntersectOSVDeltaChangedNames(pendingRecords, stored)
	lookups := append(append([]vulnerability.OSVDeltaLookup(nil), changedIntersection.Lookups...), drainIntersection.Lookups...)
	lookupSet := make(map[vulnerability.OSVDeltaLookup]struct{}, len(lookups))
	deduped := lookups[:0]
	for _, lookup := range lookups {
		if _, dup := lookupSet[lookup]; dup {
			continue
		}
		lookupSet[lookup] = struct{}{}
		deduped = append(deduped, lookup)
	}
	var resolved []vulnerability.OSVDeltaResolvedTarget
	if len(deduped) > 0 {
		resolved, err = s.OSVDeltaOwnedNameResolver.ResolveOwnedPackageNames(ctx, deduped)
		if err != nil {
			return nil, fmt.Errorf("resolve owned package names for replan: %w", err)
		}
	}
	pass := &osvDeltaPass{
		store:       s.OSVDeltaReplanStore,
		instanceID:  instance.InstanceID,
		pending:     drainIntersection.Changes,
		admitted:    admitted,
		duplicates:  duplicates,
		parseFailed: changedIntersection.ParseFailed,
	}
	pass.request = &vulnerability.OSVDeltaDrainRequest{
		Candidates: osvDeltaDrainCandidates(drainIntersection.Changes, resolved),
		Trivial:    osvDeltaTrivialRows(drainIntersection.Changes, resolved),
	}
	span.SetAttributes(
		attribute.Int("osv_delta_replan.changed", len(changedRecords)),
		attribute.Int("osv_delta_replan.pending", len(pendingRecords)),
		attribute.Int("osv_delta_replan.admitted", admitted),
		attribute.Int("osv_delta_replan.duplicates", duplicates),
		attribute.Int("osv_delta_replan.parse_failed", pass.parseFailed),
		attribute.Int("osv_delta_replan.lookups", len(deduped)),
		attribute.Int("osv_delta_replan.candidates", len(pass.request.Candidates)),
		attribute.Int("osv_delta_replan.trivial", len(pass.request.Trivial)),
	)
	return pass, nil
}

// osvDeltaDrainCandidates maps the resolution onto the queued rows: one
// candidate per resolved target per row whose union names the lookup.
func osvDeltaDrainCandidates(
	changes []vulnerability.OSVDeltaValidatedChange,
	resolved []vulnerability.OSVDeltaResolvedTarget,
) []vulnerability.OSVDeltaDrainCandidate {
	byLookup := make(map[vulnerability.OSVDeltaLookup][]vulnerability.OSVDeltaResolvedTarget)
	for _, target := range resolved {
		byLookup[target.Lookup] = append(byLookup[target.Lookup], target)
	}
	var out []vulnerability.OSVDeltaDrainCandidate
	for _, change := range changes {
		for _, name := range change.UnionNames {
			for _, target := range byLookup[vulnerability.OSVDeltaLookup{Ecosystem: change.Ecosystem, Name: name}] {
				out = append(out, vulnerability.OSVDeltaDrainCandidate{
					Ecosystem:     target.Lookup.Ecosystem,
					PackageName:   target.Lookup.Name,
					Version:       target.Version,
					KeyHashLow24:  target.KeyHashLow24,
					VirtualSlot:   target.VirtualSlot,
					RowIDs:        []int64{change.RowID},
					FeedTimestamp: change.FeedTimestamp,
				})
			}
		}
	}
	return out
}

// osvDeltaTrivialRows lists the queued rows no lookup resolved to an owned
// target: nothing they name is owned, so the drain commits past them
// instead of letting them pin the watermark.
func osvDeltaTrivialRows(
	changes []vulnerability.OSVDeltaValidatedChange,
	resolved []vulnerability.OSVDeltaResolvedTarget,
) []vulnerability.OSVDeltaTrivialRow {
	covered := make(map[vulnerability.OSVDeltaLookup]struct{}, len(resolved))
	for _, target := range resolved {
		covered[target.Lookup] = struct{}{}
	}
	var out []vulnerability.OSVDeltaTrivialRow
	for _, change := range changes {
		trivial := true
		for _, name := range change.UnionNames {
			if _, ok := covered[vulnerability.OSVDeltaLookup{Ecosystem: change.Ecosystem, Name: name}]; ok {
				trivial = false
				break
			}
		}
		if trivial {
			out = append(out, vulnerability.OSVDeltaTrivialRow{
				RowID:         change.RowID,
				FeedTimestamp: change.FeedTimestamp,
			})
		}
	}
	return out
}

// commitOSVDeltaReplan runs the post-admission half: commit exactly the
// drained rows whose scopes survived authorization, advance the watermark
// over them, record the successful poll, and report the pass. Rows whose
// scopes the tenant boundary denied stay pending for the next pass. A row
// riding a scope the plan already held commits unconditionally: that scope
// is queued by construction.
func (s Service) commitOSVDeltaReplan(
	ctx context.Context,
	pass *osvDeltaPass,
	run workflow.Run,
	items []workflow.WorkItem,
	observation vulnerability.OSVDeltaReplanObservation,
	observedAt time.Time,
) error {
	allowed := make(map[string]struct{})
	for _, item := range items {
		if strings.Contains(item.ScopeID, "/_replan/") {
			allowed[item.ScopeID] = struct{}{}
		}
	}
	if len(allowed) > 0 {
		authorized, _, err := s.authorizeWorkflowWorkItems(ctx, run, replanItems(items))
		if err != nil {
			return fmt.Errorf("authorize owned target replan items: %w", err)
		}
		allowed = make(map[string]struct{}, len(authorized))
		for _, item := range authorized {
			allowed[item.ScopeID] = struct{}{}
		}
	}
	pendingByID := make(map[int64]vulnerability.OSVDeltaValidatedChange, len(pass.pending))
	for _, change := range pass.pending {
		pendingByID[change.RowID] = change
	}
	var survivorIDs []int64
	survivorTS := time.Time{}
	for _, id := range observation.CoveredRowIDs {
		change, ok := pendingByID[id]
		if !ok {
			continue
		}
		if scopes, mapped := observation.CoveredRowScopes[id]; mapped {
			authorized := false
			for _, scopeID := range scopes {
				if _, ok := allowed[scopeID]; ok {
					authorized = true
					break
				}
			}
			if !authorized {
				continue
			}
		}
		// Unmapped rows are trivial or ride a scope the plan already held:
		// queued by construction, so they commit unconditionally.
		survivorIDs = append(survivorIDs, id)
		if survivorTS.IsZero() || change.FeedTimestamp.After(survivorTS) {
			survivorTS = change.FeedTimestamp
		}
	}
	if len(survivorIDs) > 0 && !survivorTS.IsZero() {
		if _, err := pass.store.CommitReplanDrain(ctx, pass.instanceID, survivorIDs, survivorTS, observedAt.UTC()); err != nil {
			return fmt.Errorf("commit owned target replan drain: %w", err)
		}
	}
	if err := pass.store.RecordPollSucceeded(ctx, pass.instanceID, observedAt.UTC()); err != nil {
		return fmt.Errorf("record owned target replan poll: %w", err)
	}
	s.reportOSVDeltaReplan(ctx, pass, observation, survivorIDs, observedAt)
	return nil
}

// replanItems selects the extra _replan work items of one plan.
func replanItems(items []workflow.WorkItem) []workflow.WorkItem {
	var out []workflow.WorkItem
	for _, item := range items {
		if strings.Contains(item.ScopeID, "/_replan/") {
			out = append(out, item)
		}
	}
	return out
}

// reportOSVDeltaReplan records the pass on the replan signals and warns
// when the commit watermark's last successful poll is stale. Quiet passes
// stay silent in the logs; the gauges carry them.
func (s Service) reportOSVDeltaReplan(
	ctx context.Context,
	pass *osvDeltaPass,
	observation vulnerability.OSVDeltaReplanObservation,
	survivorIDs []int64,
	observedAt time.Time,
) {
	watermark, err := pass.store.ReadCommitWatermark(ctx, pass.instanceID)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn(
				"workflow coordinator owned target replan watermark unreadable",
				"collector_instance_id", pass.instanceID,
			)
		}
		return
	}
	depth := len(pass.pending) - len(survivorIDs)
	oldest := time.Time{}
	for _, change := range pass.pending {
		skip := false
		for _, id := range survivorIDs {
			if id == change.RowID {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		if oldest.IsZero() || change.FeedTimestamp.Before(oldest) {
			oldest = change.FeedTimestamp
		}
	}
	now := observedAt.UTC()
	record := OSVDeltaReplanMetricsObservation{
		CollectorInstanceID: pass.instanceID,
		Admitted:            pass.admitted,
		Duplicates:          pass.duplicates,
		ParseFailed:         pass.parseFailed,
		Drained:             len(survivorIDs),
		Overflow:            depth,
		PendingDepth:        depth,
	}
	if !oldest.IsZero() {
		record.OldestPendingAge, record.OldestPendingKnown = now.Sub(oldest), true
	}
	if watermark.CommittedSet {
		record.WatermarkAge, record.WatermarkKnown = now.Sub(watermark.Committed.UTC()), true
	}
	if watermark.LastPollSet {
		record.PollAge, record.PollKnown = now.Sub(watermark.LastPoll.UTC()), true
	}
	if s.Metrics != nil {
		s.Metrics.RecordOSVDeltaReplan(ctx, record)
	}
	if s.Logger == nil {
		return
	}
	if len(survivorIDs) > 0 {
		s.Logger.Info(
			"workflow coordinator committed owned target replan drain",
			"collector_instance_id", pass.instanceID,
			"drained_rows", len(survivorIDs),
			"overflow_rows", depth,
			"scopes_minted", observation.ScopesMinted,
			"watermark_set", observation.WatermarkSet,
		)
	}
	if vulnerability.OSVDeltaWatermarkStale(watermark.LastPoll, watermark.LastPollSet, now) {
		s.Logger.Warn(
			"workflow coordinator owned target replan watermark is stale",
			"collector_instance_id", pass.instanceID,
			"reason", osvDeltaWatermarkStaleReason,
			"last_poll_age_seconds", record.PollAge.Seconds(),
		)
	}
}
