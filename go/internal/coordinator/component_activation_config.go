// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator //nolint:dirgate // Root owns registry readback and collector-instance construction.

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	scannercomponent "github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/coordinator/component/activation"
	"github.com/eshu-hq/eshu/go/internal/scope"
	"github.com/eshu-hq/eshu/go/internal/workflow"
)

// StaleActivationSkip describes one claims-enabled registry activation
// dropped from startup planning because its mounted config file is absent
// (#9634). The registry entry outlives the activation file when the values
// tree stops rendering it, and planning it would fail the whole coordinator.
// IDs are the exact stored bytes; presentation layers trim them.
type StaleActivationSkip struct {
	ComponentID string
	InstanceID  string
}

// StaleActivationReconciliation reports one attempt to drop a stale registry
// activation so a restart does not plan it again (#9634). Dropped reports
// whether this call removed the entry; Err is non-nil when the drop failed.
// Dropped false with nil Err means the entry needed no drop: already gone,
// or its file reappeared before the drop and the next restart re-plans it.
// A failed drop never fails startup: the skip already protects planning, so
// the next restart retries the drop.
type StaleActivationReconciliation struct {
	ComponentID string
	InstanceID  string
	Dropped     bool
	Err         error
}

func componentCollectorInstancesFromEnv(getenv func(string) string, observer component.GrantObserver) ([]workflow.DesiredCollectorInstance, []component.Manifest, []StaleActivationSkip, error) {
	home := strings.TrimSpace(getenv("ESHU_COMPONENT_HOME"))
	if home == "" {
		return nil, nil, nil, nil
	}
	registry := component.NewRegistry(home).WithGrantObserver(observer)
	readback, err := registry.Readback(componentPolicyFromEnv(getenv))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("read component registry: %w", err)
	}
	instances := make([]workflow.DesiredCollectorInstance, 0)
	// manifests carries the quota-contributing manifests alongside the
	// instances in readback order, so the quota lookup (#8561) is built
	// from the same single registry pass instead of re-reading grants
	// per component and risking a revocation landing between two reads.
	// A component that plans no claims-enabled instance cannot claim
	// this run, so its manifest contributes no budget and cannot raise
	// a kind conflict (#8582).
	manifests := make([]component.Manifest, 0)
	stale := make([]StaleActivationSkip, 0)
	for _, entry := range readback {
		if entry.Error != nil || entry.Verification == nil || !entry.Verification.Allowed {
			continue
		}
		// Reload through the registry, not component.LoadManifest: the bare
		// loader passes no producer grants and rejects any core-owned fact
		// kind, so a manifest that Readback admitted under a live grant would
		// abort coordinator startup here. LoadInstalledManifest is the loader
		// the extension worker uses, so both planners admit the same set. It
		// re-reads grants per component rather than reusing one snapshot
		// because Readback loads its own state and cannot share it; a
		// revocation landing between the two reads fails closed as a hard
		// error rather than planning an activation without authorization.
		//
		// Only a component with a claims-enabled activation is planned to run,
		// so only it reports activation-stage decisions; the rest report their
		// readback decision alone.
		load := registry.LoadInstalledManifest
		if hasClaimsEnabledActivation(entry) {
			load = registry.LoadInstalledManifestForActivation
		}
		manifest, err := load(entry.ID, entry.Version)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("load component manifest %q: %w", entry.ID, err)
		}
		// The reconcile init container installed this manifest from the
		// seeded manifest.yaml: for eshu.scanner.* it must equal the
		// platform pin, or nothing is planned for it. A drifted seed
		// image stops here, before any instance can claim (#8878).
		if err := scannercomponent.VerifySeededArtifact(manifest); err != nil {
			return nil, nil, nil, fmt.Errorf("verify seeded scanner manifest for %q: %w", entry.ID, err)
		}
		componentInstances, skipped, err := desiredInstancesForComponent(entry, manifest)
		if err != nil {
			return nil, nil, nil, err
		}
		stale = append(stale, skipped...)
		instances = append(instances, componentInstances...)
		if len(componentInstances) > 0 {
			manifests = append(manifests, manifest)
		}
	}
	return instances, manifests, stale, nil
}

// hasClaimsEnabledActivation reports whether the coordinator would plan at
// least one collector instance for the component.
func hasClaimsEnabledActivation(entry component.RegistryReadbackComponent) bool {
	for _, activation := range entry.Activations {
		if activation.ClaimsEnabled {
			return true
		}
	}
	return false
}

