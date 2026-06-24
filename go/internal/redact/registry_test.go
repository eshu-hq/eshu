// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package redact_test

import (
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/redact"
)

func TestHostedGovernanceRegistryCoversRequiredSurfaces(t *testing.T) {
	t.Parallel()

	registry := redact.HostedGovernanceRegistry()
	want := []redact.Surface{
		redact.SurfaceFacts,
		redact.SurfaceLogs,
		redact.SurfaceMetricLabels,
		redact.SurfaceStatusErrors,
		redact.SurfaceGraphProperties,
		redact.SurfaceAPIMCPBodies,
		redact.SurfaceConsoleSurfaces,
		redact.SurfaceAuditEvents,
		redact.SurfaceDocsExamples,
		redact.SurfaceOnboardingArtifacts,
	}

	for _, surface := range want {
		policy, ok := registry.Policy(surface)
		if !ok {
			t.Fatalf("Policy(%q) ok = false, want true", surface)
		}
		if got := len(policy.ForbiddenClasses); got == 0 {
			t.Fatalf("Policy(%q).ForbiddenClasses = 0, want non-empty", surface)
		}
	}
	if got, want := len(registry.Surfaces()), len(want); got != want {
		t.Fatalf("Surfaces() = %d, want %d", got, want)
	}
}

func TestHostedGovernanceRegistryDetectsForbiddenCanaries(t *testing.T) {
	t.Parallel()

	registry := redact.HostedGovernanceRegistry()
	for _, surface := range registry.Surfaces() {
		for _, sample := range registry.ForbiddenCanaries(surface) {
			t.Run(string(surface)+"/"+string(sample.Class), func(t *testing.T) {
				t.Parallel()

				err := registry.AssertNoForbiddenCanary(surface, []byte("prefix "+sample.Raw+" suffix"))
				if err == nil {
					t.Fatal("AssertNoForbiddenCanary() error = nil, want leak detection")
				}
				if strings.Contains(err.Error(), sample.Raw) {
					t.Fatalf("error %q contains raw canary", err)
				}
			})
		}
	}
}

func TestHostedGovernanceRegistryAllowsSanitizedPayloads(t *testing.T) {
	t.Parallel()

	registry := redact.HostedGovernanceRegistry()
	payload := []byte(`{"credential_source_kind":"environment_variable","provider_profile_id":"semantic-docs","source_class":"documentation","actor_class":"service_account","reason":"policy_denied"}`)

	if err := registry.AssertNoForbiddenCanary(redact.SurfaceAPIMCPBodies, payload); err != nil {
		t.Fatalf("AssertNoForbiddenCanary() error = %v, want nil", err)
	}
	if err := registry.AssertNoForbiddenCanary(redact.SurfaceStatusErrors, payload); err != nil {
		t.Fatalf("AssertNoForbiddenCanary() error = %v, want nil", err)
	}
}

func TestHostedGovernanceRegistrySafeClassesAreSurfaceBounded(t *testing.T) {
	t.Parallel()

	registry := redact.HostedGovernanceRegistry()

	if !registry.IsSafeClass(redact.SurfaceStatusErrors, redact.SafeCredentialReference) {
		t.Fatalf("status errors should allow credential reference class")
	}
	if !registry.IsSafeClass(redact.SurfaceAPIMCPBodies, redact.SafeProviderProfileID) {
		t.Fatalf("API/MCP bodies should allow provider profile ids")
	}
	if registry.IsSafeClass(redact.SurfaceMetricLabels, redact.SafeTenantWorkspaceID) {
		t.Fatalf("metric labels must not allow tenant or workspace ids")
	}
	if registry.IsSafeClass(redact.SurfaceMetricLabels, redact.SafeCredentialReference) {
		t.Fatalf("metric labels must not allow credential references")
	}
	if !registry.IsSafeClass(redact.SurfaceMetricLabels, redact.SafeReasonCode) {
		t.Fatalf("metric labels should allow low-cardinality reason codes")
	}
}

func TestHostedGovernanceRegistryCanariesAreUniqueAndNonBlank(t *testing.T) {
	t.Parallel()

	registry := redact.HostedGovernanceRegistry()
	seen := map[string]redact.SensitiveClass{}
	for _, sample := range registry.Canaries() {
		if strings.TrimSpace(sample.Raw) == "" {
			t.Fatalf("canary for class %q is blank", sample.Class)
		}
		if prior, ok := seen[sample.Raw]; ok {
			t.Fatalf("canary raw value reused by %q and %q", prior, sample.Class)
		}
		seen[sample.Raw] = sample.Class
	}
}
