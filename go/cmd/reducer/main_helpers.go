package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"sync/atomic"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/metric"
)

// registerReducerObservableGauges wires the reducer's OpenTelemetry observable
// gauges: queue depth/oldest-age (eshu_dp_queue_depth,
// eshu_dp_queue_oldest_age_seconds), the active worker-pool gauge
// (eshu_dp_worker_pool_active, backed by activeWorkers), and the
// shared-acceptance read-model gauge (eshu_dp_shared_acceptance_rows). The queue
// and acceptance observers read cheap, bounded queries; the worker observer
// reads an in-memory atomic counter. None add scan cost per metrics scrape. It
// lives here rather than in main.go to keep that file within the file-size
// budget.
func registerReducerObservableGauges(
	instruments *telemetry.Instruments,
	meter metric.Meter,
	db *sql.DB,
	activeWorkers *atomic.Int64,
) error {
	queueObserver := postgres.NewQueueObserverStore(postgres.SQLQueryer{DB: db})
	workerObserver := reducerWorkerObserver{active: activeWorkers}
	if err := telemetry.RegisterObservableGauges(instruments, meter, queueObserver, workerObserver); err != nil {
		return fmt.Errorf("register observable gauges: %w", err)
	}

	acceptanceObserver := postgres.NewSharedProjectionAcceptanceStore(postgres.SQLDB{DB: db})
	if err := telemetry.RegisterAcceptanceObservableGauges(instruments, meter, acceptanceObserver); err != nil {
		return fmt.Errorf("register acceptance observable gauge: %w", err)
	}
	return nil
}

// main is the reducer entrypoint. It prints the version when requested, then
// runs the service loop. It lives in this helper file rather than main.go to
// keep that file within the repo file-size budget.
func main() {
	if handled, err := buildinfo.PrintVersionFlag(os.Args[1:], os.Stdout, "eshu-reducer"); handled {
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if err := run(context.Background()); err != nil {
		slog.Error("reducer failed", "error", err)
		os.Exit(1)
	}
}

// reducerDomainStrings renders reducer domains as plain strings for structured
// log fields. It lives here rather than in main.go to keep that file within the
// repo file-size budget.
func reducerDomainStrings(domains []reducer.Domain) []string {
	values := make([]string, 0, len(domains))
	for _, domain := range domains {
		values = append(values, string(domain))
	}
	return values
}

// incidentRepositoryCorrelationWiring builds the production adapters for the
// durable incident -> repository correlation domain (#2161). The applied-routing
// loader supplies the PagerDuty provider service id and the Terraform backend
// locator; the backend resolver maps that locator to a single owning config
// repository using the same tfstatebackend join the config/state and cloud
// runtime drift domains use, so every backend-ownership consumer agrees. Only
// confident single-owner resolutions emit a durable edge; weaker signals stay
// provenance-only and fail-closed. It is extracted from the entrypoint so the
// reducer command stays within the repo file-size budget.
func incidentRepositoryCorrelationWiring(database postgres.ExecQueryer) (
	reducer.AppliedPagerDutyServiceRoutingLoader,
	reducer.BackendRepositoryResolver,
	reducer.IncidentRepositoryCorrelationWriter,
) {
	loader := postgres.PostgresAppliedPagerDutyServiceRoutingLoader{DB: database}
	resolver := postgres.BackendRepositoryResolverAdapter{
		Resolver: tfstatebackend.NewResolver(
			postgres.PostgresTerraformBackendQuery{DB: database},
		),
	}
	writer := reducer.PostgresIncidentRepositoryCorrelationWriter{DB: database}
	return loader, resolver, writer
}
