// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud"
	"github.com/eshu-hq/eshu/go/internal/collector/azurecloud/azureruntime"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// fallbackClaimSequence backs the deterministic fallback claim id when the
// crypto random source is unavailable.
var fallbackClaimSequence uint64

// buildClaimedService constructs the claim-driven Azure cloud collector service.
// It selects one enabled, claim-enabled Azure collector instance, wires the
// read-only live Resource Graph provider, and records bounded claim outcomes
// through the Azure status committer. Live transport is reached only here, so a
// fixture deployment never issues a live Azure call.
func buildClaimedService(
	ctx context.Context,
	database postgres.ExecQueryer,
	redactionKey redact.Key,
	getenv func(string) string,
	tracer trace.Tracer,
	meter metric.Meter,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.ClaimedService, error) {
	config, err := loadClaimedRuntimeConfig(getenv)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	factory, err := newAzureLiveProviderFactory(ctx, config.CredentialRef)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	metrics, err := azurecloud.NewMetrics(meter)
	if err != nil {
		return collector.ClaimedService{}, fmt.Errorf("azure collector metrics: %w", err)
	}
	ingestion := postgres.NewIngestionStore(database)
	ingestion.Logger = logger
	committer := newAzureStatusCommitter(ingestion, metrics)
	return collector.ClaimedService{
		ControlStore: postgres.NewWorkflowControlStore(database),
		Source: &azureruntime.Source{
			Config:          config.Source,
			ProviderFactory: factory,
			Metrics:         metrics,
			RedactionKey:    redactionKey,
			Tracer:          tracer,
			Logger:          logger,
		},
		Committer:           committer,
		CollectorKind:       scope.CollectorAzure,
		CollectorInstanceID: config.Instance.InstanceID,
		OwnerID:             config.OwnerID,
		ClaimIDFunc:         newClaimID,
		PollInterval:        config.PollInterval,
		ClaimLeaseTTL:       config.ClaimLeaseTTL,
		HeartbeatInterval:   config.HeartbeatInterval,
		MaxAttempts:         workflow.DefaultClaimMaxAttempts(),
		Clock:               time.Now,
		Tracer:              tracer,
		Instruments:         instruments,
	}, nil
}

// newClaimID returns a unique, bounded claim id. It prefers crypto random and
// falls back to a monotonic sequence so the runner always has a claim id.
func newClaimID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "azure-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("azure-claim-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}

// redactionKeyFileEnv names the env var holding the path to the read-only
// redaction key material used to fingerprint azure_tag_observation values. When
// unset, the collector runs with a zero key and emits no tag observation facts,
// so tag values are never fingerprinted or carried without an operator key.
const redactionKeyFileEnv = "ESHU_AZURE_REDACTION_KEY_FILE"

// buildCollectorService constructs the non-claimed Azure cloud collector
// service from declarative environment configuration. It selects the
// file-backed offline page provider when ESHU_AZURE_FIXTURE_PAGES_JSON is set,
// and otherwise selects the gated live seam so production wiring never issues a
// live Azure call by default.
func buildCollectorService(
	database postgres.ExecQueryer,
	getenv func(string) string,
	tracer trace.Tracer,
	meter metric.Meter,
	logger *slog.Logger,
) (collector.Service, error) {
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		return collector.Service{}, err
	}
	factory, err := buildProviderFactory(config, getenv)
	if err != nil {
		return collector.Service{}, err
	}
	metrics, err := azurecloud.NewMetrics(meter)
	if err != nil {
		return collector.Service{}, err
	}
	redactionKey, err := loadRedactionKey(getenv(redactionKeyFileEnv))
	if err != nil {
		return collector.Service{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	return collector.Service{
		Source: &azureruntime.Source{
			Config:          config,
			ProviderFactory: factory,
			Metrics:         metrics,
			RedactionKey:    redactionKey,
			Tracer:          tracer,
			Logger:          logger,
		},
		Committer:    committer,
		PollInterval: config.PollInterval,
		Tracer:       tracer,
		Logger:       logger,
	}, nil
}

// loadRedactionKey reads the read-only redaction key material from the file at
// path. An empty path returns a zero key, which disables azure_tag_observation
// emission (tag values are never fingerprinted or carried without a key). A
// configured-but-unreadable or blank key file is a hard error so the collector
// never silently runs keyless when a key was intended. The material is never
// logged.
func loadRedactionKey(path string) (redact.Key, error) {
	if strings.TrimSpace(path) == "" {
		return redact.Key{}, nil
	}
	material, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return redact.Key{}, fmt.Errorf("read azure redaction key file: %w", err)
	}
	key, err := redact.NewKey(material)
	if err != nil {
		return redact.Key{}, fmt.Errorf("azure redaction key: %w", err)
	}
	return key, nil
}

