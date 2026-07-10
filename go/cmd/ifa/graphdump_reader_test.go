// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

// TestBoltStringSliceConvertsAnySlice proves the common Bolt-decoded shape
// (a Cypher list decoded as []any of strings, what labels() always returns)
// converts to []string in order.
func TestBoltStringSliceConvertsAnySlice(t *testing.T) {
	t.Parallel()

	got := boltStringSlice([]any{"Repository", "Package"})
	want := []string{"Repository", "Package"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("boltStringSlice([]any{...}) = %#v, want %#v", got, want)
	}
}

// TestBoltStringSliceDropsNonStringElements proves a non-string list element
// (which should never occur for labels(), but which the driver's decoder does
// not itself prevent) is dropped rather than panicking or corrupting the rest
// of the label set.
func TestBoltStringSliceDropsNonStringElements(t *testing.T) {
	t.Parallel()

	got := boltStringSlice([]any{"Repository", 42, "Package"})
	want := []string{"Repository", "Package"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("boltStringSlice with a non-string element = %#v, want %#v", got, want)
	}
}

// TestBoltStringSlicePassesThroughStringSlice covers the []string shape
// directly, in case a fake or future driver path already decodes to that.
func TestBoltStringSlicePassesThroughStringSlice(t *testing.T) {
	t.Parallel()

	got := boltStringSlice([]string{"Repository"})
	want := []string{"Repository"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("boltStringSlice([]string{...}) = %#v, want %#v", got, want)
	}
}

// TestBoltStringSliceNilForUnexpectedType proves an unexpected value shape
// (nil, a scalar, ...) returns nil rather than erroring: an unlabeled node
// canonicalizes to an empty label set, not a failure.
func TestBoltStringSliceNilForUnexpectedType(t *testing.T) {
	t.Parallel()

	if got := boltStringSlice(nil); got != nil {
		t.Errorf("boltStringSlice(nil) = %#v, want nil", got)
	}
	if got := boltStringSlice(42); got != nil {
		t.Errorf("boltStringSlice(42) = %#v, want nil", got)
	}
}

// TestBoltPropsMapPassesThroughMap proves the common Bolt-decoded shape (a
// Cypher map decoded as map[string]any, what properties() always returns)
// passes straight through.
func TestBoltPropsMapPassesThroughMap(t *testing.T) {
	t.Parallel()

	in := map[string]any{"uid": "repo-1"}
	got := boltPropsMap(in)
	if !reflect.DeepEqual(got, in) {
		t.Errorf("boltPropsMap(map) = %#v, want %#v", got, in)
	}
}

// TestBoltPropsMapNilForUnexpectedType proves an unexpected value shape
// returns nil (no properties) rather than erroring.
func TestBoltPropsMapNilForUnexpectedType(t *testing.T) {
	t.Parallel()

	if got := boltPropsMap("not-a-map"); got != nil {
		t.Errorf("boltPropsMap(string) = %#v, want nil", got)
	}
	if got := boltPropsMap(nil); got != nil {
		t.Errorf("boltPropsMap(nil) = %#v, want nil", got)
	}
}

// TestOpenBoltGraphReaderFailsFastWithoutBackend proves a missing graph
// backend configuration (no NEO4J_URI/NEO4J_USERNAME/NEO4J_PASSWORD) fails
// during config load, before any dial is attempted — this case is
// hermetically testable in CI with no NornicDB/Neo4j running, exactly like
// drive_test.go's missing-cassette and missing-Postgres-DSN cases.
func TestOpenBoltGraphReaderFailsFastWithoutBackend(t *testing.T) {
	t.Parallel()

	empty := func(string) string { return "" }
	_, closeFn, err := openBoltGraphReader(context.Background(), empty)
	if err == nil {
		t.Fatal("openBoltGraphReader(empty env) = nil error, want a config error naming the required NEO4J env vars")
	}
	if closeFn != nil {
		t.Error("openBoltGraphReader(empty env) returned a non-nil close func alongside an error")
	}
	if !strings.Contains(err.Error(), "NEO4J") {
		t.Errorf("error = %v, want it to name the required NEO4J env vars", err)
	}
}
