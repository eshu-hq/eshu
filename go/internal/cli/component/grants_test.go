// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	componentcore "github.com/eshu-hq/eshu/go/internal/component"
)

var grantTestClock = time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)

// grantedCoreKindManifestYAML is a minimal valid manifest declaring the
// core-owned aws_resource kind in scope aws for producer alpha.
func grantedCoreKindManifestYAML() string {
	return `apiVersion: eshu.dev/v1alpha1
kind: ComponentPackage
metadata:
  id: dev.example.collector.alpha
  name: Alpha collector
  publisher: eshu-hq
  version: 0.1.0
spec:
  compatibleCore: ">=0.0.5 <0.1.0"
  componentType: collector
  collectorKinds:
    - aws
  runtime:
    sdkProtocol: collector-sdk/v1alpha1
    adapter: oci
  artifacts:
    - platform: linux/amd64
      image: ghcr.io/eshu-hq/components/alpha-collector@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
  emittedFacts:
    - kind: aws_resource
      schemaVersions:
        - 1.0.0
      sourceConfidence:
        - reported
  consumerContracts:
    reducer:
      phases:
        - cloud_resource_uid:canonical_nodes_committed
  telemetry:
    metricsPrefix: eshu_dp_alpha_
`
}

func grantTestPolicy(producer string) componentcore.Policy {
	return componentcore.Policy{
		Mode:              componentcore.TrustModeAllowlist,
		AllowedIDs:        []string{producer},
		AllowedPublishers: []string{"eshu-hq"},
		CoreVersion:       "0.0.9",
	}
}

func writeGrantTestManifest(t *testing.T, body string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "component.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("os.WriteFile() error = %v, want nil", err)
	}
	return path
}

// TestRunGrantRecordsCoreKindGrant proves issuance: a fully specified grant
// for a core-owned kind persists in the registry with the computed expiry and
// renders the grant line.
func TestRunGrantRecordsCoreKindGrant(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	out := &bytes.Buffer{}
	err := RunGrant(
		out, false, home,
		"dev.example.collector.alpha", "0.1.0",
		"aws_resource", []string{"1.0.0"}, "aws",
		time.Hour, grantTestClock,
	)
	if err != nil {
		t.Fatalf("RunGrant() error = %v, want nil", err)
	}
	want := "granted dev.example.collector.alpha@0.1.0 kind aws_resource scope aws expires 2026-09-17T13:00:00Z\n"
	if got := out.String(); got != want {
		t.Fatalf("RunGrant() output = %q, want %q", got, want)
	}
	grants, err := componentcore.NewRegistry(home).ProducerGrants()
	if err != nil {
		t.Fatalf("ProducerGrants() error = %v, want nil", err)
	}
	if len(grants) != 1 {
		t.Fatalf("ProducerGrants() count = %d, want 1", len(grants))
	}
	stored := grants[0]
	if stored.ProducerID != "dev.example.collector.alpha" || stored.Version != "0.1.0" ||
		stored.Kind != "aws_resource" || stored.Scope != "aws" || stored.Revoked {
		t.Fatalf("ProducerGrants()[0] = %+v, want the issued live grant", stored)
	}
	if !stored.ExpiresAt.Equal(grantTestClock.Add(time.Hour)) {
		t.Fatalf("ProducerGrants()[0].ExpiresAt = %v, want %v", stored.ExpiresAt, grantTestClock.Add(time.Hour))
	}
}

// TestRunGrantRejectsNonCoreKind locks issuance scope: namespaced kinds need
// no grant, so recording one is an operator error, not a stored no-op.
func TestRunGrantRejectsNonCoreKind(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	out := &bytes.Buffer{}
	err := RunGrant(
		out, false, home,
		"dev.example.collector.alpha", "0.1.0",
		"dev.example.demo_observation", []string{"1.0.0"}, "demo",
		time.Hour, grantTestClock,
	)
	if err == nil {
		t.Fatal("RunGrant() error = nil, want invalid-input for a non-core kind")
	}
	if got := componentcore.ErrorCodeOf(err); got != componentcore.ErrorCodeInvalidInput {
		t.Fatalf("RunGrant() code = %q, want %q", got, componentcore.ErrorCodeInvalidInput)
	}
	grants, rerr := componentcore.NewRegistry(home).ProducerGrants()
	if rerr != nil {
		t.Fatalf("ProducerGrants() error = %v, want nil", rerr)
	}
	if len(grants) != 0 {
		t.Fatalf("ProducerGrants() count = %d, want 0 after rejected issuance", len(grants))
	}
}

