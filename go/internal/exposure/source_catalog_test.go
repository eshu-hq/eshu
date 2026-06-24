// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package exposure

import (
	"strings"
	"testing"
)

// TestSourceCatalogIsWellFormed locks the curated taint-source catalog against
// authoring mistakes: an empty kind/display name, a duplicate kind, an empty or
// duplicated root-kind token, a severity outside the closed vocabulary, or a
// missing provenance. The catalog is the security review focus, so a malformed
// entry is a correctness bug.
func TestSourceCatalogIsWellFormed(t *testing.T) {
	t.Parallel()

	seenKinds := make(map[SourceKind]struct{})
	seenRootKinds := make(map[string]SourceKind)
	for _, spec := range SourceCatalog() {
		if strings.TrimSpace(string(spec.Kind)) == "" {
			t.Fatalf("catalog entry has empty kind: %+v", spec)
		}
		if _, dup := seenKinds[spec.Kind]; dup {
			t.Fatalf("duplicate source kind %q", spec.Kind)
		}
		seenKinds[spec.Kind] = struct{}{}

		if strings.TrimSpace(spec.DisplayName) == "" {
			t.Fatalf("source %q has empty display name", spec.Kind)
		}
		if _, ok := allowedSeverities[spec.BaselineSeverity]; !ok {
			t.Fatalf("source %q has severity %q outside the closed vocabulary", spec.Kind, spec.BaselineSeverity)
		}
		if strings.TrimSpace(spec.Provenance) == "" {
			t.Fatalf("source %q must cite its provenance", spec.Kind)
		}
		if len(spec.RootKinds) == 0 {
			t.Fatalf("source %q must classify at least one root-kind token", spec.Kind)
		}
		for _, rk := range spec.RootKinds {
			if strings.TrimSpace(rk) == "" {
				t.Fatalf("source %q has an empty root-kind token", spec.Kind)
			}
			// A root-kind token must map to exactly one source kind so
			// classification is unambiguous.
			if prev, dup := seenRootKinds[rk]; dup {
				t.Fatalf("root-kind %q maps to both %q and %q", rk, prev, spec.Kind)
			}
			seenRootKinds[rk] = spec.Kind
		}
	}
}

// TestClassifySourceRecognizesHandlers proves handler/root tokens classify to the
// right source kind and that non-handler tokens (entrypoints, public API, tests,
// generated code) are NOT sources — the acceptance contract for #2725.
func TestClassifySourceRecognizesHandlers(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		rootKinds []string
		wantKind  SourceKind
		wantOK    bool
	}{
		{name: "go net/http handler", rootKinds: []string{"go.net_http_handler_signature"}, wantKind: SourceHTTPHandler, wantOK: true},
		{name: "python fastapi route", rootKinds: []string{"python.fastapi_route_decorator"}, wantKind: SourceHTTPHandler, wantOK: true},
		{name: "express route", rootKinds: []string{"javascript.express_route_registration"}, wantKind: SourceHTTPHandler, wantOK: true},
		{name: "nextjs app router page", rootKinds: []string{"javascript.nextjs_app_export"}, wantKind: SourceHTTPHandler, wantOK: true},
		{name: "spring request mapping", rootKinds: []string{"java.spring_request_mapping_method"}, wantKind: SourceHTTPHandler, wantOK: true},
		{name: "rails controller action", rootKinds: []string{"ruby.rails_controller_action"}, wantKind: SourceHTTPHandler, wantOK: true},
		{name: "aws lambda handler", rootKinds: []string{"python.aws_lambda_handler"}, wantKind: SourceLambdaHandler, wantOK: true},
		{name: "cobra cli command", rootKinds: []string{"go.cobra_run_signature"}, wantKind: SourceCLICommand, wantOK: true},
		{name: "celery task consumer", rootKinds: []string{"python.celery_task_decorator"}, wantKind: SourceMessageConsumer, wantOK: true},
		// Non-sources: untrusted input does not enter here.
		{name: "go main entrypoint not a source", rootKinds: []string{"go.main"}, wantOK: false},
		{name: "public api not a source", rootKinds: []string{"go.exported_non_internal_package_symbol"}, wantOK: false},
		{name: "junit test not a source", rootKinds: []string{"java.junit_test_method"}, wantOK: false},
		{name: "no root kinds not a source", rootKinds: nil, wantOK: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			spec, ok := ClassifySource(tc.rootKinds)
			if ok != tc.wantOK {
				t.Fatalf("ClassifySource(%v) ok=%v, want %v", tc.rootKinds, ok, tc.wantOK)
			}
			if tc.wantOK && spec.Kind != tc.wantKind {
				t.Fatalf("ClassifySource(%v) kind=%q, want %q", tc.rootKinds, spec.Kind, tc.wantKind)
			}
		})
	}
}

