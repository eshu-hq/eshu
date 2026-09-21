// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package semantic

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/coordinator/env"
	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
)

const (
	// EnvProviderWorkerEnabled turns the semantic-provider execution
	// worker claim loop on. Default false.
	EnvProviderWorkerEnabled = "ESHU_SEMANTIC_PROVIDER_WORKER_ENABLED"
	// EnvProviderExecutionEnabled is the explicit, documented, default-OFF
	// flag that permits real outbound provider traffic. It only takes effect when
	// a concrete enabled provider client is also supplied (a future, security-
	// reviewed PR). Default false.
	EnvProviderExecutionEnabled = "ESHU_SEMANTIC_PROVIDER_EXECUTION_ENABLED"
	// EnvProviderWorkerScopeIDsJSON is a JSON array of queue scope ids the
	// worker drains.
	EnvProviderWorkerScopeIDsJSON = "ESHU_SEMANTIC_PROVIDER_WORKER_SCOPE_IDS_JSON"
	// EnvProviderWorkerLeaseTTL bounds how long a claim is held.
	EnvProviderWorkerLeaseTTL = "ESHU_SEMANTIC_PROVIDER_WORKER_LEASE_TTL"
	// EnvProviderWorkerMaxClaimsPerPass bounds how many jobs one pass
	// drains per scope.
	EnvProviderWorkerMaxClaimsPerPass = "ESHU_SEMANTIC_PROVIDER_WORKER_MAX_CLAIMS_PER_PASS" // #nosec G101 -- environment variable name, not a credential value
	// EnvProviderWorkerLeaseOwner identifies this worker for lease fencing.
	EnvProviderWorkerLeaseOwner = "ESHU_SEMANTIC_PROVIDER_WORKER_LEASE_OWNER"

	defaultProviderWorkerLeaseTTL  = time.Minute
	defaultProviderWorkerMaxClaims = 32
	defaultProviderWorkerOwner     = "svc:semantic-provider-worker"
)

// LoadProviderWorkerConfig parses the egress-gated semantic-provider
// worker config from environment.
//
// The worker is OFF by default: with no env set, Enabled is false and Run is a
// no-op. ExecutionEnabled is independently OFF by default and only permits real
// provider traffic in combination with a concrete enabled client supplied by a
// future security-reviewed PR. The semantic egress policy is loaded from the
// existing ESHU_SEMANTIC_EXTRACTION_POLICY_JSON contract and re-checked at claim
// time; a missing policy makes the worker fail closed on every claim.
func LoadProviderWorkerConfig(getenv func(string) string) (ProviderWorkerConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	enabled, err := env.Bool(getenv, EnvProviderWorkerEnabled, false)
	if err != nil {
		return ProviderWorkerConfig{}, err
	}
	executionEnabled, err := env.Bool(getenv, EnvProviderExecutionEnabled, false)
	if err != nil {
		return ProviderWorkerConfig{}, err
	}
	leaseTTL, err := env.Duration(getenv, EnvProviderWorkerLeaseTTL, defaultProviderWorkerLeaseTTL)
	if err != nil {
		return ProviderWorkerConfig{}, err
	}
	maxClaims, err := env.Int(getenv, EnvProviderWorkerMaxClaimsPerPass, defaultProviderWorkerMaxClaims)
	if err != nil {
		return ProviderWorkerConfig{}, err
	}
	scopeIDs, err := parseProviderScopeIDs(getenv(EnvProviderWorkerScopeIDsJSON))
	if err != nil {
		return ProviderWorkerConfig{}, err
	}
	policy, err := semanticpolicy.LoadFromEnv(getenv)
	if err != nil {
		return ProviderWorkerConfig{}, fmt.Errorf("parse %s: %w", semanticpolicy.EnvPolicyJSON, err)
	}
	leaseOwner := strings.TrimSpace(getenv(EnvProviderWorkerLeaseOwner))
	if leaseOwner == "" {
		leaseOwner = defaultProviderWorkerOwner
	}
	cfg := ProviderWorkerConfig{
		Enabled:          enabled,
		ExecutionEnabled: executionEnabled,
		LeaseOwner:       leaseOwner,
		LeaseTTL:         leaseTTL,
		MaxClaimsPerPass: maxClaims,
		ScopeIDs:         scopeIDs,
		Policy:           policy,
	}
	if err := cfg.validate(); err != nil {
		return ProviderWorkerConfig{}, err
	}
	return cfg, nil
}

// validate checks the worker config invariants. An enabled worker must name at
// least one scope and a positive lease TTL so the claim loop cannot spin.
func (c ProviderWorkerConfig) validate() error {
	if !c.Enabled {
		return nil
	}
	if len(c.ScopeIDs) == 0 {
		return fmt.Errorf("%s requires at least one scope id when the worker is enabled", EnvProviderWorkerScopeIDsJSON)
	}
	if c.LeaseTTL <= 0 {
		return fmt.Errorf("%s must be positive when the worker is enabled", EnvProviderWorkerLeaseTTL)
	}
	if c.MaxClaimsPerPass <= 0 {
		return fmt.Errorf("%s must be positive when the worker is enabled", EnvProviderWorkerMaxClaimsPerPass)
	}
	return nil
}

func parseProviderScopeIDs(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var decoded []string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("parse %s: %w", EnvProviderWorkerScopeIDsJSON, err)
	}
	scopeIDs := make([]string, 0, len(decoded))
	seen := make(map[string]struct{}, len(decoded))
	for _, candidate := range decoded {
		scopeID := strings.TrimSpace(candidate)
		if scopeID == "" {
			continue
		}
		if _, ok := seen[scopeID]; ok {
			continue
		}
		seen[scopeID] = struct{}{}
		scopeIDs = append(scopeIDs, scopeID)
	}
	return scopeIDs, nil
}
