// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package awsruntime

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/trace"

	"github.com/eshu-hq/eshu/go/internal/collector/awscloud"
	"github.com/eshu-hq/eshu/go/internal/redact"
	"github.com/eshu-hq/eshu/go/internal/telemetry"
)

// DefaultScannerFactory resolves AWS service claims to their production
// scanner adapters through the package-level scanner registry.
//
// The factory holds the runtime-wide tracer, instruments, checkpoint store,
// and redaction key. It carries no per-service knowledge; service bindings
// install themselves into the registry from runtimebind subpackages and the
// factory dispatches through awsruntime.LookupBuilder.
type DefaultScannerFactory struct {
	Tracer      trace.Tracer
	Instruments *telemetry.Instruments
	Checkpoints CheckpointStore
	// RedactionKey produces deterministic markers for sensitive metadata. The
	// services that require redaction (ECS, Lambda, Security Hub,
	// Organizations) validate it inside their builder.
	RedactionKey redact.Key
}

// SupportedServiceKinds returns the service_kind values backed by production
// scanner adapters in registration order. The list is derived from the
// package registry, so adding a scanner needs only its runtimebind import.
func SupportedServiceKinds() []string {
	return RegisteredServiceKinds()
}

// SupportsServiceKind reports whether service is backed by a registered
// production scanner builder.
func SupportsServiceKind(service string) bool {
	_, ok := LookupBuilder(service)
	return ok
}

// Scanner implements ScannerFactory by looking up the registered builder for
// the claim's service_kind and invoking it with the lease's AWS config.
func (f DefaultScannerFactory) Scanner(
	_ context.Context,
	target Target,
	boundary awscloud.Boundary,
	lease CredentialLease,
) (ServiceScanner, error) {
	configLease, ok := lease.(AWSConfigLease)
	if !ok {
		return nil, fmt.Errorf("unsupported AWS credential lease %T", lease)
	}
	build, ok := LookupBuilder(target.ServiceKind)
	if !ok {
		return nil, fmt.Errorf("unsupported AWS service_kind %q", target.ServiceKind)
	}
	return build(ScannerDeps{
		AWSConfig:    configLease.AWSConfig(),
		Boundary:     boundary,
		Tracer:       f.Tracer,
		Instruments:  f.Instruments,
		Checkpoints:  f.Checkpoints,
		RedactionKey: f.RedactionKey,
	})
}
