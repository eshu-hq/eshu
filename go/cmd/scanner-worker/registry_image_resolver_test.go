// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/workflow"
)

type fakeRegistryImageTargetStore struct{ target workflow.ScannerImageTarget }

func registryDomainJSON(t *testing.T, domain registryDomainConfig) json.RawMessage {
	t.Helper()
	raw, err := json.Marshal(domain)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (store fakeRegistryImageTargetStore) GetScannerImageTarget(_ context.Context, _, _ string, _ int64, _ string) (workflow.ScannerImageTarget, error) {
	return store.target, nil
}

func TestRegistryImageResolverRejectsWrongCredentialDomain(t *testing.T) {
	t.Parallel()
	input := testSBOMClaimInput(t)
	store := fakeRegistryImageTargetStore{target: workflow.ScannerImageTarget{
		WorkItemID: input.WorkItemID, AuthorizationNamespace: workflow.ScannerAuthorizationLocal, AuthorizationDomain: "other",
		ManifestDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		Sources: []workflow.ScannerImageSourceBinding{{
			Registry: "registry.example", Repository: "team/app",
			RuntimeDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			RuntimeDigestKind:     workflow.ScannerRuntimeDigestManifest,
			RuntimeImageReference: "registry.example/team/app@sha256:1111111111111111111111111111111111111111111111111111111111111111",
			SourceObservationID:   1, SourceCollectorInstanceID: "source-1", SourceScopeID: "scope-1", SourceGenerationID: "generation-1",
		}},
	}}
	store.target.ScanTargetID = workflow.ScannerImageTargetScopeID(store.target)
	input.Target.ScopeID = store.target.ScanTargetID
	resolver := newRegistryImageResolver(store, []registryDomainConfig{{
		AuthorizationDomain: "allowed", Registry: "registry.example", BaseURL: "https://registry.example", AuthMode: "anonymous",
	}}, func(string) string { return "" })
	_, err := resolver.ResolveImage(context.Background(), input)
	if !errors.Is(err, errRegistryImageUnauthorized) {
		t.Fatalf("ResolveImage() error = %v, want unauthorized", err)
	}
}

func TestRegistryImageResolverReadsCredentialAtClaimTime(t *testing.T) {
	t.Parallel()
	input := testSBOMClaimInput(t)
	target := workflow.ScannerImageTarget{
		WorkItemID: input.WorkItemID, AuthorizationNamespace: workflow.ScannerAuthorizationLocal, AuthorizationDomain: "team",
		ManifestDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		Sources: []workflow.ScannerImageSourceBinding{{
			Registry: "registry.example", Repository: "team/app",
			RuntimeDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			RuntimeDigestKind:     workflow.ScannerRuntimeDigestManifest,
			RuntimeImageReference: "registry.example/team/app@sha256:1111111111111111111111111111111111111111111111111111111111111111",
			SourceObservationID:   1, SourceCollectorInstanceID: "source-1", SourceScopeID: "scope-1", SourceGenerationID: "generation-1",
		}},
	}
	target.ScanTargetID = workflow.ScannerImageTargetScopeID(target)
	input.Target.ScopeID = target.ScanTargetID
	target.RegistryDomainConfig = registryDomainJSON(t, registryDomainConfig{
		AuthorizationDomain: "team", Registry: "registry.example", BaseURL: "https://registry.example", AuthMode: "bearer_env", BearerTokenEnv: "TOKEN",
	})
	resolver := newRegistryImageResolver(fakeRegistryImageTargetStore{target: target}, []registryDomainConfig{{
		AuthorizationDomain: "team", Registry: "registry.example", BaseURL: "https://registry.example", AuthMode: "bearer_env", BearerTokenEnv: "TOKEN",
	}}, func(string) string { return "" })
	_, err := resolver.ResolveImage(context.Background(), input)
	if !errors.Is(err, errRegistryImageUnauthorized) {
		t.Fatalf("missing credential error = %v, want unauthorized", err)
	}
	resolver = newRegistryImageResolver(fakeRegistryImageTargetStore{target: target}, []registryDomainConfig{{
		AuthorizationDomain: "team", Registry: "registry.example", BaseURL: "https://registry.example", AuthMode: "bearer_env", BearerTokenEnv: "TOKEN",
	}}, func(key string) string {
		if key == "TOKEN" {
			return "secret"
		}
		return ""
	})
	resolved, err := resolver.ResolveImage(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Client == nil || resolved.ScanTargetID != target.ScanTargetID {
		t.Fatalf("resolved target = %#v", resolved)
	}
	target.Sources = append(target.Sources, target.Sources[0])
	resolver = newRegistryImageResolver(fakeRegistryImageTargetStore{target: target}, []registryDomainConfig{{
		AuthorizationDomain: "team", Registry: "registry.example", BaseURL: "https://registry.example", AuthMode: "bearer_env", BearerTokenEnv: "TOKEN",
	}}, func(string) string { return "secret" })
	if _, err := resolver.ResolveImage(context.Background(), input); !errors.Is(err, errRegistryImageUnauthorized) {
		t.Fatalf("unpinned multi-source target error = %v, want unauthorized", err)
	}
}

func TestRegistryImageResolverRejectsChangedCurrentCredentialPolicy(t *testing.T) {
	t.Parallel()
	input := testSBOMClaimInput(t)
	target := workflow.ScannerImageTarget{
		WorkItemID: input.WorkItemID, AuthorizationNamespace: workflow.ScannerAuthorizationLocal, AuthorizationDomain: "team",
		ManifestDigest: "sha256:1111111111111111111111111111111111111111111111111111111111111111",
		Sources: []workflow.ScannerImageSourceBinding{{
			Registry: "registry.example", Repository: "team/app",
			RuntimeDigest:         "sha256:1111111111111111111111111111111111111111111111111111111111111111",
			RuntimeDigestKind:     workflow.ScannerRuntimeDigestManifest,
			RuntimeImageReference: "registry.example/team/app@sha256:1111111111111111111111111111111111111111111111111111111111111111",
			SourceObservationID:   1, SourceCollectorInstanceID: "source-1", SourceScopeID: "scope-1", SourceGenerationID: "generation-1",
		}},
	}
	target.ScanTargetID = workflow.ScannerImageTargetScopeID(target)
	input.Target.ScopeID = target.ScanTargetID
	target.RegistryDomainConfig = registryDomainJSON(t, registryDomainConfig{
		AuthorizationDomain: "team", Registry: "registry.example", BaseURL: "https://registry.example", AuthMode: "basic_env", UsernameEnv: "USER", PasswordEnv: "PASS",
	})
	resolver := newRegistryImageResolver(fakeRegistryImageTargetStore{target: target}, []registryDomainConfig{{
		AuthorizationDomain: "team", Registry: "registry.example", BaseURL: "https://registry.example", AuthMode: "bearer_env", BearerTokenEnv: "TOKEN",
	}}, func(string) string { return "secret" })
	_, err := resolver.ResolveImage(context.Background(), input)
	if !errors.Is(err, errRegistryImageUnauthorized) {
		t.Fatalf("ResolveImage() error = %v, want revoked startup credential policy", err)
	}
}
