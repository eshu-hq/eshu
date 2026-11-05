// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/telemetry/contract"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

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
	// rejected holds the queued rows the drain intersection dropped. They
	// stay pending in the store, so the report counts them in the depth
	// and alarms on them instead of stranding them invisibly.
	rejected []vulnerability.OSVDeltaRejectedRecord
	// freshParseFailed counts the fresh facts the intersection dropped
	// (malformed withdrawn spelling, unknown ecosystem, blank field, bad
	// timestamp). They never entered the queue, so the report logs them
	// per pass instead of aging them.
	freshParseFailed int
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
			AdvisoryID:  row.AdvisoryID,
			Ecosystem:   row.Ecosystem,
			PackageName: row.PackageName,
			// Queued rows carry a stored bool, always a valid spelling.
			Withdrawn:     strconv.FormatBool(row.Withdrawn),
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
				AdvisoryID:  row.AdvisoryID,
				Ecosystem:   row.Ecosystem,
				PackageName: row.PackageName,
				// Queued rows carry a stored bool, always a valid spelling.
				Withdrawn:     strconv.FormatBool(row.Withdrawn),
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
		parseFailed: changedIntersection.ParseFailed + drainIntersection.ParseFailed,
	}
	pass.freshParseFailed = changedIntersection.ParseFailed
	// Fresh rejects never sat in the queue (row zero); only queued drain
	// rejects strand depth, so only those feed the stewardship path.
	for _, rejected := range drainIntersection.Rejected {
		if rejected.RowID != 0 {
			pass.rejected = append(pass.rejected, rejected)
		}
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
// drained rows whose scopes the admission authorized and inserted, advance
// the watermark over them, record the successful poll, and report the pass.
// The mapped branch gates on the admission's authorized _replan scope set
// (eshu-hq/eshu#10083): the single authorization decision point, so a grant
// flap between admission and commit cannot admit-then-not-commit or vice
// versa. It also gates on the admission's inserted _replan scope set
// (eshu-hq/eshu#10149): a scope the open-target guard dropped as a duplicate
// or the insert refused never queued its work, so its row stays pending for
// the next pass instead of committing as drained. Rows whose scopes the
// tenant boundary denied stay pending for the next pass; the grant-skip log
// in createWorkflowWork and the survivor list in reportOSVDeltaReplan keep
// the exclusion operator-visible, and rows held for a missing insertion are
// logged below. A drained row with no minted scope commits only when the
// planner marked it trivial (no owned target, nothing to authorize); any
// other unscoped row stays pending instead of retiring on an unchecked held
// scope.
func (s Service) commitOSVDeltaReplan(
	ctx context.Context,
	pass *osvDeltaPass,
	run workflow.Run,
	items []workflow.WorkItem,
	observation vulnerability.OSVDeltaReplanObservation,
	observedAt time.Time,
	authorizedReplanScopes map[string]struct{},
	insertedReplanScopes map[string]struct{},
) error {
	pendingByID := make(map[int64]vulnerability.OSVDeltaValidatedChange, len(pass.pending))
	for _, change := range pass.pending {
		pendingByID[change.RowID] = change
	}
	trivial := make(map[int64]struct{}, len(observation.TrivialRowIDs))
	for _, id := range observation.TrivialRowIDs {
		trivial[id] = struct{}{}
	}
	var survivorIDs []int64
	heldDenied := 0
	heldDropped := 0
	survivorTS := time.Time{}
	for _, id := range observation.CoveredRowIDs {
		change, ok := pendingByID[id]
		if !ok {
			continue
		}
		if scopes, mapped := observation.CoveredRowScopes[id]; mapped {
			authorized := false
			inserted := false
			for _, scopeID := range scopes {
				if _, ok := authorizedReplanScopes[scopeID]; ok {
					authorized = true
				}
				if _, ok := insertedReplanScopes[scopeID]; ok {
					inserted = true
				}
				if authorized && inserted {
					break
				}
			}
			if !authorized {
				heldDenied++
				continue
			}
			if !inserted {
				heldDropped++
				continue
			}
		} else if _, ok := trivial[id]; !ok {
			// No minted scope and not trivial: the row's work was never
			// queued this pass, so it stays pending instead of retiring
			// on an unchecked held scope.
			continue
		}
		survivorIDs = append(survivorIDs, id)
		if survivorTS.IsZero() || change.FeedTimestamp.After(survivorTS) {
			survivorTS = change.FeedTimestamp
		}
	}
	// Rows held for a missing insertion are work nobody will collect this
	// pass: the guard saw a duplicate an open run covers, or the insert
	// refused the row. They stay pending with the denied rows, and this line
	// keeps the drop operator-visible beside the survivor list the report
	// carries (eshu-hq/eshu#10149).
	if s.Logger != nil && (heldDenied > 0 || heldDropped > 0) {
		s.Logger.Info(
			"workflow coordinator held owned target replan rows past the drain",
			"collector_instance_id", pass.instanceID,
			"held_denied_rows", heldDenied,
			"held_duplicate_or_refused_rows", heldDropped,
			"drained_rows", len(survivorIDs),
		)
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
