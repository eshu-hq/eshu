// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ErrScannerTargetNotAuthorized means the pinned source or current authority
// no longer permits this image target.
var ErrScannerTargetNotAuthorized = errors.New("scanner image target not authorized")

type scannerAdmissionPlan struct {
	item          workflow.WorkItem
	target        workflow.ScannerImageTarget
	bindingID     string
	sourceScopeID string
	proofs        map[int64]scannerSourceProof
	bindingIDs    []string
}

const scannerTargetInsertQuery = `
INSERT INTO scanner_image_targets
(scan_scope_id,authorization_namespace,tenant_id,workspace_id,subject_class,
 authorization_domain,manifest_digest)
VALUES ($1,$2,$3,$4,$5,$6,$7)
ON CONFLICT DO NOTHING`

const scannerBindingInsertQuery = `
INSERT INTO scanner_image_target_sources
(binding_id,scan_scope_id,source_fact_id,source_collector_instance_id,
 source_scope_id,source_generation_id,source_payload_sha256,
 scanner_collector_instance_id,policy_revision_hash,registry,repository,
 runtime_digest,runtime_digest_kind,runtime_image_reference,
 manifest_evidence_fact_id,manifest_evidence_scope_id,
 manifest_evidence_generation_id,manifest_evidence_payload_sha256,
 registry_domain_config_sha256,validated_at,canonical_observation_id,
 pinned_producer_principal_oid,pinned_source_activation_epoch,
 pinned_producer_grant_epoch,pinned_source_instance_epoch,
 pinned_tenant_epoch,pinned_workspace_epoch,pinned_scope_grant_epoch,
 pinned_registry_epoch,pinned_role_policy_revision,pinned_database_identity,
 manifest_verification_id,pinned_manifest_verification_epoch)
VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,
        NOW(),$20,$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32)
ON CONFLICT DO NOTHING`

const scannerTargetStateQuery = `
SELECT EXISTS (
         SELECT 1 FROM scanner_image_scan_results AS result
         JOIN scanner_image_target_attempts AS completed_attempt
           ON completed_attempt.work_item_id=result.work_item_id
          AND completed_attempt.status='completed'
         WHERE result.scan_scope_id=target.scan_scope_id
           AND result.binding_id=$2
       ) OR EXISTS (
         SELECT 1 FROM scanner_image_target_attempts AS terminal_attempt
         WHERE terminal_attempt.scan_scope_id=target.scan_scope_id
           AND terminal_attempt.binding_id=$2
           AND terminal_attempt.status='failed_terminal'
       ),
       EXISTS (
         SELECT 1 FROM scanner_image_target_attempts AS attempt
         JOIN workflow_work_items AS item ON item.work_item_id = attempt.work_item_id
         JOIN workflow_runs AS run ON run.run_id = item.run_id
         WHERE attempt.scan_scope_id = target.scan_scope_id
           AND run.status NOT IN ('complete','failed')
       )
FROM scanner_image_targets AS target
WHERE target.scan_scope_id = $1`

const scannerAttemptInsertQuery = `
INSERT INTO scanner_image_target_attempts
(work_item_id,scan_scope_id,binding_id,status,created_at)
VALUES ($1,$2,$3,'pending',NOW())
ON CONFLICT DO NOTHING`

// CreateScannerRunWithTargets validates source grants and facts, persists all
// source bindings, and admits at most one work item per boundary-local target.
func (s *WorkflowControlStore) CreateScannerRunWithTargets(
	ctx context.Context, run workflow.Run, items []workflow.WorkItem,
	targets []workflow.ScannerImageTarget,
) (workflow.RunAdmission, error) {
	return s.createScannerRunWithTargets(ctx, run, items, targets, nil)
}

// CreateScannerRunWithTargetsAndCursor admits a bounded discovery page and
// advances its persisted cursor in the same transaction. Even a page that
// yields no unique targets must CAS its cursor to make finite-cycle progress.
func (s *WorkflowControlStore) CreateScannerRunWithTargetsAndCursor(
	ctx context.Context, run workflow.Run, items []workflow.WorkItem,
	targets []workflow.ScannerImageTarget,
	request workflow.RunningImageDiscoveryRequest,
	expected, next workflow.RunningImageDiscoveryCursor,
) (workflow.RunAdmission, error) {
	if next.Version != expected.Version+1 || request.DiscoveryPolicyRevision <= 0 ||
		request.ScannerCollectorInstanceID == "" ||
		request.ExpectedRolePolicyRevision <= 0 || request.ExpectedDatabaseIdentity == "" {
		return workflow.RunAdmission{}, fmt.Errorf("scanner discovery cursor advance invalid")
	}
	for _, item := range items {
		if item.CollectorInstanceID != request.ScannerCollectorInstanceID {
			return workflow.RunAdmission{}, fmt.Errorf("scanner discovery instance mismatch")
		}
	}
	advance := &scannerDiscoveryAdvance{request: request, expected: expected, next: next}
	return s.createScannerRunWithTargets(ctx, run, items, targets, advance)
}