func componentPolicyFromEnv(getenv func(string) string) component.Policy {
	mode := strings.TrimSpace(getenv("ESHU_COMPONENT_TRUST_MODE"))
	if mode == "" {
		mode = component.TrustModeDisabled
	}
	return component.ConfigureProvenanceFromEnv(component.Policy{
		Mode:              mode,
		AllowedIDs:        envStringList(getenv("ESHU_COMPONENT_ALLOW_IDS")),
		AllowedPublishers: envStringList(getenv("ESHU_COMPONENT_ALLOW_PUBLISHERS")),
		RevokedIDs:        envStringList(getenv("ESHU_COMPONENT_REVOKE_IDS")),
		RevokedPublishers: envStringList(getenv("ESHU_COMPONENT_REVOKE_PUBLISHERS")),
		CoreVersion:       strings.TrimSpace(getenv("ESHU_COMPONENT_CORE_VERSION")),
	}, getenv)
}

func desiredInstancesForComponent(
	entry component.RegistryReadbackComponent,
	manifest component.Manifest,
) ([]workflow.DesiredCollectorInstance, []StaleActivationSkip, error) {
	if len(entry.Activations) == 0 {
		return nil, nil, nil
	}
	if len(manifest.Spec.CollectorKinds) != 1 {
		return nil, nil, fmt.Errorf(
			"component %q hosted activation requires exactly one collector kind, got %d",
			manifest.Metadata.ID,
			len(manifest.Spec.CollectorKinds),
		)
	}
	collectorKind := scope.CollectorKind(strings.TrimSpace(manifest.Spec.CollectorKinds[0]))
	instances := make([]workflow.DesiredCollectorInstance, 0, len(entry.Activations))
	stale := make([]StaleActivationSkip, 0)
	for _, activation := range entry.Activations {
		if !activation.ClaimsEnabled {
			continue
		}
		if isStaleMissingActivationFile(activation.ConfigPath) {
			// The values tree removed this activation while its registry
			// entry persists (for example a split rename). Skipping keeps
			// one stale reference from crashlooping the whole coordinator
			// (eshu-hq/eshu#9634). Only a missing file skips: unreadable
			// or malformed files still fail closed below, so a real
			// config error can never hide behind this path. The skip is
			// recorded so startup reporting can warn, count, and reconcile
			// the stale registry entry.
			slog.Default().Warn(
				"skipping stale component activation with a missing config file",
				"component_id", manifest.Metadata.ID,
				"instance_id", strings.TrimSpace(activation.InstanceID),
				"config_path", strings.TrimSpace(activation.ConfigPath),
			)
			stale = append(stale, StaleActivationSkip{
				ComponentID: entry.ID,
				InstanceID:  activation.InstanceID,
			})
			continue
		}
		config, err := componentActivationRuntimeConfig(entry, manifest, activation, collectorKind)
		if err != nil {
			return nil, nil, err
		}
		instances = append(instances, workflow.DesiredCollectorInstance{
			InstanceID:    strings.TrimSpace(activation.InstanceID),
			CollectorKind: collectorKind,
			Mode:          workflow.CollectorMode(strings.TrimSpace(activation.Mode)),
			Enabled:       true,
			ClaimsEnabled: true,
			DisplayName:   strings.TrimSpace(manifest.Metadata.Name),
			Configuration: config,
		})
	}
	return instances, stale, nil
}

// ReconcileStaleComponentActivations drops stale registry activations whose
// mounted config files are absent, so a restart does not plan them again
// (#9634). Every skip is revalidated against current registry state before
// any write: an entry that is already gone needs no drop, and an entry
// whose file reappeared (for example a slow volume mount catching up) is
// left for the next restart to plan, never deleted on stale evidence. A
// failed drop is reported on its outcome, never as a hard error: the skip
// already protects planning, so the next restart retries the drop.
func ReconcileStaleComponentActivations(home string, skips []StaleActivationSkip) []StaleActivationReconciliation {
	if len(skips) == 0 {
		return nil
	}
	registry := component.NewRegistry(home)
	installed, err := registry.List()
	if err != nil {
		// Without current registry state nothing can be revalidated, so
		// report every skip as unreconciled instead of deleting blindly.
		// The skip already protects planning; the next restart retries.
		outcomes := make([]StaleActivationReconciliation, 0, len(skips))
		for _, skip := range skips {
			outcomes = append(outcomes, StaleActivationReconciliation{
				ComponentID: skip.ComponentID,
				InstanceID:  skip.InstanceID,
				Err:         err,
			})
		}
		return outcomes
	}
	outcomes := make([]StaleActivationReconciliation, 0, len(skips))
	for _, skip := range skips {
		outcome := StaleActivationReconciliation{
			ComponentID: skip.ComponentID,
			InstanceID:  skip.InstanceID,
		}
		activation, found := findStoredActivation(installed, skip.ComponentID, skip.InstanceID)
		if !found {
			// An entry that is already gone needs no drop: the registry
			// no longer references it, which is the reconciled state.
			outcomes = append(outcomes, outcome)
			continue
		}
		if !isStaleMissingActivationFile(activation.ConfigPath) {
			// The file is back (or the path was never a missing file):
			// leave the entry for the next restart to plan rather than
			// deleting a live activation on stale evidence.
			outcomes = append(outcomes, outcome)
			continue
		}
		// The revalidation holds at check time: a file arriving between
		// this stat and the Disable below is not covered (check-then-act
		// with an external writer). The window is one startup pass and
		// the startup warning always names the entry. Recovery is
		// topology-scoped: activations-mode topologies re-seed the
		// pod-local registry from the ConfigMap on the next start, so a
		// present file re-registers; on durable registries the drop
		// stands until the operator re-enables the activation.
		if err := registry.Disable(skip.ComponentID, skip.InstanceID); err != nil {
			// A concurrent removal between the lookup and the drop is
			// already healed; any other failure is reported, never fatal.
			if component.ErrorCodeOf(err) != component.ErrorCodeNotInstalled {
				outcome.Err = err
			}
		} else {
			outcome.Dropped = true
		}
		outcomes = append(outcomes, outcome)
	}
	return outcomes
}

