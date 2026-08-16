// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"io"
	"time"

	componentcore "github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/componentindex"
	"github.com/eshu-hq/eshu/go/internal/extensionconformance"
)

// componentOutputSchemaVersion identifies the JSON payload shape every
// component subcommand emits under --json. Operator scripts key on it, so it
// only moves with a deliberate payload-shape change.
const componentOutputSchemaVersion = "eshu.component.cli.v1"

// CLIOutput is the stable JSON payload every component subcommand writes
// under --json. The schema_version field pins the shape; optional members are
// omitted when the command they serve did not produce them.
type CLIOutput struct {
	SchemaVersion string                            `json:"schema_version"`
	Command       string                            `json:"command"`
	Status        string                            `json:"status"`
	DryRun        bool                              `json:"dry_run,omitempty"`
	Component     *CLIComponent                     `json:"component,omitempty"`
	Components    []CLIComponent                    `json:"components,omitempty"`
	Activation    *CLIActivation                    `json:"activation,omitempty"`
	Verification  *componentcore.VerificationResult `json:"verification,omitempty"`
	Conformance   *extensionconformance.Report      `json:"conformance,omitempty"`
	IndexReport   *componentindex.Report            `json:"index_verification,omitempty"`
	Error         *CLIError                         `json:"error,omitempty"`
}

// CLIComponent is one component as the CLI reports it: manifest identity,
// registry trust state, and any activations the registry knows about.
type CLIComponent struct {
	ID             string          `json:"id"`
	Name           string          `json:"name,omitempty"`
	Publisher      string          `json:"publisher,omitempty"`
	Version        string          `json:"version,omitempty"`
	ManifestDigest string          `json:"manifest_digest,omitempty"`
	Verified       bool            `json:"verified,omitempty"`
	TrustMode      string          `json:"trust_mode,omitempty"`
	InstalledAt    string          `json:"installed_at,omitempty"`
	States         []string        `json:"states,omitempty"`
	Activations    []CLIActivation `json:"activations,omitempty"`
	Error          *CLIError       `json:"error,omitempty"`
}

// CLIActivation is one collector instance activation as the CLI reports it.
type CLIActivation struct {
	InstanceID    string `json:"instance_id"`
	Mode          string `json:"mode"`
	ClaimsEnabled bool   `json:"claims_enabled"`
	ConfigPath    string `json:"config_path,omitempty"`
	EnabledAt     string `json:"enabled_at,omitempty"`
}

// CLIError is the error member of the component CLI payload. Code is the
// component error class an operator script can branch on; Message is rendered
// verbatim.
type CLIError struct {
	Code    componentcore.ErrorCode `json:"code"`
	Message string                  `json:"message"`
}

func newCLIOutput(command string, status string) CLIOutput {
	return CLIOutput{
		SchemaVersion: componentOutputSchemaVersion,
		Command:       command,
		Status:        status,
	}
}

// renderError writes the failed-command JSON payload when --json is on, then
// returns err unchanged so the caller exits with the original failure. In
// text mode nothing is written: the CLI prints the returned error itself.
func renderError(w io.Writer, jsonOutput bool, command string, err error) error {
	if jsonOutput {
		payload := newCLIOutput(command, "failed")
		payload.Error = errorPayload(err)
		if writeErr := writeJSON(w, payload); writeErr != nil {
			return writeErr
		}
	}
	return err
}

// renderVerificationError is renderError for a trust-policy rejection: the
// JSON payload additionally carries the verification result so an operator
// can see which policy check failed.
func renderVerificationError(
	w io.Writer,
	jsonOutput bool,
	command string,
	result componentcore.VerificationResult,
	err error,
) error {
	if jsonOutput {
		payload := newCLIOutput(command, "failed")
		payload.Verification = &result
		payload.Error = errorPayload(err)
		if writeErr := writeJSON(w, payload); writeErr != nil {
			return writeErr
		}
	}
	return err
}

// errorPayload classifies err into the CLI error member. An error without a
// component error code reads as invalid input, which keeps the code field
// non-empty for operator scripts.
func errorPayload(err error) *CLIError {
	code := componentcore.ErrorCodeOf(err)
	if code == "" {
		code = componentcore.ErrorCodeInvalidInput
	}
	return &CLIError{Code: code, Message: err.Error()}
}

func manifestCLIComponent(manifest componentcore.Manifest) CLIComponent {
	return CLIComponent{
		ID:        manifest.Metadata.ID,
		Name:      manifest.Metadata.Name,
		Publisher: manifest.Metadata.Publisher,
		Version:   manifest.Metadata.Version,
	}
}

func installedCLIComponent(installed componentcore.InstalledComponent, states []string) CLIComponent {
	activations := make([]CLIActivation, 0, len(installed.Activations))
	for _, activation := range installed.Activations {
		activations = append(activations, activationCLIOutput(activation))
	}
	return CLIComponent{
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

func readbackCLIComponent(readback componentcore.RegistryReadbackComponent) CLIComponent {
	out := installedCLIComponent(readback.InstalledComponent, readback.States)
	if readback.Error != nil {
		out.Error = &CLIError{
			Code:    readback.Error.Code,
			Message: readback.Error.Message,
		}
	}
	return out
}

func activationCLIOutput(activation componentcore.Activation) CLIActivation {
	return CLIActivation{
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

// componentVerificationFailure turns a trust-policy rejection into the error
// the command exits with. A result without a code reads as an untrusted
// publisher, the broadest rejection class.
func componentVerificationFailure(result componentcore.VerificationResult) error {
	code := result.Code
	if code == "" {
		code = componentcore.ErrorCodeUntrustedPublisher
	}
	return componentcore.Errorf(code, "component verification failed: %s", result.Reason)
}
