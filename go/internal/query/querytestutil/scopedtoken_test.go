// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package querytestutil_test

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/queryauth"
	"github.com/eshu-hq/eshu/go/internal/query/querytestutil"
)

// TestFakeScopedTokenResolverAnswersFromItsFields covers the ordinary use: a
// test configures an auth context and a verdict, and the middleware under test
// receives exactly those.
func TestFakeScopedTokenResolverAnswersFromItsFields(t *testing.T) {
	t.Parallel()

	sentinel := errors.New("resolver unavailable")
	resolver := querytestutil.FakeScopedTokenResolver{
		Context: queryauth.AuthContext{TenantID: "tenant-1"},
		OK:      true,
		Err:     sentinel,
	}

	authCtx, ok, err := resolver.ResolveScopedToken(context.Background(), "presented")
	if authCtx.TenantID != "tenant-1" {
		t.Fatalf("AuthContext.TenantID = %q, want tenant-1", authCtx.TenantID)
	}
	if !ok {
		t.Fatal("ok = false, want the configured true")
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want %v", err, sentinel)
	}
}

// TestFakeScopedTokenResolverRecordsThePresentedToken pins the capture half.
// Middleware tests assert the resolver saw the scoped credential and not, say,
// the raw Authorization header, so the recorded token has to be the argument.
func TestFakeScopedTokenResolverRecordsThePresentedToken(t *testing.T) {
	t.Parallel()

	var resolver querytestutil.FakeScopedTokenResolver

	if resolver.Called() {
		t.Fatal("Called() = true before any call")
	}
	if got := resolver.Token(); got != "" {
		t.Fatalf("Token() = %q before any call, want empty", got)
	}

	if _, _, err := resolver.ResolveScopedToken(context.Background(), "scoped-token"); err != nil {
		t.Fatalf("ResolveScopedToken() error = %v", err)
	}

	if !resolver.Called() {
		t.Fatal("Called() = false after a call")
	}
	if got := resolver.Token(); got != "scoped-token" {
		t.Fatalf("Token() = %q, want scoped-token", got)
	}
}

// TestFakeScopedTokenResolverAnsweringOverridesTheFields covers the entry point
// root query's adapter uses. It keeps the answer in its own lowercase fields so
// its consuming test files stay untouched, so it needs to supply the answer per
// call while still reusing this recording.
func TestFakeScopedTokenResolverAnsweringOverridesTheFields(t *testing.T) {
	t.Parallel()

	resolver := querytestutil.FakeScopedTokenResolver{
		Context: queryauth.AuthContext{TenantID: "unused"},
		OK:      false,
	}

	authCtx, ok, err := resolver.ResolveAnswering(
		context.Background(),
		"adapter-token",
		queryauth.AuthContext{TenantID: "supplied"},
		true,
		nil,
	)
	if err != nil {
		t.Fatalf("ResolveAnswering() error = %v", err)
	}
	if authCtx.TenantID != "supplied" {
		t.Fatalf("AuthContext.TenantID = %q, want the supplied answer", authCtx.TenantID)
	}
	if !ok {
		t.Fatal("ok = false, want the supplied true rather than the field's false")
	}
	if got := resolver.Token(); got != "adapter-token" {
		t.Fatalf("Token() = %q, want adapter-token", got)
	}
	if !resolver.Called() {
		t.Fatal("Called() = false, want the supplied-answer path to record the call too")
	}
}

// TestFakeScopedTokenResolverIsSafeUnderConcurrentUse guards the reason this
// fake carries a lock at all. One resolver instance is shared across parallel
// subtests -- the package-registry adjacent-route table does exactly this --
// which calls the resolver concurrently. Without the lock the shared fake races
// under -race and aborts unrelated tests in the package.
func TestFakeScopedTokenResolverIsSafeUnderConcurrentUse(t *testing.T) {
	t.Parallel()

	var resolver querytestutil.FakeScopedTokenResolver
	var waitGroup sync.WaitGroup

	for i := 0; i < 16; i++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			_, _, _ = resolver.ResolveScopedToken(context.Background(), "concurrent")
		}()
	}
	waitGroup.Wait()

	if !resolver.Called() {
		t.Fatal("Called() = false after concurrent calls")
	}
	if got := resolver.Token(); got != "concurrent" {
		t.Fatalf("Token() = %q, want concurrent", got)
	}
}