// TestRunGrantRejectsIncompleteIssuance pins every required issuance field:
// producer, semver version, schema versions, scope, and a positive lifetime.
// Each row must fail before any registry write.
func TestRunGrantRejectsIncompleteIssuance(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		producer string
		version  string
		schemas  []string
		scope    string
		lifetime time.Duration
	}{
		{name: "empty producer", producer: "", version: "0.1.0", schemas: []string{"1.0.0"}, scope: "aws", lifetime: time.Hour},
		{name: "non-semver version", producer: "dev.example.collector.alpha", version: "next", schemas: []string{"1.0.0"}, scope: "aws", lifetime: time.Hour},
		{name: "no schema versions", producer: "dev.example.collector.alpha", version: "0.1.0", schemas: nil, scope: "aws", lifetime: time.Hour},
		{name: "blank schema version", producer: "dev.example.collector.alpha", version: "0.1.0", schemas: []string{"  "}, scope: "aws", lifetime: time.Hour},
		{name: "empty scope", producer: "dev.example.collector.alpha", version: "0.1.0", schemas: []string{"1.0.0"}, scope: "", lifetime: time.Hour},
		{name: "zero lifetime", producer: "dev.example.collector.alpha", version: "0.1.0", schemas: []string{"1.0.0"}, scope: "aws", lifetime: 0},
		{name: "negative lifetime", producer: "dev.example.collector.alpha", version: "0.1.0", schemas: []string{"1.0.0"}, scope: "aws", lifetime: -time.Hour},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			home := t.TempDir()
			out := &bytes.Buffer{}
			err := RunGrant(
				out, false, home,
				tc.producer, tc.version,
				"aws_resource", tc.schemas, tc.scope,
				tc.lifetime, grantTestClock,
			)
			if err == nil {
				t.Fatal("RunGrant() error = nil, want invalid-input")
			}
			if got := componentcore.ErrorCodeOf(err); got != componentcore.ErrorCodeInvalidInput {
				t.Fatalf("RunGrant() code = %q, want %q", got, componentcore.ErrorCodeInvalidInput)
			}
			grants, rerr := componentcore.NewRegistry(home).ProducerGrants()
			if rerr != nil {
				t.Fatalf("ProducerGrants() error = %v, want nil", rerr)
			}
			if len(grants) != 0 {
				t.Fatalf("ProducerGrants() count = %d, want 0 after rejected issuance", len(grants))
			}
		})
	}
}

// TestRunGrantJSONPinsPayloadShape proves the --json contract: the grant
// block carries the stored grant under the pinned schema version.
func TestRunGrantJSONPinsPayloadShape(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	out := &bytes.Buffer{}
	err := RunGrant(
		out, true, home,
		"dev.example.collector.alpha", "0.1.0",
		"aws_resource", []string{"1.0.0"}, "aws",
		time.Hour, grantTestClock,
	)
	if err != nil {
		t.Fatalf("RunGrant() error = %v, want nil", err)
	}
	var payload map[string]any
	if jerr := json.Unmarshal(out.Bytes(), &payload); jerr != nil {
		t.Fatalf("RunGrant() output is not JSON: %v", jerr)
	}
	if got := payload["schema_version"]; got != "eshu.component.cli.v1" {
		t.Fatalf("schema_version = %v, want eshu.component.cli.v1", got)
	}
	if got := payload["command"]; got != "grant" {
		t.Fatalf("command = %v, want grant", got)
	}
	if got := payload["status"]; got != "granted" {
		t.Fatalf("status = %v, want granted", got)
	}
	grant, ok := payload["grant"].(map[string]any)
	if !ok {
		t.Fatalf("grant block = %v, want object", payload["grant"])
	}
	for key, want := range map[string]any{
		"producer_id": "dev.example.collector.alpha",
		"version":     "0.1.0",
		"kind":        "aws_resource",
		"scope":       "aws",
		"revoked":     false,
	} {
		if got := grant[key]; got != want {
			t.Fatalf("grant.%s = %v, want %v", key, got, want)
		}
	}

	listOut := &bytes.Buffer{}
	if err := RunGrants(listOut, true, home, ""); err != nil {
		t.Fatalf("RunGrants() error = %v, want nil", err)
	}
	var listPayload map[string]any
	if jerr := json.Unmarshal(listOut.Bytes(), &listPayload); jerr != nil {
		t.Fatalf("RunGrants() output is not JSON: %v", jerr)
	}
	listed, ok := listPayload["grants"].([]any)
	if !ok || len(listed) != 1 {
		t.Fatalf("grants = %v, want one recorded grant", listPayload["grants"])
	}
}

