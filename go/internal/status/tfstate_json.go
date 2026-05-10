package status

import (
	"sort"
)

// terraformStateJSON is the operator-facing JSON shape for the tfstate admin
// status section. Empty slices are still emitted so consumers can rely on the
// shape after the first report containing tfstate evidence.
type terraformStateJSON struct {
	LastSerials    []terraformStateSerialJSON               `json:"last_serials"`
	RecentWarnings []terraformStateWarningJSON              `json:"recent_warnings"`
	WarningsByKind map[string]map[string][]terraformStateWarningJSON `json:"warnings_by_kind"`
}

type terraformStateSerialJSON struct {
	SafeLocatorHash string `json:"safe_locator_hash"`
	BackendKind     string `json:"backend_kind,omitempty"`
	Lineage         string `json:"lineage,omitempty"`
	Serial          int64  `json:"serial"`
	GenerationID    string `json:"generation_id,omitempty"`
	ObservedAt      string `json:"observed_at,omitempty"`
}

type terraformStateWarningJSON struct {
	SafeLocatorHash string `json:"safe_locator_hash"`
	BackendKind     string `json:"backend_kind,omitempty"`
	WarningKind     string `json:"warning_kind"`
	Reason          string `json:"reason,omitempty"`
	Source          string `json:"source,omitempty"`
	GenerationID    string `json:"generation_id,omitempty"`
	ObservedAt      string `json:"observed_at,omitempty"`
}

// terraformStateReportJSON projects the report-side TerraformStateReport into
// the wire JSON shape. Returns nil when the report carries no evidence so the
// admin status response stays compact for runtimes that never observe tfstate.
func terraformStateReportJSON(report TerraformStateReport) *terraformStateJSON {
	if len(report.LastSerials) == 0 && len(report.RecentWarnings) == 0 {
		return nil
	}
	out := &terraformStateJSON{
		LastSerials:    make([]terraformStateSerialJSON, 0, len(report.LastSerials)),
		RecentWarnings: make([]terraformStateWarningJSON, 0, len(report.RecentWarnings)),
		WarningsByKind: map[string]map[string][]terraformStateWarningJSON{},
	}
	for _, row := range report.LastSerials {
		out.LastSerials = append(out.LastSerials, terraformStateSerialJSON{
			SafeLocatorHash: row.SafeLocatorHash,
			BackendKind:     row.BackendKind,
			Lineage:         row.Lineage,
			Serial:          row.Serial,
			GenerationID:    row.GenerationID,
			ObservedAt:      nullableRFC3339Value(row.ObservedAt),
		})
	}
	for _, row := range report.RecentWarnings {
		out.RecentWarnings = append(out.RecentWarnings, warningRowJSON(row))
	}

	// Project WarningsByKind into stable JSON-friendly nested maps. Iteration
	// order on a map is non-deterministic; we sort kind keys before emitting
	// each locator's slice so the JSON shape is stable across reads.
	for hash, byKind := range report.WarningsByKind {
		nested := map[string][]terraformStateWarningJSON{}
		kinds := make([]string, 0, len(byKind))
		for kind := range byKind {
			kinds = append(kinds, kind)
		}
		sort.Strings(kinds)
		for _, kind := range kinds {
			rows := byKind[kind]
			projected := make([]terraformStateWarningJSON, 0, len(rows))
			for _, row := range rows {
				projected = append(projected, warningRowJSON(row))
			}
			nested[kind] = projected
		}
		out.WarningsByKind[hash] = nested
	}
	return out
}

func warningRowJSON(row TerraformStateLocatorWarning) terraformStateWarningJSON {
	return terraformStateWarningJSON{
		SafeLocatorHash: row.SafeLocatorHash,
		BackendKind:     row.BackendKind,
		WarningKind:     row.WarningKind,
		Reason:          row.Reason,
		Source:          row.Source,
		GenerationID:    row.GenerationID,
		ObservedAt:      nullableRFC3339Value(row.ObservedAt),
	}
}