func (s *WorkflowControlStore) createScannerRunWithTargets(
	ctx context.Context, run workflow.Run, items []workflow.WorkItem,
	targets []workflow.ScannerImageTarget, advance *scannerDiscoveryAdvance,
) (workflow.RunAdmission, error) {
	if s.database == nil || s.beginner == nil {
		return workflow.RunAdmission{}, fmt.Errorf("scanner target admission requires transactional store")
	}
	if err := run.Validate(); err != nil {
		return workflow.RunAdmission{}, err
	}
	if len(items) != len(targets) {
		return workflow.RunAdmission{}, fmt.Errorf("scanner target admission requires one target per planned work item")
	}
	if advance == nil {
		return workflow.RunAdmission{}, fmt.Errorf("canonical scanner target admission requires durable discovery cursor")
	}
	plans := make(map[string]*scannerAdmissionPlan, len(targets))
	seenWork := make(map[string]struct{}, len(items))
	for i, item := range items {
		target := targets[i]
		if err := item.Validate(); err != nil {
			return workflow.RunAdmission{}, err
		}
		if err := target.Validate(); err != nil {
			return workflow.RunAdmission{}, err
		}
		if item.RunID != run.RunID || item.CollectorKind != "scanner_worker" ||
			item.WorkItemID != target.WorkItemID || item.ScopeID != target.ScanTargetID ||
			item.TenantID != target.TenantID || item.WorkspaceID != target.WorkspaceID ||
			item.SubjectClass != target.SubjectClass || item.PolicyRevisionHash != target.PolicyRevisionHash {
			return workflow.RunAdmission{}, fmt.Errorf("scanner target admission work item boundary mismatch")
		}
		if _, duplicate := seenWork[item.WorkItemID]; duplicate {
			return workflow.RunAdmission{}, fmt.Errorf("scanner target admission duplicate work item")
		}
		seenWork[item.WorkItemID] = struct{}{}
		if existing := plans[target.ScanTargetID]; existing != nil {
			if existing.item.CollectorInstanceID != item.CollectorInstanceID {
				return workflow.RunAdmission{}, fmt.Errorf("scanner target admission duplicate target across scanner instances")
			}
			existing.target.Sources = append(existing.target.Sources, target.Sources...)
			continue
		}
		copyTarget := target
		copyTarget.Sources = append([]workflow.ScannerImageSourceBinding(nil), target.Sources...)
		plans[target.ScanTargetID] = &scannerAdmissionPlan{item: item, target: copyTarget}
	}
	if len(plans) == 0 && advance == nil {
		return workflow.RunAdmission{}, nil
	}
	var lastErr error
	for attempt := 0; attempt < workflowGuardedRunCreateMaxAttempts; attempt++ {
		admission, err := s.createScannerRunWithTargetsOnce(ctx, run, plans, advance)
		if err == nil {
			return admission, nil
		}
		lastErr = err
		if !isRetryableWorkflowReconciliationError(err) {
			break
		}
	}
	return workflow.RunAdmission{}, lastErr
}

