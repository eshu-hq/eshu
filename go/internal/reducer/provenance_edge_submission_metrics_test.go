// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"errors"
	"testing"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

func TestProvenanceEdgeCounterRecordsSubmittedRowsAfterSuccessfulWrites(t *testing.T) {
	reader, instruments := newProvenanceEdgeMetricReader(t)
	intent := Intent{ScopeID: "scope-1", GenerationID: "generation-1"}

	packageWriter := &recordingPackageProvenanceEdgeWriter{}
	packageHandler := PackageSourceCorrelationHandler{
		ProvenanceEdgeWriter: packageWriter,
		Instruments:          instruments,
	}
	if err := packageHandler.projectPackageProvenanceEdges(
		context.Background(),
		intent,
		[]PackageSourceCorrelationDecision{{
			PackageID: "package-1", RepositoryID: "repository-1", Outcome: PackageSourceCorrelationExact,
		}},
		[]PackagePublicationDecision{{
			PackageID: "package-2", VersionID: "version-2", RepositoryID: "repository-2", Outcome: PackageSourceCorrelationExact,
		}},
	); err != nil {
		t.Fatalf("projectPackageProvenanceEdges() error = %v", err)
	}

	builtFromWriter := &recordingContainerImageProvenanceEdgeWriter{}
	builtFromHandler := ContainerImageIdentityHandler{
		ProvenanceEdgeWriter: builtFromWriter,
		Instruments:          instruments,
	}
	if err := builtFromHandler.projectContainerImageBuiltFromRows(
		context.Background(), intent, []map[string]any{{"digest": "sha256:child", "repository_id": "repository-1"}},
	); err != nil {
		t.Fatalf("projectContainerImageBuiltFromRows() error = %v", err)
	}

	derivedFromWriter := &recordingContainerImageDerivedFromEdgeWriter{}
	derivedFromHandler := ContainerImageIdentityHandler{
		DerivedFromEdgeWriter: derivedFromWriter,
		Instruments:           instruments,
	}
	if err := derivedFromHandler.projectContainerImageDerivedFromRows(
		context.Background(), intent, []map[string]any{{"digest": "sha256:child", "base_digest": "sha256:base"}},
	); err != nil {
		t.Fatalf("projectContainerImageDerivedFromRows() error = %v", err)
	}

	metrics := collectProvenanceEdgeMetrics(t, reader)
	for _, domain := range []string{
		packageOwnershipProvenanceEvidenceSource,
		packagePublicationProvenanceEvidenceSource,
		containerImageBuiltFromProvenanceEvidenceSource,
		containerImageDerivedFromProvenanceEvidenceSource,
	} {
		if got, ok := provenanceEdgeCounterValue(metrics, domain, "submitted"); !ok || got != 1 {
			t.Errorf("submitted counter for domain %q = (%d, %t), want (1, true)", domain, got, ok)
		}
		if got, ok := provenanceEdgeCounterValue(metrics, domain, "materialized"); ok {
			t.Errorf("materialized counter for domain %q = %d, want no point", domain, got)
		}
	}
}

