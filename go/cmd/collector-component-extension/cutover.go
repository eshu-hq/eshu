// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/collector/claim/store"
	remoteclaim "github.com/eshu-hq/eshu/go/internal/collector/extensionhost/remote/claim"
	"github.com/eshu-hq/eshu/go/internal/component"
)

const (
	// envCutoverEnabled is the default-off gate for the extension-host
	// enabled flow (eshu-hq/eshu#8877 leaf 1). Unset or blank keeps the
	// in-tree claim path byte-identically: no remote client is built and
	// the URL-set-refuses-startup law stays in force. A set value must be
	// a Go boolean spelling (strconv.ParseBool); anything else fails
	// startup closed naming this variable.
	envCutoverEnabled = "ESHU_EXTENSIONHOST_CUTOVER_ENABLED"
	// envCutoverProducerID and envCutoverProducerVersion pin the remote
	// client's holder identity. Both are required when the gate is
	// enabled and both must equal the verified manifest's metadata,
	// or startup fails closed naming only the variable. The pin keeps a
	// misconfigured host from proving as another holder; the refused
	// value never appears in the error.
	envCutoverProducerID      = "ESHU_EXTENSIONHOST_CUTOVER_PRODUCER_ID"
	envCutoverProducerVersion = "ESHU_EXTENSIONHOST_CUTOVER_PRODUCER_VERSION"
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

// buildRemoteCutover binds one remote client behind the resolved plan. A
// disabled plan builds nothing and returns a nil gate: the caller's
// in-tree closures stay exactly as they are. An enabled plan fails closed
// without a client configuration the constructor accepts. Grant re-reads
// go through the component registry on disk, mirroring the in-tree
// LiveGrants closure authority; the grant observer backing that makes
// seam decisions visible to operators lands with the telemetry leaf
// (leaf 5), so Observer stays nil here by explicit deferral, not by
// omission.
func buildRemoteCutover(plan cutoverPlan, registryHome string) (*remoteclaim.Cutover, error) {
	if !plan.enabled {
		return nil, nil
	}
	client, err := remoteclaim.NewClient(remoteclaim.Config{
		BaseURL:         plan.remoteURL,
		ProducerID:      plan.producerID,
		ProducerVersion: plan.producerVersion,
		Scopes:          plan.scopes,
		ReadGrants:      remoteclaim.RegistryGrantReader(registryHome),
	})
	if err != nil {
		return nil, err
	}
	return remoteclaim.NewCutover(remoteclaim.CutoverConfig{Enabled: true, Client: client})
}
