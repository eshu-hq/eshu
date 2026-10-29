// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// PublicAdvisoryPolicy is the operator-managed, monotonic desired registration
// revision and complete set of advisory-owned vulnerability instances.
type PublicAdvisoryPolicy struct {
	ExpectedRevision int64
	DesiredRevision  int64
	InstanceIDs      []string
	Explicit         bool
}

type publicAdvisoryPolicyJSON struct {
	ExpectedRevision int64    `json:"expected_revision"`
	DesiredRevision  int64    `json:"desired_revision"`
	Instances        []string `json:"instances"`
}

const maxPublicAdvisoryPolicyBytes = 1 << 20

func parsePublicAdvisoryPolicy(raw string,
	instances []workflow.DesiredCollectorInstance,
) (PublicAdvisoryPolicy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return PublicAdvisoryPolicy{}, nil
	}
	if len(raw) > maxPublicAdvisoryPolicyBytes {
		return PublicAdvisoryPolicy{}, fmt.Errorf("public advisory policy exceeds %d bytes", maxPublicAdvisoryPolicyBytes)
	}
	if err := rejectDuplicatePublicAdvisoryPolicyFields(raw); err != nil {
		return PublicAdvisoryPolicy{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	var decoded publicAdvisoryPolicyJSON
	if err := decoder.Decode(&decoded); err != nil {
		return PublicAdvisoryPolicy{}, fmt.Errorf("decode public advisory policy: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return PublicAdvisoryPolicy{}, fmt.Errorf("public advisory policy has trailing JSON")
	}
	policy := PublicAdvisoryPolicy{
		ExpectedRevision: decoded.ExpectedRevision, DesiredRevision: decoded.DesiredRevision,
		InstanceIDs: decoded.Instances, Explicit: true,
	}
	if _, err := policy.selectInstances(instances); err != nil {
		return PublicAdvisoryPolicy{}, err
	}
	return policy, nil
}

func rejectDuplicatePublicAdvisoryPolicyFields(raw string) error {
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token != json.Delim('{') {
		return fmt.Errorf("public advisory policy must be a JSON object")
	}
	seen := make(map[string]struct{}, 3)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		key, ok := token.(string)
		if !ok {
			return fmt.Errorf("public advisory policy has a non-string field")
		}
		if _, duplicate := seen[key]; duplicate {
			return fmt.Errorf("public advisory policy has duplicate field %q", key)
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

func (policy PublicAdvisoryPolicy) selectInstances(
	instances []workflow.DesiredCollectorInstance,
) ([]workflow.DesiredCollectorInstance, error) {
	if !policy.Explicit {
		if policy.ExpectedRevision != 0 || policy.DesiredRevision != 0 || len(policy.InstanceIDs) != 0 {
			return nil, fmt.Errorf("implicit public advisory policy cannot carry authority")
		}
		return nil, nil
	}
	if policy.ExpectedRevision < 0 || policy.DesiredRevision <= policy.ExpectedRevision ||
		len(policy.InstanceIDs) > 256 {
		return nil, fmt.Errorf("public advisory policy revision or instance bound invalid")
	}
	byID := make(map[string]workflow.DesiredCollectorInstance, len(instances))
	for _, instance := range instances {
		byID[instance.InstanceID] = instance
	}
	selected := make([]workflow.DesiredCollectorInstance, 0, len(policy.InstanceIDs))
	seen := make(map[string]struct{}, len(policy.InstanceIDs))
	for _, id := range policy.InstanceIDs {
		if id == "" || id != strings.TrimSpace(id) {
			return nil, fmt.Errorf("public advisory instance ID is not canonical")
		}
		if _, duplicate := seen[id]; duplicate {
			return nil, fmt.Errorf("public advisory policy has duplicate instance ID")
		}
		seen[id] = struct{}{}
		instance, exists := byID[id]
		if !exists || instance.CollectorKind != scope.CollectorVulnerabilityIntelligence {
			return nil, fmt.Errorf("public advisory instance is missing or has unsupported collector kind")
		}
		var config map[string]json.RawMessage
		if err := json.Unmarshal([]byte(instance.Configuration), &config); err != nil {
			return nil, fmt.Errorf("public advisory instance configuration: %w", err)
		}
		targets, declared := config["public_advisory_targets"]
		if !declared {
			return nil, fmt.Errorf("public advisory instance requires explicit public_advisory_targets")
		}
		if err := validatePublicAdvisoryTargets(targets); err != nil {
			return nil, err
		}
		selected = append(selected, instance)
	}
	return selected, nil
}

func validatePublicAdvisoryTargets(raw json.RawMessage) error {
	raw = bytes.TrimSpace(raw)
	var targets []json.RawMessage
	if len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &targets) != nil || len(targets) > 64 {
		return fmt.Errorf("public_advisory_targets must be an array of at most 64 targets")
	}
	for _, rawTarget := range targets {
		rawTarget = bytes.TrimSpace(rawTarget)
		var target map[string]json.RawMessage
		if len(rawTarget) == 0 || rawTarget[0] != '{' || json.Unmarshal(rawTarget, &target) != nil {
			return fmt.Errorf("public advisory target must be an object")
		}
		var scopeID string
		if err := json.Unmarshal(target["scope_id"], &scopeID); err != nil || strings.TrimSpace(scopeID) == "" {
			return fmt.Errorf("public advisory target requires scope_id")
		}
		maxAgeSeconds, err := strconv.Atoi(string(target["max_age_seconds"]))
		if err != nil || maxAgeSeconds < 1 || maxAgeSeconds > 2592000 {
			return fmt.Errorf("public advisory target max_age_seconds must be 1 through 2592000")
		}
	}
	return nil
}
