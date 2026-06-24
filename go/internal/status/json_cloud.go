// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package status

// awsCloudScansJSON projects AWS cloud scan status rows into the stable status
// JSON shape without exposing raw provider payloads.
func awsCloudScansJSON(rows []AWSCloudScanStatus) []awsCloudScanJSON {
	projected := make([]awsCloudScanJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, awsCloudScanJSON{
			CollectorInstanceID: row.CollectorInstanceID,
			AccountID:           row.AccountID,
			Region:              row.Region,
			ServiceKind:         row.ServiceKind,
			Status:              row.Status,
			CommitStatus:        row.CommitStatus,
			FailureClass:        row.FailureClass,
			FailureMessage:      row.FailureMessage,
			APICallCount:        row.APICallCount,
			ThrottleCount:       row.ThrottleCount,
			WarningCount:        row.WarningCount,
			ResourceCount:       row.ResourceCount,
			RelationshipCount:   row.RelationshipCount,
			TagObservationCount: row.TagObservationCount,
			BudgetExhausted:     row.BudgetExhausted,
			CredentialFailed:    row.CredentialFailed,
			LastStartedAt:       nullableRFC3339Value(row.LastStartedAt),
			LastObservedAt:      nullableRFC3339Value(row.LastObservedAt),
			LastCompletedAt:     nullableRFC3339Value(row.LastCompletedAt),
			LastSuccessfulAt:    nullableRFC3339Value(row.LastSuccessfulAt),
			UpdatedAt:           nullableRFC3339Value(row.UpdatedAt),
		})
	}
	return projected
}

// vulnerabilitySourcesJSON projects vulnerability source state rows into the
// stable status JSON shape.
func vulnerabilitySourcesJSON(rows []VulnerabilitySourceState) []vulnerabilitySourceJSON {
	projected := make([]vulnerabilitySourceJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, vulnerabilitySourceJSON{
			CollectorInstanceID: row.CollectorInstanceID,
			ScopeID:             row.ScopeID,
			Source:              row.Source,
			Ecosystem:           row.Ecosystem,
			WindowStart:         nullableRFC3339Value(row.WindowStart),
			WindowEnd:           nullableRFC3339Value(row.WindowEnd),
			LastAttemptAt:       nullableRFC3339Value(row.LastAttemptAt),
			LastSuccessAt:       nullableRFC3339Value(row.LastSuccessAt),
			NextRetryAt:         nullableRFC3339Value(row.NextRetryAt),
			LastErrorClass:      row.LastErrorClass,
			FreshnessState:      row.FreshnessState,
			TerminalStatus:      row.TerminalStatus,
			ResultCount:         row.ResultCount,
			WarningCount:        row.WarningCount,
			UpdatedAt:           nullableRFC3339Value(row.UpdatedAt),
		})
	}
	return projected
}