func (s *WorkflowControlStore) createScannerRunWithTargetsOnce(
	ctx context.Context, run workflow.Run, plans map[string]*scannerAdmissionPlan,
	advance *scannerDiscoveryAdvance,
) (workflow.RunAdmission, error) {
	tx, err := s.beginner.Begin(ctx)
	if err != nil {
		return workflow.RunAdmission{}, fmt.Errorf("scanner target admission begin: %w", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if advance != nil {
		if err := authorizeScannerDiscovery(ctx, tx, advance.request); err != nil {
			return workflow.RunAdmission{}, err
		}
	}
	ordered := make([]*scannerAdmissionPlan, 0, len(plans))
	for _, plan := range plans {
		ordered = append(ordered, plan)
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].target.ScanTargetID < ordered[j].target.ScanTargetID })
	drop, skipped, err := proveScannerPlanSources(ctx, tx, ordered, advance.request)
	if err != nil {
		return workflow.RunAdmission{}, err
	}
	kept := make([]*scannerAdmissionPlan, 0, len(ordered))
	for _, plan := range ordered {
		if !drop[plan] {
			kept = append(kept, plan)
		}
	}
	ordered = kept
	// A skipped binding is never marked admitted, but the cursor still
	// completes the finite cycle past it: stopping before a mid-page bad
	// item would re-wedge on it next page.
	if len(ordered) == 0 {
		if err := advanceScannerDiscoveryCursor(ctx, tx, advance); err != nil {
			return workflow.RunAdmission{}, err
		}
		if err := tx.Commit(); err != nil {
			return workflow.RunAdmission{}, err
		}
		committed = true
		return workflow.RunAdmission{AdmissionSkipped: orNilAdmissionSkips(skipped)}, nil
	}
	items := make([]workflow.WorkItem, 0, len(ordered))
	for _, plan := range ordered {
		items = append(items, plan.item)
	}
	// Instance locks precede stable target locks on every admission path.
	if err := lockWorkflowOpenTargets(ctx, tx, items); err != nil {
		return workflow.RunAdmission{}, err
	}
	if err := lockScannerTargets(ctx, tx, ordered); err != nil {
		return workflow.RunAdmission{}, err
	}
	terminal, err := s.workflowRunIsTerminal(ctx, tx, run.RunID)
	if err != nil {
		return workflow.RunAdmission{}, err
	}
	if terminal {
		if err := tx.Commit(); err != nil {
			return workflow.RunAdmission{}, err
		}
		committed = true
		return workflow.RunAdmission{AdmissionSkipped: orNilAdmissionSkips(skipped)}, nil
	}
	candidates := make([]workflow.WorkItem, 0, len(ordered))
	for _, plan := range ordered {
		if err := admitScannerBindings(ctx, tx, plan); err != nil {
			return workflow.RunAdmission{}, err
		}
		// A target is closed only when every distinct binding it carries
		// is completed or terminally failed: one new binding still earns
		// an attempt while a repeated identical set stays suppressed.
		allClosed, scopeOpen := true, false
		for _, binding := range plan.bindingIDs {
			completed, open, err := scannerTargetState(ctx, tx,
				plan.target.ScanTargetID, binding)
			if err != nil {
				return workflow.RunAdmission{}, err
			}
			scopeOpen = scopeOpen || open
			if !completed {
				allClosed = false
			}
		}
		if !allClosed && !scopeOpen {
			candidates = append(candidates, plan.item)
		}
	}
	eligible, err := s.workItemsWithoutOpenTargets(ctx, tx, run.RunID, candidates)
	if err != nil {
		return workflow.RunAdmission{}, err
	}
	if len(eligible) == 0 {
		if advance != nil {
			if err := advanceScannerDiscoveryCursor(ctx, tx, advance); err != nil {
				return workflow.RunAdmission{}, err
			}
		}
		if err := tx.Commit(); err != nil {
			return workflow.RunAdmission{}, err
		}
		committed = true
		return workflow.RunAdmission{AdmissionSkipped: orNilAdmissionSkips(skipped)}, nil
	}
	if err := s.createRunWithExecutor(ctx, tx, run); err != nil {
		return workflow.RunAdmission{}, err
	}
	admission := workflow.RunAdmission{EligibleTargets: len(eligible)}
	for start := 0; start < len(eligible); start += workflowEnqueueBatchSize {
		end := start + workflowEnqueueBatchSize
		if end > len(eligible) {
			end = len(eligible)
		}
		inserted, err := s.enqueueWorkItemBatchWithExecutor(ctx, tx, eligible[start:end])
		if err != nil {
			return workflow.RunAdmission{}, err
		}
		if inserted != end-start {
			return workflow.RunAdmission{}, fmt.Errorf("scanner target admission lost work item at insert")
		}
		admission.InsertedWorkItems += inserted
	}
	for _, item := range eligible {
		plan := plans[item.ScopeID]
		if _, err := tx.ExecContext(ctx, `UPDATE workflow_work_items SET authorization_scope_id=$2 WHERE work_item_id=$1`,
			item.WorkItemID, plan.sourceScopeID); err != nil {
			return workflow.RunAdmission{}, err
		}
		result, err := tx.ExecContext(ctx, scannerAttemptInsertQuery,
			item.WorkItemID, item.ScopeID, plan.bindingID)
		if err != nil {
			return workflow.RunAdmission{}, err
		}
		count, err := result.RowsAffected()
		if err != nil || count != 1 {
			return workflow.RunAdmission{}, fmt.Errorf("scanner target admission lost pinned attempt")
		}
	}
	if advance != nil {
		if err := advanceScannerDiscoveryCursor(ctx, tx, advance); err != nil {
			return workflow.RunAdmission{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return workflow.RunAdmission{}, err
	}
	committed = true
	admission.AdmissionSkipped = orNilAdmissionSkips(skipped)
	return admission, nil
}
