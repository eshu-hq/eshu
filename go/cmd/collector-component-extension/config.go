// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"errors"
	"fmt"
	"os"
	goruntime "runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/eshu-hq/eshu/go/internal/collector/extensionhost"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

const (
	envComponentHome              = "ESHU_COMPONENT_HOME"
	envComponentTrustMode         = "ESHU_COMPONENT_TRUST_MODE"
	envComponentAllowIDs          = "ESHU_COMPONENT_ALLOW_IDS"
	envComponentAllowPublishers   = "ESHU_COMPONENT_ALLOW_PUBLISHERS"
	envComponentRevokeIDs         = "ESHU_COMPONENT_REVOKE_IDS"
	envComponentRevokePublishers  = "ESHU_COMPONENT_REVOKE_PUBLISHERS"
	envComponentCoreVersion       = "ESHU_COMPONENT_CORE_VERSION"
	envCollectorInstanceID        = "ESHU_COMPONENT_COLLECTOR_INSTANCE_ID"
	envCollectorOwnerID           = "ESHU_COMPONENT_COLLECTOR_OWNER_ID"
	envCollectorPollInterval      = "ESHU_COMPONENT_COLLECTOR_POLL_INTERVAL"
	envCollectorClaimLeaseTTL     = "ESHU_COMPONENT_COLLECTOR_CLAIM_LEASE_TTL"
	envCollectorHeartbeatInterval = "ESHU_COMPONENT_COLLECTOR_HEARTBEAT_INTERVAL"
	envCollectorScopeKind         = "ESHU_COMPONENT_COLLECTOR_SCOPE_KIND"
)

type runtimeConfig struct {
	ComponentHome      string
	Instance           workflow.DesiredCollectorInstance
	Manifest           component.Manifest
	CollectorKind      scope.CollectorKind
	ScopeKind          scope.ScopeKind
	ConfigHandle       string
	OwnerID            string
	PollInterval       time.Duration
	ClaimLeaseTTL      time.Duration
	HeartbeatInterval  time.Duration
	Runner             extensionhost.Runner
	ExtensionConfig    map[string]any
	ComponentConfig    component.Activation
	ManifestDigest     string
	ComponentPublisher string
}

type activationCandidate struct {
	entry         component.RegistryReadbackComponent
	manifest      component.Manifest
	activation    component.Activation
	collectorKind scope.CollectorKind
}

type componentRuntimeFile struct {
	Host    component.ActivationHostClaimMetadata `json:"host" yaml:"host"`
	Process processRuntimeConfig                  `json:"process" yaml:"process"`
	OCI     ociRuntimeConfig                      `json:"oci" yaml:"oci"`
	Config  map[string]any                        `json:"config" yaml:"config"`
}

// ociRuntimeConfig carries operator-controlled isolation knobs for the OCI
// adapter. The digest-pinned image itself is never taken from this file; it is
// read from the component's verified manifest artifact so a config edit cannot
// repoint the worker at an unverified image.
type ociRuntimeConfig struct {
	Runtime          string   `json:"runtime" yaml:"runtime"`
	Network          string   `json:"network" yaml:"network"`
	User             string   `json:"user" yaml:"user"`
	Env              []string `json:"env" yaml:"env"`
	ExtraArgs        []string `json:"extra_args" yaml:"extra_args"`
	StdoutLimitBytes int64    `json:"stdout_limit_bytes" yaml:"stdout_limit_bytes"`
	StderrLimitBytes int64    `json:"stderr_limit_bytes" yaml:"stderr_limit_bytes"`
}

type processRuntimeConfig struct {
	Command          string   `json:"command" yaml:"command"`
	Args             []string `json:"args" yaml:"args"`
	Env              []string `json:"env" yaml:"env"`
	Dir              string   `json:"dir" yaml:"dir"`
	StdoutLimitBytes int64    `json:"stdout_limit_bytes" yaml:"stdout_limit_bytes"`
	StderrLimitBytes int64    `json:"stderr_limit_bytes" yaml:"stderr_limit_bytes"`
}

