// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package collector

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
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
		Scope:          "component:template-primary",
		ExpiresAt:      time.Now().Add(time.Hour),
	}
	if !authorizesEmission(grant, ComponentID, "0.1.0", "aws_resource", "1.0.0", "component:template-primary", time.Now()) {
		t.Fatal("live grant must authorize its kind/schema/scope")
	}
	// Namespaced template kinds need no grant.
	if !authorizesEmission(grant, ComponentID, "0.1.0", FactKindRecord, "1.0.0", "component:template-primary", time.Now()) {
		t.Fatal("namespaced kind must pass without a grant")
	}
	// Wrong scope, wrong schema, and wrong producer fail closed.
	if authorizesEmission(grant, ComponentID, "0.1.0", "aws_resource", "1.0.0", "component:other", time.Now()) {
		t.Fatal("out-of-scope emission must fail closed")
	}
	if authorizesEmission(grant, ComponentID, "0.1.0", "aws_resource", "2.0.0", "component:template-primary", time.Now()) {
		t.Fatal("ungranted schema version must fail closed")
	}
	if authorizesEmission(grant, "dev.eshu.other.collector", "0.1.0", "aws_resource", "1.0.0", "component:template-primary", time.Now()) {
		t.Fatal("foreign producer must fail closed")
	}
	revoked := grant
	revoked.Revoked = true
	if authorizesEmission(revoked, ComponentID, "0.1.0", "aws_resource", "1.0.0", "component:template-primary", time.Now()) {
		t.Fatal("revoked grant must fail closed")
	}
	expired := grant
	expired.ExpiresAt = time.Now().Add(-time.Minute)
	if authorizesEmission(expired, ComponentID, "0.1.0", "aws_resource", "1.0.0", "component:template-primary", time.Now()) {
		t.Fatal("expired grant must fail closed")
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

// authorizesEmission mirrors go/internal/component.AuthorizesEmission without
// importing it: core-owned (unprefixed) kinds require the grant, namespaced
// kinds pass. Core remains the enforcement owner; this keeps the template's
// pre-submit check identical to the host's verdict.
func authorizesEmission(grant GrantRequest, producerID, version, kind, schemaVersion, scope string, now time.Time) bool {
	if grant.Revoked {
		return false
	}
	if grant.ExpiresAt.IsZero() || !now.Before(grant.ExpiresAt) {
		return false
	}
	kind = strings.TrimSpace(kind)
	scope = strings.TrimSpace(scope)
	if strings.Contains(kind, ".") {
		return true
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
