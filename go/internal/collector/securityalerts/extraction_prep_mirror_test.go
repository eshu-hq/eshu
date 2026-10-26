// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package securityalerts

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
)

// Extraction PREP mirror (#8866 AC3, in-tree path): the contracts the
// extracted collector must preserve — admission at schema 1.0.0, stable
// keys, generation binding, and credential stripping — proved against the
// current in-tree constructor, including hostile inputs.
//
// Identity model (locked here): the stable fact key carries the dedup
// identity (provider + alert number + repository) and is stable across
// generations; the fact ID binds scope + generation + stable key, so a
// rebind changes envelope identity without changing dedup identity.
//
// No-Observability-Change: existing eshu_dp_security_alert_* signals cover
// this package; this test adds no metric, span, or pipeline stage.

func extractionPrepContext() EnvelopeContext {
	return EnvelopeContext{
		ScopeID:             "security-alert:github:example-org/example-repo",
		GenerationID:        "security-alert:generation-9",
		CollectorInstanceID: "collector-security-alerts",
		FencingToken:        7,
		ObservedAt:          time.Date(2026, 5, 16, 11, 0, 0, 0, time.UTC),
		SourceURI:           "https://api.github.com/repos/example-org/example-repo/dependabot/alerts",
	}
}

func extractionPrepAlert() GitHubDependabotAlert {
	return GitHubDependabotAlert{
		Number:    42,
		State:     "open",
		HTMLURL:   "https://github.com/example-org/example-repo/security/dependabot/42",
		CreatedAt: "2026-05-20T12:00:00Z",
		UpdatedAt: "2026-05-23T10:15:00Z",
		Dependency: GitHubDependabotDependency{
			ManifestPath: "package-lock.json",
			Scope:        "runtime",
			Package: GitHubDependabotPackage{
				Ecosystem: "npm",
				Name:      "@scope/left-pad",
			},
			Relationship: "direct",
		},
		SecurityAdvisory: GitHubDependabotSecurityAdvisory{
			GHSAID:      "GHSA-abcd-1234",
			CVEID:       "CVE-2026-0001",
			Summary:     "Synthetic advisory",
			Description: "Synthetic advisory description",
			Severity:    "critical",
			CVSS:        GitHubDependabotCVSS{Score: 9.8, Vector: "CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"},
			EPSS:        GitHubDependabotEPSS{Percentage: "0.721", Percentile: "0.981"},
			CWEs:        []GitHubDependabotCWE{{CWEID: "CWE-79", Name: "Cross-site Scripting"}},
		},
		SecurityVulnerability: GitHubDependabotSecurityVulnerability{
			VulnerableVersionRange: "<1.2.3",
			FirstPatchedVersion:    GitHubDependabotVersion{Identifier: "1.2.3"},
			Package:                GitHubDependabotPackage{Ecosystem: "npm", Name: "@scope/left-pad"},
		},
	}
}

func extractionPrepEnvelope(t *testing.T, ctx EnvelopeContext) facts.Envelope {
	t.Helper()

	env, err := NewGitHubDependabotAlertEnvelope(ctx, extractionPrepAlert())
	if err != nil {
		t.Fatalf("NewGitHubDependabotAlertEnvelope() error = %v", err)
	}
	return env
}

func TestExtractionPrepAdmitsAtV1(t *testing.T) {
	t.Parallel()

	kind := facts.SecurityAlertRepositoryAlertFactKind
	version, ok := facts.SecurityAlertSchemaVersion(kind)
	if !ok || version != facts.SecurityAlertSchemaVersionV1 {
		t.Errorf("SecurityAlertSchemaVersion(%q) = %q, %v; want 1.0.0, true", kind, version, ok)
	}
	if err := facts.ValidateSchemaVersion(kind, facts.SecurityAlertSchemaVersionV1); err != nil {
		t.Errorf("ValidateSchemaVersion(%q, 1.0.0) error = %v, want nil", kind, err)
	}

	env := extractionPrepEnvelope(t, extractionPrepContext())
	if env.SchemaVersion != facts.SecurityAlertSchemaVersionV1 {
		t.Errorf("envelope SchemaVersion = %q, want 1.0.0", env.SchemaVersion)
	}
	if env.FactKind != kind {
		t.Errorf("envelope FactKind = %q, want %q", env.FactKind, kind)
	}
	if env.CollectorKind != CollectorKind {
		t.Errorf("envelope CollectorKind = %q, want %q", env.CollectorKind, CollectorKind)
	}
	if env.SourceConfidence != facts.SourceConfidenceReported {
		t.Errorf("envelope SourceConfidence = %q, want reported", env.SourceConfidence)
	}
	if err := facts.ValidateSchemaVersion(kind, "9.9.9"); err == nil {
		t.Error("ValidateSchemaVersion(security_alert.repository_alert, 9.9.9) = nil, want incompatible-schema rejection")
	}
}

