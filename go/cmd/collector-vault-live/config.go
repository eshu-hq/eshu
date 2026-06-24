// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/vaultlive"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envCollectorInstanceID = "ESHU_VAULT_LIVE_COLLECTOR_INSTANCE_ID"
	envPollInterval        = "ESHU_VAULT_LIVE_POLL_INTERVAL"
	envClaimLeaseTTL       = "ESHU_VAULT_LIVE_CLAIM_LEASE_TTL"
	envHeartbeatInterval   = "ESHU_VAULT_LIVE_HEARTBEAT_INTERVAL"
	envOwnerID             = "ESHU_VAULT_LIVE_COLLECTOR_OWNER_ID"
	envCollectorInstances  = "ESHU_COLLECTOR_INSTANCES_JSON"
	envRedactionKey        = "ESHU_VAULT_LIVE_REDACTION_KEY"
)

const defaultPollInterval = 5 * time.Minute

// targetJSON is one configured Vault target. The token is NOT serialized here;
// token_env names the environment variable that holds the read-only token, so a
// secret never appears in the targets JSON.
type targetJSON struct {
	VaultClusterID string `json:"vault_cluster_id"`
	Namespace      string `json:"namespace"`
	DisplayName    string `json:"display_name"`
	Environment    string `json:"environment"`
	Address        string `json:"address"`
	TokenEnv       string `json:"token_env"`
	SourceURI      string `json:"source_uri"`
	FencingToken   int64  `json:"fencing_token"`
}

type targetsConfiguration struct {
	Targets []targetJSON `json:"targets"`
}

type claimedRuntimeConfig struct {
	Instance          workflow.DesiredCollectorInstance
	OwnerID           string
	PollInterval      time.Duration
	ClaimLeaseTTL     time.Duration
	HeartbeatInterval time.Duration
	Collector         vaultlive.Config
	Auth              map[string]vaultAuth
}

func loadClaimedRuntimeConfig(getenv func(string) string) (claimedRuntimeConfig, error) {
	instances, err := workflow.ParseDesiredCollectorInstancesJSON(getenv(envCollectorInstances))
	if err != nil {
		return claimedRuntimeConfig{}, fmt.Errorf("parse %s: %w", envCollectorInstances, err)
	}
	instance, err := selectVaultLiveInstance(instances, getenv(envCollectorInstanceID))
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if err := validateVaultLiveInstance(instance); err != nil {
		return claimedRuntimeConfig{}, err
	}
	collectorConfig, auth, err := parseVaultLiveRuntimeConfiguration(instance, getenv)
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	pollInterval, err := envDuration(getenv, envPollInterval, defaultPollInterval)
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	claimLeaseTTL, err := envDuration(getenv, envClaimLeaseTTL, workflow.DefaultClaimLeaseTTL())
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	heartbeatInterval, err := envDuration(getenv, envHeartbeatInterval, workflow.DefaultHeartbeatInterval())
	if err != nil {
		return claimedRuntimeConfig{}, err
	}
	if heartbeatInterval >= claimLeaseTTL {
		return claimedRuntimeConfig{}, fmt.Errorf("vault live collector heartbeat interval must be less than claim lease TTL")
	}
	return claimedRuntimeConfig{
		Instance:          instance,
		OwnerID:           ownerID(getenv),
		PollInterval:      pollInterval,
		ClaimLeaseTTL:     claimLeaseTTL,
		HeartbeatInterval: heartbeatInterval,
		Collector:         collectorConfig,
		Auth:              auth,
	}, nil
}

func selectVaultLiveInstance(
	instances []workflow.DesiredCollectorInstance,
	requestedInstanceID string,
) (workflow.DesiredCollectorInstance, error) {
	requestedInstanceID = strings.TrimSpace(requestedInstanceID)
	var matches []workflow.DesiredCollectorInstance
	for _, instance := range instances {
		if instance.CollectorKind != scope.CollectorVaultLive {
			continue
		}
		if requestedInstanceID != "" && instance.InstanceID != requestedInstanceID {
			continue
		}
		matches = append(matches, instance)
	}
	switch len(matches) {
	case 0:
		if requestedInstanceID != "" {
			return workflow.DesiredCollectorInstance{}, fmt.Errorf("vault live collector instance %q not found", requestedInstanceID)
		}
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("no vault live collector instance configured")
	case 1:
		return matches[0], nil
	default:
		return workflow.DesiredCollectorInstance{}, fmt.Errorf("multiple vault live collector instances configured; set %s", envCollectorInstanceID)
	}
}

