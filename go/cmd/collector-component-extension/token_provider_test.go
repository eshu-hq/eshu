// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace/noop"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/github/app"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// hostTokenTestKey generates an ephemeral RSA key for provider tests. It is
// test-only material, never a real credential.
func hostTokenTestKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func hostTokenTestEnv(keyPEM, installations string) map[string]string {
	return map[string]string{
		envGitHubAppID:            "42",
		envGitHubAppPrivateKey:    keyPEM,
		envGitHubAppInstallations: installations,
	}
}

// TestTokenProviderFromEnvBuildsScopedProvider proves the all-present path
// returns a working scope-routed provider: a claim for an unmapped scope
// fails closed with a bounded config error before any mint is attempted.
func TestTokenProviderFromEnvBuildsScopedProvider(t *testing.T) {
	t.Parallel()

	provider, err := tokenProviderFromEnv(mapEnv(hostTokenTestEnv(
		hostTokenTestKey(t),
		`{"scope-a":"111","scope-b":"222"}`,
	)))
	if err != nil {
		t.Fatalf("tokenProviderFromEnv() error = %v, want nil", err)
	}
	if provider == nil {
		t.Fatal("tokenProviderFromEnv() = nil, want scoped provider")
	}
	_, err = provider(context.Background(), workflow.WorkItem{ScopeID: "scope-unknown"})
	if err == nil {
		t.Fatal("provider() error = nil, want unknown-scope denial")
	}
	var configErr *app.ConfigError
	if !errors.As(err, &configErr) {
		t.Fatalf("provider() error type = %T, want *app.ConfigError", err)
	}
}

// TestTokenProviderFromEnvToleratesSingleLinePEM proves a single-line PEM
// body (the usual secret-environment shape) is accepted at construction;
// the provider re-wraps it at mint time.
func TestTokenProviderFromEnvToleratesSingleLinePEM(t *testing.T) {
	t.Parallel()

	wrapped := hostTokenTestKey(t)
	body := strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(wrapped,
		"-----BEGIN RSA PRIVATE KEY-----", ""), "-----END RSA PRIVATE KEY-----", ""))
	collapsed := "-----BEGIN RSA PRIVATE KEY-----" + strings.ReplaceAll(body, "\n", "") + "-----END RSA PRIVATE KEY-----"

	provider, err := tokenProviderFromEnv(mapEnv(hostTokenTestEnv(
		collapsed,
		`{"scope-a":"111"}`,
	)))
	if err != nil {
		t.Fatalf("tokenProviderFromEnv() error = %v, want nil for single-line PEM", err)
	}
	if provider == nil {
		t.Fatal("tokenProviderFromEnv() = nil, want provider for single-line PEM")
	}
}

// TestTokenProviderFromEnvLeavesProviderUnsetWhenAnyMissing proves a
// partially configured host keeps the static config path instead of
// constructing a broken provider.
func TestTokenProviderFromEnvLeavesProviderUnsetWhenAnyMissing(t *testing.T) {
	t.Parallel()

	full := hostTokenTestEnv(hostTokenTestKey(t), `{"scope-a":"111"}`)
	tests := []struct {
		name  string
		clear string
	}{
		{name: "missing app id", clear: envGitHubAppID},
		{name: "missing private key", clear: envGitHubAppPrivateKey},
		{name: "missing installations", clear: envGitHubAppInstallations},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			values := make(map[string]string, len(full))
			for k, v := range full {
				values[k] = v
			}
			delete(values, tt.clear)
			provider, err := tokenProviderFromEnv(mapEnv(values))
			if err != nil {
				t.Fatalf("tokenProviderFromEnv() error = %v, want nil", err)
			}
			if provider != nil {
				t.Fatalf("tokenProviderFromEnv() = non-nil, want nil when %s is missing", tt.clear)
			}
		})
	}
	if provider, err := tokenProviderFromEnv(mapEnv(nil)); err != nil || provider != nil {
		t.Fatalf("tokenProviderFromEnv() = (%v, %v), want (nil, nil) when nothing is set", provider, err)
	}
}

