// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/claim/store"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost/vulnerability"
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
//
// Unknown runner types fail closed: a future Runner that inherits the host
// environment must add its scrub case here, or startup is refused with an
// error naming the type (eshu-hq/eshu#9413). Only the OCI adapter — which
// never inherits host env — passes through, in both its value and pointer
// shapes.
func scrubHostCredentialEnv(runner extensionhost.Runner) (extensionhost.Runner, error) {
	// RunCollector has a value receiver, so both extensionhost.ProcessRunner
	// and *extensionhost.ProcessRunner satisfy Runner; the scrub must cover
	// both shapes.
	switch scrubbed := runner.(type) {
	case extensionhost.ProcessRunner:
		scrubbed.ScrubEnv = append(scrubbed.ScrubEnv, envGitHubAppID, envGitHubAppPrivateKey, envGitHubAppInstallations)
		return scrubbed, nil
	case *extensionhost.ProcessRunner:
		if scrubbed == nil {
			return nil, fmt.Errorf("extension process runner is nil: refusing startup rather than launching with unknown host-env inheritance")
		}
		scrubbed.ScrubEnv = append(scrubbed.ScrubEnv, envGitHubAppID, envGitHubAppPrivateKey, envGitHubAppInstallations)
		return scrubbed, nil
	case extensionhost.OCIRunner:
		return runner, nil
	case *extensionhost.OCIRunner:
		if scrubbed == nil {
			return nil, fmt.Errorf("extension OCI runner is nil: refusing startup rather than panicking at first claim")
		}
		return runner, nil
	default:
		return nil, fmt.Errorf("unknown extension runner type %T: refusing startup; declare its host-env inheritance in scrubHostCredentialEnv", runner)
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
	// A runner the scrub does not know fails closed here, before the worker
	// can claim work with unknown host-env inheritance (eshu-hq/eshu#9413).
	runner, err := scrubHostCredentialEnv(config.Runner)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	outcomeRecorder, err := sourceStateRecorder(
		config.CollectorKind, database, config.Instance.InstanceID, config.PollInterval, instruments, logger,
	)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	// controlStore is the core workflow control store. It serves the
	// slot-scope resolver's run-row reads in every mode: the claim plane
	// has no run-row route, so the enabled path keeps those reads on core
	// by explicit carve-out below, never by falling through to whichever
	// store serves claims.
	controlStore := postgres.NewWorkflowControlStore(database)
	// The CoreOnlyReason lift is reserved for the designed enabled path:
	// the flag is on, the holder is pinned to the verified manifest, and
	// the routes below (core run reads, fenced committer, cutover grant
	// re-read) are all bound. Any other URL-set state keeps the refusal
	// (#8875): the resolver reads workflow_runs rows only core holds.
	coreOnlyReason := "reads workflow_runs.requested_scope_set from core Postgres for slot scopes"
	if config.Cutover.enabled {
		coreOnlyReason = ""
	}
	claimStore, err := store.New(context.Background(), store.Options{
		Service:        "collector-component-extension",
		Getenv:         getenv,
		Postgres:       controlStore,
		CoreOnlyReason: coreOnlyReason,
		Logger:         logger,
		Instruments:    instruments,
	})
	if err != nil {
		return collector.ClaimedService{}, err
	}
	// The cutover gate is nil unless the enabled plan resolved: the
	// in-tree grant closure below stays exactly as it is when off. The
	// enabled client reports seam wiring and emission decisions to the same
	// grant observer as the in-tree source, so revocations stay visible on
	// the extracted path with no new metric.
	cutover, err := buildRemoteCutover(config.Cutover, grantHome, grantObserver)
	if err != nil {
		return collector.ClaimedService{}, err
	}
	// liveGrants re-reads the registry on every emission so a grant
	// revoked during execution fails the next result closed. On the
	// enabled path the re-read routes through the cutover gate under the
	// pinned holder identity; the backing authority (the on-disk
	// registry) is the same either way.
	liveGrants := func() ([]component.ProducerGrant, error) {
		return component.NewRegistry(grantHome).ProducerGrants()
	}
	if cutover != nil {
		liveGrants = func() ([]component.ProducerGrant, error) {
			// The registry read is a local disk read and the Source
			// call site supplies no context; cancellation of the
			// surrounding claim still applies at the next claim
			// operation.
			return cutover.ReadLiveGrants(context.Background())
		}
	}
	source, err := extensionhost.NewSource(extensionhost.Config{
		Manifest:            config.Manifest,
		CollectorInstanceID: config.Instance.InstanceID,
		ScopeKind:           config.ScopeKind,
		ConfigHandle:        config.ConfigHandle,
		Config:              config.ExtensionConfig,
		Runner:              runner,
		// Stay-core carve-out: slot-scope run rows live only in core
		// Postgres, so the resolver stays bound to controlStore even
		// when claims flow through the plane client above. The plane
		// client cannot serve these reads (it has no run-row route),
		// which keeps a misbinding a compile error, not a wrong
		// answer at runtime.
		ScopeQueryResolver: vulnerability.SlotScopeQueryResolver(controlStore),
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
		LiveGrants: liveGrants,
		// ClaimOutcomeRecorder writes vulnerability_source_states from claim
		// outcomes for the vulnerability_intelligence kind only (#9420); it
		// is nil for every other kind.
		ClaimOutcomeRecorder: outcomeRecorder,
	})
	if err != nil {
		return collector.ClaimedService{}, err
	}
	committer := postgres.NewIngestionStore(database)
	committer.Logger = logger
	committer.Instruments = instruments
	if config.Cutover.enabled {
		// Plane-fence route: a plane-held claim has no core claim row,
		// so commits must pin on the plane through the selected
		// client. An enabled plan without that pinner is a startup
		// refusal, never an unfenced commit.
		if claimStore.Backend != store.BackendClaimPlane || claimStore.Pinner == nil {
			return collector.ClaimedService{}, fmt.Errorf(
				"cutover enabled but no claim-plane pinner selected: refusing startup rather than committing unfenced")
		}
		committer.ClaimPlane = &postgres.ClaimPlaneFence{Pinner: claimStore.Pinner}
	}
	return collector.ClaimedService{
		ControlStore:        claimStore.Store,
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
