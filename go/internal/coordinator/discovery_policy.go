// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// ScannerImageDiscoveryMode names the server-owned source authorization mode.
type ScannerImageDiscoveryMode string

const (
	// ScannerImageDiscoveryDisabled admits no derived running-image discovery.
	ScannerImageDiscoveryDisabled ScannerImageDiscoveryMode = "disabled"
	// ScannerImageDiscoveryLocal requires an exact local source-scope allowlist.
	ScannerImageDiscoveryLocal ScannerImageDiscoveryMode = "local"
	// ScannerImageDiscoveryHosted requires a complete hosted tenant boundary.
	ScannerImageDiscoveryHosted ScannerImageDiscoveryMode = "hosted"
)

// ScannerImageDiscoveryPolicy is the explicit, revisioned coordinator policy.
// A zero-value policy represents the legacy state with no derived discovery;
// an explicit disabled policy is a revisioned authority transition.
type ScannerImageDiscoveryPolicy struct {
	Mode           ScannerImageDiscoveryMode
	Revision       int64
	SourceScopeIDs []string
	Explicit       bool
}

type scannerImageDiscoveryPolicyJSON struct {
	Mode           ScannerImageDiscoveryMode `json:"mode"`
	Revision       int64                     `json:"revision"`
	SourceScopeIDs []string                  `json:"source_scope_ids"`
}

const (
	maxScannerImageDiscoveryPolicyBytes = 1 << 20
	maxScannerImageLocalSourceScopes    = 1024
)

func parseScannerImageDiscoveryPolicy(raw string, boundary WorkflowTenantBoundary) (ScannerImageDiscoveryPolicy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ScannerImageDiscoveryPolicy{Mode: ScannerImageDiscoveryDisabled}, nil
	}
	if len(raw) > maxScannerImageDiscoveryPolicyBytes {
		return ScannerImageDiscoveryPolicy{}, fmt.Errorf("scanner image discovery policy exceeds %d bytes", maxScannerImageDiscoveryPolicyBytes)
	}
	if err := rejectDuplicateScannerPolicyFields(raw); err != nil {
		return ScannerImageDiscoveryPolicy{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var decoded scannerImageDiscoveryPolicyJSON
	if err := decoder.Decode(&decoded); err != nil {
		return ScannerImageDiscoveryPolicy{}, err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return ScannerImageDiscoveryPolicy{}, fmt.Errorf("scanner image discovery policy has trailing JSON")
	}
	policy := ScannerImageDiscoveryPolicy{
		Mode: decoded.Mode, Revision: decoded.Revision,
		SourceScopeIDs: decoded.SourceScopeIDs, Explicit: true,
	}
	if err := policy.validate(boundary); err != nil {
		return ScannerImageDiscoveryPolicy{}, err
	}
	return policy, nil
}

func rejectDuplicateScannerPolicyFields(raw string) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return fmt.Errorf("scanner image discovery policy must be a JSON object")
	}
	seen := make(map[string]struct{}, 3)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("scanner image discovery policy has a non-string key")
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("scanner image discovery policy has duplicate field %q", key)
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}

func (policy ScannerImageDiscoveryPolicy) validate(boundary WorkflowTenantBoundary) error {
	mode := policy.Mode
	if mode == "" && !policy.Explicit {
		mode = ScannerImageDiscoveryDisabled
	}
	if policy.Explicit && policy.Revision <= 0 {
		return fmt.Errorf("scanner image discovery policy revision must be positive")
	}
	if !policy.Explicit && policy.Revision != 0 {
		return fmt.Errorf("implicit scanner image discovery policy cannot carry a revision")
	}
	if !policy.Explicit && mode != ScannerImageDiscoveryDisabled {
		return fmt.Errorf("scanner image discovery requires an explicit revisioned policy")
	}
	switch mode {
	case ScannerImageDiscoveryDisabled:
		if len(policy.SourceScopeIDs) != 0 {
			return fmt.Errorf("disabled scanner image discovery cannot have source_scope_ids")
		}
	case ScannerImageDiscoveryLocal:
		if boundary.configured() {
			return fmt.Errorf("local scanner image discovery cannot use a hosted tenant boundary")
		}
		if len(policy.SourceScopeIDs) == 0 || len(policy.SourceScopeIDs) > maxScannerImageLocalSourceScopes {
			return fmt.Errorf("local scanner image discovery requires 1..%d source_scope_ids", maxScannerImageLocalSourceScopes)
		}
		seen := make(map[string]struct{}, len(policy.SourceScopeIDs))
		for _, rawScopeID := range policy.SourceScopeIDs {
			scopeID := strings.TrimSpace(rawScopeID)
			if scopeID == "" || scopeID != rawScopeID {
				return fmt.Errorf("local scanner image discovery source_scope_ids must be nonempty canonical values")
			}
			if _, duplicate := seen[scopeID]; duplicate {
				return fmt.Errorf("local scanner image discovery has duplicate source_scope_ids")
			}
			seen[scopeID] = struct{}{}
		}
	case ScannerImageDiscoveryHosted:
		if !boundary.configured() {
			return fmt.Errorf("hosted scanner image discovery requires a tenant boundary")
		}
		if len(policy.SourceScopeIDs) != 0 {
			return fmt.Errorf("hosted scanner image discovery cannot have source_scope_ids")
		}
	default:
		return fmt.Errorf("scanner image discovery mode %q is unsupported", mode)
	}
	return nil
}

func scannerInstanceDerivesFromRunningImages(instance workflow.DesiredCollectorInstance) (bool, error) {
	if instance.CollectorKind != scope.CollectorScannerWorker {
		return false, nil
	}
	configuration := strings.TrimSpace(instance.Configuration)
	if configuration == "" {
		return false, nil
	}
	var decoded struct {
		Analyzer                string `json:"analyzer"`
		DeriveFromRunningImages bool   `json:"derive_from_running_images"`
	}
	if err := json.Unmarshal([]byte(configuration), &decoded); err != nil {
		return false, fmt.Errorf("invalid scanner image derivation configuration: %w", err)
	}
	if decoded.DeriveFromRunningImages && decoded.Analyzer != "image_unpacking" {
		return false, fmt.Errorf("running image derivation requires image_unpacking analyzer")
	}
	return decoded.DeriveFromRunningImages, nil
}
