package main

import (
	"testing"
	"time"
)

func envFromMap(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestLoadRuntimeConfigResolvesTokenFromEnv(t *testing.T) {
	t.Parallel()

	getenv := envFromMap(map[string]string{
		envCollectorInstanceID: "vaultlive-1",
		envTargetsJSON:         `{"targets":[{"vault_cluster_id":"vault-a","namespace":"admin","address":"https://a:8200","token_env":"VAULT_TOKEN_A","fencing_token":7}]}`,
		"VAULT_TOKEN_A":        "s.readonly-token",
		envPollInterval:        "2m",
	})

	cfg, err := loadRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadRuntimeConfig err = %v", err)
	}
	if cfg.PollInterval != 2*time.Minute {
		t.Fatalf("PollInterval = %v, want 2m", cfg.PollInterval)
	}
	if len(cfg.Collector.Targets) != 1 || cfg.Collector.Targets[0].VaultClusterID != "vault-a" {
		t.Fatalf("targets = %+v", cfg.Collector.Targets)
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
		envTargetsJSON: `{"targets":[
			{"vault_cluster_id":"vault-a","namespace":"admin","address":"https://a","token_env":"T_ADMIN"},
			{"vault_cluster_id":"vault-a","namespace":"team","address":"https://a","token_env":"T_TEAM"}
		]}`,
		"T_ADMIN": "tok-admin",
		"T_TEAM":  "tok-team",
	})

	cfg, err := loadRuntimeConfig(getenv)
	if err != nil {
		t.Fatalf("loadRuntimeConfig err = %v (multi-namespace per cluster must be allowed)", err)
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

func TestLoadRuntimeConfigErrors(t *testing.T) {
	t.Parallel()

	base := map[string]string{
		envCollectorInstanceID: "ci",
		envTargetsJSON:         `{"targets":[{"vault_cluster_id":"v","address":"https://v","token_env":"T"}]}`,
		"T":                    "tok",
	}
	cases := map[string]map[string]string{
		"missing instance id": {envTargetsJSON: base[envTargetsJSON], "T": "tok"},
		"missing targets":     {envCollectorInstanceID: "ci"},
		"bad json":            {envCollectorInstanceID: "ci", envTargetsJSON: "{not json"},
		"empty targets":       {envCollectorInstanceID: "ci", envTargetsJSON: `{"targets":[]}`},
		"blank cluster id":    {envCollectorInstanceID: "ci", envTargetsJSON: `{"targets":[{"address":"https://v","token_env":"T"}]}`, "T": "tok"},
		"missing address":     {envCollectorInstanceID: "ci", envTargetsJSON: `{"targets":[{"vault_cluster_id":"v","token_env":"T"}]}`, "T": "tok"},
		"missing token env":   {envCollectorInstanceID: "ci", envTargetsJSON: `{"targets":[{"vault_cluster_id":"v","address":"https://v"}]}`},
		"empty token":         {envCollectorInstanceID: "ci", envTargetsJSON: `{"targets":[{"vault_cluster_id":"v","address":"https://v","token_env":"T"}]}`},
	}
	for name, env := range cases {
		name, env := name, env
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if _, err := loadRuntimeConfig(envFromMap(env)); err == nil {
				t.Fatalf("%s: loadRuntimeConfig err = nil, want error", name)
			}
		})
	}
}
