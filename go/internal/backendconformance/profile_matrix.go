package backendconformance

import (
	"fmt"
	"strings"
)

// ProfileID names a query/runtime profile that participates in backend
// promotion decisions.
type ProfileID string

const (
	// ProfileLocalAuthoritative is the laptop profile with a managed graph
	// sidecar and embedded stores.
	ProfileLocalAuthoritative ProfileID = "local_authoritative"
	// ProfileLocalFullStack is the Docker Compose profile used for full local
	// service verification.
	ProfileLocalFullStack ProfileID = "local_full_stack"
	// ProfileProduction is the deployed-services profile.
	ProfileProduction ProfileID = "production"
)

// ProfileGateStatus records whether one backend/profile gate is promoted or
// still collecting evidence.
type ProfileGateStatus string

const (
	// ProfileGateStatusUnknown is the zero value and is rejected by validation.
	ProfileGateStatusUnknown ProfileGateStatus = ""
	// ProfileGateStatusPassing means the gate has passing evidence for the
	// current ADR scope.
	ProfileGateStatusPassing ProfileGateStatus = "passing"
	// ProfileGateStatusEvidencePending means the gate has useful evidence but
	// still needs a named proof before promotion.
	ProfileGateStatusEvidencePending ProfileGateStatus = "evidence_pending"
	// ProfileGateStatusBlocked means the gate cannot be promoted until another
	// dependency lands.
	ProfileGateStatusBlocked ProfileGateStatus = "blocked"
)

// ProfileGate records promotion evidence for one backend under one runtime
// profile.
type ProfileGate struct {
	BackendID    BackendID          `yaml:"backend_id"`
	Profile      ProfileID          `yaml:"profile"`
	Status       ProfileGateStatus  `yaml:"status"`
	Verification []Verification     `yaml:"verification"`
	Notes        string             `yaml:"notes"`
	Remaining    []ProfileRemaining `yaml:"remaining"`
}

// ProfileRemaining records one explicit follow-up needed before the gate can
// be described as closed.
type ProfileRemaining struct {
	Item string `yaml:"item"`
}

// RequiredProfileMatrixProfiles returns the profile gates that Chunk 5b must
// track for the NornicDB promotion path.
func RequiredProfileMatrixProfiles() []ProfileID {
	return []ProfileID{
		ProfileLocalAuthoritative,
		ProfileLocalFullStack,
		ProfileProduction,
	}
}

// ProfileGate returns the gate for a backend/profile pair.
func (m Matrix) ProfileGate(backendID BackendID, profile ProfileID) (ProfileGate, bool) {
	for _, gate := range m.ProfileMatrix {
		if gate.BackendID == backendID && gate.Profile == profile {
			return gate, true
		}
	}
	return ProfileGate{}, false
}

// validateProfileMatrix checks the Chunk 5b promotion gates for duplicate rows,
// valid statuses, and explicit evidence.
func (m Matrix) validateProfileMatrix() error {
	if len(m.ProfileMatrix) == 0 {
		return fmt.Errorf("profile_matrix must not be empty")
	}

	knownBackends := make(map[BackendID]struct{}, len(m.Backends))
	for _, backend := range m.Backends {
		knownBackends[backend.ID] = struct{}{}
	}

	seen := make(map[string]struct{}, len(m.ProfileMatrix))
	for _, gate := range m.ProfileMatrix {
		if _, ok := knownBackends[gate.BackendID]; !ok {
			return fmt.Errorf("profile gate backend %q is not defined", gate.BackendID)
		}
		if !validProfile(gate.Profile) {
			return fmt.Errorf("profile gate backend %q has invalid profile %q", gate.BackendID, gate.Profile)
		}
		if !validProfileGateStatus(gate.Status) {
			return fmt.Errorf(
				"profile gate backend %q profile %q has invalid status %q",
				gate.BackendID,
				gate.Profile,
				gate.Status,
			)
		}
		if strings.TrimSpace(gate.Notes) == "" {
			return fmt.Errorf("profile gate backend %q profile %q notes are required", gate.BackendID, gate.Profile)
		}
		if len(gate.Verification) == 0 {
			return fmt.Errorf("profile gate backend %q profile %q verification is required", gate.BackendID, gate.Profile)
		}
		for i, verification := range gate.Verification {
			if err := validateVerification(verification); err != nil {
				return fmt.Errorf(
					"profile gate backend %q profile %q verification %d: %w",
					gate.BackendID,
					gate.Profile,
					i,
					err,
				)
			}
		}
		if gate.Status == ProfileGateStatusPassing && len(gate.Remaining) > 0 {
			return fmt.Errorf("profile gate backend %q profile %q is passing but still lists remaining work", gate.BackendID, gate.Profile)
		}
		if gate.Status != ProfileGateStatusPassing && len(gate.Remaining) == 0 {
			return fmt.Errorf("profile gate backend %q profile %q must list remaining work", gate.BackendID, gate.Profile)
		}
		for i, remaining := range gate.Remaining {
			if strings.TrimSpace(remaining.Item) == "" {
				return fmt.Errorf("profile gate backend %q profile %q remaining %d item is required", gate.BackendID, gate.Profile, i)
			}
		}
		key := string(gate.BackendID) + "/" + string(gate.Profile)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("profile gate %q is duplicated", key)
		}
		seen[key] = struct{}{}
	}

	for _, profile := range RequiredProfileMatrixProfiles() {
		if _, ok := m.ProfileGate(BackendNornicDB, profile); !ok {
			return fmt.Errorf("NornicDB profile gate %q is required", profile)
		}
	}
	return nil
}

// validProfile returns true for profiles that participate in Chunk 5b.
func validProfile(value ProfileID) bool {
	for _, profile := range RequiredProfileMatrixProfiles() {
		if value == profile {
			return true
		}
	}
	return false
}

// validProfileGateStatus returns true for profile promotion states accepted by
// the backend matrix schema.
func validProfileGateStatus(value ProfileGateStatus) bool {
	switch value {
	case ProfileGateStatusPassing,
		ProfileGateStatusEvidencePending,
		ProfileGateStatusBlocked:
		return true
	default:
		return false
	}
}
