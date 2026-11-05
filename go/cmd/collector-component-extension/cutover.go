// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/claim/store"
	remoteclaim "github.com/eshu-hq/eshu/go/internal/collector/extensionhost/remote/claim"
	"github.com/eshu-hq/eshu/go/internal/component"
)

// cutoverPlan is the resolved enabled-flow wiring for one worker process.
// The zero value is the default-off gate: every route stays in-tree.
type cutoverPlan struct {
	enabled         bool
	remoteURL       string
	producerID      string
	producerVersion string
	scopes          []string
}

// resolveCutoverPlan resolves the cutover gate from the environment against
// the verified manifest. Disabled (unset, blank, or a false spelling) is
// success with a zero plan: the caller keeps the in-tree path and ignores
// every other cutover variable. Enabled requires the claim-plane URL and
// the producer pin, and the pin must match the manifest. There is no third
// outcome: an unrecognized flag spelling is a startup error, not a silent
// default in either direction.
func resolveCutoverPlan(getenv func(string) string, manifest component.Manifest) (cutoverPlan, error) {
	raw := strings.TrimSpace(getenv(envCutoverEnabled))
	if raw == "" {
		return cutoverPlan{}, nil
	}
	enabled, err := strconv.ParseBool(raw)
	if err != nil {
		return cutoverPlan{}, fmt.Errorf(
			"invalid %s: want a Go boolean spelling (1, t, true, 0, f, false)",
			envCutoverEnabled,
		)
	}
	if !enabled {
		return cutoverPlan{}, nil
	}
	remoteURL := strings.TrimSpace(getenv(store.EnvClaimPlaneURL))
	if remoteURL == "" {
		return cutoverPlan{}, fmt.Errorf(
			"%s is required when %s is enabled: the remote client, claim store, and commit fence share one plane",
			store.EnvClaimPlaneURL, envCutoverEnabled,
		)
	}
	producerID := strings.TrimSpace(getenv(envCutoverProducerID))
	if producerID == "" {
		return cutoverPlan{}, fmt.Errorf(
			"%s is required when %s is enabled: the holder identity is pinned, never derived silently",
			envCutoverProducerID, envCutoverEnabled,
		)
	}
	producerVersion := strings.TrimSpace(getenv(envCutoverProducerVersion))
	if producerVersion == "" {
		return cutoverPlan{}, fmt.Errorf(
			"%s is required when %s is enabled: the holder identity is pinned, never derived silently",
			envCutoverProducerVersion, envCutoverEnabled,
		)
	}
	if producerID != strings.TrimSpace(manifest.Metadata.ID) {
		return cutoverPlan{}, fmt.Errorf(
			"%s does not match the verified component manifest; refusing startup rather than proving as another holder",
			envCutoverProducerID,
		)
	}
	if producerVersion != strings.TrimSpace(manifest.Metadata.Version) {
		return cutoverPlan{}, fmt.Errorf(
			"%s does not match the verified component manifest; refusing startup rather than proving as another holder",
			envCutoverProducerVersion,
		)
	}
	scopes := trimStringSlice(manifest.Spec.CollectorKinds)
	if len(scopes) == 0 {
		return cutoverPlan{}, fmt.Errorf(
			"component %q declares no collector kinds for the cutover client",
			manifest.Metadata.ID,
		)
	}
	return cutoverPlan{
		enabled:         true,
		remoteURL:       remoteURL,
		producerID:      producerID,
		producerVersion: producerVersion,
		scopes:          scopes,
	}, nil
}

// cutoverGrantObserver adapts a component.GrantObserver to the seam
// remoteclaim.Observer so enabled-path wiring and emission decisions reach
// the worker's grant telemetry recorder. It carries identifiers only, never
// payloads or credentials, and it never changes an allow/deny outcome: the
// seam observes after deciding, and a nil observer disables observation.
type cutoverGrantObserver struct {
	inner component.GrantObserver
}

// ObserveDecision implements remoteclaim.Observer.
func (a cutoverGrantObserver) ObserveDecision(ctx context.Context, decision remoteclaim.Decision) {
	if a.inner == nil {
		return
	}
	a.inner.ObserveGrantDecision(ctx, component.GrantDecision{
		Stage:      cutoverStageToGrantStage(decision.Stage),
		Allowed:    decision.Allowed,
		Reason:     decision.Reason,
		ProducerID: decision.ProducerID,
		Version:    decision.Version,
		Kind:       decision.Kind,
	})
}

// cutoverStageToGrantStage maps the seam decision stage onto the closed
// component stage set the grant counter labels. Wiring is the seam-side
// counterpart of the in-tree activation gate; emission maps to itself. Any
// future seam stage defaults to activation, never to a new label value.
func cutoverStageToGrantStage(stage remoteclaim.DecisionStage) component.GrantStage {
	if stage == remoteclaim.StageEmission {
		return component.GrantStageEmission
	}
	return component.GrantStageActivation
}

// adaptCutoverObserver binds the worker observer to the seam, or disables
// seam observation when the worker runs without one. A nil observer never
// changes an allow/deny outcome.
func adaptCutoverObserver(observer component.GrantObserver) remoteclaim.Observer {
	if observer == nil {
		return nil
	}
	return cutoverGrantObserver{inner: observer}
}

// buildRemoteCutover binds one remote client behind the resolved plan. A
// disabled plan builds nothing and returns a nil gate: the caller's
// in-tree closures stay exactly as they are. An enabled plan fails closed
// without a client configuration the constructor accepts. Grant re-reads
// go through the component registry on disk, mirroring the in-tree
// LiveGrants closure authority; seam wiring and emission decisions report
// to the worker observer through the adapter above, so the enabled path
// stays visible on the same grant counter, span event, and log line as the
// in-tree path, with no new metric or label value.
func buildRemoteCutover(plan cutoverPlan, registryHome string, observer component.GrantObserver) (*remoteclaim.Cutover, error) {
	if !plan.enabled {
		return nil, nil
	}
	client, err := remoteclaim.NewClient(remoteclaim.Config{
		BaseURL:         plan.remoteURL,
		ProducerID:      plan.producerID,
		ProducerVersion: plan.producerVersion,
		Scopes:          plan.scopes,
		ReadGrants:      remoteclaim.RegistryGrantReader(registryHome),
		Observer:        adaptCutoverObserver(observer),
	})
	if err != nil {
		return nil, err
	}
	return remoteclaim.NewCutover(remoteclaim.CutoverConfig{Enabled: true, Client: client})
}
