// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package backup

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Scanner emits AWS Backup metadata facts for one claimed account and region.
//
// Scanner never accesses recovery point contents (the actual snapshot bodies),
// never persists backup vault access policy JSON bodies, and never persists
// framework control input parameter values. Mutation APIs
// (Create/Update/Delete vault/plan/selection/report plan/restore testing
// plan/framework, StartBackupJob, StartRestoreJob, StartCopyJob,
// DeleteRecoveryPoint, PutBackupVaultAccessPolicy) are unreachable from the
// scanner because the Client interface does not expose them.
type Scanner struct {
	Client Client
}

// Scan observes AWS Backup metadata for one boundary and returns
// reported-confidence AWS facts.
func (s Scanner) Scan(ctx context.Context, boundary awscloud.Boundary) ([]facts.Envelope, error) {
	if s.Client == nil {
		return nil, fmt.Errorf("backup scanner client is required")
	}
	switch strings.TrimSpace(boundary.ServiceKind) {
	case "", awscloud.ServiceBackup:
		// Canonicalize so emitted facts and telemetry always carry the exact
		// service_kind string, even when the caller passes whitespace padding.
		boundary.ServiceKind = awscloud.ServiceBackup
	default:
		return nil, fmt.Errorf("backup scanner received service_kind %q", boundary.ServiceKind)
	}

	var envelopes []facts.Envelope

	vaultEnvelopes, vaultNames, err := s.scanVaults(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, vaultEnvelopes...)

	planEnvelopes, err := s.scanPlansAndSelections(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, planEnvelopes...)

	rpEnvelopes, err := s.scanRecoveryPoints(ctx, boundary, vaultNames)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, rpEnvelopes...)

	reportEnvelopes, err := s.scanReportPlans(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, reportEnvelopes...)

	restoreEnvelopes, err := s.scanRestoreTestingPlans(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, restoreEnvelopes...)

	frameworkEnvelopes, err := s.scanFrameworks(ctx, boundary)
	if err != nil {
		return nil, err
	}
	envelopes = append(envelopes, frameworkEnvelopes...)

	return envelopes, nil
}

func (s Scanner) scanVaults(
	ctx context.Context,
	boundary awscloud.Boundary,
) ([]facts.Envelope, []string, error) {
	vaults, err := s.Client.ListBackupVaults(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list AWS Backup vaults: %w", err)
	}
	var envelopes []facts.Envelope
	names := make([]string, 0, len(vaults))
	for _, vault := range vaults {
		envelope, err := awscloud.NewResourceEnvelope(vaultObservation(boundary, vault))
		if err != nil {
			return nil, nil, err
		}
		envelopes = append(envelopes, envelope)
		if rel, ok := vaultKMSRelationship(boundary, vault); ok {
			relEnvelope, err := awscloud.NewRelationshipEnvelope(rel)
			if err != nil {
				return nil, nil, err
			}
			envelopes = append(envelopes, relEnvelope)
		}
		if name := strings.TrimSpace(vault.Name); name != "" {
			names = append(names, name)
		}
	}
	return envelopes, names, nil
}

func (s Scanner) scanPlansAndSelections(
	ctx context.Context,
	boundary awscloud.Boundary,
) ([]facts.Envelope, error) {
	plans, err := s.Client.ListBackupPlans(ctx)
	if err != nil {
		return nil, fmt.Errorf("list AWS Backup plans: %w", err)
	}
	var envelopes []facts.Envelope
	for _, plan := range plans {
		envelope, err := awscloud.NewResourceEnvelope(planObservation(boundary, plan))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
		selectionEnvelopes, err := s.scanSelectionsForPlan(ctx, boundary, plan)
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, selectionEnvelopes...)
	}
	return envelopes, nil
}

