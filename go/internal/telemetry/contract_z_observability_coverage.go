package telemetry

import "slices"

const (
	// SpanQueryObservabilityCoverageCorrelations wraps reducer-owned
	// observability coverage correlation reads from durable facts (which
	// monitored resources or services have alarm/dashboard/log/trace coverage
	// versus which are gaps).
	SpanQueryObservabilityCoverageCorrelations = "query.observability_coverage_correlations"
)

// init lands this span right after the service catalog correlation span. Go runs
// cross-file package init in filename order, so the contract_z_ prefix forces this
// file to initialize after contract_service_catalog.go (and the other contract_*.go
// span inserts), guaranteeing the service catalog anchor is already present.
func init() {
	for idx, name := range spanNames {
		if name == SpanQueryServiceCatalogCorrelations {
			spanNames = slices.Insert(spanNames, idx+1, SpanQueryObservabilityCoverageCorrelations)
			return
		}
	}
	spanNames = append(spanNames, SpanQueryObservabilityCoverageCorrelations)
}
