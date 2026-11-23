// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build conformance

package main

// Read-transport conformance tests (Refs eshu-hq/eshu#9669, part of #6707).
//
// Run explicitly; the default suite never selects this tag:
//
//	CGO_CFLAGS="-std=gnu17" go test -tags conformance ./cmd/api/ -run 'TestReadTransport|TestNativeNow' -count=1 -v
//	CGO_CFLAGS="-std=gnu17" go vet -tags conformance ./cmd/api/
//
// (Run from the worktree's go/ directory; the tag is
// intentionally not referenced by any CI workflow, Makefile target, or
// script — it gates the cross-repo parity harness, which runs against a
// satellite deployment, not in unit CI.)
//
// TDD record: TestReadTransportV0Parity is RED as committed —
// RunReadTransportConformance is a skeleton returning
// ErrTransportNotImplemented, so the test fails before the transport
// exists. That red is expected and recorded; it is NOT fixed by editing
// this test. The satellite implementer replaces the stub probe body in
// read_transport_conformance.go (keeping the signature and report shape)
// and this test turns green unedited.

import (
	"context"
	"errors"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// TestReadTransportVersionPinned keeps the transport spec on the same
// contract version as the core-owned read ports. A core port break bumps
// querycontract.CoreReadContractsVersion and fails here until this lane's
// spec moves with it.
func TestReadTransportVersionPinned(t *testing.T) {
	t.Parallel()

	if ReadTransportContractsVersion != "v0" {
		t.Errorf("ReadTransportContractsVersion = %q, want %q: bump it deliberately with the spec", ReadTransportContractsVersion, "v0")
	}
	if ReadTransportContractsVersion != querycontract.CoreReadContractsVersion {
		t.Errorf("ReadTransportContractsVersion = %q, core = %q: the transport spec drifted from the core ports",
			ReadTransportContractsVersion, querycontract.CoreReadContractsVersion)
	}
}

// frozenNativeNowWave pins the exact Native-now first-wave surface the
// satellite must implement. A wave change (route added, removed, renamed,
// or rehomed) fails here until the spec doc, the skeleton table, and this
// pin move together — the same frozen-set pattern as
// TestCoreReadContractsVersioned.
func TestNativeNowWaveFrozen(t *testing.T) {
	t.Parallel()

	want := []nativeNowRoute{
		{Method: "GET", Path: "/api/v0/openapi.json", Family: "APIRouter", OpenAPIFragment: "paths/status/routes.go"},
		{Method: "GET", Path: "/health", Family: "APIRouter", OpenAPIFragment: "<mux-health>"},
		{Method: "GET", Path: "/api/v0/docs", Family: "APIRouter", OpenAPIFragment: "<doc-ui>"},
		{Method: "GET", Path: "/api/v0/redoc", Family: "APIRouter", OpenAPIFragment: "<doc-ui>"},
		{Method: "GET", Path: "/api/v0/capabilities", Family: "Capabilities", OpenAPIFragment: "paths/catalog/capabilities.go"},
		{Method: "POST", Path: "/api/v0/answer-quality/score", Family: "CLISupport", OpenAPIFragment: "paths/cli/score.go"},
		{Method: "POST", Path: "/api/v0/investigation/packet/render", Family: "CLISupport", OpenAPIFragment: "paths/cli/render.go"},
		{Method: "POST", Path: "/api/v0/report/bundle/capture", Family: "CLISupport", OpenAPIFragment: "paths/cli/capture.go"},
		{Method: "POST", Path: "/api/v0/report/bundle/validate", Family: "CLISupport", OpenAPIFragment: "paths/cli/validate.go"},
		{Method: "POST", Path: "/api/v0/service/report/compose", Family: "CLISupport", OpenAPIFragment: "paths/cli/compose.go"},
		{Method: "POST", Path: "/api/v0/ask", Family: "Ask", OpenAPIFragment: "paths/search/ask.go"},
		{Method: "GET", Path: "/api/v0/services/{service_name}/intelligence-report", Family: "ServiceIntelHTTP", OpenAPIFragment: "paths/service/intelligence_report.go"},
		{Method: "GET", Path: "/api/v0/auth/providers", Family: "AuthProviders", OpenAPIFragment: "paths/auth/routes.go"},
		{Method: "GET", Path: "/api/v0/auth/sign-in-policy", Family: "SignInPolicyReads", OpenAPIFragment: "paths/auth/sign_in_policy.go"},
		{Method: "GET", Path: "/api/v0/auth/admin/sign-in-policy", Family: "SignInPolicyReads", OpenAPIFragment: "paths/auth/sign_in_policy.go"},
		{Method: "GET", Path: "/api/v0/auth/admin/provider-configs", Family: "AdminProviderConfigReads", OpenAPIFragment: "paths/auth/admin_provider_configs.go"},
		{Method: "GET", Path: "/api/v0/auth/admin/provider-configs/{provider_config_id}", Family: "AdminProviderConfigReads", OpenAPIFragment: "paths/auth/admin_provider_configs.go"},
		{Method: "GET", Path: "/api/v0/auth/admin/provider-configs/{provider_config_id}/revisions", Family: "AdminProviderConfigReads", OpenAPIFragment: "paths/auth/admin_provider_configs.go"},
	}
	if len(nativeNowWave) != len(want) {
		t.Fatalf("nativeNowWave holds %d routes, want frozen %d: move the spec, table, and pin together",
			len(nativeNowWave), len(want))
	}
	got := make([]string, 0, len(nativeNowWave))
	for _, route := range nativeNowWave {
		got = append(got, route.Method+" "+route.Path+" "+route.Family+" "+route.OpenAPIFragment)
	}
	frozen := make([]string, 0, len(want))
	for _, route := range want {
		frozen = append(frozen, route.Method+" "+route.Path+" "+route.Family+" "+route.OpenAPIFragment)
	}
	sort.Strings(got)
	sort.Strings(frozen)
	for i := range got {
		if got[i] != frozen[i] {
			t.Errorf("nativeNowWave differs at index %d: got %q, want %q", i, got[i], frozen[i])
		}
	}
}

// TestReadTransportV0Parity is the TDD-red conformance gate: it demands a
// clean probe report for the Native-now wave at ReadTransportContractsVersion.
// RED as committed — the skeleton returns ErrTransportNotImplemented, so
// this fails before the transport exists (expected, recorded). It turns
// green only when the satellite probe body lands; never by editing this test.
func TestReadTransportV0Parity(t *testing.T) {
	t.Parallel()

	report, err := RunReadTransportConformance(context.Background(), ConformanceConfig{
		BaseURL: "http://127.0.0.1:1",
		Token:   "conformance-probe",
	})
	if err != nil {
		if errors.Is(err, ErrTransportNotImplemented) {
			t.Fatalf("RED (expected until the satellite probe lands): %v", err)
		}
		t.Fatalf("probe errored for an unexpected reason: %v", err)
	}
	if report.ContractsVersion != ReadTransportContractsVersion {
		t.Errorf("report version = %q, want %q", report.ContractsVersion, ReadTransportContractsVersion)
	}
	if report.RoutesProbed != len(nativeNowWave) {
		t.Errorf("routes probed = %d, want %d (the full Native-now wave)", report.RoutesProbed, len(nativeNowWave))
	}
	for _, finding := range report.Findings {
		t.Errorf("parity finding [%s %s]: %s: %s", finding.Family, finding.Route, finding.Check, finding.Detail)
	}
}
