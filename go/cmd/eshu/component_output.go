// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"encoding/json"
	"io"
	"time"

	"github.com/spf13/cobra"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/componentindex"
	"github.com/eshu-hq/eshu/go/internal/extensionconformance"
)

const componentOutputSchemaVersion = "eshu.component.cli.v1"

type componentCLIOutput struct {
	SchemaVersion string                        `json:"schema_version"`
	Command       string                        `json:"command"`
	Status        string                        `json:"status"`
	DryRun        bool                          `json:"dry_run,omitempty"`
	Component     *componentCLIComponent        `json:"component,omitempty"`
	Components    []componentCLIComponent       `json:"components,omitempty"`
	Activation    *componentCLIActivation       `json:"activation,omitempty"`
	Verification  *component.VerificationResult `json:"verification,omitempty"`
	Conformance   *extensionconformance.Report  `json:"conformance,omitempty"`
	IndexReport   *componentindex.Report        `json:"index_verification,omitempty"`
	Error         *componentCLIError            `json:"error,omitempty"`
}

type componentCLIComponent struct {
	ID             string                   `json:"id"`
	Name           string                   `json:"name,omitempty"`
	Publisher      string                   `json:"publisher,omitempty"`
	Version        string                   `json:"version,omitempty"`
	ManifestDigest string                   `json:"manifest_digest,omitempty"`
	Verified       bool                     `json:"verified,omitempty"`
	TrustMode      string                   `json:"trust_mode,omitempty"`
	InstalledAt    string                   `json:"installed_at,omitempty"`
	States         []string                 `json:"states,omitempty"`
	Activations    []componentCLIActivation `json:"activations,omitempty"`
	Error          *componentCLIError       `json:"error,omitempty"`
}

type componentCLIActivation struct {
	InstanceID    string `json:"instance_id"`
	Mode          string `json:"mode"`
	ClaimsEnabled bool   `json:"claims_enabled"`
	ConfigPath    string `json:"config_path,omitempty"`
	EnabledAt     string `json:"enabled_at,omitempty"`
}

type componentCLIError struct {
	Code    component.ErrorCode `json:"code"`
	Message string              `json:"message"`
}

func newComponentCLIOutput(command string, status string) componentCLIOutput {
	return componentCLIOutput{
		SchemaVersion: componentOutputSchemaVersion,
		Command:       command,
		Status:        status,
	}
}

func componentJSONEnabled(cmd *cobra.Command) bool {
	if cmd.Flags().Lookup(componentJSONFlag) == nil {
		return false
	}
	enabled, _ := cmd.Flags().GetBool(componentJSONFlag)
	return enabled
}

func componentDryRunEnabled(cmd *cobra.Command) bool {
	if cmd.Flags().Lookup(componentDryRunFlag) == nil {
		return false
	}
	enabled, _ := cmd.Flags().GetBool(componentDryRunFlag)
	return enabled
}

func writeComponentJSON(w io.Writer, payload componentCLIOutput) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	return encoder.Encode(payload)
}

func renderComponentError(cmd *cobra.Command, command string, err error) error {
	if componentJSONEnabled(cmd) {
		payload := newComponentCLIOutput(command, "failed")
		payload.Error = componentErrorPayload(err)
		if writeErr := writeComponentJSON(cmd.OutOrStdout(), payload); writeErr != nil {
			return writeErr
		}
	}
	return err
}

func renderComponentVerificationError(
	cmd *cobra.Command,
	command string,
	result component.VerificationResult,
	err error,
) error {
	if componentJSONEnabled(cmd) {
		payload := newComponentCLIOutput(command, "failed")
		payload.Verification = &result
		payload.Error = componentErrorPayload(err)
		if writeErr := writeComponentJSON(cmd.OutOrStdout(), payload); writeErr != nil {
			return writeErr
		}
	}
	return err
}

func componentErrorPayload(err error) *componentCLIError {
	code := component.ErrorCodeOf(err)
	if code == "" {
		code = component.ErrorCodeInvalidInput
	}
	return &componentCLIError{Code: code, Message: err.Error()}
}

func manifestCLIComponent(manifest component.Manifest) componentCLIComponent {
	return componentCLIComponent{
		ID:        manifest.Metadata.ID,
		Name:      manifest.Metadata.Name,
		Publisher: manifest.Metadata.Publisher,
		Version:   manifest.Metadata.Version,
	}
}

func installedCLIComponent(installed component.InstalledComponent, states []string) componentCLIComponent {
	activations := make([]componentCLIActivation, 0, len(installed.Activations))
	for _, activation := range installed.Activations {
		activations = append(activations, activationCLIOutput(activation))
	}
	return componentCLIComponent{
		ID:             installed.ID,
		Name:           installed.Name,
		Publisher:      installed.Publisher,
		Version:        installed.Version,
		ManifestDigest: installed.ManifestDigest,
		Verified:       installed.Verified,
		TrustMode:      installed.TrustMode,
		InstalledAt:    formatComponentTime(installed.InstalledAt),
		States:         states,
		Activations:    activations,
	}
}

func readbackCLIComponent(readback component.RegistryReadbackComponent) componentCLIComponent {
	out := installedCLIComponent(readback.InstalledComponent, readback.States)
	if readback.Error != nil {
		out.Error = &componentCLIError{
			Code:    readback.Error.Code,
			Message: readback.Error.Message,
		}
	}
	return out
}

func activationCLIOutput(activation component.Activation) componentCLIActivation {
	return componentCLIActivation{
		InstanceID:    activation.InstanceID,
		Mode:          activation.Mode,
		ClaimsEnabled: activation.ClaimsEnabled,
		ConfigPath:    activation.ConfigPath,
		EnabledAt:     formatComponentTime(activation.EnabledAt),
	}
}

func formatComponentTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func componentVerificationFailure(result component.VerificationResult) error {
	code := result.Code
	if code == "" {
		code = component.ErrorCodeUntrustedPublisher
	}
	return component.Errorf(code, "component verification failed: %s", result.Reason)
}
