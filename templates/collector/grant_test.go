// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/sdk/go/collector/conformance"
)

// GrantRequest is the template-side shape of a #6709 first-party producer
// grant request. The core issues the signed grant; the collector stores only
// this metadata and never mints canonical identity itself. Revoked grants
// never match and a zero expiry is already expired, mirroring the host.
type GrantRequest struct {
	ProducerID     string    `json:"producer_id"`
	Version        string    `json:"version"`
	Kind           string    `json:"kind"`
	SchemaVersions []string  `json:"schema_versions"`
	Scope          string    `json:"scope"`
	ExpiresAt      time.Time `json:"expires_at"`
	Revoked        bool      `json:"revoked"`
}

// TestProducerGrantContract proves the template's grant wiring follows the
// merged #6709 contract: namespaced kinds need no grant, core-owned kinds
// require a live grant naming this producer/version/kind/scope, and anything
// outside the grant fails closed.
func TestProducerGrantContract(t *testing.T) {
	t.Parallel()

	grant := GrantRequest{
		ProducerID:     ComponentID,
		Version:        "0.1.0",
		Kind:           "aws_resource",
		SchemaVersions: []string{"1.0.0"},
		Scope:          CollectorKind,
		ExpiresAt:      time.Now().Add(time.Hour),
	}
	if !authorizesEmission(grant, ComponentID, "0.1.0", "aws_resource", "1.0.0", CollectorKind, time.Now()) {
		t.Fatal("live grant must authorize its kind/schema/scope")
	}
	// Namespaced template kinds need no grant.
	if !authorizesEmission(grant, ComponentID, "0.1.0", FactKindRecord, "1.0.0", CollectorKind, time.Now()) {
		t.Fatal("namespaced kind must pass without a grant")
	}
	// Wrong scope, wrong schema, and wrong producer fail closed.
	if authorizesEmission(grant, ComponentID, "0.1.0", "aws_resource", "1.0.0", "other", time.Now()) {
		t.Fatal("out-of-scope emission must fail closed")
	}
	if authorizesEmission(grant, ComponentID, "0.1.0", "aws_resource", "2.0.0", CollectorKind, time.Now()) {
		t.Fatal("ungranted schema version must fail closed")
	}
	if authorizesEmission(grant, "dev.eshu.other.collector", "0.1.0", "aws_resource", "1.0.0", CollectorKind, time.Now()) {
		t.Fatal("foreign producer must fail closed")
	}
	revoked := grant
	revoked.Revoked = true
	if authorizesEmission(revoked, ComponentID, "0.1.0", "aws_resource", "1.0.0", CollectorKind, time.Now()) {
		t.Fatal("revoked grant must fail closed")
	}
	expired := grant
	expired.ExpiresAt = time.Now().Add(-time.Minute)
	if authorizesEmission(expired, ComponentID, "0.1.0", "aws_resource", "1.0.0", CollectorKind, time.Now()) {
		t.Fatal("expired grant must fail closed")
	}
	// Revocation and expiry constrain core-owned kinds only: a revoked grant
	// object must not block namespaced-kind emission.
	if !authorizesEmission(revoked, ComponentID, "0.1.0", FactKindRecord, "1.0.0", CollectorKind, time.Now()) {
		t.Fatal("revoked grant must not block namespaced kinds")
	}
	// Conflicting producers never share one scope+generation: the template
	// emits under its own claim scope only.
	result := mustCollect(t, "complete.json", testClaim(), testObservedAt(), "")
	for _, fact := range result.Facts {
		if fact.SourceRef.ScopeID != testClaim().Scope.ID {
			t.Fatalf("fact scope %q escapes the claim scope", fact.SourceRef.ScopeID)
		}
	}
}

// TestGrantExampleParses proves the checked-in grant example matches the
// request shape operators submit for issuance.
func TestGrantExampleParses(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("grant.example.json")
	if err != nil {
		t.Fatalf("os.ReadFile(grant.example.json) error = %v", err)
	}
	var grant GrantRequest
	if err := json.Unmarshal(raw, &grant); err != nil {
		t.Fatalf("json.Unmarshal(grant.example.json) error = %v", err)
	}
	if strings.TrimSpace(grant.ProducerID) == "" || strings.TrimSpace(grant.Scope) == "" {
		t.Fatalf("grant example missing producer or scope: %+v", grant)
	}
}

