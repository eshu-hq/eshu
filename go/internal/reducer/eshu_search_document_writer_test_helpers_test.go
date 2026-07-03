// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/searchdocs"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// fakeSearchDocExecCall records a single ExecContext invocation for assertion.
type fakeSearchDocExecCall struct {
	query string
	args  []any
}

type fakeSearchDocResult struct {
	affected int64
}

func (r fakeSearchDocResult) LastInsertId() (int64, error) { return 0, nil }
func (r fakeSearchDocResult) RowsAffected() (int64, error) { return r.affected, nil }

// fakeSearchDocExecer is a test double for eshuSearchDocumentExecer. It records
// all ExecContext calls and returns configurable row-affected counts or errors.
type fakeSearchDocExecer struct {
	execs          []fakeSearchDocExecCall
	retireAffected int64
	failOn         string
	affected       []fakeSearchDocAffected
	delay          time.Duration
}

type fakeSearchDocAffected struct {
	fragment string
	affected int64
}

func (f *fakeSearchDocExecer) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	f.execs = append(f.execs, fakeSearchDocExecCall{query: query, args: args})
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.failOn != "" && strings.Contains(query, f.failOn) {
		return nil, errors.New("boom")
	}
	for _, match := range f.affected {
		if strings.Contains(query, match.fragment) {
			return fakeSearchDocResult{affected: match.affected}, nil
		}
	}
	if strings.Contains(query, "DELETE FROM fact_records") {
		return fakeSearchDocResult{affected: f.retireAffected}, nil
	}
	return fakeSearchDocResult{affected: 1}, nil
}

type fakeSearchIndexTermCopier struct {
	calls       []fakeSearchIndexTermCopyCall
	err         error
	copiedCount int64
}

type fakeSearchIndexTermCopyCall struct {
	scopeID      string
	generationID string
	documentIDs  []string
	terms        []string
	termKeys     []string
	frequencies  []int
}

func (f *fakeSearchIndexTermCopier) CopySearchIndexTerms(
	_ context.Context,
	scopeID string,
	generationID string,
	documentIDs []string,
	terms []string,
	termKeys []string,
	frequencies []int,
) (int64, error) {
	f.calls = append(f.calls, fakeSearchIndexTermCopyCall{
		scopeID:      scopeID,
		generationID: generationID,
		documentIDs:  append([]string(nil), documentIDs...),
		terms:        append([]string(nil), terms...),
		termKeys:     append([]string(nil), termKeys...),
		frequencies:  append([]int(nil), frequencies...),
	})
	if f.err != nil {
		return 0, f.err
	}
	if f.copiedCount > 0 {
		return f.copiedCount, nil
	}
	return int64(len(terms)), nil
}

// sampleSearchDoc returns a minimal valid Document for use in writer tests.
func sampleSearchDoc(id string) searchdocs.Document {
	return searchdocs.Document{
		ID:           id,
		RepoID:       "repo-1",
		SourceKind:   searchdocs.SourceKindCodeEntity,
		Title:        "Function Handle",
		GraphHandles: []searchdocs.GraphHandle{{Kind: "content_entity", ID: id}},
		TruthScope:   searchdocs.TruthScope{Level: searchdocs.TruthLevelDerived, Basis: searchdocs.TruthBasisContentIndex},
		Freshness:    searchdocs.Freshness{State: searchdocs.FreshnessFresh},
	}
}

// int64MetricValue returns the current value for the named counter metric
// filtered by the given attribute set. Fails the test if the metric is absent.
func int64MetricValue(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	wantAttrs map[string]string,
) int64 {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, metricRecord := range scope.Metrics {
			if metricRecord.Name != name {
				continue
			}
			sum, ok := metricRecord.Data.(metricdata.Sum[int64])
			if !ok {
				t.Fatalf("%s data type = %T, want Int64 sum", name, metricRecord.Data)
			}
			for _, point := range sum.DataPoints {
				if metricPointHasAttrs(point.Attributes.ToSlice(), wantAttrs) {
					return point.Value
				}
			}
		}
	}
	t.Fatalf("metric %s with attrs %#v not found", name, wantAttrs)
	return 0
}

// assertHistogramPoint asserts a histogram data point exists for the given
// metric name and attribute set.
func assertHistogramPoint(
	t *testing.T,
	rm metricdata.ResourceMetrics,
	name string,
	wantAttrs map[string]string,
) {
	t.Helper()
	for _, scope := range rm.ScopeMetrics {
		for _, metricRecord := range scope.Metrics {
			if metricRecord.Name != name {
				continue
			}
			histogram, ok := metricRecord.Data.(metricdata.Histogram[float64])
			if !ok {
				t.Fatalf("%s data type = %T, want Float64 histogram", name, metricRecord.Data)
			}
			for _, point := range histogram.DataPoints {
				if metricPointHasAttrs(point.Attributes.ToSlice(), wantAttrs) {
					return
				}
			}
		}
	}
	t.Fatalf("histogram %s with attrs %#v not found", name, wantAttrs)
}

func metricPointHasAttrs(attrs []attribute.KeyValue, want map[string]string) bool {
	for wantKey, wantValue := range want {
		found := false
		for _, attr := range attrs {
			if string(attr.Key) == wantKey && attr.Value.AsString() == wantValue {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func requireSpan(t *testing.T, spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, span := range spans {
		if span.Name() == name {
			return span
		}
	}
	t.Fatalf("span %q not found", name)
	return nil
}

func assertSpanStringAttribute(t *testing.T, span sdktrace.ReadOnlySpan, key string, want string) {
	t.Helper()
	for _, attr := range span.Attributes() {
		if string(attr.Key) == key {
			if got := attr.Value.AsString(); got != want {
				t.Fatalf("span attr %s = %q, want %q", key, got, want)
			}
			return
		}
	}
	t.Fatalf("span attr %s not found", key)
}
