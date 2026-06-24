// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/vaultlive"
)

func envFromMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func vaultLiveInstancesJSON(instanceID string, targets string) string {
	return fmt.Sprintf(`[{
		"instance_id":%q,
		"collector_kind":"vault_live",
		"mode":"continuous",
		"enabled":true,
		"claims_enabled":true,
		"configuration":{"targets":%s}
	}]`, instanceID, targets)
}

func TestLoadRuntimeConfigResolvesTokenFromEnv(t *testing.T) {
	t.Parallel()

	getenv := envFromMap(map[string]string{
		envCollectorInstanceID: "vaultlive-1",
		envCollectorInstances:  vaultLiveInstancesJSON("vaultlive-1", `[{"vault_cluster_id":"vault-a","namespace":"admin","address":"https://a:8200","token_env":"VAULT_TOKEN_A","fencing_token":7}]`),
		"VAULT_TOKEN_A":        "s.readonly-token",
		envRedactionKey:        "vault-redaction-key",
		envPollInterval:        "2m",
	})

	cfg, err := loadClaimedRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig err = %v", err)
	}
	if cfg.PollInterval != 2*time.Minute {
		t.Fatalf("PollInterval = %v, want 2m", cfg.PollInterval)
	}
	if len(cfg.Collector.Targets) != 1 || cfg.Collector.Targets[0].VaultClusterID != "vault-a" {
		t.Fatalf("targets = %+v", cfg.Collector.Targets)
	}
	if cfg.Collector.RedactionKey.IsZero() {
		t.Fatalf("RedactionKey is zero, want resolved key from %s", envRedactionKey)
	}
	auth, ok := cfg.Auth[authKey("vault-a", "admin")]
	if !ok || auth.Token != "s.readonly-token" || auth.Address != "https://a:8200" || auth.Namespace != "admin" {
		t.Fatalf("auth = %+v ok=%v", auth, ok)
	}
}

func TestLoadRuntimeConfigAllowsMultipleNamespacesPerCluster(t *testing.T) {
	t.Parallel()

	getenv := envFromMap(map[string]string{
		envCollectorInstanceID: "ci",
		envCollectorInstances: vaultLiveInstancesJSON("ci", `[
			{"vault_cluster_id":"vault-a","namespace":"admin","address":"https://a","token_env":"T_ADMIN"},
			{"vault_cluster_id":"vault-a","namespace":"team","address":"https://a","token_env":"T_TEAM"}
		]`),
		envRedactionKey: "vault-redaction-key",
		"T_ADMIN":       "tok-admin",
		"T_TEAM":        "tok-team",
	})

	cfg, err := loadClaimedRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadClaimedRuntimeConfig err = %v (multi-namespace per cluster must be allowed)", err)
	}
	if len(cfg.Collector.Targets) != 2 {
		t.Fatalf("targets = %d, want 2", len(cfg.Collector.Targets))
	}
	admin := cfg.Auth[authKey("vault-a", "admin")]
	team := cfg.Auth[authKey("vault-a", "team")]
	if admin.Token != "tok-admin" || team.Token != "tok-team" {
		t.Fatalf("per-namespace tokens not keyed distinctly: admin=%q team=%q", admin.Token, team.Token)
	}
}

func TestLoadRuntimeConfigDuplicateTargetErrorDoesNotExposeRawNamespace(t *testing.T) {
	t.Parallel()

	getenv := envFromMap(map[string]string{
		envCollectorInstanceID: "ci",
		envCollectorInstances: vaultLiveInstancesJSON("ci", `[
			{"vault_cluster_id":"vault-a","namespace":"admin","address":"https://a","token_env":"T_ADMIN"},
			{"vault_cluster_id":"vault-a","namespace":"admin","address":"https://a","token_env":"T_ADMIN_2"}
		]`),
		envRedactionKey: "vault-redaction-key",
		"T_ADMIN":       "tok-admin",
		"T_ADMIN_2":     "tok-admin-2",
	})

	_, err := loadClaimedRuntimeConfig(getenv)
	if err == nil {
		t.Fatal("loadClaimedRuntimeConfig() error = nil, want duplicate-target error")
	}
	if strings.Contains(err.Error(), "vault-a") || strings.Contains(err.Error(), "admin") {
		t.Fatalf("duplicate-target error exposed raw target identity: %v", err)
	}
}

func TestVaultClientFactoryMissingAuthErrorDoesNotExposeRawTargetIdentity(t *testing.T) {
	t.Parallel()

	_, err := (vaultClientFactory{}).Client(context.Background(), vaultlive.ClusterTarget{
		VaultClusterID: "vault-a",
		Namespace:      "admin",
	})
	if err == nil {
		t.Fatal("Client() error = nil, want missing-auth error")
	}
	if strings.Contains(err.Error(), "vault-a") || strings.Contains(err.Error(), "admin") {
		t.Fatalf("missing-auth error exposed raw target identity: %v", err)
	}
}

func TestLoadRuntimeConfigErrors(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]string{
		"requested instance missing": {envCollectorInstanceID: "missing", envCollectorInstances: vaultLiveInstancesJSON("ci", `[{"vault_cluster_id":"v","address":"https://v","token_env":"T"}]`), "T": "tok"},
		"missing instances":          {envCollectorInstanceID: "ci"},
		"bad json":                   {envCollectorInstanceID: "ci", envCollectorInstances: "{not json"},
		"empty targets":              {envCollectorInstanceID: "ci", envCollectorInstances: vaultLiveInstancesJSON("ci", `[]`)},
		"blank cluster id":           {envCollectorInstanceID: "ci", envCollectorInstances: vaultLiveInstancesJSON("ci", `[{"address":"https://v","token_env":"T"}]`), "T": "tok"},
		"missing address":            {envCollectorInstanceID: "ci", envCollectorInstances: vaultLiveInstancesJSON("ci", `[{"vault_cluster_id":"v","token_env":"T"}]`), "T": "tok"},
		"missing token env":          {envCollectorInstanceID: "ci", envCollectorInstances: vaultLiveInstancesJSON("ci", `[{"vault_cluster_id":"v","address":"https://v"}]`)},
		"empty token":                {envCollectorInstanceID: "ci", envCollectorInstances: vaultLiveInstancesJSON("ci", `[{"vault_cluster_id":"v","address":"https://v","token_env":"T"}]`)},
		"missing redaction key":      {envCollectorInstanceID: "ci", envCollectorInstances: vaultLiveInstancesJSON("ci", `[{"vault_cluster_id":"v","address":"https://v","token_env":"T"}]`), "T": "tok"},
	}
	for name, env := range cases {
		name, env := name, env
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := loadClaimedRuntimeConfig(envFromMap(env)); err == nil {
				t.Fatalf("%s: loadClaimedRuntimeConfig err = nil, want error", name)
			}
		})
	}
}