func TestProvenanceEdgeCounterSkipsUnacceptedRows(t *testing.T) {
	tests := []struct {
		name      string
		wantError bool
		run       func(*telemetry.Instruments) error
	}{
		{
			name:      "package ownership write error",
			wantError: true,
			run: func(instruments *telemetry.Instruments) error {
				writer := &recordingPackageProvenanceEdgeWriter{writeErr: errors.New("write failed")}
				return (PackageSourceCorrelationHandler{ProvenanceEdgeWriter: writer, Instruments: instruments}).
					projectPackageProvenanceEdges(
						context.Background(),
						Intent{ScopeID: "scope-1", GenerationID: "generation-1"},
						[]PackageSourceCorrelationDecision{{
							PackageID: "package-1", RepositoryID: "repository-1", Outcome: PackageSourceCorrelationExact,
						}},
						nil,
					)
			},
		},
		{
			name:      "package publication write error",
			wantError: true,
			run: func(instruments *telemetry.Instruments) error {
				writer := &recordingPackageProvenanceEdgeWriter{writeErr: errors.New("write failed")}
				return (PackageSourceCorrelationHandler{ProvenanceEdgeWriter: writer, Instruments: instruments}).
					projectPackageProvenanceEdges(
						context.Background(),
						Intent{ScopeID: "scope-1", GenerationID: "generation-1"},
						nil,
						[]PackagePublicationDecision{{
							PackageID: "package-2", VersionID: "version-2", RepositoryID: "repository-2", Outcome: PackageSourceCorrelationExact,
						}},
					)
			},
		},
		{
			name:      "built from write error",
			wantError: true,
			run: func(instruments *telemetry.Instruments) error {
				writer := &recordingContainerImageProvenanceEdgeWriter{writeErr: errors.New("write failed")}
				return (ContainerImageIdentityHandler{ProvenanceEdgeWriter: writer, Instruments: instruments}).
					projectContainerImageBuiltFromRows(
						context.Background(),
						Intent{ScopeID: "scope-1", GenerationID: "generation-1"},
						[]map[string]any{{"digest": "sha256:child", "repository_id": "repository-1"}},
					)
			},
		},
		{
			name:      "derived from write error",
			wantError: true,
			run: func(instruments *telemetry.Instruments) error {
				writer := &recordingContainerImageDerivedFromEdgeWriter{writeErr: errors.New("write failed")}
				return (ContainerImageIdentityHandler{DerivedFromEdgeWriter: writer, Instruments: instruments}).
					projectContainerImageDerivedFromRows(
						context.Background(),
						Intent{ScopeID: "scope-1", GenerationID: "generation-1"},
						[]map[string]any{{"digest": "sha256:child", "base_digest": "sha256:base"}},
					)
			},
		},
		{
			name:      "retract error",
			wantError: true,
			run: func(instruments *telemetry.Instruments) error {
				writer := &recordingContainerImageProvenanceEdgeWriter{retractErr: errors.New("retract failed")}
				return (ContainerImageIdentityHandler{ProvenanceEdgeWriter: writer, Instruments: instruments}).
					projectContainerImageBuiltFromRows(
						context.Background(),
						Intent{ScopeID: "scope-1", GenerationID: "generation-1"},
						[]map[string]any{{"digest": "sha256:child", "repository_id": "repository-1"}},
					)
			},
		},
		{
			name: "empty projection",
			run: func(instruments *telemetry.Instruments) error {
				writer := &recordingContainerImageProvenanceEdgeWriter{}
				return (ContainerImageIdentityHandler{ProvenanceEdgeWriter: writer, Instruments: instruments}).
					projectContainerImageBuiltFromRows(
						context.Background(),
						Intent{ScopeID: "scope-1", GenerationID: "generation-1"},
						nil,
					)
			},
		},
		{
			name: "unwired projection",
			run: func(instruments *telemetry.Instruments) error {
				return (ContainerImageIdentityHandler{Instruments: instruments}).projectContainerImageBuiltFromEdges(
					context.Background(),
					Intent{ScopeID: "scope-1", GenerationID: "generation-1"},
					[]ContainerImageIdentityDecision{{
						Digest:                       "sha256:child",
						BuildProvenanceRepositoryIDs: []string{"repository-1"},
						Outcome:                      ContainerImageIdentityExactDigest,
					}},
				)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reader, instruments := newProvenanceEdgeMetricReader(t)
			err := test.run(instruments)
			if test.wantError && err == nil {
				t.Fatal("projection error = nil, want error")
			}
			if !test.wantError && err != nil {
				t.Fatalf("projection error = %v, want nil", err)
			}
			metrics := collectProvenanceEdgeMetrics(t, reader)
			if provenanceEdgeCounterHasPoint(metrics) {
				t.Fatal("provenance edge counter emitted a point for rows not accepted by a successful writer call")
			}
		})
	}
}

