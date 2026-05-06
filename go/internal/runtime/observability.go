package runtime

import "github.com/eshu-hq/eshu/go/internal/telemetry"

// Observability carries the frozen OTEL contract through bootstrap wiring.
type Observability struct {
	MetricDimensions []string
	SpanNames        []string
	LogKeys          []string
}

// NewObservability snapshots the shared telemetry contract for a service.
func NewObservability() Observability {
	return Observability{
		MetricDimensions: telemetry.MetricDimensionKeys(),
		SpanNames:        telemetry.SpanNames(),
		LogKeys:          telemetry.LogKeys(),
	}
}