func loadRuntimeConfig(getenv func(string) string) (runtimeConfig, error) {
	home := strings.TrimSpace(getenv(envComponentHome))
	if home == "" {
		return runtimeConfig{}, fmt.Errorf("%s is required", envComponentHome)
	}
	candidate, err := selectActivation(home, componentPolicyFromEnv(getenv), strings.TrimSpace(getenv(envCollectorInstanceID)))
	if err != nil {
		return runtimeConfig{}, err
	}
	fileConfig, err := loadComponentRuntimeFile(candidate.activation.ConfigPath)
	if err != nil {
		return runtimeConfig{}, err
	}
	runner, err := runnerForAdapter(candidate.manifest, fileConfig)
	if err != nil {
		return runtimeConfig{}, err
	}
	pollInterval, err := envDuration(getenv, envCollectorPollInterval, time.Second)
	if err != nil {
		return runtimeConfig{}, err
	}
	claimLeaseTTL, err := envDuration(getenv, envCollectorClaimLeaseTTL, workflow.DefaultClaimLeaseTTL())
	if err != nil {
		return runtimeConfig{}, err
	}
	heartbeatInterval, err := envDuration(getenv, envCollectorHeartbeatInterval, workflow.DefaultHeartbeatInterval())
	if err != nil {
		return runtimeConfig{}, err
	}
	if heartbeatInterval >= claimLeaseTTL {
		return runtimeConfig{}, fmt.Errorf("component extension collector heartbeat interval must be less than claim lease TTL")
	}
	return runtimeConfig{
		ComponentHome:      home,
		Instance:           desiredInstance(candidate),
		Manifest:           candidate.manifest,
		CollectorKind:      candidate.collectorKind,
		ScopeKind:          scopeKindFromConfig(fileConfig.Host, getenv),
		ConfigHandle:       component.ActivationConfigHandle(candidate.manifest.Metadata.ID, candidate.manifest.Metadata.Version, candidate.activation),
		OwnerID:            ownerID(getenv),
		PollInterval:       pollInterval,
		ClaimLeaseTTL:      claimLeaseTTL,
		HeartbeatInterval:  heartbeatInterval,
		Runner:             runner,
		ExtensionConfig:    fileConfig.Config,
		ComponentConfig:    candidate.activation,
		ManifestDigest:     candidate.entry.ManifestDigest,
		ComponentPublisher: candidate.manifest.Metadata.Publisher,
	}, nil
}

func selectActivation(
	home string,
	policy component.Policy,
	requestedInstanceID string,
) (activationCandidate, error) {
	readback, err := component.NewRegistry(home).Readback(policy)
	if err != nil {
		return activationCandidate{}, fmt.Errorf("read component registry: %w", err)
	}
	matches := make([]activationCandidate, 0, 1)
	for _, entry := range readback {
		if entry.Error != nil || entry.Verification == nil || !entry.Verification.Allowed {
			continue
		}
		manifest, err := component.LoadManifest(entry.ManifestPath)
		if err != nil {
			return activationCandidate{}, fmt.Errorf("load component manifest %q: %w", entry.ID, err)
		}
		collectorKind, err := collectorKindForManifest(manifest)
		if err != nil {
			return activationCandidate{}, err
		}
		for _, activation := range entry.Activations {
			if !activation.ClaimsEnabled {
				continue
			}
			if requestedInstanceID != "" && activation.InstanceID != requestedInstanceID {
				continue
			}
			matches = append(matches, activationCandidate{
				entry:         entry,
				manifest:      manifest,
				activation:    activation,
				collectorKind: collectorKind,
			})
		}
	}
	switch len(matches) {
	case 0:
		if requestedInstanceID != "" {
			return activationCandidate{}, fmt.Errorf("no trusted claim-capable component activation found for instance %q", requestedInstanceID)
		}
		return activationCandidate{}, fmt.Errorf("no trusted claim-capable component activation found")
	case 1:
		return matches[0], nil
	default:
		return activationCandidate{}, fmt.Errorf("multiple trusted claim-capable component activations found; set %s", envCollectorInstanceID)
	}
}

