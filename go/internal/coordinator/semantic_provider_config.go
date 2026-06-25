// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/semanticpolicy"
)

const (
	// EnvSemanticProviderWorkerEnabled turns the semantic-provider execution
	// worker claim loop on. Default false.
	EnvSemanticProviderWorkerEnabled = "ESHU_SEMANTIC_PROVIDER_WORKER_ENABLED"
	// EnvSemanticProviderExecutionEnabled is the explicit, documented, default-OFF
	// flag that permits real outbound provider traffic. It only takes effect when
	// a concrete enabled provider client is also supplied (a future, security-
	// reviewed PR). Default false.
	EnvSemanticProviderExecutionEnabled = "ESHU_SEMANTIC_PROVIDER_EXECUTION_ENABLED"
	// EnvSemanticProviderWorkerScopeIDsJSON is a JSON array of queue scope ids the
	// worker drains.
	EnvSemanticProviderWorkerScopeIDsJSON = "ESHU_SEMANTIC_PROVIDER_WORKER_SCOPE_IDS_JSON"
	// EnvSemanticProviderWorkerLeaseTTL bounds how long a claim is held.
	EnvSemanticProviderWorkerLeaseTTL = "ESHU_SEMANTIC_PROVIDER_WORKER_LEASE_TTL"
	// EnvSemanticProviderWorkerMaxClaimsPerPass bounds how many jobs one pass
	// drains per scope.
	EnvSemanticProviderWorkerMaxClaimsPerPass = "ESHU_SEMANTIC_PROVIDER_WORKER_MAX_CLAIMS_PER_PASS" // #nosec G101 -- environment variable name, not a credential value
	// EnvSemanticProviderWorkerLeaseOwner identifies this worker for lease fencing.
	EnvSemanticProviderWorkerLeaseOwner = "ESHU_SEMANTIC_PROVIDER_WORKER_LEASE_OWNER"

	defaultSemanticProviderWorkerLeaseTTL  = time.Minute
	defaultSemanticProviderWorkerMaxClaims = 32
	defaultSemanticProviderWorkerOwner     = "svc:semantic-provider-worker"
)

// LoadSemanticProviderWorkerConfig parses the egress-gated semantic-provider
// worker config from environment.
//
// The worker is OFF by default: with no env set, Enabled is false and Run is a
// no-op. ExecutionEnabled is independently OFF by default and only permits real
// provider traffic in combination with a concrete enabled client supplied by a
// future security-reviewed PR. The semantic egress policy is loaded from the
// existing ESHU_SEMANTIC_EXTRACTION_POLICY_JSON contract and re-checked at claim
// time; a missing policy makes the worker fail closed on every claim.
func LoadSemanticProviderWorkerConfig(getenv func(string) string) (SemanticProviderWorkerConfig, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	enabled, err := envBool(getenv, EnvSemanticProviderWorkerEnabled, false)
	if err != nil {
		return SemanticProviderWorkerConfig{}, err
	}
	executionEnabled, err := envBool(getenv, EnvSemanticProviderExecutionEnabled, false)
	if err != nil {
		return SemanticProviderWorkerConfig{}, err
	}
	leaseTTL, err := envDuration(getenv, EnvSemanticProviderWorkerLeaseTTL, defaultSemanticProviderWorkerLeaseTTL)
	if err != nil {
		return SemanticProviderWorkerConfig{}, err
	}
	maxClaims, err := envInt(getenv, EnvSemanticProviderWorkerMaxClaimsPerPass, defaultSemanticProviderWorkerMaxClaims)
	if err != nil {
		return SemanticProviderWorkerConfig{}, err
	}
	scopeIDs, err := parseSemanticProviderScopeIDs(getenv(EnvSemanticProviderWorkerScopeIDsJSON))
	if err != nil {
		return SemanticProviderWorkerConfig{}, err
	}
	policy, err := semanticpolicy.LoadFromEnv(getenv)
	if err != nil {
		return SemanticProviderWorkerConfig{}, fmt.Errorf("parse %s: %w", semanticpolicy.EnvPolicyJSON, err)
	}
	leaseOwner := strings.TrimSpace(getenv(EnvSemanticProviderWorkerLeaseOwner))
	if leaseOwner == "" {
		leaseOwner = defaultSemanticProviderWorkerOwner
	}
	cfg := SemanticProviderWorkerConfig{
		Enabled:          enabled,
		ExecutionEnabled: executionEnabled,
		LeaseOwner:       leaseOwner,
		LeaseTTL:         leaseTTL,
		MaxClaimsPerPass: maxClaims,
		ScopeIDs:         scopeIDs,
		Policy:           policy,
	}
	if err := cfg.validate(); err != nil {
		return SemanticProviderWorkerConfig{}, err
	}
	return cfg, nil
}

// validate checks the worker config invariants. An enabled worker must name at
// least one scope and a positive lease TTL so the claim loop cannot spin.
func (c SemanticProviderWorkerConfig) validate() error {
	if !c.Enabled {
		return nil
	}
	if len(c.ScopeIDs) == 0 {
		return fmt.Errorf("%s requires at least one scope id when the worker is enabled", EnvSemanticProviderWorkerScopeIDsJSON)
	}
	if c.LeaseTTL <= 0 {
		return fmt.Errorf("%s must be positive when the worker is enabled", EnvSemanticProviderWorkerLeaseTTL)
	}
	if c.MaxClaimsPerPass <= 0 {
		return fmt.Errorf("%s must be positive when the worker is enabled", EnvSemanticProviderWorkerMaxClaimsPerPass)
	}
	return nil
}

func parseSemanticProviderScopeIDs(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var decoded []string
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, fmt.Errorf("parse %s: %w", EnvSemanticProviderWorkerScopeIDsJSON, err)
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