// TestRunRevokeGrantMarksRevoked proves the rollback half of issuance: after
// revocation the durable grant stays recorded but reads revoked, so the next
// emission fails closed.
func TestRunRevokeGrantMarksRevoked(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	grantOut := &bytes.Buffer{}
	if err := RunGrant(
		grantOut, false, home,
		"dev.example.collector.alpha", "0.1.0",
		"aws_resource", []string{"1.0.0"}, "aws",
		time.Hour, grantTestClock,
	); err != nil {
		t.Fatalf("RunGrant() error = %v, want nil", err)
	}
	out := &bytes.Buffer{}
	err := RunRevokeGrant(out, false, home, "dev.example.collector.alpha", "0.1.0", "aws_resource", "aws")
	if err != nil {
		t.Fatalf("RunRevokeGrant() error = %v, want nil", err)
	}
	want := "revoked grant dev.example.collector.alpha@0.1.0 kind aws_resource scope aws\n"
	if got := out.String(); got != want {
		t.Fatalf("RunRevokeGrant() output = %q, want %q", got, want)
	}
	grants, rerr := componentcore.NewRegistry(home).ProducerGrants()
	if rerr != nil {
		t.Fatalf("ProducerGrants() error = %v, want nil", rerr)
	}
	if len(grants) != 1 || !grants[0].Revoked {
		t.Fatalf("ProducerGrants() = %+v, want one revoked grant", grants)
	}
}

// TestRunRevokeGrantMissingReportsNotFound proves a typo cannot masquerade
// as a completed revocation: revoking an absent grant fails with the stable
// grant_not_found code.
func TestRunRevokeGrantMissingReportsNotFound(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	out := &bytes.Buffer{}
	err := RunRevokeGrant(out, false, home, "dev.example.collector.ghost", "0.1.0", "aws_resource", "aws")
	if err == nil {
		t.Fatal("RunRevokeGrant() error = nil, want grant_not_found")
	}
	if got := componentcore.ErrorCodeOf(err); got != componentcore.ErrorCodeGrantNotFound {
		t.Fatalf("RunRevokeGrant() code = %q, want %q", got, componentcore.ErrorCodeGrantNotFound)
	}
}

// TestRunGrantsListsDurableGrants proves the audit half of issuance: every
// recorded grant, including revoked ones, is listed with identity, scope,
// covered schemas, expiry, and revocation state, optionally filtered by
// producer.
func TestRunGrantsListsDurableGrants(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	issue := func(producer, kind, scope string, lifetime time.Duration) {
		t.Helper()
		if err := RunGrant(
			&bytes.Buffer{}, false, home,
			producer, "0.1.0", kind, []string{"1.0.0"}, scope,
			lifetime, grantTestClock,
		); err != nil {
			t.Fatalf("RunGrant(%s) error = %v, want nil", producer, err)
		}
	}
	issue("dev.example.collector.alpha", "aws_resource", "aws", time.Hour)
	issue("dev.example.collector.beta", "aws_resource", "aws", 2*time.Hour)
	if err := RunRevokeGrant(
		&bytes.Buffer{}, false, home,
		"dev.example.collector.beta", "0.1.0", "aws_resource", "aws",
	); err != nil {
		t.Fatalf("RunRevokeGrant() error = %v, want nil", err)
	}

	out := &bytes.Buffer{}
	if err := RunGrants(out, false, home, ""); err != nil {
		t.Fatalf("RunGrants() error = %v, want nil", err)
	}
	want := "dev.example.collector.alpha\t0.1.0\taws_resource\tscope=aws\tschemas=1.0.0\texpires=2026-09-17T13:00:00Z\trevoked=false\n" +
		"dev.example.collector.beta\t0.1.0\taws_resource\tscope=aws\tschemas=1.0.0\texpires=2026-09-17T14:00:00Z\trevoked=true\n"
	if got := out.String(); got != want {
		t.Fatalf("RunGrants() output = %q, want %q", got, want)
	}

	filtered := &bytes.Buffer{}
	if err := RunGrants(filtered, false, home, "dev.example.collector.alpha"); err != nil {
		t.Fatalf("RunGrants(producer) error = %v, want nil", err)
	}
	if got := filtered.String(); !strings.HasPrefix(got, "dev.example.collector.alpha\t") || strings.Contains(got, "beta") {
		t.Fatalf("RunGrants(producer) output = %q, want only the alpha grant", got)
	}

	empty := &bytes.Buffer{}
	if err := RunGrants(empty, false, t.TempDir(), ""); err != nil {
		t.Fatalf("RunGrants() on empty home error = %v, want nil", err)
	}
	if got, want := empty.String(), "no producer grants\n"; got != want {
		t.Fatalf("RunGrants() on empty home output = %q, want %q", got, want)
	}
}

