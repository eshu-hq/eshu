// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"context"
	"testing"
	"time"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/eshu-hq/eshu/go/internal/packageidentity"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// TestManifestDependencyQueryTextsAllHaveReadLabels fails closed when a new
// manifest dependency query text is added without a read label: every
// ecosystem, alone and beside Maven, must select a text the label table names.
func TestManifestDependencyQueryTextsAllHaveReadLabels(t *testing.T) {
	t.Parallel()

	ecosystems := []packageidentity.Ecosystem{
		packageidentity.EcosystemNPM, packageidentity.EcosystemPyPI, packageidentity.EcosystemGoModule,
		packageidentity.EcosystemMaven, packageidentity.EcosystemNuGet, packageidentity.EcosystemComposer,
		packageidentity.EcosystemRubyGems, packageidentity.EcosystemCargo, packageidentity.EcosystemSwift,
		packageidentity.EcosystemHex, packageidentity.EcosystemPub, packageidentity.EcosystemOS,
		packageidentity.EcosystemGeneric,
	}
	for _, ecosystem := range ecosystems {
		for _, set := range [][]string{{string(ecosystem)}, {string(ecosystem), string(packageidentity.EcosystemMaven)}} {
			_, query := activePackageManifestDependencyQuery(set)
			if _, ok := manifestDependencyReadLabels[query]; !ok {
				t.Fatalf("ecosystems %v select a query text with no read label in manifestDependencyReadLabels", set)
			}
		}
	}
}

// TestListActivePackageManifestDependencyFactsLabelsEngagedSnapshotReads
// pins the #9299 engaged path: on a snapshot clone the static rewrite
// changes the query text, but the read label still names the query that
// ran (plain/maven_family), never unclassified.
func TestListActivePackageManifestDependencyFactsLabelsEngagedSnapshotReads(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	db := &fakeExecQueryer{queryResponses: []queueFakeRows{{}, {}}}
	store := NewFactStore(db).WithInstruments(instruments)
	store.impactStaticSnapshot = true
	if _, err := store.ListActivePackageManifestDependencyFacts(context.Background(), []string{"npm"}, []string{"left-pad"}); err != nil {
		t.Fatalf("engaged plain read: %v", err)
	}
	if _, err := store.ListActivePackageManifestDependencyFacts(context.Background(), []string{"maven"}, []string{"g:a"}); err != nil {
		t.Fatalf("engaged maven read: %v", err)
	}

	pages := collectManifestDependencyRead(t, reader, "eshu_dp_package_manifest_dependency_read_pages")
	if got, want := pages["plain"], (manifestDependencyReadSample{count: 1, sum: 1}); got != want {
		t.Fatalf("engaged plain pages = %+v, want %+v (all: %+v)", got, want, pages)
	}
	if got, want := pages["maven_family"], (manifestDependencyReadSample{count: 1, sum: 1}); got != want {
		t.Fatalf("engaged maven_family pages = %+v, want %+v (all: %+v)", got, want, pages)
	}
}

// TestRecordManifestDependencyReadLabelsUnknownTextUnclassified pins the
// fail-closed default: a query text missing from the label table records
// read=unclassified, never a silent plain.
func TestRecordManifestDependencyReadLabelsUnknownTextUnclassified(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments: %v", err)
	}
	store := NewFactStore(&fakeExecQueryer{}).WithInstruments(instruments)
	store.recordManifestDependencyRead(context.Background(), "SELECT 1", time.Now(), 0)

	pages := collectManifestDependencyRead(t, reader, "eshu_dp_package_manifest_dependency_read_pages")
	if _, ok := pages["unclassified"]; !ok || len(pages) != 1 {
		t.Fatalf("unknown query text labels = %+v, want only unclassified", pages)
	}
}
