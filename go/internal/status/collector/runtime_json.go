// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"github.com/eshu-hq/eshu/go/internal/status/shared"
)

type RuntimeStatusJSON struct {
	InstanceID            string   `json:"instance_id"`
	CollectorKind         string   `json:"collector_kind"`
	Mode                  string   `json:"mode,omitempty"`
	RuntimeMode           string   `json:"runtime_mode"`
	StatusCategory        string   `json:"status_category"`
	CoordinatorRegistered bool     `json:"coordinator_registered"`
	Enabled               bool     `json:"enabled"`
	Bootstrap             bool     `json:"bootstrap"`
	ClaimsEnabled         bool     `json:"claims_enabled"`
	DisplayName           string   `json:"display_name,omitempty"`
	Health                string   `json:"health,omitempty"`
	EvidenceSources       []string `json:"evidence_sources"`
	SourceSystems         []string `json:"source_systems,omitempty"`
	ObservationCount      int      `json:"observation_count,omitempty"`
	LastObservedAt        string   `json:"last_observed_at,omitempty"`
	UpdatedAt             string   `json:"updated_at,omitempty"`
	DeactivatedAt         string   `json:"deactivated_at,omitempty"`
	Detail                string   `json:"detail,omitempty"`
}

func RuntimeStatusesJSON(rows []RuntimeStatus) []RuntimeStatusJSON {
	projected := make([]RuntimeStatusJSON, 0, len(rows))
	for _, row := range rows {
		projected = append(projected, RuntimeStatusJSON{
			InstanceID:            row.InstanceID,
			CollectorKind:         row.CollectorKind,
			Mode:                  row.Mode,
			RuntimeMode:           row.RuntimeMode,
			StatusCategory:        row.StatusCategory,
			CoordinatorRegistered: row.CoordinatorRegistered,
			Enabled:               row.Enabled,
			Bootstrap:             row.Bootstrap,
			ClaimsEnabled:         row.ClaimsEnabled,
			DisplayName:           row.DisplayName,
			Health:                row.Health,
			EvidenceSources:       row.EvidenceSources,
			SourceSystems:         row.SourceSystems,
			ObservationCount:      row.ObservationCount,
			LastObservedAt:        shared.NullableRFC3339Value(row.LastObservedAt),
			UpdatedAt:             shared.NullableRFC3339Value(row.UpdatedAt),
			DeactivatedAt:         shared.NullableRFC3339Value(row.DeactivatedAt),
			Detail:                row.Detail,
		})
	}
	return projected
}
