package backendconformance

import (
	"fmt"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"
)

// BackendID names an officially tracked graph backend adapter.
type BackendID string

const (
	// BackendNeo4j is the Neo4j compatibility backend.
	BackendNeo4j BackendID = "neo4j"
	// BackendNornicDB is the default NornicDB backend.
	BackendNornicDB BackendID = "nornicdb"
)

// Classification describes how far a backend has progressed through the
// conformance ladder.
type Classification string

const (
	// ClassificationUnsupported means the backend is known but not usable.
	ClassificationUnsupported Classification = "unsupported"
	// ClassificationExperimental means the backend is supported only with open
	// promotion gates or limited evidence.
	ClassificationExperimental Classification = "experimental"
	// ClassificationLocalOnly means the backend is suitable for local workflows
	// but not production.
	ClassificationLocalOnly Classification = "local-only"
	// ClassificationProductionCapable means the backend has production evidence.
	ClassificationProductionCapable Classification = "production-capable"
)

// CapabilityClass names a backend behavior class the conformance suite tracks.
type CapabilityClass string

const (
	// CapabilityCanonicalWrites covers idempotent canonical graph mutation.
	CapabilityCanonicalWrites CapabilityClass = "canonical_writes"
	// CapabilityDirectGraphReads covers bounded label/property graph reads.
	CapabilityDirectGraphReads CapabilityClass = "direct_graph_reads"
	// CapabilityPathTraversal covers bounded path and relationship traversal.
	CapabilityPathTraversal CapabilityClass = "path_traversal"
	// CapabilityFullTextSupport covers graph-native full-text search.
	CapabilityFullTextSupport CapabilityClass = "full_text_support"
	// CapabilityDeadCodeReadiness covers graph shapes needed by dead-code scans.
	CapabilityDeadCodeReadiness CapabilityClass = "dead_code_readiness"
	// CapabilityPerformanceEnvelope covers the backend's measured runtime budget.
	CapabilityPerformanceEnvelope CapabilityClass = "performance_envelope"
)

// CapabilityStatus describes support for one backend capability class.
type CapabilityStatus string

const (
	// CapabilityStatusSupported means the backend is expected to pass the class.
	CapabilityStatusSupported CapabilityStatus = "supported"
	// CapabilityStatusUnsupported means the backend is known not to pass.
	CapabilityStatusUnsupported CapabilityStatus = "unsupported"
	// CapabilityStatusExperimental means the class is still under evaluation.
	CapabilityStatusExperimental CapabilityStatus = "experimental"
	// CapabilityStatusNotRequired means Eshu does not require that backend-native
	// behavior for the current product contract.
	CapabilityStatusNotRequired CapabilityStatus = "not_required"
)

// Matrix is the machine-readable backend conformance contract.
type Matrix struct {
	Version       string        `yaml:"version"`
	UpdatedAt     string        `yaml:"updated_at"`
	Owners        []string      `yaml:"owners"`
	Backends      []Backend     `yaml:"backends"`
	ProfileMatrix []ProfileGate `yaml:"profile_matrix"`
}

// Backend captures conformance status for one graph backend adapter.
type Backend struct {
	ID             BackendID                           `yaml:"id"`
	Default        bool                                `yaml:"default"`
	Official       bool                                `yaml:"official"`
	Classification Classification                      `yaml:"classification"`
	Capabilities   map[CapabilityClass]CapabilityEntry `yaml:"capabilities"`
}

// CapabilityEntry records the status and evidence for one capability class.
type CapabilityEntry struct {
	Status       CapabilityStatus `yaml:"status"`
	Verification []Verification   `yaml:"verification"`
	Notes        string           `yaml:"notes"`
}

// Verification records one proof gate from the backend conformance matrix.
type Verification map[string]string

var allowedVerificationKeys = map[string]struct{}{
	"go_test":           {},
	"integration_test":  {},
	"compose_e2e":       {},
	"remote_validation": {},
}

// ParseMatrix parses a backend conformance matrix YAML document.
func ParseMatrix(raw []byte) (Matrix, error) {
	var matrix Matrix
	if err := yaml.Unmarshal(raw, &matrix); err != nil {
		return Matrix{}, fmt.Errorf("unmarshal backend conformance matrix: %w", err)
	}
	return matrix, nil
}

// ParseCapabilityMatrixBackendIDs extracts graph_backend IDs from the public
// capability matrix so the backend matrix cannot drift from product support.
func ParseCapabilityMatrixBackendIDs(raw []byte) ([]BackendID, error) {
	var document struct {
		GraphBackends []BackendID `yaml:"graph_backends"`
	}
	if err := yaml.Unmarshal(raw, &document); err != nil {
		return nil, fmt.Errorf("unmarshal capability matrix: %w", err)
	}
	return document.GraphBackends, nil
}

// RequiredCapabilityClasses returns the capability classes every tracked
// backend must classify, even when the class is not required by Eshu today.
func RequiredCapabilityClasses() []CapabilityClass {
	return []CapabilityClass{
		CapabilityCanonicalWrites,
		CapabilityDirectGraphReads,
		CapabilityPathTraversal,
		CapabilityFullTextSupport,
		CapabilityDeadCodeReadiness,
		CapabilityPerformanceEnvelope,
	}
}

