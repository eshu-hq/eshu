package reducer

import "github.com/eshu-hq/eshu/go/internal/telemetry"

// Observability captures the frozen telemetry shape for the reducer runtime.
type Observability struct {
	MetricDimensions []string
	SpanNames        []string
	LogKeys          []string
}

// ObservabilityContract returns the reducer-specific telemetry contract.
func ObservabilityContract() Observability {
	return Observability{
		MetricDimensions: telemetry.MetricDimensionKeys(),
		SpanNames: []string{
			telemetry.SpanReducerIntentEnqueue,
			telemetry.SpanReducerRun,
			telemetry.SpanCanonicalWrite,
		},
		LogKeys: telemetry.LogKeys(),
	}
}