func collectorKindForManifest(manifest component.Manifest) (scope.CollectorKind, error) {
	if len(manifest.Spec.CollectorKinds) != 1 {
		return "", fmt.Errorf(
			"component %q worker requires exactly one collector kind, got %d",
			manifest.Metadata.ID,
			len(manifest.Spec.CollectorKinds),
		)
	}
	return scope.CollectorKind(strings.TrimSpace(manifest.Spec.CollectorKinds[0])), nil
}

func loadComponentRuntimeFile(path string) (componentRuntimeFile, error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return componentRuntimeFile{}, fmt.Errorf("component activation config path is required for process adapter")
	}
	raw, err := os.ReadFile(trimmed) // #nosec G304 -- trimmed is a validated, operator-supplied config path (CLI flag / env var), not untrusted external input
	if err != nil {
		return componentRuntimeFile{}, fmt.Errorf("read component activation config: %w", sanitizedActivationConfigReadError(err))
	}
	var config componentRuntimeFile
	if err := yaml.Unmarshal(raw, &config); err != nil {
		return componentRuntimeFile{}, fmt.Errorf("decode component activation config: %w", err)
	}
	config.Host = config.Host.Normalized()
	if !config.Host.Empty() {
		if err := config.Host.Validate(); err != nil {
			return componentRuntimeFile{}, err
		}
	}
	return config, nil
}

func sanitizedActivationConfigReadError(err error) error {
	switch {
	case os.IsNotExist(err):
		return errors.New("file does not exist")
	case os.IsPermission(err):
		return errors.New("permission denied")
	default:
		return errors.New("read failed")
	}
}

// runnerForAdapter builds the SDK runner declared by the component's verified
// manifest. The process adapter remains the local-development path; the OCI
// adapter launches the manifest's digest-pinned artifact under container
// isolation. Any other adapter is not runnable by this worker.
func runnerForAdapter(manifest component.Manifest, fileConfig componentRuntimeFile) (extensionhost.Runner, error) {
	switch strings.TrimSpace(manifest.Spec.Runtime.Adapter) {
	case component.RuntimeAdapterProcess:
		return processRunnerFromConfig(fileConfig.Process)
	case component.RuntimeAdapterOCI:
		return ociRunnerFromConfig(manifest, fileConfig.OCI)
	default:
		return nil, fmt.Errorf(
			"component extension adapter %q is not runnable by this worker; only %q and %q are supported",
			manifest.Spec.Runtime.Adapter,
			component.RuntimeAdapterProcess,
			component.RuntimeAdapterOCI,
		)
	}
}

// ociRunnerFromConfig builds an OCI runner whose image is the manifest's
// digest-pinned artifact for the worker's platform. The config file only
// supplies isolation knobs, never the image, so a config edit cannot repoint
// the worker at an unverified artifact.
func ociRunnerFromConfig(manifest component.Manifest, config ociRuntimeConfig) (extensionhost.OCIRunner, error) {
	image, err := ociArtifactImage(manifest)
	if err != nil {
		return extensionhost.OCIRunner{}, err
	}
	return extensionhost.OCIRunner{
		Runtime:          strings.TrimSpace(config.Runtime),
		ImageRef:         image,
		Network:          strings.TrimSpace(config.Network),
		User:             strings.TrimSpace(config.User),
		Env:              trimStringSlice(config.Env),
		ExtraArgs:        trimStringSlice(config.ExtraArgs),
		StdoutLimitBytes: config.StdoutLimitBytes,
		StderrLimitBytes: config.StderrLimitBytes,
	}, nil
}

