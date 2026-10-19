// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"log/slog"
	"time"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/membership"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/preparse"
	"github.com/eshu-hq/eshu/go/internal/collector/repo/git/rest"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	membershipstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/membership"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/webhook"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

const defaultCollectorPollInterval = time.Second

func buildCollectorService(
	database postgres.SQLDB,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.Service, error) {
	config, err := git.LoadRepoSyncConfig("collector-git", getenv)
	if err != nil {
		return collector.Service{}, err
	}
	discoveryOptions, err := git.LoadDiscoveryOptionsFromEnv(getenv)
	if err != nil {
		return collector.Service{}, err
	}

	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	committer.Instruments = instruments

	snapshotter := git.NativeRepositorySnapshotter{
		SCIP:             git.LoadSnapshotSCIPConfig(getenv),
		ParseWorkers:     config.ParseWorkers,
		DiscoveryOptions: discoveryOptions,
		EmitDataflow:     git.LoadEmitDataflowGate(getenv),
		Tracer:           tracer,
		Instruments:      instruments,
		Logger:           logger,
	}
	// One configuration source for the pre-parse identity the selectors
	// compare and stamp (#7978 follow-up): built from this snapshotter, so the
	// identity cannot drift from what the snapshotter parses with.
	preparseSource := func() preparse.Config { return git.PreparseConfigFor(snapshotter, config, nil, nil) }

	// committer doubles as the delta-baseline resolver so git delta syncs
	// baseline on the last projected commit per scope rather than local HEAD
	// (epic #2340).
	// One GitHub discovery failure counter per process, shared by both
	// selectors so a retryable mint failure on the webhook sync path is capped
	// the same way as a listing failure (#8084).
	discoveryFailures := &rest.Failures{}
	nativeSelector := &git.NativeRepositorySelector{
		DiscoveryFailures: discoveryFailures,
		Config:            config,
		Logger:            logger,
		BaselineResolver:  committer,
		Instruments:       instruments,
		Preparse:          preparseSource,
		// Only the native selector sees a full githubOrg listing, so only it
		// records repository selection observations (#7625).
		SelectionObserver: membership.Observer{
			Store:       membershipstore.NewObservationStore(database),
			Instruments: instruments,
			Logger:      logger,
		},
	}
	selector := git.RepositorySelector(nativeSelector)
	handoffConfig := git.LoadWebhookTriggerHandoffConfig("collector-git", getenv)
	if handoffConfig.Enabled {
		triggerStore := webhookstore.NewWebhookTriggerStore(database)
		// The scheduled sweep yields to a queued trigger at a
		// repository boundary (#8123): the same store answers the
		// read-only probe, and the trigger is claimed next cycle
		// instead of after the sweep tail.
		nativeSelector.SweepYieldProbe = triggerStore
		nativeSelector.SweepYieldMaxPerSweep = handoffConfig.SweepMaxYields
		nativeSelector.SweepYieldCheckEvery = handoffConfig.SweepYieldCheckEvery
		selector = git.PriorityRepositorySelector{Selectors: []git.RepositorySelector{
			git.WebhookTriggerRepositorySelector{
				DiscoveryFailures: discoveryFailures,
				Config:            config,
				Store:             triggerStore,
				Owner:             handoffConfig.Owner,
				ClaimLimit:        handoffConfig.ClaimLimit,
				ClaimLeaseWindow:  handoffConfig.ClaimLeaseWindow,
				MaxClaimAttempts:  handoffConfig.MaxClaimAttempts,
				Logger:            logger,
				BaselineResolver:  committer,
				Instruments:       instruments,
				Preparse:          preparseSource,
			},
			selector,
		}}
	}

	return collector.Service{
		Source: &git.GitSource{
			Component:              "collector-git",
			Selector:               selector,
			Snapshotter:            snapshotter,
			SnapshotWorkers:        config.SnapshotWorkers,
			LargeRepoThreshold:     config.LargeRepoThreshold,
			LargeRepoMaxConcurrent: config.LargeRepoMaxConcurrent,
			StreamBuffer:           config.StreamBuffer,
			Tracer:                 tracer,
			Instruments:            instruments,
			Logger:                 logger,
		},
		Committer:    committer,
		DeadLetters:  postgres.NewCollectorGenerationDeadLetterStore(database),
		PollInterval: defaultCollectorPollInterval,
		Tracer:       tracer,
		Instruments:  instruments,
		Logger:       logger,
	}, nil
}
