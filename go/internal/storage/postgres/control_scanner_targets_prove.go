// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/coordinator/schedule"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// proveScannerPlanSources validates every distinct source binding of each
// admission plan and records its binding identity for the closed-target
// check. A repeated byte-identical binding is an idempotent retry; a
// repeated observation with different claims is a forgery and drops the
// whole plan for this cycle. A binding the current authority refuses to
// prove skips in isolation: it is pruned from its plan while clean siblings
// admit, so one bad binding cannot wedge its page. Only the classified
// authorization sentinel is skippable; any other proof error aborts the
// whole admission with no cursor advance.
//
// It returns the plans to drop whole and the per-reason skip counts for the
// operator signal. A plan that keeps at least one proved binding stays; a
// plan with none is dropped and earns no target row, attempt, or work item.
// A skip is re-proof, not a blacklist: the next cycle re-reads the
// observation and admits it if it then proves in full.
func proveScannerPlanSources(ctx context.Context, tx db.Queryer, ordered []*scannerAdmissionPlan,
	request workflow.RunningImageDiscoveryRequest,
) (map[*scannerAdmissionPlan]bool, map[string]map[string]int, error) {
	drop := make(map[*scannerAdmissionPlan]bool)
	skipped := make(map[string]map[string]int)
	record := func(plan *scannerAdmissionPlan, reason string) {
		if skipped[reason] == nil {
			skipped[reason] = make(map[string]int)
		}
		skipped[reason][plan.target.AuthorizationDomain]++
	}
	for _, plan := range ordered {
		plan.proofs = make(map[int64]scannerSourceProof, len(plan.target.Sources))
		plan.bindingIDs = nil
		seen := make(map[int64]workflow.ScannerImageSourceBinding, len(plan.target.Sources))
		sources := append([]workflow.ScannerImageSourceBinding(nil), plan.target.Sources...)
		sort.Slice(sources, func(i, j int) bool { return sources[i].SourceObservationID < sources[j].SourceObservationID })
		// Conflict is detected before any binding proves, so a conflicted
		// plan records exactly one conflicting_duplicate skip regardless of
		// source order: the plan-level verdict subsumes the binding-level
		// refusal.
		conflicted := false
		for _, source := range sources {
			if prior, exists := seen[source.SourceObservationID]; exists {
				if prior != source {
					conflicted = true
					break
				}
				continue
			}
			seen[source.SourceObservationID] = source
		}
		if conflicted {
			record(plan, schedule.DerivedTargetSkipReasonConflictingDuplicate)
			drop[plan] = true
			plan.proofs = make(map[int64]scannerSourceProof)
			plan.bindingIDs = nil
			plan.target.Sources = nil
			continue
		}
		proved := make([]workflow.ScannerImageSourceBinding, 0, len(sources))
		provedIDs := make(map[int64]bool, len(sources))
		for _, source := range sources {
			if provedIDs[source.SourceObservationID] {
				continue
			}
			provedIDs[source.SourceObservationID] = true
			proof, err := proveScannerSource(ctx, tx, plan.item, plan.target, source, request)
			if err != nil {
				if !errors.Is(err, ErrScannerTargetNotAuthorized) {
					return nil, nil, err
				}
				record(plan, schedule.DerivedTargetSkipReasonAdmissionUnauthorized)
				continue
			}
			plan.proofs[source.SourceObservationID] = proof
			plan.bindingIDs = append(plan.bindingIDs,
				scannerBindingID(plan.target, plan.item, source, proof))
			proved = append(proved, source)
		}
		plan.target.Sources = proved
		if len(proved) == 0 && len(sources) > 0 {
			drop[plan] = true
		}
	}
	return drop, skipped, nil
}

// orNilAdmissionSkips keeps the admission signal nil when every planned
// binding admitted, so callers can distinguish a clean page from a page
// with counted skips without inspecting map length.
func orNilAdmissionSkips(skipped map[string]map[string]int) map[string]map[string]int {
	if len(skipped) == 0 {
		return nil
	}
	return skipped
}

