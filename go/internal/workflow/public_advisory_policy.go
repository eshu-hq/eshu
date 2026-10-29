// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package workflow

import (
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/scope"
)

// PublicAdvisoryPolicyReconcile is one monotonic desired-policy transition.
// Instances contains the complete set of advisory-owned collector instances;
// omitting an adopted instance deactivates it and its public registrations.
type PublicAdvisoryPolicyReconcile struct {
	ExpectedRevision         int64
	DesiredRevision          int64
	ExpectedRoleRevision     int64
	ExpectedDatabaseIdentity string
	Instances                []DesiredCollectorInstance
}

// Validate checks the fixed authority and collector input boundary.
func (r PublicAdvisoryPolicyReconcile) Validate() error {
	if r.ExpectedRevision < 0 || r.DesiredRevision <= 0 ||
		r.ExpectedRoleRevision <= 0 || strings.TrimSpace(r.ExpectedDatabaseIdentity) == "" {
		return fmt.Errorf("public advisory policy revision or database identity invalid")
	}
	if len(r.Instances) > 256 {
		return fmt.Errorf("public advisory policy has too many instances")
	}
	seen := make(map[string]struct{}, len(r.Instances))
	for _, instance := range r.Instances {
		if err := instance.Validate(); err != nil {
			return fmt.Errorf("public advisory instance: %w", err)
		}
		if instance.CollectorKind != scope.CollectorVulnerabilityIntelligence {
			return fmt.Errorf("public advisory instance has unsupported collector kind")
		}
		if _, exists := seen[instance.InstanceID]; exists {
			return fmt.Errorf("public advisory policy has duplicate instance")
		}
		seen[instance.InstanceID] = struct{}{}
	}
	return nil
}
