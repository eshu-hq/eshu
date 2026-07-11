package telemetry

import "go.opentelemetry.io/otel/metric"

type Inits struct{}

func InitInstruments(meter metric.Meter) (*Inits, error) {
	if _, err := meter.Int64Counter(
		"eshu_dp_facts_emitted_total",
		metric.WithDescription("facts emitted"),
	); err != nil {
		return nil, err
	}
	if _, err := meter.Float64Histogram(
		"eshu_dp_reducer_run_duration_seconds",
		metric.WithDescription("reducer run duration"),
	); err != nil {
		return nil, err
	}
	if _, err := meter.Int64Counter(
		"eshu_dp_tfstate_snapshots_observed_total",
		metric.WithDescription("tfstate snapshots observed"),
	); err != nil {
		return nil, err
	}
	return &Inits{}, nil
}
