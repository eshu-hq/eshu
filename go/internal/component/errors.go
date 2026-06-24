// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package component

import (
	"errors"
	"fmt"
)

// ErrorCode is a stable component-manager failure class for CLI and API
// consumers.
type ErrorCode string

const (
	// ErrorCodeInvalidManifest identifies a malformed, unreadable, or
	// contract-invalid component manifest.
	ErrorCodeInvalidManifest ErrorCode = "invalid_manifest"
	// ErrorCodeIncompatibleCore identifies a package whose compatibleCore
	// range does not include the running Eshu core version.
	ErrorCodeIncompatibleCore ErrorCode = "incompatible_core"
	// ErrorCodeRevokedPackage identifies a revoked component ID or publisher.
	ErrorCodeRevokedPackage ErrorCode = "revoked_package"
	// ErrorCodeUntrustedPublisher identifies a package rejected by local trust
	// policy because its ID, publisher, or provenance is not trusted.
	ErrorCodeUntrustedPublisher ErrorCode = "untrusted_publisher"
	// ErrorCodeProvenanceRequired identifies strict trust policy input missing
	// the required provenance verifier, certificate identity, or issuer.
	ErrorCodeProvenanceRequired ErrorCode = "provenance_required"
	// ErrorCodeProvenanceInvalid identifies signature, digest-claim, or
	// provenance verification material that failed validation.
	ErrorCodeProvenanceInvalid ErrorCode = "provenance_invalid"
	// ErrorCodeUnsupportedProvenance identifies signed provenance material that
	// uses an unsupported attestation shape.
	ErrorCodeUnsupportedProvenance ErrorCode = "unsupported_provenance"
	// ErrorCodeFactKindCollision identifies a component fact-kind claim that
	// overlaps with another installed component owner.
	ErrorCodeFactKindCollision ErrorCode = "fact_kind_collision"
	// ErrorCodeActiveUninstall identifies an uninstall attempt for an active
	// component version.
	ErrorCodeActiveUninstall ErrorCode = "active_uninstall"
	// ErrorCodeDuplicateActivation identifies an instance that is already
	// enabled for the component.
	ErrorCodeDuplicateActivation ErrorCode = "duplicate_activation"
	// ErrorCodeCorruptedRegistryState identifies registry JSON that cannot be
	// decoded or read consistently.
	ErrorCodeCorruptedRegistryState ErrorCode = "corrupted_registry_state"
	// ErrorCodeActiveReplacement identifies replacement content for an active
	// installed component version.
	ErrorCodeActiveReplacement ErrorCode = "active_replacement"
	// ErrorCodeNotInstalled identifies an operation targeting a package or
	// version absent from the registry.
	ErrorCodeNotInstalled ErrorCode = "not_installed"
	// ErrorCodeInvalidInput identifies invalid local CLI or registry input.
	ErrorCodeInvalidInput ErrorCode = "invalid_input"
	// ErrorCodeConformanceFailed identifies a component extension conformance
	// run that emitted publication or hosted-activation blockers.
	ErrorCodeConformanceFailed ErrorCode = "conformance_failed"
	// ErrorCodeUnverifiedPackage identifies install input that has not passed
	// local verification.
	ErrorCodeUnverifiedPackage ErrorCode = "unverified_package"
	// ErrorCodeRegistryWriteFailed identifies a failed atomic registry update
	// or package-content write.
	ErrorCodeRegistryWriteFailed ErrorCode = "registry_write_failed"
)

// Error carries a stable component-manager error code and a sanitized message.
type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	cause   error
}

// Error returns the sanitized operator-facing message.
func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Message
}

// Unwrap returns the underlying error for callers that need errors.Is/As.
func (e *Error) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// NewError creates a classified component-manager error.
func NewError(code ErrorCode, message string) error {
	return &Error{Code: code, Message: message}
}

// Errorf creates a classified component-manager error with formatted text.
func Errorf(code ErrorCode, format string, args ...any) error {
	return NewError(code, fmt.Sprintf(format, args...))
}

// WrapError creates a classified error while preserving an underlying cause.
func WrapError(code ErrorCode, message string, cause error) error {
	return &Error{Code: code, Message: message, cause: cause}
}

// ErrorCodeOf returns the stable component-manager error code, when present.
func ErrorCodeOf(err error) ErrorCode {
	var componentErr *Error
	if errors.As(err, &componentErr) {
		return componentErr.Code
	}
	return ""
}

func resultErrorCode(result VerificationResult, fallback ErrorCode) ErrorCode {
	if result.Code != "" {
		return result.Code
	}
	return fallback
}
