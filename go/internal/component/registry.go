// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"golang.org/x/mod/semver"
)

const registryFileName = "registry.json"

// Registry manages a local component installation home.
type Registry struct {
	home string
}

// InstalledComponent is one locally installed component package.
type InstalledComponent struct {
	ID             string       `json:"id"`
	Name           string       `json:"name"`
	Publisher      string       `json:"publisher"`
	Version        string       `json:"version"`
	ManifestPath   string       `json:"manifest_path"`
	ManifestDigest string       `json:"manifest_digest"`
	Verified       bool         `json:"verified"`
	TrustMode      string       `json:"trust_mode"`
	InstalledAt    time.Time    `json:"installed_at"`
	Activations    []Activation `json:"activations,omitempty"`
}

// Activation records one enabled runtime instance for a component.
type Activation struct {
	InstanceID    string    `json:"instance_id"`
	Mode          string    `json:"mode"`
	ClaimsEnabled bool      `json:"claims_enabled"`
	ConfigPath    string    `json:"config_path,omitempty"`
	EnabledAt     time.Time `json:"enabled_at"`
}

type registryState struct {
	Components []InstalledComponent `json:"components"`
}

// NewRegistry creates a local component registry rooted at home.
func NewRegistry(home string) Registry {
	return Registry{home: filepath.Clean(strings.TrimSpace(home))}
}

// Install validates, copies, and records a verified component manifest.
func (r Registry) Install(manifestPath string, verification VerificationResult) (InstalledComponent, error) {
	if !verification.Allowed {
		return InstalledComponent{}, Errorf(
			resultErrorCode(verification, ErrorCodeUnverifiedPackage),
			"component package is not verified: %s",
			verification.Reason,
		)
	}
	manifest, err := LoadManifest(manifestPath)
	if err != nil {
		return InstalledComponent{}, err
	}
	if verification.Component != manifest.Metadata.ID ||
		verification.Publisher != manifest.Metadata.Publisher ||
		verification.Version != manifest.Metadata.Version {
		return InstalledComponent{}, NewError(ErrorCodeUnverifiedPackage, "verification result does not match manifest")
	}
	raw, err := os.ReadFile(manifestPath)
	if err != nil {
		return InstalledComponent{}, WrapError(ErrorCodeInvalidManifest, "read component manifest", err)
	}
	manifestDigest := sha256Hex(raw)
	state, err := r.load()
	if err != nil {
		return InstalledComponent{}, err
	}
	if err := r.validateInstallFactKindClaims(manifest, state); err != nil {
		return InstalledComponent{}, err
	}
	if existing := state.findVersion(manifest.Metadata.ID, manifest.Metadata.Version); existing != nil &&
		len(existing.Activations) > 0 &&
		existing.ManifestDigest != manifestDigest {
		return InstalledComponent{}, Errorf(
			ErrorCodeActiveReplacement,
			"component %q version %q is active; disable it before replacing package content",
			manifest.Metadata.ID,
			manifest.Metadata.Version,
		)
	}
	installed := InstalledComponent{
		ID:             manifest.Metadata.ID,
		Name:           manifest.Metadata.Name,
		Publisher:      manifest.Metadata.Publisher,
		Version:        manifest.Metadata.Version,
		ManifestPath:   r.manifestPath(manifest.Metadata.ID, manifest.Metadata.Version),
		ManifestDigest: manifestDigest,
		Verified:       true,
		TrustMode:      verification.Mode,
		InstalledAt:    time.Now().UTC(),
	}
	if err := os.MkdirAll(filepath.Dir(installed.ManifestPath), 0o755); err != nil {
		return InstalledComponent{}, WrapError(ErrorCodeRegistryWriteFailed, "create component package directory", err)
	}
	if err := os.WriteFile(installed.ManifestPath, raw, 0o600); err != nil {
		return InstalledComponent{}, WrapError(ErrorCodeRegistryWriteFailed, "copy component manifest", err)
	}
	state.upsert(installed)
	if err := r.save(state); err != nil {
		return InstalledComponent{}, err
	}
	return installed, nil
}

// List returns installed components in stable ID/version order.
func (r Registry) List() ([]InstalledComponent, error) {
	state, err := r.load()
	if err != nil {
		return nil, err
	}
	components := append([]InstalledComponent(nil), state.Components...)
	sortComponents(components)
	return components, nil
}

// Enable records one active component instance.
func (r Registry) Enable(componentID string, activation Activation) (Activation, error) {
	state, err := r.load()
	if err != nil {
		return Activation{}, err
	}
	component, activation, err := state.prepareEnable(componentID, activation)
	if err != nil {
		return Activation{}, err
	}
	if err := r.validateEnableFactKindClaims(*component, state); err != nil {
		return Activation{}, err
	}
	component.upsertActivation(activation)
	if err := r.save(state); err != nil {
		return Activation{}, err
	}
	return activation, nil
}

