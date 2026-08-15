// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"strings"
	"testing"

	componentcore "github.com/eshu-hq/eshu/go/internal/component"
)

// TestNewCollectorSpecDerivesTemplateValues pins the derivation every
// scaffold template renders from. The expected values mirror the assertions
// the end-to-end `component init collector` test in go/cmd/eshu makes
// against the generated files, so a drift here fails before a scaffold is
// ever written.
func TestNewCollectorSpecDerivesTemplateValues(t *testing.T) {
	t.Parallel()

	spec, err := newCollectorSpec(
		"dev.example.collector.demo",
		"example",
		"dev.example.demo_observation",
		"/tmp/never-created/demo-component",
	)
	if err != nil {
		t.Fatalf("newCollectorSpec() error = %v, want nil", err)
	}
	for field, got := range map[string]struct {
		got  string
		want string
	}{
		"CollectorKind":   {spec.CollectorKind, "demo"},
		"PackageName":     {spec.PackageName, "democollector"},
		"ModulePath":      {spec.ModulePath, "example.com/dev-example-collector-demo"},
		"MetricsPrefix":   {spec.MetricsPrefix, "eshu_dp_dev_example_demo_observation_"},
		"ConsumerPhase":   {spec.ConsumerPhase, "dev_example_demo_observation:provenance_recorded"},
		"FactConstSuffix": {spec.FactConstSuffix, "DemoObservation"},
		"Name":            {spec.Name, "Demo Observation collector extension"},
	} {
		if got.got != got.want {
			t.Fatalf("%s = %q, want %q", field, got.got, got.want)
		}
	}
	if !strings.HasSuffix(spec.OutputDir, "demo-component") {
		t.Fatalf("OutputDir = %q, want absolute path ending in demo-component", spec.OutputDir)
	}
	if !strings.HasPrefix(spec.ImageRef, "ghcr.io/example/dev-example-collector-demo@sha256:") {
		t.Fatalf("ImageRef = %q, want placeholder-digest ghcr ref", spec.ImageRef)
	}
}

func TestNewCollectorSpecRejectsBadInputs(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		id        string
		publisher string
		factKind  string
		wantIn    string
	}{
		{name: "missing id", id: "", publisher: "example", factKind: "dev.example.demo", wantIn: "--id is required"},
		{name: "unsafe id", id: "Dev.Example/collector", publisher: "example", factKind: "dev.example.demo", wantIn: "must use lowercase"},
		{name: "missing publisher", id: "dev.example.collector.demo", publisher: "", factKind: "dev.example.demo", wantIn: "--publisher is required"},
		{name: "non-namespaced fact kind", id: "dev.example.collector.demo", publisher: "example", factKind: "demo_observation", wantIn: "must be namespaced"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := newCollectorSpec(tc.id, tc.publisher, tc.factKind, "")
			if err == nil {
				t.Fatalf("newCollectorSpec() error = nil, want %q", tc.wantIn)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Fatalf("error = %q, want it to contain %q", err, tc.wantIn)
			}
			if code := componentcore.ErrorCodeOf(err); code != componentcore.ErrorCodeInvalidInput {
				t.Fatalf("error code = %q, want %q", code, componentcore.ErrorCodeInvalidInput)
			}
		})
	}
}