// TestRunInstallAdmitsGrantedCoreKindClaim is the P0 regression: the CLI
// install path must honor the registry's durable grants exactly like
// Registry.Install does, so grant-first-then-install works through the
// operator CLI instead of the Go API only.
func TestRunInstallAdmitsGrantedCoreKindClaim(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := RunGrant(
		&bytes.Buffer{}, false, home,
		"dev.example.collector.alpha", "0.1.0",
		"aws_resource", []string{"1.0.0"}, "aws",
		time.Hour, grantTestClock,
	); err != nil {
		t.Fatalf("RunGrant() error = %v, want nil", err)
	}
	manifestPath := writeGrantTestManifest(t, grantedCoreKindManifestYAML())

	out := &bytes.Buffer{}
	err := RunInstall(out, false, false, home, grantTestPolicy("dev.example.collector.alpha"), manifestPath)
	if err != nil {
		t.Fatalf("RunInstall() error = %v, want nil for a granted core-kind manifest", err)
	}
	if got, want := out.String(), "installed dev.example.collector.alpha@0.1.0\n"; got != want {
		t.Fatalf("RunInstall() output = %q, want %q", got, want)
	}
	installed, lerr := componentcore.NewRegistry(home).List()
	if lerr != nil {
		t.Fatalf("List() error = %v, want nil", lerr)
	}
	if len(installed) != 1 || installed[0].ID != "dev.example.collector.alpha" {
		t.Fatalf("List() = %+v, want the granted producer installed", installed)
	}
}

// TestRunInstallStillRejectsUngrantedCoreKindClaim locks the fail-closed
// default through the fixed path: without a recorded grant the CLI install
// still rejects the core-owned declaration.
func TestRunInstallStillRejectsUngrantedCoreKindClaim(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	manifestPath := writeGrantTestManifest(t, grantedCoreKindManifestYAML())

	out := &bytes.Buffer{}
	err := RunInstall(out, false, false, home, grantTestPolicy("dev.example.collector.alpha"), manifestPath)
	if err == nil {
		t.Fatal("RunInstall() error = nil, want rejection without a grant")
	}
	if got := componentcore.ErrorCodeOf(err); got != componentcore.ErrorCodeInvalidManifest {
		t.Fatalf("RunInstall() code = %q, want %q", got, componentcore.ErrorCodeInvalidManifest)
	}
}

// TestRunGrantTrimsIssuanceInputs proves padded operator input cannot store
// a silent inert grant: values are canonicalized before validation and
// storage so the stored grant matches the exact-match admission checks.
func TestRunGrantTrimsIssuanceInputs(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	out := &bytes.Buffer{}
	err := RunGrant(
		out, false, home,
		"  dev.example.collector.alpha  ", "0.1.0",
		"aws_resource", []string{"  1.0.0  "}, "  aws  ",
		time.Hour, grantTestClock,
	)
	if err != nil {
		t.Fatalf("RunGrant() error = %v, want nil for padded-but-valid input", err)
	}
	grants, rerr := componentcore.NewRegistry(home).ProducerGrants()
	if rerr != nil {
		t.Fatalf("ProducerGrants() error = %v, want nil", rerr)
	}
	if len(grants) != 1 {
		t.Fatalf("ProducerGrants() count = %d, want 1", len(grants))
	}
	stored := grants[0]
	if stored.ProducerID != "dev.example.collector.alpha" || stored.Scope != "aws" {
		t.Fatalf("ProducerGrants()[0] = %+v, want trimmed identity and scope", stored)
	}
	if len(stored.SchemaVersions) != 1 || stored.SchemaVersions[0] != "1.0.0" {
		t.Fatalf("ProducerGrants()[0].SchemaVersions = %q, want trimmed [1.0.0]", stored.SchemaVersions)
	}
}