func TestProvenanceEdgeCounterKeepsSuccessfulSubmissionBeforeLaterFailure(t *testing.T) {
	reader, instruments := newProvenanceEdgeMetricReader(t)
	writer := &publicationFailingPackageProvenanceEdgeWriter{}
	handler := PackageSourceCorrelationHandler{ProvenanceEdgeWriter: writer, Instruments: instruments}
	err := handler.projectPackageProvenanceEdges(
		context.Background(),
		Intent{ScopeID: "scope-1", GenerationID: "generation-1"},
		[]PackageSourceCorrelationDecision{{
			PackageID: "package-1", RepositoryID: "repository-1", Outcome: PackageSourceCorrelationExact,
		}},
		[]PackagePublicationDecision{{
			PackageID: "package-2", VersionID: "version-2", RepositoryID: "repository-2", Outcome: PackageSourceCorrelationExact,
		}},
	)
	if err == nil {
		t.Fatal("projectPackageProvenanceEdges() error = nil, want publication write error")
	}

	metrics := collectProvenanceEdgeMetrics(t, reader)
	if got, ok := provenanceEdgeCounterValue(metrics, packageOwnershipProvenanceEvidenceSource, "submitted"); !ok || got != 1 {
		t.Fatalf("ownership submitted counter = (%d, %t), want (1, true)", got, ok)
	}
	if _, ok := provenanceEdgeCounterValue(metrics, packagePublicationProvenanceEvidenceSource, "submitted"); ok {
		t.Fatal("publication submitted counter emitted for a failed writer call")
	}
}

type publicationFailingPackageProvenanceEdgeWriter struct{}

func (*publicationFailingPackageProvenanceEdgeWriter) WritePublishesEdges(
	_ context.Context, _ []map[string]any, _ string, _ string, evidenceSource string,
) error {
	if evidenceSource == packagePublicationProvenanceEvidenceSource {
		return errors.New("publication write failed")
	}
	return nil
}

func (*publicationFailingPackageProvenanceEdgeWriter) RetractPublishesEdges(
	_ context.Context, _ string, _ string, _ string,
) error {
	return nil
}

func newProvenanceEdgeMetricReader(t *testing.T) (*sdkmetric.ManualReader, *telemetry.Instruments) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	instruments, err := telemetry.NewInstruments(provider.Meter("provenance-edge-submission-test"))
	if err != nil {
		t.Fatalf("telemetry.NewInstruments() error = %v", err)
	}
	return reader, instruments
}

func collectProvenanceEdgeMetrics(t *testing.T, reader *sdkmetric.ManualReader) metricdata.ResourceMetrics {
	t.Helper()
	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("ManualReader.Collect() error = %v", err)
	}
	return metrics
}

func provenanceEdgeCounterValue(metrics metricdata.ResourceMetrics, domain, outcome string) (int64, bool) {
	for _, scope := range metrics.ScopeMetrics {
		for _, observed := range scope.Metrics {
			if observed.Name != "eshu_dp_provenance_edges_total" {
				continue
			}
			sum, ok := observed.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				pointDomain, hasDomain := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionDomain))
				pointOutcome, hasOutcome := point.Attributes.Value(attribute.Key(telemetry.MetricDimensionOutcome))
				if hasDomain && hasOutcome && pointDomain.AsString() == domain && pointOutcome.AsString() == outcome {
					return point.Value, true
				}
			}
		}
	}
	return 0, false
}

func provenanceEdgeCounterHasPoint(metrics metricdata.ResourceMetrics) bool {
	for _, scope := range metrics.ScopeMetrics {
		for _, observed := range scope.Metrics {
			if observed.Name != "eshu_dp_provenance_edges_total" {
				continue
			}
			sum, ok := observed.Data.(metricdata.Sum[int64])
			if ok && len(sum.DataPoints) > 0 {
				return true
			}
		}
	}
	return false
}
