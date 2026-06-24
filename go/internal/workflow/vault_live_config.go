// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
)

type vaultLiveCollectorConfiguration struct {
	Targets []vaultLiveTargetConfiguration `json:"targets"`
}

type vaultLiveTargetConfiguration struct {
	VaultClusterID string `json:"vault_cluster_id"`
	Namespace      string `json:"namespace"`
	Address        string `json:"address"`
	TokenEnv       string `json:"token_env"`
	Token          string `json:"token"`
	SourceURI      string `json:"source_uri"`
}

// ValidateVaultLiveCollectorConfiguration checks bounded Vault metadata targets
// without resolving credentials or contacting Vault.
func ValidateVaultLiveCollectorConfiguration(raw string) error {
	var decoded vaultLiveCollectorConfiguration
	if err := json.Unmarshal([]byte(normalizeJSONDocument(raw)), &decoded); err != nil {
		return fmt.Errorf("decode vault live collector configuration: %w", err)
	}
	if len(decoded.Targets) == 0 {
		return fmt.Errorf("vault live collector configuration requires targets")
	}
	seen := make(map[string]struct{}, len(decoded.Targets))
	for i, target := range decoded.Targets {
		if err := validateVaultLiveTargetConfiguration(target); err != nil {
			return fmt.Errorf("targets[%d]: %w", i, err)
		}
		key := vaultLiveTargetScopeKey(target)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate vault live target scope")
		}
		seen[key] = struct{}{}
	}
	return nil
}

func validateVaultLiveTargetConfiguration(target vaultLiveTargetConfiguration) error {
	if strings.TrimSpace(target.VaultClusterID) == "" {
		return fmt.Errorf("vault_cluster_id is required")
	}
	if strings.TrimSpace(target.Token) != "" {
		return fmt.Errorf("token must not be serialized in collector configuration")
	}
	if strings.TrimSpace(target.TokenEnv) == "" {
		return fmt.Errorf("token_env is required")
	}
	if err := validateVaultLiveURL("address", target.Address, true); err != nil {
		return err
	}
	return validateVaultLiveURL("source_uri", target.SourceURI, false)
}

func validateVaultLiveURL(field, raw string, required bool) error {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		if required {
			return fmt.Errorf("%s is required", field)
		}
		return nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("parse %s: %w", field, err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return fmt.Errorf("%s must include scheme and host", field)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%s must use http or https", field)
	}
	if parsed.User != nil {
		return fmt.Errorf("%s must not include credentials", field)
	}
	if parsed.RawQuery != "" || parsed.ForceQuery || parsed.Fragment != "" || parsed.RawFragment != "" {
		return fmt.Errorf("%s must not include query or fragment", field)
	}
	return nil
}

func vaultLiveTargetScopeKey(target vaultLiveTargetConfiguration) string {
	return strings.TrimSpace(target.VaultClusterID) + "\x00" + strings.TrimSpace(target.Namespace)
}
