// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package telemetry

import (
	"fmt"

	"go.opentelemetry.io/otel/metric"
)

// registerPackageManifestDependencyReadInstruments registers the per-read
// wall-time and page-count histograms of the paged manifest dependency read
// (#9586) on inst.
func registerPackageManifestDependencyReadInstruments(meter metric.Meter, inst *Instruments) error {
	var err error
	if inst.PackageManifestDependencyReadDuration, err = meter.Float64Histogram(
		"eshu_dp_package_manifest_dependency_read_seconds",
		metric.WithDescription("Wall time of one completed manifest dependency read across all of its keyset pages, by read (plain or maven_family) (#9586)"),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60),
	); err != nil {
		return fmt.Errorf("register PackageManifestDependencyReadDuration histogram: %w", err)
	}
	if inst.PackageManifestDependencyReadPages, err = meter.Int64Histogram(
		"eshu_dp_package_manifest_dependency_read_pages",
		metric.WithDescription("Keyset pages one completed manifest dependency read issued, by read (plain or maven_family) (#9586)"),
		metric.WithExplicitBucketBoundaries(1, 2, 3, 5, 10, 20, 50, 100, 200, 500, 1000),
	); err != nil {
		return fmt.Errorf("register PackageManifestDependencyReadPages histogram: %w", err)
	}
	return nil
}
