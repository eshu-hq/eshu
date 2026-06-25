// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// TLSMode names the transport trust posture an HTTP collector client uses. It
// is a low-cardinality label safe for spans, logs, and operator dashboards.
type TLSMode string

const (
	// TLSModeSystem trusts the host system certificate pool only. It is the
	// safe default used when no custom trust is configured.
	TLSModeSystem TLSMode = "system_roots"
	// TLSModeCustomCA trusts a caller-supplied PEM bundle in addition to the
	// system pool. Use this for self-signed or private-CA registries such as a
	// local mkcert-backed registry:2 over TLS.
	TLSModeCustomCA TLSMode = "custom_ca"
	// TLSModeInsecureSkipVerify disables server certificate verification. It is
	// test/local-only, never a production default, and must be opted into
	// explicitly. It logs as an insecure posture so operators can detect it.
	TLSModeInsecureSkipVerify TLSMode = "insecure_skip_verify"
)

// TLSOptions describes optional transport trust customization for a collector
// HTTP client. The zero value selects the system trust pool, matching
// DefaultHTTPClient behavior.
//
// Trust precedence is custom-CA over blanket skip-verify: prefer pinning a CA
// bundle. InsecureSkipVerify exists only for throwaway local fixtures and is
// always explicit and default-off.
type TLSOptions struct {
	// CACertPath is a filesystem path to a PEM bundle whose certificates are
	// added to the trusted root pool. Empty means no custom CA.
	CACertPath string
	// CACertPEM is an in-memory PEM bundle added to the trusted root pool. It is
	// used by tests that mint ephemeral certificates without touching disk. When
	// both CACertPath and CACertPEM are set, both are appended.
	CACertPEM []byte
	// InsecureSkipVerify disables certificate verification. It is mutually
	// exclusive with a custom CA in intent: if a CA is supplied it takes
	// precedence and skip-verify is rejected to avoid silently weakening trust.
	InsecureSkipVerify bool
}

// configured reports whether any non-default trust customization was requested.
func (o TLSOptions) configured() bool {
	return strings.TrimSpace(o.CACertPath) != "" || len(o.CACertPEM) > 0 || o.InsecureSkipVerify
}

// Mode returns the resolved TLSMode for the options without building a client.
// It reports custom-CA whenever any CA material is supplied, even alongside the
// skip-verify flag, because the validated path rejects that combination.
func (o TLSOptions) Mode() TLSMode {
	switch {
	case strings.TrimSpace(o.CACertPath) != "" || len(o.CACertPEM) > 0:
		return TLSModeCustomCA
	case o.InsecureSkipVerify:
		return TLSModeInsecureSkipVerify
	default:
		return TLSModeSystem
	}
}

// HTTPClientWithTLS returns a bounded collector HTTP client whose transport
// honors the supplied trust options, plus the resolved TLSMode for telemetry.
//
// When no customization is requested it returns the same shape as
// DefaultHTTPClient. A custom CA bundle is layered on top of the system pool so
// public registries keep working. InsecureSkipVerify is rejected when CA
// material is also supplied, so a caller cannot accidentally disable
// verification while believing the CA is in force.
func HTTPClientWithTLS(timeout time.Duration, options TLSOptions) (*http.Client, TLSMode, error) {
	if timeout <= 0 {
		timeout = defaultHTTPTimeout
	}
	if !options.configured() {
		return &http.Client{Timeout: timeout}, TLSModeSystem, nil
	}

	hasCA := strings.TrimSpace(options.CACertPath) != "" || len(options.CACertPEM) > 0
	if hasCA && options.InsecureSkipVerify {
		return nil, "", fmt.Errorf("tls options: insecure_skip_verify must not be combined with a custom CA; choose one trust mode")
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	mode := TLSModeSystem
	if hasCA {
		pool, err := systemCertPool()
		if err != nil {
			return nil, "", err
		}
		if err := appendCACert(pool, options); err != nil {
			return nil, "", err
		}
		tlsConfig.RootCAs = pool
		mode = TLSModeCustomCA
	}
	if options.InsecureSkipVerify {
		// #nosec G402 -- explicit, default-off, test/local-only opt-in. The
		// caller asked for skip-verify and no CA was supplied; the mode is
		// surfaced in telemetry so an operator can detect it.
		tlsConfig.InsecureSkipVerify = true
		mode = TLSModeInsecureSkipVerify
	}

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = tlsConfig
	return &http.Client{Timeout: timeout, Transport: transport}, mode, nil
}

// systemCertPool returns a copy of the system root pool, falling back to an
// empty pool when the system pool is unavailable so a custom CA still works.
func systemCertPool() (*x509.CertPool, error) {
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	return pool, nil
}

// appendCACert adds the configured PEM material to the pool, failing loudly on
// an unreadable path or PEM bytes that contain no usable certificate.
func appendCACert(pool *x509.CertPool, options TLSOptions) error {
	var appended bool
	if path := strings.TrimSpace(options.CACertPath); path != "" {
		pem, err := os.ReadFile(path) // #nosec G304 -- reads TLS CA bundle at path from operator-supplied TLSOptions configuration
		if err != nil {
			return fmt.Errorf("read TLS CA bundle %q: %w", path, err)
		}
		if !pool.AppendCertsFromPEM(pem) {
			return fmt.Errorf("TLS CA bundle %q contained no usable certificate", path)
		}
		appended = true
	}
	if len(options.CACertPEM) > 0 {
		if !pool.AppendCertsFromPEM(options.CACertPEM) {
			return fmt.Errorf("in-memory TLS CA bundle contained no usable certificate")
		}
		appended = true
	}
	if !appended {
		return fmt.Errorf("TLS CA bundle was empty")
	}
	return nil
}