func validateVaultLiveInstance(instance workflow.DesiredCollectorInstance) error {
	if err := instance.Validate(); err != nil {
		return fmt.Errorf("vault live collector instance: %w", err)
	}
	if instance.CollectorKind != scope.CollectorVaultLive {
		return fmt.Errorf("vault live collector requires collector_kind %q", scope.CollectorVaultLive)
	}
	if !instance.Enabled {
		return fmt.Errorf("vault live collector requires enabled collector instance")
	}
	if !instance.ClaimsEnabled {
		return fmt.Errorf("vault live collector requires claim-enabled collector instance")
	}
	return nil
}

func parseVaultLiveRuntimeConfiguration(
	instance workflow.DesiredCollectorInstance,
	getenv func(string) string,
) (vaultlive.Config, map[string]vaultAuth, error) {
	var decoded targetsConfiguration
	if err := json.Unmarshal([]byte(instance.Configuration), &decoded); err != nil {
		return vaultlive.Config{}, nil, fmt.Errorf("decode vault live collector configuration: %w", err)
	}
	if len(decoded.Targets) == 0 {
		return vaultlive.Config{}, nil, fmt.Errorf("vault live collector configuration requires at least one target")
	}
	redactionKey, err := loadRedactionKey(getenv)
	if err != nil {
		return vaultlive.Config{}, nil, err
	}

	targets := make([]vaultlive.ClusterTarget, 0, len(decoded.Targets))
	auth := make(map[string]vaultAuth, len(decoded.Targets))
	for i, target := range decoded.Targets {
		clusterID := strings.TrimSpace(target.VaultClusterID)
		if clusterID == "" {
			return vaultlive.Config{}, nil, fmt.Errorf("targets[%d] vault_cluster_id must not be blank", i)
		}
		address := strings.TrimSpace(target.Address)
		if address == "" {
			return vaultlive.Config{}, nil, fmt.Errorf("targets[%d] (%s) address must not be blank", i, clusterID)
		}
		tokenEnv := strings.TrimSpace(target.TokenEnv)
		if tokenEnv == "" {
			return vaultlive.Config{}, nil, fmt.Errorf("targets[%d] (%s) token_env must name the env var holding the read-only token", i, clusterID)
		}
		token := strings.TrimSpace(getenv(tokenEnv))
		if token == "" {
			return vaultlive.Config{}, nil, fmt.Errorf("targets[%d] (%s): env %s is empty", i, clusterID, tokenEnv)
		}
		namespace := strings.TrimSpace(target.Namespace)
		// Key by (cluster, namespace): the scope identity is namespace-scoped, so
		// one cluster may legitimately host multiple namespace targets.
		key := authKey(clusterID, namespace)
		if _, dup := auth[key]; dup {
			return vaultlive.Config{}, nil, fmt.Errorf("duplicate vault target scope")
		}
		auth[key] = vaultAuth{Address: address, Token: token, Namespace: namespace}
		targets = append(targets, vaultlive.ClusterTarget{
			VaultClusterID: clusterID,
			Namespace:      namespace,
			DisplayName:    strings.TrimSpace(target.DisplayName),
			Environment:    strings.TrimSpace(target.Environment),
			FencingToken:   target.FencingToken,
			SourceURI:      strings.TrimSpace(target.SourceURI),
		})
	}

	return vaultlive.Config{CollectorInstanceID: instance.InstanceID, RedactionKey: redactionKey, Targets: targets}, auth, nil
}

func loadRedactionKey(getenv func(string) string) (redact.Key, error) {
	raw := strings.TrimSpace(getenv(envRedactionKey))
	if raw == "" {
		return redact.Key{}, fmt.Errorf("%s is required for Vault metadata redaction", envRedactionKey)
	}
	key, err := redact.NewKey([]byte(raw))
	if err != nil {
		return redact.Key{}, fmt.Errorf("invalid %s: %w", envRedactionKey, err)
	}
	return key, nil
}

func envDuration(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", key, raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return d, nil
}

func ownerID(getenv func(string) string) string {
	for _, key := range []string{envOwnerID, "HOSTNAME"} {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			return value
		}
	}
	return "collector-vault-live"
}
