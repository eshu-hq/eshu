package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/vaultlive"
)

const (
	envCollectorInstanceID = "ESHU_VAULT_LIVE_COLLECTOR_INSTANCE_ID"
	envTargetsJSON         = "ESHU_VAULT_LIVE_TARGETS_JSON"
	envPollInterval        = "ESHU_VAULT_LIVE_POLL_INTERVAL"
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

// runtimeConfig is the resolved collector configuration plus the per-target auth
// map consumed by the client factory.
type runtimeConfig struct {
	Collector    vaultlive.Config
	Auth         map[string]vaultAuth
	PollInterval time.Duration
}

func loadRuntimeConfig(getenv func(string) string) (runtimeConfig, error) {
	collectorID := strings.TrimSpace(getenv(envCollectorInstanceID))
	if collectorID == "" {
		return runtimeConfig{}, fmt.Errorf("%s is required", envCollectorInstanceID)
	}
	rawTargets := strings.TrimSpace(getenv(envTargetsJSON))
	if rawTargets == "" {
		return runtimeConfig{}, fmt.Errorf("%s is required", envTargetsJSON)
	}
	var decoded targetsConfiguration
	if err := json.Unmarshal([]byte(rawTargets), &decoded); err != nil {
		return runtimeConfig{}, fmt.Errorf("decode %s: %w", envTargetsJSON, err)
	}
	if len(decoded.Targets) == 0 {
		return runtimeConfig{}, fmt.Errorf("%s requires at least one target", envTargetsJSON)
	}

	targets := make([]vaultlive.ClusterTarget, 0, len(decoded.Targets))
	auth := make(map[string]vaultAuth, len(decoded.Targets))
	for i, target := range decoded.Targets {
		clusterID := strings.TrimSpace(target.VaultClusterID)
		if clusterID == "" {
			return runtimeConfig{}, fmt.Errorf("targets[%d] vault_cluster_id must not be blank", i)
		}
		address := strings.TrimSpace(target.Address)
		if address == "" {
			return runtimeConfig{}, fmt.Errorf("targets[%d] (%s) address must not be blank", i, clusterID)
		}
		tokenEnv := strings.TrimSpace(target.TokenEnv)
		if tokenEnv == "" {
			return runtimeConfig{}, fmt.Errorf("targets[%d] (%s) token_env must name the env var holding the read-only token", i, clusterID)
		}
		token := strings.TrimSpace(getenv(tokenEnv))
		if token == "" {
			return runtimeConfig{}, fmt.Errorf("targets[%d] (%s): env %s is empty", i, clusterID, tokenEnv)
		}
		namespace := strings.TrimSpace(target.Namespace)
		// Key by (cluster, namespace): the scope identity is namespace-scoped, so
		// one cluster may legitimately host multiple namespace targets.
		key := authKey(clusterID, namespace)
		if _, dup := auth[key]; dup {
			return runtimeConfig{}, fmt.Errorf("duplicate vault target %q (namespace %q)", clusterID, namespace)
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

	pollInterval, err := parsePollInterval(getenv(envPollInterval))
	if err != nil {
		return runtimeConfig{}, err
	}

	return runtimeConfig{
		Collector:    vaultlive.Config{CollectorInstanceID: collectorID, Targets: targets},
		Auth:         auth,
		PollInterval: pollInterval,
	}, nil
}

func parsePollInterval(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultPollInterval, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("invalid %s %q: %w", envPollInterval, raw, err)
	}
	if d <= 0 {
		return 0, fmt.Errorf("%s must be positive", envPollInterval)
	}
	return d, nil
}