func TestExtractionPrepDeterminism(t *testing.T) {
	t.Parallel()

	first := extractionPrepEnvelope(t, extractionPrepContext())
	second := extractionPrepEnvelope(t, extractionPrepContext())
	if first.StableFactKey != second.StableFactKey {
		t.Errorf("StableFactKey not deterministic: %q vs %q", first.StableFactKey, second.StableFactKey)
	}
	if first.FactID != second.FactID {
		t.Errorf("FactID not deterministic: %q vs %q", first.FactID, second.FactID)
	}
}

func TestExtractionPrepGenerationRebind(t *testing.T) {
	t.Parallel()

	first := extractionPrepEnvelope(t, extractionPrepContext())
	rebound := extractionPrepContext()
	rebound.GenerationID = "security-alert:generation-10"
	rebound.FencingToken = 8
	second := extractionPrepEnvelope(t, rebound)

	// Dedup identity survives the rebind: the same provider alert in a new
	// generation keeps its stable key.
	if first.StableFactKey != second.StableFactKey {
		t.Errorf("StableFactKey changed across generations: %q vs %q; dedup identity must survive rebind", first.StableFactKey, second.StableFactKey)
	}
	// Envelope identity binds the generation: a rebind must change the
	// fact ID, never silently reuse the prior generation's identity.
	if first.FactID == second.FactID {
		t.Error("FactID identical across generations; generation must rebind envelope identity")
	}
	if second.GenerationID != rebound.GenerationID || second.FencingToken != 8 {
		t.Errorf("generation binding lost: %+v", second)
	}
	// Provider identity survives the rebind.
	if second.SourceRef.SourceSystem != CollectorKind {
		t.Errorf("SourceSystem = %q, want %q", second.SourceRef.SourceSystem, CollectorKind)
	}
}

func TestExtractionPrepURLSanitizationSentinel(t *testing.T) {
	t.Parallel()

	const sentinel = "PREP-SENTINEL-8866"
	ctx := extractionPrepContext()
	ctx.SourceURI = "https://api.github.com/repos/example-org/example-repo/dependabot/alerts?token=" + sentinel

	hostile := extractionPrepAlert()
	hostile.HTMLURL = "https://svc:" + sentinel + "@github.com/example-org/example-repo/security/dependabot/42?access_token=" + sentinel + "&plain=1"
	env, err := NewGitHubDependabotAlertEnvelope(ctx, hostile)
	if err != nil {
		t.Fatalf("NewGitHubDependabotAlertEnvelope(hostile URL) error = %v, want nil", err)
	}
	raw, err := json.Marshal(env)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(raw), sentinel) {
		t.Errorf("credential sentinel reached persisted envelope %q", env.FactKind)
	}
	// Benign parameters survive sanitization; only credentials are stripped.
	if got := env.Payload["source_url"]; got != "https://github.com/example-org/example-repo/security/dependabot/42?plain=1" {
		t.Errorf("source_url = %v, want benign plain parameter preserved", got)
	}
	if got := env.SourceRef.SourceURI; got != "https://api.github.com/repos/example-org/example-repo/dependabot/alerts" {
		t.Errorf("SourceRef.SourceURI = %q, want token parameter stripped", got)
	}
	// Positive control: benign provider identity survives so the absence
	// above is meaningful.
	benign := extractionPrepEnvelope(t, extractionPrepContext())
	benignRaw, _ := json.Marshal(benign)
	if !strings.Contains(string(benignRaw), "GHSA-abcd-1234") {
		t.Error("positive control: benign advisory identity missing from persisted envelope")
	}
}

func TestExtractionPrepHostileInputsFailClosed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*EnvelopeContext)
	}{
		{"blank scope", func(c *EnvelopeContext) { c.ScopeID = "  " }},
		{"blank generation", func(c *EnvelopeContext) { c.GenerationID = "" }},
		{"blank collector instance", func(c *EnvelopeContext) { c.CollectorInstanceID = "" }},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			ctx := extractionPrepContext()
			tt.mutate(&ctx)
			if _, err := NewGitHubDependabotAlertEnvelope(ctx, extractionPrepAlert()); err == nil {
				t.Errorf("NewGitHubDependabotAlertEnvelope() = nil, want fail-closed error for %s", tt.name)
			}
		})
	}
	// A non-positive alert number carries no provider identity: fail closed.
	ctx := extractionPrepContext()
	badNumber := extractionPrepAlert()
	badNumber.Number = 0
	if _, err := NewGitHubDependabotAlertEnvelope(ctx, badNumber); err == nil {
		t.Error("NewGitHubDependabotAlertEnvelope(number 0) = nil, want error")
	}
	// An unsupported ecosystem cannot be normalized to a package identity.
	badEcosystem := extractionPrepAlert()
	badEcosystem.Dependency.Package.Ecosystem = "not-an-ecosystem"
	badEcosystem.SecurityVulnerability.Package.Ecosystem = "not-an-ecosystem"
	if _, err := NewGitHubDependabotAlertEnvelope(ctx, badEcosystem); err == nil {
		t.Error("NewGitHubDependabotAlertEnvelope(unsupported ecosystem) = nil, want error")
	}
}