// Validate checks the backend conformance matrix for required metadata,
// duplicate backends, one default backend, allowed statuses, and required
// capability classes.
func (m Matrix) Validate() error {
	if strings.TrimSpace(m.Version) != "v1" {
		return fmt.Errorf("version must be v1")
	}
	if strings.TrimSpace(m.UpdatedAt) == "" {
		return fmt.Errorf("updated_at is required")
	}
	if len(m.Owners) == 0 {
		return fmt.Errorf("owners must not be empty")
	}
	if len(m.Backends) == 0 {
		return fmt.Errorf("backends must not be empty")
	}

	seen := make(map[BackendID]struct{}, len(m.Backends))
	defaults := 0
	for _, backend := range m.Backends {
		if err := validateBackend(backend); err != nil {
			return err
		}
		if _, ok := seen[backend.ID]; ok {
			return fmt.Errorf("backend %q is duplicated", backend.ID)
		}
		seen[backend.ID] = struct{}{}
		if backend.Default {
			defaults++
		}
	}
	if defaults != 1 {
		return fmt.Errorf("matrix must define exactly one default backend, got %d", defaults)
	}
	if err := m.validateProfileMatrix(); err != nil {
		return err
	}
	return nil
}

// DefaultBackend returns the single backend marked as the default.
func (m Matrix) DefaultBackend() (Backend, error) {
	if err := m.Validate(); err != nil {
		return Backend{}, err
	}
	for _, backend := range m.Backends {
		if backend.Default {
			return backend, nil
		}
	}
	return Backend{}, fmt.Errorf("default backend is not defined")
}

// DiffBackendIDs returns a human-readable difference between the backend IDs
// in this matrix and the backend IDs from the capability matrix.
func (m Matrix) DiffBackendIDs(want []BackendID) string {
	got := make([]string, 0, len(m.Backends))
	for _, backend := range m.Backends {
		got = append(got, string(backend.ID))
	}
	wantStrings := make([]string, 0, len(want))
	for _, backend := range want {
		wantStrings = append(wantStrings, string(backend))
	}
	slices.Sort(got)
	slices.Sort(wantStrings)
	if slices.Equal(got, wantStrings) {
		return ""
	}
	return fmt.Sprintf("got %v, want %v", got, wantStrings)
}

// validateBackend checks one backend row without looking at global uniqueness.
func validateBackend(backend Backend) error {
	if backend.ID == "" {
		return fmt.Errorf("backend id is required")
	}
	if !backend.Official {
		return fmt.Errorf("backend %q must be marked official before conformance", backend.ID)
	}
	if !validClassification(backend.Classification) {
		return fmt.Errorf("backend %q has invalid classification %q", backend.ID, backend.Classification)
	}
	if len(backend.Capabilities) == 0 {
		return fmt.Errorf("backend %q capabilities must not be empty", backend.ID)
	}
	for _, capability := range RequiredCapabilityClasses() {
		entry, ok := backend.Capabilities[capability]
		if !ok {
			return fmt.Errorf("backend %q missing capability %q", backend.ID, capability)
		}
		if err := validateCapabilityEntry(backend.ID, capability, entry); err != nil {
			return err
		}
	}
	return nil
}

// validateCapabilityEntry checks one capability row for useful status,
// verification, and operator-readable notes.
func validateCapabilityEntry(backend BackendID, capability CapabilityClass, entry CapabilityEntry) error {
	if !validCapabilityStatus(entry.Status) {
		return fmt.Errorf("backend %q capability %q has invalid status %q", backend, capability, entry.Status)
	}
	if strings.TrimSpace(entry.Notes) == "" {
		return fmt.Errorf("backend %q capability %q notes are required", backend, capability)
	}
	if entry.Status != CapabilityStatusNotRequired && len(entry.Verification) == 0 {
		return fmt.Errorf("backend %q capability %q verification is required", backend, capability)
	}
	for i, verification := range entry.Verification {
		if err := validateVerification(verification); err != nil {
			return fmt.Errorf("backend %q capability %q verification %d: %w", backend, capability, i, err)
		}
	}
	return nil
}

// validateVerification requires each proof gate to name exactly one known
// verification type and one actionable target.
func validateVerification(verification Verification) error {
	if len(verification) != 1 {
		return fmt.Errorf("must contain exactly one verification key")
	}
	for key, value := range verification {
		key = strings.TrimSpace(key)
		if key == "" {
			return fmt.Errorf("verification key must not be empty")
		}
		if _, ok := allowedVerificationKeys[key]; !ok {
			return fmt.Errorf("unknown verification key %q", key)
		}
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("verification value for %q must not be empty", key)
		}
	}
	return nil
}

// validClassification returns true for classifications named by the ADR.
func validClassification(value Classification) bool {
	switch value {
	case ClassificationUnsupported,
		ClassificationExperimental,
		ClassificationLocalOnly,
		ClassificationProductionCapable:
		return true
	default:
		return false
	}
}

// validCapabilityStatus returns true for capability support statuses accepted
// by the backend matrix schema.
func validCapabilityStatus(value CapabilityStatus) bool {
	switch value {
	case CapabilityStatusSupported,
		CapabilityStatusUnsupported,
		CapabilityStatusExperimental,
		CapabilityStatusNotRequired:
		return true
	default:
		return false
	}
}
