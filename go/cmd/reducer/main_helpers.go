package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/eshu-hq/eshu/go/internal/buildinfo"
	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships/tfstatebackend"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres"
)

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
