package accuracygate

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

// LoadBaseline reads and validates a checked-in accuracy baseline file.
func LoadBaseline(path string) (Baseline, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Baseline{}, fmt.Errorf("read accuracy baseline %q: %w", path, err)
	}
	var baseline Baseline
	if err := json.Unmarshal(data, &baseline); err != nil {
		return Baseline{}, fmt.Errorf("parse accuracy baseline %q: %w", path, err)
	}
	if err := validateBaseline(baseline); err != nil {
		return Baseline{}, fmt.Errorf("accuracy baseline %q is invalid: %w", path, err)
	}
	return baseline, nil
}

// validateBaseline rejects a malformed or partial baseline before it can be used
// as a floor. Every known dimension must have a threshold so a dropped dimension
// cannot silently disable its gate, thresholds must stay in range, and unknown
// dimension keys are fixture errors rather than silent no-ops.
func validateBaseline(baseline Baseline) error {
	if baseline.SchemaVersion != schemaVersion {
		return fmt.Errorf("schema_version = %q, want %q", baseline.SchemaVersion, schemaVersion)
	}
	if len(baseline.Thresholds) == 0 {
		return fmt.Errorf("thresholds is required")
	}
	known := make(map[Dimension]struct{}, len(orderedDimensions))
	for _, dimension := range orderedDimensions {
		known[dimension] = struct{}{}
		threshold, ok := baseline.Thresholds[dimension]
		if !ok {
			return fmt.Errorf("thresholds is missing required dimension %q", dimension)
		}
		if err := validateThreshold(dimension, threshold); err != nil {
			return err
		}
	}
	for dimension := range baseline.Thresholds {
		if _, ok := known[dimension]; !ok {
			return fmt.Errorf("thresholds has unknown dimension %q", dimension)
		}
	}
	return nil
}

// validateThreshold bounds-checks a single dimension floor.
func validateThreshold(dimension Dimension, threshold Threshold) error {
	if threshold.MinPrecision < 0 || threshold.MinPrecision > 1 {
		return fmt.Errorf("dimension %q min_precision = %v, want [0,1]", dimension, threshold.MinPrecision)
	}
	if threshold.MinRecall < 0 || threshold.MinRecall > 1 {
		return fmt.Errorf("dimension %q min_recall = %v, want [0,1]", dimension, threshold.MinRecall)
	}
	if threshold.MinCoveredItems < 0 {
		return fmt.Errorf("dimension %q min_covered_items = %d, want >= 0", dimension, threshold.MinCoveredItems)
	}
	return nil
}

// PublishedMetrics is the deterministic per-dimension metrics snapshot emitted
// for tracking accuracy over time. It carries the schema version, the measured
// metric and applied floor per dimension, the pass/fail, and the per-label
// detail so a reviewer can diff two runs to see exactly which language or
// evidence kind moved.
type PublishedMetrics struct {
	SchemaVersion string               `json:"schema_version"`
	Dimensions    []PublishedDimension `json:"dimensions"`
	Pass          bool                 `json:"pass"`
}

// PublishedDimension is one dimension's published row in PublishedMetrics.
type PublishedDimension struct {
	Dimension    Dimension         `json:"dimension"`
	Precision    float64           `json:"precision"`
	Recall       float64           `json:"recall"`
	CoveredItems int               `json:"covered_items"`
	MinPrecision float64           `json:"min_precision"`
	MinRecall    float64           `json:"min_recall"`
	MinCovered   int               `json:"min_covered_items"`
	Pass         bool              `json:"pass"`
	Labels       map[string]string `json:"labels,omitempty"`
}

// Publish renders a gate result into a deterministic metrics snapshot. Dimension
// rows follow the gate's ordering and label maps are emitted as-is; JSON
// marshals map keys sorted, so the output is reproducible for over-time diffing.
func Publish(result GateResult) PublishedMetrics {
	published := PublishedMetrics{
		SchemaVersion: schemaVersion,
		Dimensions:    make([]PublishedDimension, 0, len(result.Results)),
		Pass:          result.Pass(),
	}
	for _, dimensionResult := range result.Results {
		published.Dimensions = append(published.Dimensions, PublishedDimension{
			Dimension:    dimensionResult.Dimension,
			Precision:    dimensionResult.Metric.Precision,
			Recall:       dimensionResult.Metric.Recall,
			CoveredItems: dimensionResult.Metric.CoveredItems,
			MinPrecision: dimensionResult.Threshold.MinPrecision,
			MinRecall:    dimensionResult.Threshold.MinRecall,
			MinCovered:   dimensionResult.Threshold.MinCoveredItems,
			Pass:         dimensionResult.Pass,
			Labels:       dimensionResult.Metric.Labels,
		})
	}
	return published
}

// Encode renders published metrics as stable, indented JSON suitable for writing
// to a tracked artifact or printing in CI logs.
func (p PublishedMetrics) Encode() ([]byte, error) {
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode published accuracy metrics: %w", err)
	}
	return append(data, '\n'), nil
}

// SortedLabelKeys returns a published dimension's label keys in sorted order, a
// convenience for callers that render labels in a stable sequence outside JSON.
func (d PublishedDimension) SortedLabelKeys() []string {
	keys := make([]string, 0, len(d.Labels))
	for key := range d.Labels {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
