// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

var fallbackClaimSequence uint64

// scrubHostCredentialEnv withholds the host's GitHub App credential variables
// from a process-adapter satellite child, which otherwise inherits the whole
// host environment including the App private key (eshu-hq/eshu#9256, epic
// #9182 AC1). The satellite needs none of these names: the host injects the
// minted token per claim over stdio, and the static path uses token_env
// names, never these. Scrubbing is unconditional — it also covers partial
// configuration where the provider stays nil but the key is still present.
// OCI-adapter runners already forward only explicit env, so only process
// runners are touched. Explicit runner Env entries still apply afterwards,
// so operator intent wins.
func scrubHostCredentialEnv(runner extensionhost.Runner) extensionhost.Runner {
	// RunCollector has a value receiver, so both extensionhost.ProcessRunner
	// and *extensionhost.ProcessRunner satisfy Runner; the scrub must cover
	// both shapes. Runners of any other type pass through only because every
	// non-process runner today (the OCI adapter) forwards explicit env and
	// never inherits the host environment — a new inheriting runner type
	// must add its case here.
	switch process := runner.(type) {
	case extensionhost.ProcessRunner:
		process.ScrubEnv = append(process.ScrubEnv, envGitHubAppID, envGitHubAppPrivateKey, envGitHubAppInstallations)
		return process
	case *extensionhost.ProcessRunner:
		process.ScrubEnv = append(process.ScrubEnv, envGitHubAppID, envGitHubAppPrivateKey, envGitHubAppInstallations)
		return process
	default:
		return runner
	}
}

func buildClaimedService(
	database db.ExecQueryer,
	getenv func(string) string,
	tracer trace.Tracer,
	instruments *telemetry.Instruments,
	logger *slog.Logger,
) (collector.ClaimedService, error) {
	// One observer serves the whole worker: registry readback (here) and the
	// extension host's activation and per-emission decisions (below) report
	// through it, so an operator sees grant allows as well as denials.
	grantObserver := extensionhost.NewGrantTelemetry(instruments, logger)
	config, err := loadRuntimeConfigObserved(getenv, grantObserver)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	grantHome := config.ComponentHome
	// Host-minted App tokens are optional: when the operator configures all
	// three GitHub App variables the host mints one installation token per
	// claim for the security-alerts activation; otherwise the static config
	// path applies. A malformed mapping fails startup closed here.
	tokenProvider, err := tokenProviderFromEnv(getenv)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	source, err := extensionhost.NewSource(extensionhost.Config{
		Manifest:            config.Manifest,
		CollectorInstanceID: config.Instance.InstanceID,
		ScopeKind:           config.ScopeKind,
		ConfigHandle:        config.ConfigHandle,
		Config:              config.ExtensionConfig,
		Runner:              scrubHostCredentialEnv(config.Runner),
		// StatusTelemetry turns the extension's SDK status rows into the
		// eshu_dp_component_extension_* instruments and one bounded log
		// line per claim result (#8908); an extracted collector emits no
		// telemetry of its own.
		StatusRecorder: extensionhost.NewStatusTelemetry(instruments, logger),
		Clock:          time.Now,
		// TokenProvider is nil unless the GitHub App environment is fully
		// set; only the minted token crosses the stdio boundary, never the
		// App private key.
		TokenProvider: tokenProvider,
		Grants:        config.Grants,
		GrantObserver: grantObserver,
		// LiveGrants re-reads the registry on every emission so a
		// grant revoked during execution fails the next result
		// closed. A read failure is returned, not swallowed: the host
		// denies core-owned kinds (reason grants_unreadable) rather
		// than emitting under unknown authorization.
		LiveGrants: func() ([]component.ProducerGrant, error) {
			return component.NewRegistry(grantHome).ProducerGrants()
		},
	})
	if err != nil {
		return collector.ClaimedService{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	committer.Instruments = instruments
	return collector.ClaimedService{
		ControlStore:        postgres.NewWorkflowControlStore(database),
		Source:              source,
		Committer:           committer,
		CollectorKind:       config.CollectorKind,
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

func newClaimID() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err == nil {
		return "component-extension-claim-" + hex.EncodeToString(raw[:])
	}
	next := atomic.AddUint64(&fallbackClaimSequence, 1)
	return fmt.Sprintf("component-extension-claim-fallback-%d-%d", time.Now().UTC().UnixNano(), next)
}