// PlanEnable validates an activation without writing registry state.
func (r Registry) PlanEnable(componentID string, activation Activation) (Activation, error) {
	state, err := r.load()
	if err != nil {
		return Activation{}, err
	}
	component, activation, err := state.prepareEnable(componentID, activation)
	if err != nil {
		return Activation{}, err
	}
	if err := r.validateEnableFactKindClaims(*component, state); err != nil {
		return Activation{}, err
	}
	return activation, nil
}

func (s *registryState) prepareEnable(componentID string, activation Activation) (*InstalledComponent, Activation, error) {
	if err := validateIdentifier("component id", componentID); err != nil {
		return nil, Activation{}, WrapError(ErrorCodeInvalidInput, err.Error(), err)
	}
	if err := validateIdentifier("instance_id", activation.InstanceID); err != nil {
		return nil, Activation{}, WrapError(ErrorCodeInvalidInput, err.Error(), err)
	}
	if strings.TrimSpace(activation.Mode) == "" {
		activation.Mode = "manual"
	}
	if err := validateIdentifier("mode", activation.Mode); err != nil {
		return nil, Activation{}, WrapError(ErrorCodeInvalidInput, err.Error(), err)
	}
	if activation.EnabledAt.IsZero() {
		activation.EnabledAt = time.Now().UTC()
	}
	component := s.findLatest(componentID)
	if component == nil {
		return nil, Activation{}, Errorf(ErrorCodeNotInstalled, "component %q is not installed", componentID)
	}
	if activeComponent := s.findActivation(componentID, activation.InstanceID); activeComponent != nil {
		return nil, Activation{}, Errorf(
			ErrorCodeDuplicateActivation,
			"activation %q is already enabled for component %q version %q",
			activation.InstanceID,
			componentID,
			activeComponent.Version,
		)
	}
	return component, activation, nil
}

// Disable removes one active component instance.
func (r Registry) Disable(componentID, instanceID string) error {
	if err := validateIdentifier("component id", componentID); err != nil {
		return WrapError(ErrorCodeInvalidInput, err.Error(), err)
	}
	if err := validateIdentifier("instance_id", instanceID); err != nil {
		return WrapError(ErrorCodeInvalidInput, err.Error(), err)
	}
	state, err := r.load()
	if err != nil {
		return err
	}
	components := state.findActivations(componentID, instanceID)
	if len(components) == 0 {
		if state.findLatest(componentID) == nil {
			return Errorf(ErrorCodeNotInstalled, "component %q is not installed", componentID)
		}
		return Errorf(ErrorCodeNotInstalled, "activation %q is not enabled for component %q", instanceID, componentID)
	}
	if len(components) > 1 {
		return Errorf(ErrorCodeDuplicateActivation, "activation %q is enabled for component %q on multiple versions", instanceID, componentID)
	}
	component := components[0]
	if !component.removeActivation(instanceID) {
		return Errorf(ErrorCodeNotInstalled, "activation %q is not enabled for component %q", instanceID, componentID)
	}
	return r.save(state)
}

// Uninstall removes an inactive component version.
func (r Registry) Uninstall(componentID, version string) error {
	state, err := r.load()
	if err != nil {
		return err
	}
	index := -1
	for i := range state.Components {
		component := state.Components[i]
		if component.ID == componentID && component.Version == version {
			if len(component.Activations) > 0 {
				return Errorf(ErrorCodeActiveUninstall, "component %q version %q is active", componentID, version)
			}
			index = i
			break
		}
	}
	if index < 0 {
		return Errorf(ErrorCodeNotInstalled, "component %q version %q is not installed", componentID, version)
	}
	state.Components = append(state.Components[:index], state.Components[index+1:]...)
	if err := r.save(state); err != nil {
		return err
	}
	packageDir := filepath.Dir(r.manifestPath(componentID, version))
	if err := os.RemoveAll(packageDir); err != nil {
		return WrapError(ErrorCodeRegistryWriteFailed, "remove component package directory", err)
	}
	return nil
}

func (r Registry) load() (registryState, error) {
	if r.home == "" || r.home == "." {
		return registryState{}, NewError(ErrorCodeInvalidInput, "component home is required")
	}
	raw, err := os.ReadFile(r.registryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return registryState{}, nil
		}
		return registryState{}, WrapError(ErrorCodeCorruptedRegistryState, "read component registry", err)
	}
	var state registryState
	if err := json.Unmarshal(raw, &state); err != nil {
		return registryState{}, WrapError(ErrorCodeCorruptedRegistryState, "decode component registry", err)
	}
	return state, nil
}

