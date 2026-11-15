// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strings"

	scannercomponent "github.com/eshu-hq/eshu/go/internal/collector/scannercomponent"
	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// selectActivation picks the single trusted, claim-capable activation this
// worker runs, verifying each installed manifest's seeded artifact for the
// host platform before it can be claimed (#8878, #10285). requestedInstanceID
// narrows the choice when set.
func selectActivation(
	home string,
	policy component.Policy,
	requestedInstanceID string,
	observer component.GrantObserver,
	platform string,
) (activationCandidate, error) {
	registry := component.NewRegistry(home).WithGrantObserver(observer)
	readback, err := registry.Readback(policy)
	if err != nil {
		return activationCandidate{}, fmt.Errorf("read component registry: %w", err)
	}
	matches := make([]activationCandidate, 0, 1)
	for _, entry := range readback {
		if entry.Error != nil || entry.Verification == nil || !entry.Verification.Allowed {
			continue
		}
		manifest, err := registry.LoadInstalledManifest(entry.ID, entry.Version)
		if err != nil {
			return activationCandidate{}, fmt.Errorf("load component manifest %q: %w", entry.ID, err)
		}
		// The reconcile init container installed this manifest from the
		// seeded manifest.yaml: for eshu.scanner.* it must equal the
		// platform pin, and for a registry-scoped satellite its artifact
		// for this host's platform must equal the satellite pin, or the
		// worker must not claim. A drifted seed image stops here, before
		// any claim or execution (#8878, #10285).
		if err := scannercomponent.VerifySeededArtifact(manifest); err != nil {
			return activationCandidate{}, fmt.Errorf("verify seeded scanner manifest for %q: %w", entry.ID, err)
		}
		if err := scannercomponent.VerifySatelliteSeededArtifact(manifest, platform); err != nil {
			return activationCandidate{}, fmt.Errorf("verify seeded satellite manifest for %q: %w", entry.ID, err)
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
