// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"io"
	"strings"

	componentcore "github.com/eshu-hq/eshu/go/internal/component"
)

// The Run functions in this file are the bodies of the `eshu component`
// registry subcommands. go/cmd/eshu resolves every flag, environment
// variable, and stream, then passes plain values here; each function returns
// the error the command exits with, already rendered onto w in the shape the
// operator asked for. None of them read process state, which is what makes
// them testable outside the binary.

// RunInspect prints a manifest's identity line, or the full manifest
// component payload under --json. Inspect reads the manifest only: it
// consults neither the trust policy nor the registry, so a manifest that
// would fail verification still inspects cleanly.
func RunInspect(w io.Writer, jsonOutput bool, manifestPath string) error {
	manifest, err := componentcore.LoadManifest(manifestPath)
	if err != nil {
		return renderError(w, jsonOutput, "inspect", err)
	}
	if jsonOutput {
		payload := newCLIOutput("inspect", "inspected")
		componentPayload := manifestCLIComponent(manifest)
		payload.Component = &componentPayload
		return writeJSON(w, payload)
	}
	return writef(
		w,
		"%s\t%s\t%s\t%s\n",
		manifest.Metadata.ID,
		manifest.Metadata.Name,
		manifest.Metadata.Publisher,
		manifest.Metadata.Version,
	)
}

// RunVerify checks a manifest against the trust policy and reports the
// verification verdict. A policy rejection renders the failed payload and
// returns the verification error the command exits with.
func RunVerify(w io.Writer, jsonOutput bool, policy componentcore.Policy, manifestPath string) error {
	manifest, err := componentcore.LoadManifest(manifestPath)
	if err != nil {
		return renderError(w, jsonOutput, "verify", err)
	}
	result := policy.Verify(manifest)
	if !result.Allowed {
		return renderVerificationError(w, jsonOutput, "verify", result, componentVerificationFailure(result))
	}
	if jsonOutput {
		payload := newCLIOutput("verify", "verified")
		componentPayload := manifestCLIComponent(manifest)
		payload.Component = &componentPayload
		payload.Verification = &result
		return writeJSON(w, payload)
	}
	return writef(
		w, "verified %s@%s with %s policy\n",
		manifest.Metadata.ID,
		manifest.Metadata.Version,
		result.Mode,
	)
}

// RunInstall verifies a manifest and installs it into the registry at home.
// With dryRun the planned result is rendered and nothing is written.
func RunInstall(
	w io.Writer,
	jsonOutput bool,
	dryRun bool,
	home string,
	policy componentcore.Policy,
	manifestPath string,
) error {
	manifest, err := componentcore.LoadManifest(manifestPath)
	if err != nil {
		return renderError(w, jsonOutput, "install", err)
	}
	result := policy.Verify(manifest)
	if !result.Allowed {
		return renderVerificationError(w, jsonOutput, "install", result, componentVerificationFailure(result))
	}
	if dryRun {
		if jsonOutput {
			payload := newCLIOutput("install", "would_install")
			componentPayload := manifestCLIComponent(manifest)
			payload.DryRun = true
			payload.Component = &componentPayload
			payload.Verification = &result
			return writeJSON(w, payload)
		}
		return writef(w, "would install %s@%s\n", manifest.Metadata.ID, manifest.Metadata.Version)
	}
	registry := componentcore.NewRegistry(home)
	installed, err := registry.Install(manifestPath, result)
	if err != nil {
		return renderError(w, jsonOutput, "install", err)
	}
	if jsonOutput {
		payload := newCLIOutput("install", "installed")
		componentPayload := installedCLIComponent(installed, []string{componentcore.RegistryStateInstalled})
		payload.Component = &componentPayload
		payload.Verification = &result
		return writeJSON(w, payload)
	}
	return writef(w, "installed %s@%s\n", installed.ID, installed.Version)
}