// TestGrantExampleScopeIsCollectorKind proves the example grant is scoped the
// way the host matches it. The host compares a grant's scope against the
// manifest's spec.collectorKinds, not against a claim scope ID such as the
// activation config's host.scope.id, so an example scoped by ID would be
// issued and then never match at install or activation.
func TestGrantExampleScopeIsCollectorKind(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("grant.example.json")
	if err != nil {
		t.Fatalf("os.ReadFile(grant.example.json) error = %v", err)
	}
	var grant GrantRequest
	if err := json.Unmarshal(raw, &grant); err != nil {
		t.Fatalf("json.Unmarshal(grant.example.json) error = %v", err)
	}
	manifest := loadManifest(t)
	if !slices.Contains(manifest.Spec.CollectorKinds, grant.Scope) {
		t.Fatalf("grant scope %q is not a collector kind in the manifest %v", grant.Scope, manifest.Spec.CollectorKinds)
	}
	if grant.Scope != CollectorKind {
		t.Fatalf("grant scope %q, want the collector kind %q", grant.Scope, CollectorKind)
	}
}

// TestManifestEmissionsAreAuthorized proves every kind the shipped manifest
// declares is either namespaced in the template's own namespace or covered by
// a grant in grant.example.json. The check derives its verdict from the
// manifest alone: the template cannot import the core registry, so it never
// guesses which kinds are core-owned.
func TestManifestEmissionsAreAuthorized(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("grant.example.json")
	if err != nil {
		t.Fatalf("os.ReadFile(grant.example.json) error = %v", err)
	}
	var grant GrantRequest
	if err := json.Unmarshal(raw, &grant); err != nil {
		t.Fatalf("json.Unmarshal(grant.example.json) error = %v", err)
	}
	problems := uncoveredEmissions(loadManifest(t), []GrantRequest{grant}, time.Now())
	if len(problems) != 0 {
		t.Fatalf("shipped manifest has unauthorized emissions: %v", problems)
	}
}

// TestUncoveredEmissionsRejectsDottedCoreLookingKind is the regression for the
// original defect: a dotted heuristic treated incident.record as namespaced and
// let it through without a grant. The new check treats a kind as exempt only
// when it sits in the template's own namespace.
func TestUncoveredEmissionsRejectsDottedCoreLookingKind(t *testing.T) {
	t.Parallel()

	now := time.Now()
	manifest := loadManifest(t)
	manifest.Spec.EmittedFacts = append(manifest.Spec.EmittedFacts, conformance.FactFamily{
		Kind:             "incident.record",
		SchemaVersions:   []string{"1.0.0", "1.1.0"},
		SourceConfidence: []string{"observed"},
	})
	grantFor := func(mutate func(*GrantRequest)) GrantRequest {
		g := GrantRequest{
			ProducerID:     ComponentID,
			Version:        manifest.Metadata.Version,
			Kind:           "incident.record",
			SchemaVersions: []string{"1.0.0", "1.1.0"},
			Scope:          CollectorKind,
			ExpiresAt:      now.Add(time.Hour),
		}
		mutate(&g)
		return g
	}
	cases := []struct {
		name    string
		grants  []GrantRequest
		covered bool
	}{
		{"no grant", nil, false},
		{"grant scoped to the collector kind", []GrantRequest{grantFor(func(*GrantRequest) {})}, true},
		{"v-prefixed grant version", []GrantRequest{grantFor(func(g *GrantRequest) { g.Version = "v" + g.Version })}, true},
		{"scope not in collectorKinds", []GrantRequest{grantFor(func(g *GrantRequest) { g.Scope = "other" })}, false},
		{"missing one declared schema version", []GrantRequest{grantFor(func(g *GrantRequest) { g.SchemaVersions = []string{"1.0.0"} })}, false},
		{"revoked grant", []GrantRequest{grantFor(func(g *GrantRequest) { g.Revoked = true })}, false},
		{"expired grant", []GrantRequest{grantFor(func(g *GrantRequest) { g.ExpiresAt = now.Add(-time.Minute) })}, false},
		{"zero expiry", []GrantRequest{grantFor(func(g *GrantRequest) { g.ExpiresAt = time.Time{} })}, false},
		{"foreign producer", []GrantRequest{grantFor(func(g *GrantRequest) { g.ProducerID = "dev.eshu.other.collector" })}, false},
		{"other kind", []GrantRequest{grantFor(func(g *GrantRequest) { g.Kind = "change.record" })}, false},
		{"versions split across two grants", []GrantRequest{
			grantFor(func(g *GrantRequest) { g.SchemaVersions = []string{"1.0.0"} }),
			grantFor(func(g *GrantRequest) { g.SchemaVersions = []string{"1.1.0"} }),
		}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			problems := uncoveredEmissions(manifest, tc.grants, now)
			if tc.covered && len(problems) != 0 {
				t.Fatalf("uncoveredEmissions() = %v, want none", problems)
			}
			if !tc.covered && !slices.ContainsFunc(problems, func(p string) bool { return strings.Contains(p, "incident.record") }) {
				t.Fatalf("uncoveredEmissions() = %v, want incident.record reported", problems)
			}
		})
	}
}

