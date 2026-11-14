// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package registry

import "testing"

func TestECRPrivateRegistryHostBuildsECRHost(t *testing.T) {
	t.Parallel()

	got, err := ECRPrivateRegistryHost("123456789012", "us-east-1")
	if err != nil {
		t.Fatalf("ECRPrivateRegistryHost() error = %v", err)
	}
	want := "123456789012.dkr.ecr.us-east-1.amazonaws.com"
	if got != want {
		t.Fatalf("ECRPrivateRegistryHost() = %q, want %q", got, want)
	}
}

func TestECRRepositoryIdentityUsesECRProvider(t *testing.T) {
	t.Parallel()

	identity := ECRRepositoryIdentity("123456789012.dkr.ecr.us-east-1.amazonaws.com", "team/api")
	if identity.Provider != "ecr" {
		t.Fatalf("Provider = %q", identity.Provider)
	}
	if identity.Repository != "team/api" {
		t.Fatalf("Repository = %q", identity.Repository)
	}
}

func TestECRPrivateRegistryHostRejectsMissingFields(t *testing.T) {
	t.Parallel()

	if _, err := ECRPrivateRegistryHost("", "us-east-1"); err == nil {
		t.Fatal("ECRPrivateRegistryHost() error = nil for blank account")
	}
	if _, err := ECRPrivateRegistryHost("123456789012", ""); err == nil {
		t.Fatal("ECRPrivateRegistryHost() error = nil for blank region")
	}
}
