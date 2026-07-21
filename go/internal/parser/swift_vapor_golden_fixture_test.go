// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package parser

import (
	"path/filepath"
	"testing"
)

// TestSwiftVaporGoldenFixtureDiscriminatesRouteHandler pins the swift_vapor_app
// golden-corpus fixture's positive-vs-foil discrimination for the #5337
// vapor_route_handler dead-code root: routes.swift has `import Vapor` so its
// `use: healthCheck` handler is rooted, while plain.swift carries the identical
// `use:` shape without `import Vapor`, so its statusReport handler must NOT be
// rooted. This is the parser-tier proof that the golden fixture staged in
// scripts/verify-golden-corpus-gate.sh actually exercises the detector; the
// end-to-end proof is the B-7 gate's dead-code query shape scoped to
// swift_vapor_app.
func TestSwiftVaporGoldenFixtureDiscriminatesRouteHandler(t *testing.T) {
	t.Parallel()

	repoRoot := repoFixturePath("ecosystems", "swift_vapor_app")
	engine, err := DefaultEngine()
	if err != nil {
		t.Fatalf("DefaultEngine() error = %v, want nil", err)
	}

	routesPath := filepath.Join(repoRoot, "Sources", "App", "routes.swift")
	routesPayload, err := engine.ParsePath(repoRoot, routesPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", routesPath, err)
	}
	assertParserStringSliceContains(
		t,
		assertFunctionByName(t, routesPayload, "healthCheck"),
		"dead_code_root_kinds",
		"swift.vapor_route_handler",
	)

	plainPath := filepath.Join(repoRoot, "Sources", "App", "plain.swift")
	plainPayload, err := engine.ParsePath(repoRoot, plainPath, false, Options{})
	if err != nil {
		t.Fatalf("ParsePath(%q) error = %v, want nil", plainPath, err)
	}
	if foil := assertFunctionByName(t, plainPayload, "statusReport"); foil["dead_code_root_kinds"] != nil {
		t.Fatalf("statusReport dead_code_root_kinds = %#v, want nil (no import Vapor)", foil["dead_code_root_kinds"])
	}
}
