// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator"
	"github.com/eshu-hq/eshu/go/internal/coordinator/vulnerability"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	vulnerabilitystore "github.com/eshu-hq/eshu/go/internal/storage/postgres/vulnerability"
)

// nvdBaselineCheckpointReader serves the coordinator's stored-checkpoint
// probe from the checkpoint table the collector advances in its commit
// transactions. The read keys the run's rows and the plan resumes from
// them, so a failure reaches the coordinator as an error, never as an
// empty map the planner would replay from zero.
type nvdBaselineCheckpointReader struct {
	database db.ExecQueryer
}

func (r nvdBaselineCheckpointReader) ReadNVDBaselineCheckpoints(ctx context.Context, baselineRunID string) (map[int]coordinator.NVDBaselineStoredCheckpoint, error) {
	rows, err := vulnerabilitystore.NewNVDBaselineCheckpointStore(r.database).ReadNVDBaselineCheckpoints(ctx, baselineRunID)
	if err != nil {
		return nil, err
	}
	checkpoints := make(map[int]coordinator.NVDBaselineStoredCheckpoint, len(rows))
	for year, row := range rows {
		checkpoints[year] = coordinator.NVDBaselineStoredCheckpoint{
			Checkpoint: vulnerability.NVDBaselineCheckpoint{
				ResumeUnixMicro: row.ResumeUnixMicro,
				ResumeCVEID:     row.ResumeCVEID,
				Complete:        row.Complete,
				RecordsDone:     row.RecordsDone,
				RecordsTotal:    row.RecordsTotal,
			},
			LastScopeID:      row.LastScopeID,
			LastGenerationID: row.LastGenerationID,
		}
	}
	return checkpoints, nil
}

// nvdBaselinePlanReader serves the coordinator's per-year plan verdicts
// from the plan rows the admission transaction wrote beside the baseline
// work items it inserted (eshu-hq/eshu#10828): each year's last planned
// scope and database-clock time, with whether that scope still has an open
// work item. The read is keyed by the run id in a single statement, so plan
// rows a superseded run left behind (eshu-hq/eshu#10831) never enter another
// run's inputs. A failure reaches the coordinator as an error, never as an
// empty map the planner would read as "never planned".
type nvdBaselinePlanReader struct {
	database db.ExecQueryer
}

func (r nvdBaselinePlanReader) ReadNVDBaselinePlans(ctx context.Context, baselineRunID string) (map[int]coordinator.NVDBaselineStoredPlan, error) {
	rows, err := vulnerabilitystore.NewNVDBaselinePlanStore(r.database).ReadNVDBaselinePlans(ctx, baselineRunID)
	if err != nil {
		return nil, err
	}
	plans := make(map[int]coordinator.NVDBaselineStoredPlan, len(rows))
	for year, row := range rows {
		plans[year] = coordinator.NVDBaselineStoredPlan{
			LastPlannedScopeID: row.LastPlannedScopeID,
			LastPlannedAt:      row.LastPlannedAt,
			InFlight:           row.InFlight,
		}
	}
	return plans, nil
}

// nvdBaselineLatchStore adapts the durable trip latch row onto the
// coordinator's latch shape. A missing row reports found=false; the
// coordinator folds the first hourly sample onto its own zero latch.
type nvdBaselineLatchStore struct {
	database db.ExecQueryer
}

func (s nvdBaselineLatchStore) ReadNVDBaselineTripLatch(ctx context.Context, baselineRunID string) (vulnerability.NVDBaselineTripLatch, bool, error) {
	row, found, err := vulnerabilitystore.NewNVDBaselineTripLatchStore(s.database).ReadNVDBaselineTripLatch(ctx, baselineRunID)
	if err != nil || !found {
		return vulnerability.NVDBaselineTripLatch{}, found, err
	}
	return vulnerability.NVDBaselineTripLatch{
		State: vulnerability.NVDBaselineTripState{
			Open:             row.Open,
			ConsecutiveHigh:  row.ConsecutiveHigh,
			Page:             row.Page,
			ReEnableSamples:  row.ReEnableSamples,
			ReEnableEligible: row.ReEnableEligible,
			SoftQuietStreak:  row.SoftQuietStreak,
		},
		SampledAt: row.SampledAt,
		Revision:  row.Revision,
	}, true, nil
}

