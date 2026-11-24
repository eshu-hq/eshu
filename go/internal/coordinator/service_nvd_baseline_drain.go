// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
)

// loadNVDBaselineDrainForPlan probes every undrained generation of each
// incomplete checkpointed year and returns the per-year map for the request:
// the checkpoint's last committed chunk beside every older undrained window
// the shared set names (eshu-hq/eshu#11380), folded to one state per year.
// A year with no older windows probes exactly its last chunk, as before.
// Blank keys probe nothing: the year stays out of the map and holds as
// drain_unloaded. The load runs under a span (the duration signal) and the
// tick's own deadline; a joined error warns once with the probe/read counts
// and the timeouts it carried count apart from broken queries.
func (s Service) loadNVDBaselineDrainForPlan(ctx context.Context, stored map[int]NVDBaselineStoredCheckpoint) map[int]vulnerability.NVDBaselineDrainState {
	states := make(map[int]vulnerability.NVDBaselineDrainState, len(stored))
	type yearKeys struct {
		year int
		keys []vulnerability.NVDBaselineDrainKey
	}
	var order []yearKeys
	for year, row := range stored {
		if row.Checkpoint.Complete {
			continue
		}
		if strings.TrimSpace(row.LastScopeID) == "" || strings.TrimSpace(row.LastGenerationID) == "" {
			continue
		}
		last := vulnerability.NVDBaselineDrainKey{ScopeID: row.LastScopeID, GenerationID: row.LastGenerationID}
		keys := []vulnerability.NVDBaselineDrainKey{last}
		seen := map[vulnerability.NVDBaselineDrainKey]bool{last: true}
		for _, extra := range row.UndrainedGenerations {
			if strings.TrimSpace(extra.ScopeID) == "" || strings.TrimSpace(extra.GenerationID) == "" {
				continue
			}
			if seen[extra] {
				continue
			}
			seen[extra] = true
			keys = append(keys, extra)
		}
		order = append(order, yearKeys{year: year, keys: keys})
	}
	if len(order) == 0 {
		return states
	}
	slices.SortFunc(order, func(a, b yearKeys) int { return a.year - b.year })
	ctx, span := otel.Tracer(telemetry.DefaultSignalName).Start(ctx, nvdBaselineDrainLoadSpan)
	defer span.End()
	probed := 0
	var errs []error
	for _, year := range order {
		var yearStates []vulnerability.NVDBaselineDrainState
		tainted := false
		for _, key := range year.keys {
			loaded, err := vulnerability.LoadNVDBaselineDrain(ctx, s.NVDBaselineDrainStore, map[int]vulnerability.NVDBaselineDrainKey{year.year: key})
			probed++
			if err != nil {
				errs = append(errs, err)
			}
			state, ok := loaded[year.year]
			if !ok {
				// A generation the loader could not prove holds the whole
				// year: the fold never sees a partial set.
				tainted = true
				continue
			}
			yearStates = append(yearStates, state)
		}
		if tainted {
			continue
		}
		states[year.year] = vulnerability.FoldNVDBaselineDrainStates(yearStates)
	}
	span.SetAttributes(
		attribute.Int("nvd_baseline_drain_years_probed", probed),
		attribute.Int("nvd_baseline_drain_years_loaded", len(states)),
	)
	if err := errors.Join(errs...); err != nil {
		span.RecordError(err)
		// The joined error names years (ints) only: stages and database
		// causes carry no scope id, so the line is safe to log whole.
		s.warnNVDBaseline(ctx, "workflow coordinator NVD baseline drain probe failed, affected years held",
			"years_probed", probed, "years_loaded", len(states),
			"timeouts", vulnerability.NVDBaselineDrainTimeouts(err),
			"error", err.Error())
		s.NVDBaselineInstruments.RecordNVDBaselineDrainProbeTimeouts(ctx, vulnerability.NVDBaselineDrainTimeouts(err))
	}
	return states
}