// ociArtifactImage selects the manifest artifact image for the worker's
// platform, falling back to a single declared artifact. The manifest validator
// already guarantees each artifact image is digest-pinned.
func ociArtifactImage(manifest component.Manifest) (string, error) {
	artifacts := manifest.Spec.Artifacts
	if len(artifacts) == 0 {
		return "", fmt.Errorf("component %q declares no artifacts for the oci adapter", manifest.Metadata.ID)
	}
	platform := goruntime.GOOS + "/" + goruntime.GOARCH
	for _, artifact := range artifacts {
		if strings.TrimSpace(artifact.Platform) == platform {
			return strings.TrimSpace(artifact.Image), nil
		}
	}
	if len(artifacts) == 1 {
		return strings.TrimSpace(artifacts[0].Image), nil
	}
	return "", fmt.Errorf(
		"component %q declares no oci artifact for platform %q",
		manifest.Metadata.ID,
		platform,
	)
}

func processRunnerFromConfig(config processRuntimeConfig) (extensionhost.ProcessRunner, error) {
	if strings.TrimSpace(config.Command) == "" {
		return extensionhost.ProcessRunner{}, fmt.Errorf("component process.command is required")
	}
	return extensionhost.ProcessRunner{
		Command:          strings.TrimSpace(config.Command),
		Args:             trimStringSlice(config.Args),
		Env:              trimStringSlice(config.Env),
		Dir:              strings.TrimSpace(config.Dir),
		StdoutLimitBytes: config.StdoutLimitBytes,
		StderrLimitBytes: config.StderrLimitBytes,
	}, nil
}

func desiredInstance(candidate activationCandidate) workflow.DesiredCollectorInstance {
	return workflow.DesiredCollectorInstance{
		InstanceID:    strings.TrimSpace(candidate.activation.InstanceID),
		CollectorKind: candidate.collectorKind,
		Mode:          workflow.CollectorMode(strings.TrimSpace(candidate.activation.Mode)),
		Enabled:       true,
		ClaimsEnabled: true,
		DisplayName:   strings.TrimSpace(candidate.manifest.Metadata.Name),
	}
}

func componentPolicyFromEnv(getenv func(string) string) component.Policy {
	mode := strings.TrimSpace(getenv(envComponentTrustMode))
	if mode == "" {
		mode = component.TrustModeDisabled
	}
	return component.ConfigureProvenanceFromEnv(component.Policy{
		Mode:              mode,
		AllowedIDs:        envStringList(getenv(envComponentAllowIDs)),
		AllowedPublishers: envStringList(getenv(envComponentAllowPublishers)),
		RevokedIDs:        envStringList(getenv(envComponentRevokeIDs)),
		RevokedPublishers: envStringList(getenv(envComponentRevokePublishers)),
		CoreVersion:       strings.TrimSpace(getenv(envComponentCoreVersion)),
	}, getenv)
}

func envDuration(getenv func(string) string, key string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(getenv(key))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", key, err)
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", key)
	}
	return value, nil
}

func scopeKindFromEnv(getenv func(string) string) scope.ScopeKind {
	if value := strings.TrimSpace(getenv(envCollectorScopeKind)); value != "" {
		return scope.ScopeKind(value)
	}
	return scope.ScopeKind("component")
}

func scopeKindFromConfig(host component.ActivationHostClaimMetadata, getenv func(string) string) scope.ScopeKind {
	host = host.Normalized()
	if !host.Empty() {
		return scope.ScopeKind(host.Scope.Kind)
	}
	return scopeKindFromEnv(getenv)
}

func ownerID(getenv func(string) string) string {
	for _, key := range []string{envCollectorOwnerID, "HOSTNAME"} {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			return value
		}
	}
	return "collector-component-extension"
}

func envStringList(raw string) []string {
	fields := strings.Split(raw, ",")
	values := make([]string, 0, len(fields))
	for _, field := range fields {
		trimmed := strings.TrimSpace(field)
		if trimmed != "" {
			values = append(values, trimmed)
		}
	}
	return values
}

func trimStringSlice(values []string) []string {
	trimmed := make([]string, 0, len(values))
	for _, value := range values {
		if next := strings.TrimSpace(value); next != "" {
			trimmed = append(trimmed, next)
		}
	}
	return trimmed
}
