// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sdk

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ephemeralTLSServer starts an httptest TLS server backed by a freshly minted
// self-signed certificate and returns the server plus its CA PEM. All key
// material is created at test runtime and never written to the repository.
func ephemeralTLSServer(t *testing.T) (*httptest.Server, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "eshu-test-localhost"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.IPv4(127, 0, 0, 1), net.IPv6loopback},
		IsCA:                  true,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create certificate: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}}
	server.StartTLS()
	t.Cleanup(server.Close)
	return server, certPEM
}

func TestHTTPClientWithTLSSystemRootsRejectsSelfSigned(t *testing.T) {
	t.Parallel()
	server, _ := ephemeralTLSServer(t)

	client, mode, err := HTTPClientWithTLS(5*time.Second, TLSOptions{})
	if err != nil {
		t.Fatalf("HTTPClientWithTLS() error = %v", err)
	}
	if mode != TLSModeSystem {
		t.Fatalf("mode = %q, want %q", mode, TLSModeSystem)
	}
	if _, err := client.Get(server.URL); err == nil {
		t.Fatal("system-roots client trusted a self-signed cert, want verification failure")
	}
}

func TestHTTPClientWithTLSCustomCATrustsSelfSigned(t *testing.T) {
	t.Parallel()
	server, caPEM := ephemeralTLSServer(t)

	client, mode, err := HTTPClientWithTLS(5*time.Second, TLSOptions{CACertPEM: caPEM})
	if err != nil {
		t.Fatalf("HTTPClientWithTLS() error = %v", err)
	}
	if mode != TLSModeCustomCA {
		t.Fatalf("mode = %q, want %q", mode, TLSModeCustomCA)
	}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("custom-CA client failed to trust its CA: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
}

func TestHTTPClientWithTLSCustomCAFromPath(t *testing.T) {
	t.Parallel()
	server, caPEM := ephemeralTLSServer(t)
	path := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(path, caPEM, 0o600); err != nil {
		t.Fatalf("write CA: %v", err)
	}

	client, mode, err := HTTPClientWithTLS(5*time.Second, TLSOptions{CACertPath: path})
	if err != nil {
		t.Fatalf("HTTPClientWithTLS() error = %v", err)
	}
	if mode != TLSModeCustomCA {
		t.Fatalf("mode = %q, want %q", mode, TLSModeCustomCA)
	}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("path-backed custom CA failed: %v", err)
	}
	_ = resp.Body.Close()
}

func TestHTTPClientWithTLSInsecureSkipVerify(t *testing.T) {
	t.Parallel()
	server, _ := ephemeralTLSServer(t)

	client, mode, err := HTTPClientWithTLS(5*time.Second, TLSOptions{InsecureSkipVerify: true})
	if err != nil {
		t.Fatalf("HTTPClientWithTLS() error = %v", err)
	}
	if mode != TLSModeInsecureSkipVerify {
		t.Fatalf("mode = %q, want %q", mode, TLSModeInsecureSkipVerify)
	}
	resp, err := client.Get(server.URL)
	if err != nil {
		t.Fatalf("skip-verify client failed: %v", err)
	}
	_ = resp.Body.Close()
}

func TestHTTPClientWithTLSRejectsCAWithSkipVerify(t *testing.T) {
	t.Parallel()
	_, caPEM := ephemeralTLSServer(t)

	_, _, err := HTTPClientWithTLS(5*time.Second, TLSOptions{CACertPEM: caPEM, InsecureSkipVerify: true})
	if err == nil {
		t.Fatal("HTTPClientWithTLS() error = nil, want rejection of CA + skip-verify")
	}
}

func TestHTTPClientWithTLSBadCAPath(t *testing.T) {
	t.Parallel()

	_, _, err := HTTPClientWithTLS(5*time.Second, TLSOptions{CACertPath: filepath.Join(t.TempDir(), "missing.pem")})
	if err == nil {
		t.Fatal("HTTPClientWithTLS() error = nil, want unreadable CA path failure")
	}
}

func TestHTTPClientWithTLSEmptyPEMRejected(t *testing.T) {
	t.Parallel()

	_, _, err := HTTPClientWithTLS(5*time.Second, TLSOptions{CACertPEM: []byte("not a certificate")})
	if err == nil {
		t.Fatal("HTTPClientWithTLS() error = nil, want non-certificate PEM failure")
	}
}

func TestTLSOptionsModeReportsCustomCAEvenWithSkipVerify(t *testing.T) {
	t.Parallel()

	got := TLSOptions{CACertPath: "/tmp/ca.pem", InsecureSkipVerify: true}.Mode()
	if got != TLSModeCustomCA {
		t.Fatalf("Mode() = %q, want %q", got, TLSModeCustomCA)
	}
}

// ensure the helper does not depend on a real context cancelation path.
var _ = context.Background