// TestRunRevokeGrantJSONReportsStoredGrant proves revoke-grant --json echoes
// the durable stored grant (schemas, expiry, revoked) instead of a partial
// fabricated block, so issuance and revocation outputs diff cleanly.
func TestRunRevokeGrantJSONReportsStoredGrant(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := RunGrant(
		&bytes.Buffer{}, false, home,
		"dev.example.collector.alpha", "0.1.0",
		"aws_resource", []string{"1.0.0", "1.1.0"}, "aws",
		time.Hour, grantTestClock,
	); err != nil {
		t.Fatalf("RunGrant() error = %v, want nil", err)
	}
	out := &bytes.Buffer{}
	err := RunRevokeGrant(out, true, home, "dev.example.collector.alpha", "0.1.0", "aws_resource", "aws")
	if err != nil {
		t.Fatalf("RunRevokeGrant() error = %v, want nil", err)
	}
	var payload map[string]any
	if jerr := json.Unmarshal(out.Bytes(), &payload); jerr != nil {
		t.Fatalf("RunRevokeGrant() output is not JSON: %v", jerr)
	}
	grant, ok := payload["grant"].(map[string]any)
	if !ok {
		t.Fatalf("grant block = %v, want object", payload["grant"])
	}
	if got := grant["revoked"]; got != true {
		t.Fatalf("grant.revoked = %v, want true", got)
	}
	schemas, ok := grant["schema_versions"].([]any)
	if !ok || len(schemas) != 2 {
		t.Fatalf("grant.schema_versions = %v, want the stored [1.0.0 1.1.0]", grant["schema_versions"])
	}
	expires, ok := grant["expires_at"].(string)
	if !ok || expires == "" {
		t.Fatalf("grant.expires_at = %v, want the stored expiry", grant["expires_at"])
	}
}

// TestRunRevokeGrantTrimsLookup proves revocation canonicalizes like
// issuance: a padded spelling of a stored grant revokes it instead of
// reporting grant_not_found, so scripted rollback cannot mistake whitespace
// for convergence.
func TestRunRevokeGrantTrimsLookup(t *testing.T) {
	t.Parallel()

	home := t.TempDir()
	if err := RunGrant(
		&bytes.Buffer{}, false, home,
		"dev.example.collector.alpha", "0.1.0",
		"aws_resource", []string{"1.0.0"}, "aws",
		time.Hour, grantTestClock,
	); err != nil {
		t.Fatalf("RunGrant() error = %v, want nil", err)
	}
	out := &bytes.Buffer{}
	err := RunRevokeGrant(out, false, home, "  dev.example.collector.alpha  ", "0.1.0", "aws_resource", "  aws  ")
	if err != nil {
		t.Fatalf("RunRevokeGrant() error = %v, want nil for padded spelling of a stored grant", err)
	}
	grants, rerr := componentcore.NewRegistry(home).ProducerGrants()
	if rerr != nil {
		t.Fatalf("ProducerGrants() error = %v, want nil", rerr)
	}
	if len(grants) != 1 || !grants[0].Revoked {
		t.Fatalf("ProducerGrants() = %+v, want one revoked grant", grants)
	}
	filtered := &bytes.Buffer{}
	if err := RunGrants(filtered, false, home, "  dev.example.collector.alpha  "); err != nil {
		t.Fatalf("RunGrants() error = %v, want nil", err)
	}
	if got := filtered.String(); !strings.HasPrefix(got, "dev.example.collector.alpha\t") {
		t.Fatalf("RunGrants(padded producer) output = %q, want the stored grant", got)
	}
}