func (s Scanner) scanSelectionsForPlan(
	ctx context.Context,
	boundary awscloud.Boundary,
	plan Plan,
) ([]facts.Envelope, error) {
	planID := strings.TrimSpace(plan.ID)
	if planID == "" {
		return nil, nil
	}
	selections, err := s.Client.ListBackupSelections(ctx, planID)
	if err != nil {
		return nil, fmt.Errorf("list AWS Backup selections for plan %q: %w", planID, err)
	}
	var envelopes []facts.Envelope
	for _, selection := range selections {
		envelope, err := awscloud.NewResourceEnvelope(selectionObservation(boundary, selection))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
		if rel, ok := planHasSelectionRelationship(boundary, plan, selection); ok {
			relEnvelope, err := awscloud.NewRelationshipEnvelope(rel)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relEnvelope)
		}
		if rel, ok := selectionRoleRelationship(boundary, selection); ok {
			relEnvelope, err := awscloud.NewRelationshipEnvelope(rel)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relEnvelope)
		}
		for _, target := range uniqueARNs(selection.Resources) {
			relEnvelope, err := awscloud.NewRelationshipEnvelope(
				selectionIncludesResourceRelationship(boundary, selection, target),
			)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, relEnvelope)
		}
	}
	return envelopes, nil
}

func (s Scanner) scanRecoveryPoints(
	ctx context.Context,
	boundary awscloud.Boundary,
	vaultNames []string,
) ([]facts.Envelope, error) {
	var envelopes []facts.Envelope
	for _, name := range vaultNames {
		recoveryPoints, err := s.Client.ListRecoveryPoints(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("list AWS Backup recovery points for vault %q: %w", name, err)
		}
		for _, rp := range recoveryPoints {
			envelope, err := awscloud.NewResourceEnvelope(recoveryPointObservation(boundary, rp))
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, envelope)
			if rel, ok := recoveryPointInVaultRelationship(boundary, rp); ok {
				relEnvelope, err := awscloud.NewRelationshipEnvelope(rel)
				if err != nil {
					return nil, err
				}
				envelopes = append(envelopes, relEnvelope)
			}
			if rel, ok := recoveryPointOfResourceRelationship(boundary, rp); ok {
				relEnvelope, err := awscloud.NewRelationshipEnvelope(rel)
				if err != nil {
					return nil, err
				}
				envelopes = append(envelopes, relEnvelope)
			}
		}
	}
	return envelopes, nil
}

func (s Scanner) scanReportPlans(
	ctx context.Context,
	boundary awscloud.Boundary,
) ([]facts.Envelope, error) {
	reportPlans, err := s.Client.ListReportPlans(ctx)
	if err != nil {
		return nil, fmt.Errorf("list AWS Backup report plans: %w", err)
	}
	envelopes := make([]facts.Envelope, 0, len(reportPlans))
	for _, plan := range reportPlans {
		envelope, err := awscloud.NewResourceEnvelope(reportPlanObservation(boundary, plan))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func (s Scanner) scanRestoreTestingPlans(
	ctx context.Context,
	boundary awscloud.Boundary,
) ([]facts.Envelope, error) {
	plans, err := s.Client.ListRestoreTestingPlans(ctx)
	if err != nil {
		return nil, fmt.Errorf("list AWS Backup restore testing plans: %w", err)
	}
	envelopes := make([]facts.Envelope, 0, len(plans))
	for _, plan := range plans {
		envelope, err := awscloud.NewResourceEnvelope(restoreTestingPlanObservation(boundary, plan))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
	}
	return envelopes, nil
}

func (s Scanner) scanFrameworks(
	ctx context.Context,
	boundary awscloud.Boundary,
) ([]facts.Envelope, error) {
	frameworks, err := s.Client.ListFrameworks(ctx)
	if err != nil {
		return nil, fmt.Errorf("list AWS Backup frameworks: %w", err)
	}
	var envelopes []facts.Envelope
	for _, framework := range frameworks {
		envelope, err := awscloud.NewResourceEnvelope(frameworkObservation(boundary, framework))
		if err != nil {
			return nil, err
		}
		envelopes = append(envelopes, envelope)
		for _, control := range framework.Controls {
			controlEnvelope, err := awscloud.NewResourceEnvelope(
				frameworkControlObservation(boundary, framework, control),
			)
			if err != nil {
				return nil, err
			}
			envelopes = append(envelopes, controlEnvelope)
			if rel, ok := frameworkHasControlRelationship(boundary, framework, control); ok {
				relEnvelope, err := awscloud.NewRelationshipEnvelope(rel)
				if err != nil {
					return nil, err
				}
				envelopes = append(envelopes, relEnvelope)
			}
		}
	}
	return envelopes, nil
}