// buildProviderFactory selects the page provider seam. The file-backed offline
// provider is for local proof and smoke tests only; the default is the gated
// live seam, which is inert until a real read-only adapter is injected.
func buildProviderFactory(
	config azureruntime.Config,
	getenv func(string) string,
) (azureruntime.PageProviderFactory, error) {
	fixture, ok, err := loadFixturePagesConfig(getenv)
	if err != nil {
		return nil, err
	}
	if !ok {
		return azureruntime.LiveProviderFactory{}, nil
	}
	access := azurecloud.ScopeAccess{
		Partial:             fixture.Partial,
		HiddenResourceCount: fixture.HiddenResourceCount,
		Reason:              fixture.Reason,
		Message:             fixture.Message,
	}
	if err := validateSingleFixtureSourceLane(config.Targets); err != nil {
		return nil, err
	}
	var resourceGraphProvider azurecloud.PageProvider
	var resourceChangesProvider azurecloud.PageProvider
	for _, target := range config.Targets {
		switch target.SourceLane {
		case "", azurecloud.SourceLaneResourceGraph:
			if resourceGraphProvider == nil {
				provider, err := azureruntime.NewFixturePageProviderFromFiles(access, fixture.PagePaths...)
				if err != nil {
					return nil, err
				}
				resourceGraphProvider = provider
			}
		case azurecloud.SourceLaneResourceChanges:
			if resourceChangesProvider == nil {
				provider, err := azureruntime.NewFixtureResourceChangesPageProviderFromFiles(access, fixture.PagePaths...)
				if err != nil {
					return nil, err
				}
				resourceChangesProvider = provider
			}
		default:
			return nil, fmt.Errorf("azure fixture page provider does not support source lane %q", target.SourceLane)
		}
	}
	return azureruntime.PageProviderFactoryFunc(func(
		_ context.Context,
		_ azurecloud.Boundary,
		target azureruntime.TargetConfig,
	) (azurecloud.PageProvider, error) {
		switch target.SourceLane {
		case "", azurecloud.SourceLaneResourceGraph:
			if resourceGraphProvider == nil {
				return nil, fmt.Errorf("azure fixture Resource Graph provider is not configured")
			}
			return resourceGraphProvider, nil
		case azurecloud.SourceLaneResourceChanges:
			if resourceChangesProvider == nil {
				return nil, fmt.Errorf("azure fixture resourcechanges provider is not configured")
			}
			return resourceChangesProvider, nil
		default:
			return nil, fmt.Errorf("azure fixture page provider does not support source lane %q", target.SourceLane)
		}
	}), nil
}

func validateSingleFixtureSourceLane(targets []azureruntime.TargetConfig) error {
	var fixtureLane string
	for _, target := range targets {
		lane, err := normalizeFixtureSourceLane(target.SourceLane)
		if err != nil {
			return err
		}
		if fixtureLane == "" {
			fixtureLane = lane
			continue
		}
		if fixtureLane != lane {
			return fmt.Errorf(
				"%s cannot share page_paths across mixed source lanes %q and %q",
				envFixturePagesJSON,
				fixtureLane,
				lane,
			)
		}
	}
	return nil
}

func normalizeFixtureSourceLane(lane string) (string, error) {
	switch lane {
	case "", azurecloud.SourceLaneResourceGraph:
		return azurecloud.SourceLaneResourceGraph, nil
	case azurecloud.SourceLaneResourceChanges:
		return azurecloud.SourceLaneResourceChanges, nil
	default:
		return "", fmt.Errorf("azure fixture page provider does not support source lane %q", lane)
	}
}