func (s nvdBaselineLatchStore) WriteNVDBaselineTripLatch(ctx context.Context, baselineRunID string, next vulnerability.NVDBaselineTripLatch, expectedRevision int64) (bool, error) {
	return vulnerabilitystore.NewNVDBaselineTripLatchStore(s.database).WriteNVDBaselineTripLatch(ctx, baselineRunID, vulnerabilitystore.NVDBaselineTripLatchRow{
		Open:             next.State.Open,
		ConsecutiveHigh:  next.State.ConsecutiveHigh,
		Page:             next.State.Page,
		ReEnableSamples:  next.State.ReEnableSamples,
		ReEnableEligible: next.State.ReEnableEligible,
		SoftQuietStreak:  next.State.SoftQuietStreak,
		SampledAt:        next.SampledAt,
	}, expectedRevision)
}

// nvdBaselineDrainReader is the probe surface the drain adapter maps: the
// production drain probe satisfies it, and tests stub it.
type nvdBaselineDrainReader interface {
	ReadNVDBaselineDrain(ctx context.Context, scopeID, generationID string) (vulnerabilitystore.NVDBaselineDrainRow, error)
}

// nvdBaselineDrainStore adapts one drain probe read onto the coordinator's
// drain shape, mapping the phase by value (an unknown phase reads as
// unloaded in the planner). A statement timeout or context deadline maps
// onto the coordinator's timeout type so the service counts it apart from
// a broken query; every other failure passes through unchanged.
type nvdBaselineDrainStore struct {
	reader nvdBaselineDrainReader
}

func (s nvdBaselineDrainStore) ReadNVDBaselineDrain(ctx context.Context, key vulnerability.NVDBaselineDrainKey) (vulnerability.NVDBaselineDrainState, error) {
	row, err := s.reader.ReadNVDBaselineDrain(ctx, key.ScopeID, key.GenerationID)
	if err != nil {
		if errors.Is(err, vulnerabilitystore.ErrNVDBaselineDrainTimeout) {
			return vulnerability.NVDBaselineDrainState{}, &vulnerability.NVDBaselineDrainTimeoutError{Err: err}
		}
		return vulnerability.NVDBaselineDrainState{}, err
	}
	return vulnerability.NVDBaselineDrainState{
		Phase:        vulnerability.NVDBaselineDrainPhase(row.Phase),
		NotDoneSince: row.NotDoneSince,
		DrainedAt:    row.DrainedAt,
	}, nil
}

// nvdBaselineTripSampler adapts the winners dirty-log sampler onto the
// coordinator's hourly sample shape. The scope-row rate rides along next
// to the ruled CVE-keyed rate; the latch folds only the ruled fields.
type nvdBaselineTripSampler struct {
	database db.ExecQueryer
}

func (s nvdBaselineTripSampler) SampleNVDBaselineTrip(ctx context.Context, now time.Time) (coordinator.NVDBaselineTripSample, error) {
	sample, err := vulnerabilitystore.NewNVDBaselineTripSampler(s.database).SampleNVDBaselineTrip(ctx, now)
	if err != nil {
		return coordinator.NVDBaselineTripSample{}, err
	}
	return coordinator.NVDBaselineTripSample{
		At:                    sample.At,
		DirtyProxyRowsPerHour: sample.DirtyProxyRowsPerHour,
		NVDScopeRowsPerHour:   sample.NVDScopeRowsPerHour,
		GHSAProxyRowsPerHour:  sample.GHSAProxyRowsPerHour,
		OldestDirtyAge:        sample.OldestDirtyAge,
	}, nil
}