// TestTokenProviderFromEnvRejectsMalformedInstallations proves malformed
// installations JSON fails closed at startup with an error that names the
// variable but never carries credential material.
func TestTokenProviderFromEnvRejectsMalformedInstallations(t *testing.T) {
	t.Parallel()

	keyPEM := hostTokenTestKey(t)
	tests := []struct {
		name          string
		installations string
	}{
		{name: "not json", installations: `{not-json`},
		{name: "not an object", installations: `["scope-a"]`},
		{name: "non-string installation", installations: `{"scope-a": 111}`},
		{name: "no installations", installations: `{}`},
		{name: "blank scope", installations: `{" ": "111"}`},
		{name: "blank installation", installations: `{"scope-a": " "}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := tokenProviderFromEnv(mapEnv(hostTokenTestEnv(keyPEM, tt.installations)))
			if err == nil {
				t.Fatalf("tokenProviderFromEnv() error = nil, want rejection of %s", tt.name)
			}
			if !strings.Contains(err.Error(), envGitHubAppInstallations) {
				t.Fatalf("tokenProviderFromEnv() error = %q, want it to name %s", err, envGitHubAppInstallations)
			}
			if strings.Contains(err.Error(), "PRIVATE") {
				t.Fatalf("tokenProviderFromEnv() error = %q, must not carry key material", err)
			}
		})
	}
}

// TestBuildClaimedServiceRejectsMalformedInstallationsJSON proves the
// fail-closed startup wiring: malformed installations JSON aborts worker
// startup with an error naming the variable.
func TestBuildClaimedServiceRejectsMalformedInstallationsJSON(t *testing.T) {
	t.Parallel()

	componentHome := t.TempDir()
	installEnabledComponent(t, componentHome, "process", writeComponentExtensionConfig(t))

	values := hostTokenTestEnv(hostTokenTestKey(t), `{not-json`)
	values[envComponentHome] = componentHome
	values[envComponentTrustMode] = component.TrustModeAllowlist
	values[envComponentAllowIDs] = "dev.eshu.examples.scorecard"
	values[envComponentAllowPublishers] = "eshu-hq"
	values[envComponentCoreVersion] = "dev"

	_, err := buildClaimedService(
		&fakeExecQueryer{},
		mapEnv(values),
		noop.NewTracerProvider().Tracer("test"),
		nil,
		nil,
	)
	if err == nil {
		t.Fatal("buildClaimedService() error = nil, want malformed installations rejection")
	}
	if !strings.Contains(err.Error(), envGitHubAppInstallations) {
		t.Fatalf("buildClaimedService() error = %q, want it to name %s", err, envGitHubAppInstallations)
	}
}

// TestBuildClaimedServiceSucceedsWithGitHubAppEnvConfigured proves a fully
// configured host still builds: the provider and the static activation
// config coexist because the fixture defines no host-minted token key.
func TestBuildClaimedServiceSucceedsWithGitHubAppEnvConfigured(t *testing.T) {
	t.Parallel()

	componentHome := t.TempDir()
	installEnabledComponent(t, componentHome, "process", writeComponentExtensionConfig(t))

	values := hostTokenTestEnv(hostTokenTestKey(t), `{"scope-a":"111"}`)
	values[envComponentHome] = componentHome
	values[envComponentTrustMode] = component.TrustModeAllowlist
	values[envComponentAllowIDs] = "dev.eshu.examples.scorecard"
	values[envComponentAllowPublishers] = "eshu-hq"
	values[envComponentCoreVersion] = "dev"
	values[envCollectorOwnerID] = "component-owner-a"

	if _, err := buildClaimedService(
		&fakeExecQueryer{},
		mapEnv(values),
		noop.NewTracerProvider().Tracer("test"),
		nil,
		nil,
	); err != nil {
		t.Fatalf("buildClaimedService() error = %v, want nil with GitHub App env configured", err)
	}
}