// RunList prints every installed component with its trust state and
// activation count, reading the registry at home back through policy.
func RunList(w io.Writer, jsonOutput bool, home string, policy componentcore.Policy) error {
	readback, err := componentcore.NewRegistry(home).Readback(policy)
	if err != nil {
		return renderError(w, jsonOutput, "list", err)
	}
	if jsonOutput {
		payload := newCLIOutput("list", "listed")
		payload.Components = make([]CLIComponent, 0, len(readback))
		for _, entry := range readback {
			payload.Components = append(payload.Components, readbackCLIComponent(entry))
		}
		return writeJSON(w, payload)
	}
	if len(readback) == 0 {
		return writef(w, "no components installed\n")
	}
	for _, installed := range readback {
		if err := writef(
			w,
			"%s\t%s\t%s\tstates=%s\tactivations=%d\n",
			installed.ID,
			installed.Version,
			installed.TrustMode,
			strings.Join(installed.States, ","),
			len(installed.Activations),
		); err != nil {
			return err
		}
	}
	return nil
}

// RunEnable activates a collector instance of an installed component. The
// request's InstanceID is required; with dryRun the activation is planned and
// rendered without writing registry state.
func RunEnable(
	w io.Writer,
	jsonOutput bool,
	dryRun bool,
	home string,
	componentID string,
	request componentcore.Activation,
) error {
	if strings.TrimSpace(request.InstanceID) == "" {
		err := componentcore.Errorf(componentcore.ErrorCodeInvalidInput, "--%s is required", "instance")
		return renderError(w, jsonOutput, "enable", err)
	}
	registry := componentcore.NewRegistry(home)
	var activation componentcore.Activation
	var err error
	if dryRun {
		activation, err = registry.PlanEnable(componentID, request)
	} else {
		activation, err = registry.Enable(componentID, request)
	}
	if err != nil {
		return renderError(w, jsonOutput, "enable", err)
	}
	if jsonOutput {
		status := "enabled"
		if dryRun {
			status = "would_enable"
		}
		payload := newCLIOutput("enable", status)
		payload.DryRun = dryRun
		activationPayload := activationCLIOutput(activation)
		payload.Activation = &activationPayload
		componentPayload := CLIComponent{ID: componentID}
		payload.Component = &componentPayload
		return writeJSON(w, payload)
	}
	if dryRun {
		return writef(w, "would enable %s instance %s\n", componentID, activation.InstanceID)
	}
	return writef(w, "enabled %s instance %s\n", componentID, activation.InstanceID)
}

// RunDisable deactivates one collector instance of an installed component.
// instanceID is required.
func RunDisable(w io.Writer, jsonOutput bool, home string, componentID string, instanceID string) error {
	if strings.TrimSpace(instanceID) == "" {
		err := componentcore.Errorf(componentcore.ErrorCodeInvalidInput, "--%s is required", "instance")
		return renderError(w, jsonOutput, "disable", err)
	}
	if err := componentcore.NewRegistry(home).Disable(componentID, instanceID); err != nil {
		return renderError(w, jsonOutput, "disable", err)
	}
	if jsonOutput {
		payload := newCLIOutput("disable", "disabled")
		componentPayload := CLIComponent{ID: componentID}
		activationPayload := CLIActivation{InstanceID: instanceID}
		payload.Component = &componentPayload
		payload.Activation = &activationPayload
		return writeJSON(w, payload)
	}
	return writef(w, "disabled %s instance %s\n", componentID, instanceID)
}

// RunUninstall removes an inactive component version from the registry.
// version is required.
func RunUninstall(w io.Writer, jsonOutput bool, home string, componentID string, version string) error {
	if strings.TrimSpace(version) == "" {
		err := componentcore.Errorf(componentcore.ErrorCodeInvalidInput, "--%s is required", "version")
		return renderError(w, jsonOutput, "uninstall", err)
	}
	if err := componentcore.NewRegistry(home).Uninstall(componentID, version); err != nil {
		return renderError(w, jsonOutput, "uninstall", err)
	}
	if jsonOutput {
		payload := newCLIOutput("uninstall", "uninstalled")
		componentPayload := CLIComponent{ID: componentID, Version: version}
		payload.Component = &componentPayload
		return writeJSON(w, payload)
	}
	return writef(w, "uninstalled %s@%s\n", componentID, version)
}