func (r Registry) save(state registryState) error {
	if err := os.MkdirAll(r.home, 0o755); err != nil {
		return WrapError(ErrorCodeRegistryWriteFailed, "create component home", err)
	}
	sortComponents(state.Components)
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return WrapError(ErrorCodeCorruptedRegistryState, "encode component registry", err)
	}
	tmp := r.registryPath() + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o600); err != nil {
		return WrapError(ErrorCodeRegistryWriteFailed, "write component registry", err)
	}
	if err := replaceRegistryFile(tmp, r.registryPath()); err != nil {
		return WrapError(ErrorCodeRegistryWriteFailed, "commit component registry", err)
	}
	return nil
}

func (r Registry) registryPath() string {
	return filepath.Join(r.home, registryFileName)
}

func (r Registry) manifestPath(componentID, version string) string {
	return filepath.Join(r.home, "packages", componentID, version, "manifest.yaml")
}

// ActivationConfigHandle returns the stable, non-secret handle used by hosted
// coordinator and worker paths for one component activation configuration.
func ActivationConfigHandle(componentID string, version string, activation Activation) string {
	return "component-config:" + facts.StableID("ComponentActivationConfig", map[string]any{
		"component_id": componentID,
		"version":      version,
		"instance_id":  strings.TrimSpace(activation.InstanceID),
		"config_path":  strings.TrimSpace(activation.ConfigPath),
	})
}

func (s *registryState) upsert(component InstalledComponent) {
	for i := range s.Components {
		if s.Components[i].ID == component.ID && s.Components[i].Version == component.Version {
			component.Activations = s.Components[i].Activations
			s.Components[i] = component
			return
		}
	}
	s.Components = append(s.Components, component)
}

func (s *registryState) findLatest(componentID string) *InstalledComponent {
	var latest *InstalledComponent
	for i := range s.Components {
		if s.Components[i].ID != componentID {
			continue
		}
		if latest == nil || compareVersions(s.Components[i].Version, latest.Version) > 0 {
			latest = &s.Components[i]
		}
	}
	return latest
}

func (s *registryState) findVersion(componentID, version string) *InstalledComponent {
	for i := range s.Components {
		if s.Components[i].ID == componentID && s.Components[i].Version == version {
			return &s.Components[i]
		}
	}
	return nil
}

func (s *registryState) findActivation(componentID, instanceID string) *InstalledComponent {
	components := s.findActivations(componentID, instanceID)
	if len(components) == 0 {
		return nil
	}
	return components[0]
}

func (s *registryState) findActivations(componentID, instanceID string) []*InstalledComponent {
	var components []*InstalledComponent
	for i := range s.Components {
		if s.Components[i].ID != componentID {
			continue
		}
		for _, activation := range s.Components[i].Activations {
			if activation.InstanceID == instanceID {
				components = append(components, &s.Components[i])
				break
			}
		}
	}
	return components
}

func (c *InstalledComponent) upsertActivation(activation Activation) {
	for i := range c.Activations {
		if c.Activations[i].InstanceID == activation.InstanceID {
			c.Activations[i] = activation
			return
		}
	}
	c.Activations = append(c.Activations, activation)
}

func (c *InstalledComponent) removeActivation(instanceID string) bool {
	for i := range c.Activations {
		if c.Activations[i].InstanceID == instanceID {
			c.Activations = append(c.Activations[:i], c.Activations[i+1:]...)
			return true
		}
	}
	return false
}

func sha256Hex(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func compareVersions(left, right string) int {
	leftVersion := normalizeSemver(left)
	rightVersion := normalizeSemver(right)
	if semver.IsValid(leftVersion) && semver.IsValid(rightVersion) {
		return semver.Compare(leftVersion, rightVersion)
	}
	if left > right {
		return 1
	}
	if left < right {
		return -1
	}
	return 0
}

func normalizeSemver(version string) string {
	trimmed := strings.TrimSpace(version)
	if strings.HasPrefix(trimmed, "v") {
		return trimmed
	}
	return "v" + trimmed
}

func sortComponents(components []InstalledComponent) {
	sort.Slice(components, func(i, j int) bool {
		if components[i].ID == components[j].ID {
			return compareVersions(components[i].Version, components[j].Version) < 0
		}
		return components[i].ID < components[j].ID
	})
}

func replaceRegistryFile(tmpPath, targetPath string) error {
	if err := os.Rename(tmpPath, targetPath); err == nil {
		return nil
	} else if runtime.GOOS != "windows" {
		return err
	}
	if err := os.Remove(targetPath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.Rename(tmpPath, targetPath)
}
