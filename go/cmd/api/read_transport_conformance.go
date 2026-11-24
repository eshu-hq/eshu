// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"
)

// ReadTransportContractsVersion pins the core-owned read-contract set this
// transport spec conforms to (Refs eshu-hq/eshu#9669, part of #6707). It
// must equal querycontract.CoreReadContractsVersion; the pin test enforces
// it, so a core port break cannot drift the satellite silently. Spec-only:
// no serving-path behavior change.
const ReadTransportContractsVersion = "v0"

// ErrTransportNotImplemented is returned by RunReadTransportConformance
// until the satellite repo implements the probe body. The red conformance
// test asserts a clean report, so it fails with this error first (TDD red);
// that failure is the recorded expectation, not a defect.
var ErrTransportNotImplemented = errors.New("read transport probe not implemented (satellite TBD)")

// nativeNowRoute is one frozen Native-now wave entry: the exact cross-repo
// surface the satellite must implement first for CoreReadContracts v0.
// Method+Path must match CUTOVER_OWNERSHIP.md; OpenAPIFragment names the
// parity fragment (or the mux-level marker for non-contract routes).
type nativeNowRoute struct {
	Method          string
	Path            string
	Family          string
	OpenAPIFragment string
}

// nativeNowWave is the frozen first conformance target: 18 routes across
// the 8 Native-now families (APIRouter, Capabilities, CLISupport, Ask,
// ServiceIntelHTTP, AuthProviders, SignInPolicyReads,
// AdminProviderConfigReads). TestNativeNowWaveFrozen pins it entry by
// entry; a wave change moves this table, the spec doc, and the pin together.
var nativeNowWave = []nativeNowRoute{
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

// ConformanceConfig tells the probe where the satellite under test lives.
// BaseURL is the satellite deployment root (e.g. http://127.0.0.1:8080);
// Token is a Bearer [REDACTED] admitted by the satellite's auth wrapper.
type ConformanceConfig struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
	Timeout    time.Duration
}

// ConformanceFinding is one failed parity check.
type ConformanceFinding struct {
	Route  string
	Family string
	Check  string
	Detail string
}

// ConformanceReport is the probe outcome the red test asserts clean.
type ConformanceReport struct {
	ContractsVersion string
	RoutesProbed     int
	Findings         []ConformanceFinding
}

// Clean reports whether the satellite satisfied every probe.
func (r *ConformanceReport) Clean() bool {
	return r != nil && len(r.Findings) == 0
}

// RunReadTransportConformance probes the satellite deployment for v0
// parity: envelope/plain negotiation, pagination bounds, capability
// gating, OpenAPI-fragment parity, auth placement, and error envelopes
// per READ_TRANSPORT_SPEC.md. Skeleton only: it validates the config and
// returns ErrTransportNotImplemented listing the unimplemented probe
// steps. The satellite implementer replaces the stub return with the
// per-route probe loop over nativeNowWave, keeping the signature and
// the report shape so the red test turns green unedited.
func RunReadTransportConformance(ctx context.Context, cfg ConformanceConfig) (*ConformanceReport, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if strings.TrimSpace(cfg.BaseURL) == "" {
		return nil, fmt.Errorf("%w: ConformanceConfig.BaseURL is empty (satellite TBD #2)",
			ErrTransportNotImplemented)
	}
	pending := make([]string, 0, len(nativeNowWave))
	for _, route := range nativeNowWave {
		pending = append(pending, route.Method+" "+route.Path)
	}
	sort.Strings(pending)
	return nil, fmt.Errorf("%w: %d Native-now routes pending: %s",
		ErrTransportNotImplemented, len(pending), strings.Join(pending, ", "))
}