// findStoredActivation returns the registry activation stored under the
// exact component and instance bytes, mirroring Registry.Disable's match
// semantics across installed versions.
func findStoredActivation(installed []component.InstalledComponent, componentID, instanceID string) (component.Activation, bool) {
	for _, entry := range installed {
		if entry.ID != componentID {
			continue
		}
		for _, activation := range entry.Activations {
			if activation.InstanceID == instanceID {
				return activation, true
			}
		}
	}
	return component.Activation{}, false
}

// isStaleMissingActivationFile reports whether a claims-enabled activation
// names a config file that no longer exists. An empty path names no file at
// all (the loader treats it as absent host metadata), so only a non-empty
// path that stat reports missing counts as stale. Permission and other stat
// errors return false so they keep failing closed in the loader.
func isStaleMissingActivationFile(configPath string) bool {
	path := strings.TrimSpace(configPath)
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

func componentActivationRuntimeConfig(
	entry component.RegistryReadbackComponent,
	manifest component.Manifest,
	registeredActivation component.Activation,
	collectorKind scope.CollectorKind,
) (string, error) {
	// One read of the activation file serves both host and, for a kind with
	// host-planned derived scopes, the config block (#9311).
	file, err := component.LoadActivationRuntimeFile(registeredActivation.ConfigPath)
	if err != nil {
		return "", fmt.Errorf(
			"load component activation host metadata for %q: %w",
			strings.TrimSpace(registeredActivation.InstanceID),
			err,
		)
	}
	config := activation.Config{
		SchemaVersion:    activation.ConfigSchema,
		ComponentID:      manifest.Metadata.ID,
		ComponentVersion: manifest.Metadata.Version,
		Publisher:        manifest.Metadata.Publisher,
		ManifestDigest:   entry.ManifestDigest,
		ConfigHandle:     componentConfigHandle(manifest.Metadata.ID, manifest.Metadata.Version, registeredActivation),
		Runtime: activation.RuntimeConfig{
			SDKProtocol: manifest.Spec.Runtime.SDKProtocol,
			Adapter:     manifest.Spec.Runtime.Adapter,
		},
	}
	if file.HasHost {
		host := file.Host
		config.Host = &host
	}
	if activationCarriesComponentConfig(collectorKind) {
		block, err := file.ConfigJSON()
		if err != nil {
			return "", fmt.Errorf(
				"load component activation config block for %q: %w",
				strings.TrimSpace(registeredActivation.InstanceID),
				err,
			)
		}
		config.ComponentConfig = block
		// The reconcile hook applies the same rules as a backstop; checking
		// here refuses a bad derive block at LoadConfig, before any durable
		// write, the same phase an in-tree instance's bad config fails.
		if _, err := checkActivationDerivation(collectorKind, config); err != nil {
			return "", fmt.Errorf(
				"load component activation config block for %q: %w",
				strings.TrimSpace(registeredActivation.InstanceID),
				err,
			)
		}
	}
	raw, err := json.Marshal(config)
	if err != nil {
		return "", fmt.Errorf("encode component activation configuration: %w", err)
	}
	return string(raw), nil
}

func componentConfigHandle(componentID string, version string, activation component.Activation) string {
	return component.ActivationConfigHandle(componentID, version, activation)
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

func mergeCollectorInstances(
	static []workflow.DesiredCollectorInstance,
	components []workflow.DesiredCollectorInstance,
) ([]workflow.DesiredCollectorInstance, error) {
	if len(components) == 0 {
		return static, nil
	}
	merged := make([]workflow.DesiredCollectorInstance, 0, len(static)+len(components))
	seen := make(map[string]struct{}, len(static)+len(components))
	for _, instance := range append(static, components...) {
		key := strings.TrimSpace(instance.InstanceID)
		if _, ok := seen[key]; ok {
			return nil, fmt.Errorf("duplicate collector instance %q", key)
		}
		seen[key] = struct{}{}
		merged = append(merged, instance)
	}
	return merged, nil
}
