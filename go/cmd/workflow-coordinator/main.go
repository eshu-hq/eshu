package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/eshu-hq/eshu/go/internal/app"
	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/coordinator"
	runtimecfg "github.com/eshu-hq/eshu/go/internal/runtime"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-workflow-coordinator"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(context.Background()); err != nil {
		slog.Error("workflow coordinator failed", "error", err)
		os.Exit(1)
	}
}

func run(parent context.Context) error {
	bootstrap, err := telemetry.NewBootstrap("workflow-coordinator")
	if err != nil {
		return fmt.Errorf("telemetry bootstrap: %w", err)
	}
	providers, err := telemetry.NewProviders(parent, bootstrap)
	if err != nil {
		return fmt.Errorf("telemetry providers: %w", err)
	}
	defer func() {
		_ = providers.Shutdown(context.Background())
	}()

	logger := telemetry.NewLogger(bootstrap, "workflow-coordinator", "workflow-coordinator")

	db, err := runtimecfg.OpenPostgres(parent, os.Getenv)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	cfg, err := coordinator.LoadConfig(os.Getenv)
	if err != nil {
		return err
	}

	meter := providers.MeterProvider.Meter(telemetry.DefaultSignalName)
	metrics, err := coordinator.NewMetrics(meter)
	if err != nil {
		return fmt.Errorf("coordinator metrics: %w", err)
	}
	instruments, err := telemetry.NewInstruments(meter)
	if err != nil {
		return fmt.Errorf("telemetry instruments: %w", err)
	}

	store := postgres.NewWorkflowControlStore(postgres.SQLDB{DB: db})
	awsFreshnessDB := &postgres.InstrumentedDB{
		Inner:       postgres.SQLDB{DB: db},
		Tracer:      providers.TracerProvider.Tracer(telemetry.DefaultSignalName),
		Instruments: instruments,
		StoreName:   "aws_freshness_triggers",
	}
	awsFreshnessStore := postgres.NewAWSFreshnessStore(awsFreshnessDB)
	if err := awsFreshnessStore.EnsureSchema(parent); err != nil {
		return err
	}
	serviceRunner := coordinator.Service{
		Config: cfg,
		Store:  store,
		TerraformStatePlanner: coordinator.TerraformStateWorkPlanner{
			GitReadiness: postgres.TerraformStateGitReadinessChecker{DB: postgres.SQLQueryer{DB: db}},
			BackendFacts: postgres.TerraformStateBackendFactReader{DB: postgres.SQLQueryer{DB: db}},
		},
		OCIRegistryPlanner:     coordinator.OCIRegistryWorkPlanner{},
		PackageRegistryPlanner: coordinator.PackageRegistryWorkPlanner{},
		AWSFreshnessTriggers:   awsFreshnessStore,
		AWSFreshnessPlanner:    coordinator.AWSFreshnessWorkPlanner{},
		AWSFreshnessEvents:     instruments.AWSFreshnessEvents,
		Metrics:                metrics,
		Logger:                 logger,
	}
	statusReader := postgres.NewStatusStore(postgres.SQLQueryer{DB: db})
	service, err := app.NewHostedWithStatusServer(
		"workflow-coordinator",
		serviceRunner,
		statusReader,
		runtimecfg.WithPrometheusHandler(providers.PrometheusHandler),
	)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	return service.Run(ctx)
}
