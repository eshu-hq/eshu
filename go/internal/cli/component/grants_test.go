// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"bytes"
	"strings"
	"testing"
	"time"

	componentcore "github.com/eshu-hq/eshu/go/internal/component"
)

var grantTestClock = time.Date(2026, time.September, 17, 12, 0, 0, 0, time.UTC)

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
