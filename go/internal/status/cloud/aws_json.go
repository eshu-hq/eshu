// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cloud

import (
	"github.com/eshu-hq/eshu/go/internal/status/shared"
)

type AWSScanJSON struct {
	CollectorInstanceID string `json:"collector_instance_id"`
	AccountID           string `json:"account_id"`
	Region              string `json:"region"`
	ServiceKind         string `json:"service_kind"`
	Status              string `json:"status"`
	CommitStatus        string `json:"commit_status"`
	FailureClass        string `json:"failure_class,omitempty"`
	FailureMessage      string `json:"failure_message,omitempty"`
	APICallCount        int    `json:"api_call_count"`
	ThrottleCount       int    `json:"throttle_count"`
	WarningCount        int    `json:"warning_count"`
	ResourceCount       int    `json:"resource_count"`
	RelationshipCount   int    `json:"relationship_count"`
	TagObservationCount int    `json:"tag_observation_count"`
	BudgetExhausted     bool   `json:"budget_exhausted"`
	CredentialFailed    bool   `json:"credential_failed"`
	LastStartedAt       string `json:"last_started_at,omitempty"`
	LastObservedAt      string `json:"last_observed_at,omitempty"`
	LastCompletedAt     string `json:"last_completed_at,omitempty"`
	LastSuccessfulAt    string `json:"last_successful_at,omitempty"`
	UpdatedAt           string `json:"updated_at,omitempty"`
}

// AWSScansJSON projects AWS cloud scan status rows into the stable status
// JSON shape without exposing raw provider payloads.
func AWSScansJSON(rows []AWSScanStatus) []AWSScanJSON {
	projected := make([]AWSScanJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, AWSScanJSON{
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
			LastStartedAt:       shared.NullableRFC3339Value(row.LastStartedAt),
			LastObservedAt:      shared.NullableRFC3339Value(row.LastObservedAt),
			LastCompletedAt:     shared.NullableRFC3339Value(row.LastCompletedAt),
			LastSuccessfulAt:    shared.NullableRFC3339Value(row.LastSuccessfulAt),
			UpdatedAt:           shared.NullableRFC3339Value(row.UpdatedAt),
		})
	}
	return projected
}
