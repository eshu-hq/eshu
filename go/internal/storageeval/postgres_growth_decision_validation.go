// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package storageeval

import "fmt"

func validateHostedGrowthDecision(proof HostedGrowthPostgresProof) error {
	decision := proof.Decision
	if !supportedHostedGrowthRecommendation(decision.Recommendation) {
		return fmt.Errorf("decision recommendation is required")
	}
	if !supportedHostedGrowthRationaleClass(decision.RationaleClass) {
		return fmt.Errorf("decision rationale class is unsupported")
	}
	if err := validateHostedGrowthLinkedIssues(decision.LinkedIssues); err != nil {
		return err
	}
	if !supportedHostedGrowthImplication(decision.MigrationImplications) ||
		!supportedHostedGrowthImplication(decision.RollbackImplications) ||
		!supportedHostedGrowthImplication(decision.RetentionImplications) ||
		!supportedHostedGrowthImplication(decision.TenantIsolationImplications) {
		return fmt.Errorf("decision implications must use supported public-safe labels")
	}
	switch decision.Recommendation {
	case HostedGrowthRecommendationDefer:
		return validateHostedGrowthDeferDecision(proof)
	case HostedGrowthRecommendationRetentionTune:
		return validateHostedGrowthRetentionTuneDecision(proof)
	case HostedGrowthRecommendationPartition:
		return validateHostedGrowthPartitionDecision(proof)
	case HostedGrowthRecommendationArchive:
		return validateHostedGrowthArchiveDecision(proof)
	case HostedGrowthRecommendationSplit:
		return validateHostedGrowthSplitDecision(proof)
	}
	return nil
}

func validateHostedGrowthDeferDecision(proof HostedGrowthPostgresProof) error {
	if proof.Decision.SchemaChangeRequired ||
		proof.FactGrowth.After.FactRecordsRows > proof.Gate.FactRowsThreshold ||
		proof.FactGrowth.After.IndexBytes > proof.Gate.IndexBytesThreshold ||
		liveQueueRows(proof.QueueDrain) > proof.Gate.QueueRowsThreshold ||
		proof.QueueDrain.OldestAge > proof.Gate.OldestQueueAgeThreshold ||
		proof.Retention.RetentionLag > 0 ||
		proof.Retention.ArchiveRequired {
		return fmt.Errorf("defer decision requires fact rows, index bytes, queue rows, queue age, and retention lag below gate thresholds")
	}
	return nil
}

func validateHostedGrowthRetentionTuneDecision(proof HostedGrowthPostgresProof) error {
	if proof.Decision.SchemaChangeRequired ||
		proof.Retention.RetentionLag <= 0 ||
		proof.Retention.ArchiveRequired {
		return fmt.Errorf("retention_tune decision requires retention lag without schema or archive requirements")
	}
	return nil
}

func validateHostedGrowthPartitionDecision(proof HostedGrowthPostgresProof) error {
	if !proof.Decision.SchemaChangeRequired ||
		!proof.Migration.NativePartitioning ||
		(proof.FactGrowth.After.FactRecordsRows <= proof.Gate.FactRowsThreshold &&
			proof.FactGrowth.After.IndexBytes <= proof.Gate.IndexBytesThreshold) {
		return fmt.Errorf("partition decision requires native partitioning and row or index growth over threshold")
	}
	return nil
}

func validateHostedGrowthArchiveDecision(proof HostedGrowthPostgresProof) error {
	if !proof.Decision.SchemaChangeRequired ||
		!proof.Retention.ArchiveRequired ||
		proof.Retention.RetentionLag <= 0 {
		return fmt.Errorf("archive decision requires archive posture and measured retention lag")
	}
	return nil
}

func validateHostedGrowthSplitDecision(proof HostedGrowthPostgresProof) error {
	if !proof.Decision.SchemaChangeRequired ||
		(dominantHostedGrowthFamilyRows(proof.FactGrowth.Families)*2) <= proof.FactGrowth.After.FactRecordsRows {
		return fmt.Errorf("split decision requires a dominant fact family")
	}
	return nil
}

func validateHostedGrowthLinkedIssues(issues []int) error {
	required := requiredHostedGrowthLinkedIssues()
	if len(issues) != len(required) {
		return fmt.Errorf("decision linked issues must match the required issue set")
	}
	seen := make(map[int]struct{}, len(issues))
	for _, issue := range issues {
		if _, ok := seen[issue]; ok {
			return fmt.Errorf("decision linked issue %d is duplicated", issue)
		}
		seen[issue] = struct{}{}
	}
	for _, issue := range required {
		if _, ok := seen[issue]; !ok {
			return fmt.Errorf("decision linked issue %d is required", issue)
		}
	}
	for _, issue := range issues {
		if !requiredHostedGrowthLinkedIssue(issue) {
			return fmt.Errorf("decision linked issue %d is unsupported", issue)
		}
	}
	return nil
}

func dominantHostedGrowthFamilyRows(families []HostedGrowthFactFamilyGrowth) int64 {
	var dominant int64
	for _, family := range families {
		if family.AfterRows > dominant {
			dominant = family.AfterRows
		}
	}
	return dominant
}

func liveQueueRows(queue HostedGrowthQueueDrainMeasurement) int64 {
	return queue.PendingRows + queue.RetryRows + queue.ClaimedRows
}
