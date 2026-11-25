// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/eshu-hq/eshu/go/internal/coordinator"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/gomodules"
	tenantstore "github.com/eshu-hq/eshu/go/internal/storage/postgres/tenant"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// plan-go-reachability-scope is the out-of-rotation one-shot for eshu-hq/eshu#10002
// (arbiter ruling B.3): it plans one repository through the real
// reachability.Plan and the real open-target admission, under a distinct
// go-modules-operator-<request-id> plan key and the operator_recovery trigger.
// It builds the same config, control store, repository reader, and fence the
// reconciler uses, performs exactly one plan attempt, prints the outcome, and
// exits. It never starts the reconciler, the claim reaper, or any loop, and it
// creates no prod manifest: the venue is a reviewed in-cluster QA Job.
//
// Exit codes: 0 = planned, already_open, or dry_run; 1 = not_eligible (a
// business refusal, nothing was written); 2 = invocation or infrastructure
// failure.
func runPlanGoReachabilityScope(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("plan-go-reachability-scope", flag.ContinueOnError)
	flags.SetOutput(stderr)
	instanceID := flags.String("instance-id", "", "durable collector instance id to plan for (required)")
	repositoryID := flags.String("repository-id", "", "repository source_key in literal repository:<id> form, for example repository:r_efdfb126 (required)")
	requestID := flags.String("request-id", "", "operator-chosen idempotency token, for example 10032-fixture-20261123 (required)")
	requestedBy := flags.String("requested-by", "", "who fired this plan (required, audit only)")
	reason := flags.String("reason", "", "why this plan fires out of rotation (required, audit only)")
	dryRun := flags.Bool("dry-run", false, "evaluate eligibility and the fence decision without writing anything")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	request := coordinator.OperatorPlanRequest{
		InstanceID: *instanceID, RepositoryID: *repositoryID, RequestID: *requestID,
		RequestedBy: *requestedBy, Reason: *reason, DryRun: *dryRun,
	}
	if request.InstanceID == "" || request.RepositoryID == "" || request.RequestID == "" ||
		request.RequestedBy == "" || request.Reason == "" {
		_, _ = fmt.Fprintln(stderr, "plan-go-reachability-scope: --instance-id, --repository-id, --request-id, --requested-by, and --reason are all required")
		flags.Usage()
		return 2
	}

	bootstrap, err := telemetry.NewBootstrap("workflow-coordinator")
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "telemetry bootstrap:", err)
		return 2
	}
	providers, err := telemetry.NewProviders(ctx, bootstrap)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "telemetry providers:", err)
		return 2
	}
	defer func() { _ = providers.Shutdown(context.Background()) }()
	logger := telemetry.NewLogger(bootstrap, "workflow-coordinator", "workflow-coordinator")

	database, err := runtimecfg.OpenPostgresForWorkload(ctx, os.Getenv, runtimecfg.PostgresWorkloadCoordinator)
	if err != nil {
		logger.Error("workflow coordinator operator plan failed to open postgres", "error", err)
		return 2
	}
	defer func() { _ = database.Close() }()

	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "telemetry instruments:", err)
		return 2
	}
	coordinatorMetrics, err := coordinator.NewMetrics(meter)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "coordinator metrics:", err)
		return 2
	}
	cfg, err := coordinator.LoadConfigObserved(os.Getenv, newGrantObserver(instruments, logger))
	if err != nil {
		logger.Error("workflow coordinator operator plan failed to load config", "error", err)
		return 2
	}
	scannerRoleContract, err := runtimecfg.LoadPostgresRoleContract(os.Getenv,
		runtimecfg.PostgresWorkloadCoordinator, runtimecfg.PostgresRouteControl)
	if err != nil {
		logger.Error("workflow coordinator operator plan failed to load role contract", "error", err)
		return 2
	}
	if (cfg.ScannerImageDiscovery.Mode != coordinator.ScannerImageDiscoveryDisabled ||
		cfg.PublicAdvisoryPolicy.Explicit) && scannerRoleContract == nil {
		_, _ = fmt.Fprintln(stderr, "plan-go-reachability-scope: protected scanner and advisory policy requires a PostgreSQL role contract")
		return 2
	}

	store := newWorkflowControlStore(postgres.SQLDB{DB: database}, instruments, cfg.QuotaLookup)
	tenantGrantDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: database},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "tenant_workspace_grants",
	}
	tenantGrantStore := tenantstore.NewTenantWorkspaceGrantStore(tenantGrantDB)
	if err := tenantGrantStore.EnsureSchema(ctx); err != nil {
		logger.Error("workflow coordinator operator plan failed to ensure grant schema", "error", err)
		return 2
	}
	goModuleRepositoriesDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: database},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "go_module_repositories",
	}
	service := coordinator.Service{
		Config:                         cfg,
		Store:                          store,
		GoModuleRepositoryReader:       gomodules.NewStore(goModuleRepositoriesDB),
		GoModuleSingleRepositoryReader: gomodules.NewStore(goModuleRepositoriesDB),
		GoEvidenceFencer:               gomodules.NewStore(goModuleRepositoriesDB).WithHolderReopener(postgres.ReopenSupplyChainImpactHolderInTransaction).WithSupportsReadiness(goModulesFenceSupportsReadiness(instruments)).WithGoEvidenceLockObserver(goEvidenceLockObserver(instruments)),
		TenantGrantReader:              tenantGrantReader{store: tenantGrantStore},
		ActivationDerivedScopes:        instruments.ActivationDerivedScopesPlanned,
		ActivationDerivedScopesFenced:  instruments.ActivationDerivedScopesFenced,
		GenerationRetentionSoftCutoff:  generationRetentionSoftCutoff(os.Getenv),
		Metrics:                        coordinatorMetrics,
		Logger:                         logger,
	}
	if scannerRoleContract != nil {
		service.ControlRolePolicyRevision = scannerRoleContract.PolicyRevision
		service.ControlDatabaseIdentity = scannerRoleContract.DatabaseIdentity
	}

	result, err := service.PlanGoReachabilityScopeNow(ctx, request)
	if err != nil {
		logger.Error("workflow coordinator operator plan failed", "error", err)
		_, _ = fmt.Fprintln(stderr, "plan-go-reachability-scope:", err)
		return 2
	}
	document, err := json.Marshal(result)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "plan-go-reachability-scope:", err)
		return 2
	}
	_, _ = fmt.Fprintln(stdout, string(document))
	if result.Outcome == coordinator.OperatorPlanNotEligible {
		return 1
	}
	return 0
}

// runOperatorPlanWithSignals runs the one-shot under the same signal handling
// as the reconciler, so an interrupt cancels the attempt instead of stranding it.
func runOperatorPlanWithSignals(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return runPlanGoReachabilityScope(ctx, args, stdout, stderr)
}

// runOperatorCommand routes argv to the one-shot operator entry point without
// starting the reconciler. It reports whether it handled the invocation along
// with the one-shot's exit code; main exits with that code when handled.
func runOperatorCommand(args []string, stdout, stderr io.Writer) (int, bool) {
	if len(args) > 0 && args[0] == "plan-go-reachability-scope" {
		return runOperatorPlanWithSignals(args[1:], stdout, stderr), true
	}
	return 0, false
}
