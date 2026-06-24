// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociruntime

import (
	"net/http"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// targetHTTPTimeout bounds the per-target OCI Distribution HTTP client. It
// matches the distribution client default so resolving a custom-trust client
// does not silently change request timeouts.
const targetHTTPTimeout = 30 * time.Second

// TLSConfig describes optional transport trust customization for one OCI
// registry target. The zero value selects the host system trust pool, which is
// the safe default for public cloud registries.
//
// It exists so an operator can point the collector at a registry served with a
// private or self-signed CA, such as a local registry:2 over TLS, without a
// cloud account and without weakening trust globally. Prefer pinning a CA
// bundle (CACertPath) over InsecureSkipVerify: the skip-verify knob is
// test/local-only, default-off, and must be opted into explicitly.
type TLSConfig struct {
	// CACertPath is a filesystem path to a PEM bundle whose certificates are
	// trusted in addition to the system pool. Empty means no custom CA.
	CACertPath string
	// InsecureSkipVerify disables server certificate verification. It is
	// test/local-only, never a production default, and is rejected when a custom
	// CA is also supplied so trust cannot be silently weakened.
	InsecureSkipVerify bool
}

// options converts the target TLS configuration into the collector SDK trust
// options that build the underlying HTTP client.
func (c TLSConfig) options() sdk.TLSOptions {
	return sdk.TLSOptions{
		CACertPath:         c.CACertPath,
		InsecureSkipVerify: c.InsecureSkipVerify,
	}
}

// HTTPClient resolves the bounded HTTP client a target uses to reach its
// registry, honoring any configured custom-CA trust, and returns the resolved
// low-cardinality TLSMode for telemetry. The zero TLSConfig yields a
// system-roots client identical to the distribution client default.
func (t TargetConfig) HTTPClient() (*http.Client, sdk.TLSMode, error) {
	return sdk.HTTPClientWithTLS(targetHTTPTimeout, t.TLS.options())
}

// TLSMode reports the resolved transport trust posture for the target without
// building a client. It is a low-cardinality value safe for spans and logs.
func (t TargetConfig) TLSMode() sdk.TLSMode {
	return t.TLS.options().Mode()
}
