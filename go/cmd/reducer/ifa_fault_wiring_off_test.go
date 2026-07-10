// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build !ifafaultinjection

package main

import (
	"context"
	"testing"

	sourcecypher "github.com/eshu-hq/eshu/go/internal/storage/cypher"
)

// ifaWiringOffTestExecutor is a trivial sourcecypher.Executor whose identity
// a test can compare against wrapIfaFaultExecutor's return value to prove
// passthrough.
type ifaWiringOffTestExecutor struct{}

func (ifaWiringOffTestExecutor) Execute(context.Context, sourcecypher.Statement) error { return nil }

// TestWrapIfaFaultExecutorExcludesFaultByDefault proves the ifafaultinjection
// build tag's real ESHU_IFA_FAULT_SCRIPT wiring (ifa_fault_wiring.go) is
// absent from every normal build: this test carries the !ifafaultinjection
// tag, so it runs in the default `go test` and CI lane, where
// wrapIfaFaultExecutor must return inner unchanged and must never call
// getenv -- proven here by a getenv that fails the test if invoked at all.
func TestWrapIfaFaultExecutorExcludesFaultByDefault(t *testing.T) {
	t.Parallel()

	inner := ifaWiringOffTestExecutor{}
	getenv := func(name string) string {
		t.Fatalf("wrapIfaFaultExecutor must not read any environment variable outside the ifafaultinjection build tag; got a lookup for %q", name)
		return ""
	}

	got, err := wrapIfaFaultExecutor(inner, getenv, nil)
	if err != nil {
		t.Fatalf("wrapIfaFaultExecutor: %v", err)
	}
	if got != sourcecypher.Executor(inner) {
		t.Fatalf("expected wrapIfaFaultExecutor to return inner unchanged outside the ifafaultinjection build tag, got %#v", got)
	}
}
