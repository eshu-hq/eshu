// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"strings"
	"testing"
)

func TestValidateVaultLiveCollectorConfiguration(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[{"vault_cluster_id":"vault-a","namespace":"admin","address":"http://127.0.0.1:8200","token_env":"VAULT_TOKEN","source_uri":"https://vault.example.com"}]}`
	if err := ValidateVaultLiveCollectorConfiguration(raw); err != nil {
		t.Fatalf("ValidateVaultLiveCollectorConfiguration() error = %v, want nil", err)
	}
}

func TestValidateVaultLiveCollectorConfigurationRejectsCredentialBearingTarget(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"serialized token": `{"targets":[{"vault_cluster_id":"vault-a","address":"https://vault.example.com","token_env":"VAULT_TOKEN","token":"s.secret"}]}`,
		"address userinfo": `{"targets":[{"vault_cluster_id":"vault-a","address":"https://user:pass@vault.example.com","token_env":"VAULT_TOKEN"}]}`,
		"source userinfo":  `{"targets":[{"vault_cluster_id":"vault-a","address":"https://vault.example.com","token_env":"VAULT_TOKEN","source_uri":"https://user:pass@vault.example.com"}]}`,
		"source query":     `{"targets":[{"vault_cluster_id":"vault-a","address":"https://vault.example.com","token_env":"VAULT_TOKEN","source_uri":"https://vault.example.com?token=s.secret"}]}`,
	}
	for name, raw := range cases {
		name, raw := name, raw
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := ValidateVaultLiveCollectorConfiguration(raw); err == nil {
				t.Fatalf("%s: ValidateVaultLiveCollectorConfiguration() error = nil, want error", name)
			}
		})
	}
}

func TestValidateVaultLiveCollectorConfigurationRejectsDuplicateScope(t *testing.T) {
	t.Parallel()

	raw := `{"targets":[
		{"vault_cluster_id":"vault-a","namespace":"admin","address":"https://vault.example.com","token_env":"VAULT_TOKEN"},
		{"vault_cluster_id":"vault-a","namespace":"admin","address":"https://vault.example.com","token_env":"VAULT_TOKEN_2"}
	]}`
	if err := ValidateVaultLiveCollectorConfiguration(raw); err == nil {
		t.Fatal("ValidateVaultLiveCollectorConfiguration() error = nil, want duplicate-scope error")
	} else if strings.Contains(err.Error(), "vault-a") || strings.Contains(err.Error(), "admin") {
		t.Fatalf("duplicate-scope error exposed raw target identity: %v", err)
	}
}