// TestClassifySourcePrefersInternetExposable proves a function carrying both an
// internet-exposable handler token and a non-exposable token classifies as the
// internet-exposable kind (the higher-value source), deterministically.
func TestClassifySourcePrefersInternetExposable(t *testing.T) {
	t.Parallel()

	spec, ok := ClassifySource([]string{"go.cobra_run_signature", "go.net_http_handler_signature"})
	if !ok {
		t.Fatal("expected a classification for mixed handler tokens")
	}
	if !spec.InternetExposable {
		t.Fatalf("mixed tokens classified as non-exposable %q; want the internet-exposable HTTP handler", spec.Kind)
	}
}

// TestRankSourceExposure proves the exposure ranking is honest: a non-network
// source is internal regardless of reachability; an internet-exposable source
// whose endpoint provably reaches 0.0.0.0/0 (the boolean the tracer derives from
// reducer/security_group_reachability.go) is internet_exposed; an
// internet-exposable source without proven reachability is network_reachable, not
// over-claimed as internet-exposed.
func TestRankSourceExposure(t *testing.T) {
	t.Parallel()

	httpSpec, ok := ClassifySource([]string{"go.net_http_handler_signature"})
	if !ok {
		t.Fatal("expected http handler classification")
	}
	cliSpec, ok := ClassifySource([]string{"go.cobra_run_signature"})
	if !ok {
		t.Fatal("expected cli classification")
	}

	if got := RankSourceExposure(httpSpec, true); got != ExposureInternetExposed {
		t.Fatalf("internet-reachable http handler rank=%q, want %q", got, ExposureInternetExposed)
	}
	if got := RankSourceExposure(httpSpec, false); got != ExposureNetworkReachable {
		t.Fatalf("unproven http handler rank=%q, want %q", got, ExposureNetworkReachable)
	}
	if got := RankSourceExposure(cliSpec, true); got != ExposureInternal {
		t.Fatalf("cli command rank=%q, want %q even when reachesInternet=true", got, ExposureInternal)
	}
}

// TestSourceCatalogVersionIsStableAndChangeSensitive mirrors the sink catalog's
// content-hash discipline for the source catalog.
func TestSourceCatalogVersionIsStableAndChangeSensitive(t *testing.T) {
	t.Parallel()

	v1 := SourceCatalogVersion()
	if v1 != SourceCatalogVersion() {
		t.Fatal("SourceCatalogVersion not deterministic")
	}
	if v1 != sourceCatalogVersionGolden {
		t.Fatalf("SourceCatalogVersion = %q, want pinned golden %q; update the golden deliberately when the catalog changes", v1, sourceCatalogVersionGolden)
	}

	mutated := SourceCatalog()
	mutated[0].BaselineSeverity = SeverityLow
	if hashSourceSpecs(mutated) == v1 {
		t.Fatal("hashSourceSpecs insensitive to a severity change")
	}
	extra := append(SourceCatalog(), SourceSpec{Kind: "synthetic", DisplayName: "synthetic", RootKinds: []string{"x"}, BaselineSeverity: SeverityLow, Provenance: "test"})
	if hashSourceSpecs(extra) == v1 {
		t.Fatal("hashSourceSpecs insensitive to an added entry")
	}

	// Reordering specs changes ClassifySource priority, so it MUST change the
	// version even though the set of specs is identical.
	reordered := SourceCatalog()
	if len(reordered) >= 2 {
		reordered[0], reordered[1] = reordered[1], reordered[0]
		if hashSourceSpecs(reordered) == v1 {
			t.Fatal("hashSourceSpecs insensitive to a catalog reorder (which changes ClassifySource priority)")
		}
	}
}

// TestSourceCatalogReturnsIsolatedRootKinds proves SourceCatalog hands back a
// deep copy: mutating a returned spec's RootKinds must not corrupt the
// package-level catalog.
func TestSourceCatalogReturnsIsolatedRootKinds(t *testing.T) {
	t.Parallel()

	first := SourceCatalog()
	for i := range first {
		for j := range first[i].RootKinds {
			first[i].RootKinds[j] = "mutated"
		}
	}
	for _, spec := range SourceCatalog() {
		for _, rk := range spec.RootKinds {
			if rk == "mutated" {
				t.Fatalf("SourceCatalog leaked a mutable root-kind on %q", spec.Kind)
			}
		}
	}
}