// TestUncoveredEmissionsNamespaceBoundary proves the namespace exemption is the
// template's own namespace only: a revoked grant never blocks an own-namespace
// kind, a foreign dotted namespace is not exempt, and the bare namespace
// without a following dot does not count.
func TestUncoveredEmissionsNamespaceBoundary(t *testing.T) {
	t.Parallel()

	now := time.Now()
	revoked := GrantRequest{ProducerID: ComponentID, Version: "0.1.0", Kind: "x", Revoked: true}
	own := loadManifest(t)
	if problems := uncoveredEmissions(own, []GrantRequest{revoked}, now); len(problems) != 0 {
		t.Fatalf("own-namespace kinds with a revoked unrelated grant: %v, want none", problems)
	}
	for _, kind := range []string{"dev.eshu.other.collector.record", ComponentID, ComponentID + "x.record"} {
		manifest := loadManifest(t)
		manifest.Spec.EmittedFacts = []conformance.FactFamily{{Kind: kind, SchemaVersions: []string{"1.0.0"}}}
		if problems := uncoveredEmissions(manifest, nil, now); len(problems) == 0 {
			t.Fatalf("kind %q outside the template namespace was exempt without a grant", kind)
		}
	}
}

// authorizesEmission mirrors go/internal/component.AuthorizesEmission without
// importing it: core-owned (unprefixed) kinds require the grant, namespaced
// kinds pass. Core remains the enforcement owner; this keeps the template's
// pre-submit check identical to the host's verdict.
func authorizesEmission(grant GrantRequest, producerID, version, kind, schemaVersion, scope string, now time.Time) bool {
	// Namespaced kinds need no grant, so they bypass before revocation and
	// expiry are consulted: those only constrain core-owned kinds, and a
	// revoked or expired grant object must never block namespaced emission.
	kind = strings.TrimSpace(kind)
	scope = strings.TrimSpace(scope)
	if strings.Contains(kind, ".") {
		return true
	}
	if grant.Revoked {
		return false
	}
	if grant.ExpiresAt.IsZero() || !now.Before(grant.ExpiresAt) {
		return false
	}
	if strings.TrimSpace(grant.ProducerID) != strings.TrimSpace(producerID) ||
		normalizeVersion(grant.Version) != normalizeVersion(version) {
		return false
	}
	if strings.TrimSpace(grant.Kind) != kind || strings.TrimSpace(grant.Scope) != scope {
		return false
	}
	for _, covered := range grant.SchemaVersions {
		if covered == schemaVersion {
			return true
		}
	}
	return false
}

func normalizeVersion(version string) string {
	return strings.TrimPrefix(strings.TrimSpace(version), "v")
}
