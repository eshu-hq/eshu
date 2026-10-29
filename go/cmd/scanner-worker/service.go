// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/ospackagevulnerability/osruntime"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/imageanalyzer"
	"github.com/eshu-hq/eshu/go/internal/collector/scannerworker/sbomgenerator"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

var fallbackClaimSequence uint64

func buildService(
	database db.ExecQueryer,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (scannerworker.Service, error) {
	config, err := loadRuntimeConfig(getenv)
	if err != nil {
		return scannerworker.Service{}, err
	}
	controlStore := postgres.NewWorkflowControlStore(database)
	var registryResolver registryImageResolver
	if config.DeriveFromRunningImages {
		domains, err := parseRegistryDomains(config.Instance.Configuration)
		if err != nil {
			return scannerworker.Service{}, err
		}
		registryResolver = newRegistryImageResolver(controlStore, domains, getenv)
	}
	analyzer, err := buildAnalyzerWithRegistry(config, registryResolver)
	if err != nil {
		return scannerworker.Service{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	committer.Instruments = instruments
	return scannerworker.Service{
		ControlStore:        controlStore,
		Committer:           committer,
		DerivedCommitter:    committer,
		Analyzer:            analyzer,
		AnalyzerKind:        config.Analyzer,
		CollectorInstanceID: config.Instance.InstanceID,
		OwnerID:             config.OwnerID,
		ClaimIDFunc:         newClaimID,
		PollInterval:        config.PollInterval,
		ClaimLeaseTTL:       config.ClaimLeaseTTL,
		HeartbeatInterval:   config.HeartbeatInterval,
		ResourceLimits:      config.Limits,
		Clock:               time.Now,
		Tracer:              tracer,
		Instruments:         instruments,
		Logger:              logger,
	}, nil
}

// buildAnalyzer returns the analyzer implementation for one configured
// analyzer kind. Analyzer kinds whose concrete source is not wired still commit
// an explicit warning fact instead of pretending the target scanned clean.
func buildAnalyzer(config runtimeConfig) (scannerworker.Analyzer, error) {
	return buildAnalyzerWithRegistry(config, nil)
}

func buildAnalyzerWithRegistry(config runtimeConfig, registryResolver registryImageResolver) (scannerworker.Analyzer, error) {
	if config.DeriveFromRunningImages && config.Analyzer != scannerworker.AnalyzerImageUnpacking {
		return nil, fmt.Errorf("running image derivation requires image_unpacking analyzer")
	}
	switch config.Analyzer {
	case scannerworker.AnalyzerImageUnpacking:
		if config.DeriveFromRunningImages {
			if registryResolver == nil {
				return nil, fmt.Errorf("derived image resolver and registry domains are required")
			}
			derived := registryImageAnalyzer{resolver: registryResolver, collectorInstanceID: config.Instance.InstanceID}
			if len(config.ImageTargets) == 0 {
				return derived, nil
			}
			for _, target := range config.ImageTargets {
				if strings.HasPrefix(target.ScopeID, derivedImageScopePrefix) {
					return nil, fmt.Errorf("static image target uses reserved derived scope")
				}
			}
			static, err := imageanalyzer.NewAnalyzer(imageanalyzer.AnalyzerConfig{
				CollectorInstanceID: config.Instance.InstanceID, Targets: config.ImageTargets, Now: time.Now,
			})
			if err != nil {
				return nil, err
			}
			return compositeImageAnalyzer{static: static, derived: derived}, nil
		}
		return imageanalyzer.NewAnalyzer(imageanalyzer.AnalyzerConfig{
			CollectorInstanceID: config.Instance.InstanceID,
			Targets:             config.ImageTargets,
			Now:                 time.Now,
		})
	case scannerworker.AnalyzerOSPackageExtraction:
		return osruntime.NewAnalyzer(osruntime.AnalyzerConfig{
			CollectorInstanceID: config.Instance.InstanceID,
			Targets:             config.OSPackageTargets,
			Provider:            osruntime.LocalRootFSProvider{},
			Now:                 time.Now,
		})
	case scannerworker.AnalyzerSBOMGeneration:
		source, err := sbomgenerator.NewRepositorySource(config.SBOMTargets)
		if err != nil {
			if errors.Is(err, sbomgenerator.ErrNoSBOMTargets) {
				return scannerworker.WarningAnalyzer{Reason: "sbom_generator_source_not_configured"}, nil
			}
			return nil, err
		}
		return sbomgenerator.Analyzer{Source: source, Now: time.Now}, nil
	default:
		return scannerworker.WarningAnalyzer{Reason: "analyzer_not_configured"}, nil
	}
}

func newClaimID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return string(scope.CollectorScannerWorker) + "-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("%s-claim-fallback-%d-%d", scope.CollectorScannerWorker, time.Now().UTC().UnixNano(), next)
}