func admitScannerBindings(ctx context.Context, tx db.ExecQueryer, plan *scannerAdmissionPlan) error {
	target := plan.target
	accepted := 0
	seenSource := make(map[int64]struct{}, len(target.Sources))
	sources := append([]workflow.ScannerImageSourceBinding(nil), target.Sources...)
	sort.Slice(sources, func(i, j int) bool { return sources[i].SourceObservationID < sources[j].SourceObservationID })
	for _, source := range sources {
		if _, duplicate := seenSource[source.SourceObservationID]; duplicate {
			continue
		}
		seenSource[source.SourceObservationID] = struct{}{}
		proof, exists := plan.proofs[source.SourceObservationID]
		if !exists {
			return ErrScannerTargetNotAuthorized
		}
		if accepted == 0 {
			if _, err := tx.ExecContext(ctx, scannerTargetInsertQuery,
				target.ScanTargetID, string(target.AuthorizationNamespace), target.TenantID,
				target.WorkspaceID, target.SubjectClass, target.AuthorizationDomain,
				target.ManifestDigest); err != nil {
				return fmt.Errorf("scanner target admission insert target: %w", err)
			}
			plan.bindingID = scannerBindingID(target, plan.item, source, proof)
			plan.sourceScopeID = source.SourceScopeID
		}
		accepted++
		bindingID := scannerBindingID(target, plan.item, source, proof)
		var verificationID, verificationEpoch any
		if source.RuntimeDigestKind == workflow.ScannerRuntimeDigestConfig {
			verificationID = source.ManifestVerificationID
			verificationEpoch = proof.manifestVerificationEpoch
		}
		if _, err := tx.ExecContext(ctx, scannerBindingInsertQuery,
			bindingID, target.ScanTargetID,
			fmt.Sprintf("typed-observation:%d", source.SourceObservationID),
			source.SourceCollectorInstanceID,
			source.SourceScopeID, source.SourceGenerationID, proof.sourcePayloadHash,
			plan.item.CollectorInstanceID, target.PolicyRevisionHash, source.Registry,
			source.Repository, source.RuntimeDigest, string(source.RuntimeDigestKind),
			source.RuntimeImageReference, "", "", "", "",
			proof.domainConfigHash, source.SourceObservationID,
			proof.producerPrincipalOID, proof.sourceActivationEpoch,
			proof.producerGrantEpoch, proof.sourceInstanceEpoch,
			proof.tenantEpoch, proof.workspaceEpoch, proof.scopeGrantEpoch,
			proof.registryEpoch, proof.rolePolicyRevision, proof.databaseIdentity,
			verificationID, verificationEpoch); err != nil {
			return fmt.Errorf("scanner target admission insert binding: %w", err)
		}
	}
	if accepted == 0 {
		return fmt.Errorf("scanner target admission has no distinct source: %w", ErrScannerTargetNotAuthorized)
	}
	return nil
}

func scannerTargetState(ctx context.Context, tx db.Queryer,
	scanScopeID, bindingID string,
) (bool, bool, error) {
	rows, err := tx.QueryContext(ctx, scannerTargetStateQuery,
		scanScopeID, bindingID)
	if err != nil {
		return false, false, err
	}
	defer func() { _ = rows.Close() }()
	if !rows.Next() {
		return false, false, fmt.Errorf("scanner target admission missing target")
	}
	var completed, open bool
	if err := rows.Scan(&completed, &open); err != nil {
		return false, false, err
	}
	return completed, open, rows.Err()
}

func lockScannerTargets(ctx context.Context, tx db.Executor, plans []*scannerAdmissionPlan) error {
	keys := make([]int64, 0, len(plans))
	seen := make(map[int64]struct{}, len(plans))
	for _, plan := range plans {
		h := fnv.New64a()
		_, _ = h.Write([]byte("scanner-image\x00" + plan.target.ScanTargetID))
		key := int64(h.Sum64()) // #nosec G115 -- advisory-lock bit reinterpretation
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	for _, key := range keys {
		if _, err := tx.ExecContext(ctx, workflowAdvisoryTargetLockQuery, key); err != nil {
			return fmt.Errorf("scanner target admission lock: %w", err)
		}
	}
	return nil
}
