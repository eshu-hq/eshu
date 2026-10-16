// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package producerstore

import (
	"context"
	"reflect"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/eshu-hq/eshu/go/internal/parser/php/composer"
	"github.com/eshu-hq/eshu/go/internal/storage/postgres/db"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// queryRowsFake serves different rows per statement, so a test can tell the
// package.json read from the composer.json read.
type queryRowsFake struct {
	queries []string
	rows    map[string][][3]any
}

func (f *queryRowsFake) QueryContext(_ context.Context, query string, _ ...any) (db.Rows, error) {
	f.queries = append(f.queries, query)
	return &fakeRows{rows: f.rows[query]}, nil
}

// TestPackageScopeIDsRoutesComposerNamesToComposerManifests pins the #7601
// name-space split: a composer name (vendor/name) reads only composer.json
// manifests, an npm name only package.json manifests, and a request carrying
// both unions the two reads. A composer.json whose name equals an npm key, or a
// package.json whose name equals a composer key, never anchors.
func TestPackageScopeIDsRoutesComposerNamesToComposerManifests(t *testing.T) {
	t.Parallel()

	q := &queryRowsFake{rows: map[string][][3]any{
		PackageManifestsQuery: {
			{"scope-npm", `{"name":"@acme/logging"}`, "clean"},
			{"scope-npm-named-like-composer", `{"name":"acme/logging"}`, "clean"},
		},
		ComposerManifestsQuery: {
			{"scope-php", `{"name":"acme/logging"}`, "clean"},
			{"scope-php-named-like-npm", `{"name":"@acme/logging"}`, "clean"},
			{"scope-php-dirty", nil, "unactivated_tag"},
		},
	}}
	got, err := New(q).PackageScopeIDs(context.Background(), []string{
		"package:@acme/logging#Logger",
		`package:acme/logging#Acme\Logging\Logger`,
	})
	if err != nil {
		t.Fatalf("PackageScopeIDs() error = %v, want nil", err)
	}
	if want := []string{"scope-npm", "scope-php", "scope-php-dirty"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
	if want := []string{PackageManifestsQuery, ComposerManifestsQuery}; !reflect.DeepEqual(q.queries, want) {
		t.Fatalf("issued %d queries, want the package.json read then the composer.json read", len(q.queries))
	}
}

func TestPackageScopeIDsReadsOnlyComposerManifestsForComposerKeys(t *testing.T) {
	t.Parallel()

	q := &queryRowsFake{rows: map[string][][3]any{
		ComposerManifestsQuery: {{"scope-php", `{"name":"acme/logging"}`, "clean"}},
	}}
	got, err := New(q).PackageScopeIDs(context.Background(), []string{`package:acme/logging#Acme\Logging\Logger::create`})
	if err != nil {
		t.Fatalf("PackageScopeIDs() error = %v, want nil", err)
	}
	if want := []string{"scope-php"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("scopes = %v, want %v", got, want)
	}
	if want := []string{ComposerManifestsQuery}; !reflect.DeepEqual(q.queries, want) {
		t.Fatalf("queries = %d, want only the composer.json read", len(q.queries))
	}
}

// TestComposerManifestsQueryShape pins the statement's load-bearing parts: the
// MATERIALIZED CTE (one content_files read), the composer.json path
// predicates, the vendor/ exclusion, the fixture-directory exclusion (#8223,
// the pattern the parser's ProducerPackage shares), and the generation-tag
// columns.
func TestComposerManifestsQueryShape(t *testing.T) {
	t.Parallel()

	for _, want := range []string{
		"WITH manifest AS MATERIALIZED (",
		"relative_path = 'composer.json' OR relative_path LIKE '%/composer.json'",
		"relative_path !~ '(^|/)vendor/'",
		"relative_path !~* '" + composer.FixtureDirPattern + "'",
		"WHEN tagged.tag_activated_at IS NOT NULL THEN 'clean'",
		"SELECT scope.scope_id, NULL, 'manifest_less'",
	} {
		if !strings.Contains(ComposerManifestsQuery, want) {
			t.Fatalf("ComposerManifestsQuery missing %q", want)
		}
	}
	if strings.Contains(ComposerManifestsQuery, "package.json") {
		t.Fatal("ComposerManifestsQuery must not read package.json")
	}
}

func TestComposerManifestName(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		content string
		want    string
	}{
		{`{"name":"acme/logging"}`, "acme/logging"},
		{`{"name":"acme-corp/log.ging_x"}`, "acme-corp/log.ging_x"},
		{`{"name":"a/b--c"}`, "a/b--c"},
		{`{"name":"Acme/Logging"}`, ""},
		{`{"name":"@acme/logging"}`, ""},
		{`{"name":"logging"}`, ""},
		{`{"name":"acme/logging/extra"}`, ""},
		{`{"name":" acme/logging"}`, ""},
		{`{"name":"acme/-logging"}`, ""},
		{`{"name":42}`, ""},
		{`{"name":`, ""},
		{`[]`, ""},
	} {
		if got := ComposerManifestName(tc.content); got != tc.want {
			t.Errorf("ComposerManifestName(%s) = %q, want %q", tc.content, got, tc.want)
		}
	}
}

// TestScopeIDsRecordComposerTagOutcomesUnderTheirOwnKind pins the third closed
// kind label: composer rows are counted as kind=composer, never folded into
// package or gomod.
func TestScopeIDsRecordComposerTagOutcomesUnderTheirOwnKind(t *testing.T) {
	t.Parallel()

	reader := sdkmetric.NewManualReader()
	instruments, err := telemetry.NewInstruments(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)).Meter("test"))
	if err != nil {
		t.Fatalf("NewInstruments() error = %v", err)
	}
	q := &queryRowsFake{rows: map[string][][3]any{
		ComposerManifestsQuery: {
			{"scope-php", `{"name":"acme/logging"}`, "clean"},
			{"scope-dirty", nil, "null_tag"},
		},
	}}
	if _, err := New(q).WithInstruments(instruments).PackageScopeIDs(context.Background(), []string{`package:acme/logging#Acme\Logging\Logger`}); err != nil {
		t.Fatalf("PackageScopeIDs() error = %v, want nil", err)
	}

	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}
	got := map[string]int64{}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "eshu_dp_producer_manifest_tag_outcomes_total" {
				continue
			}
			for _, point := range m.Data.(metricdata.Sum[int64]).DataPoints {
				kind, _ := point.Attributes.Value(telemetry.MetricDimensionKind)
				outcome, _ := point.Attributes.Value(telemetry.MetricDimensionOutcome)
				got[kind.AsString()+"/"+outcome.AsString()] += point.Value
			}
		}
	}
	if want := map[string]int64{"composer/clean": 1, "composer/null_tag": 1}; !reflect.DeepEqual(got, want) {
		t.Fatalf("tag outcomes = %v, want %v", got, want)
	}
}
