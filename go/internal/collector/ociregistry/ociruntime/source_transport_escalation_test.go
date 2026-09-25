// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociruntime

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
	"github.com/eshu-hq/eshu/go/internal/collector/sdk"
)

// connectionRefused builds the error net/http returns when nothing listens on
// the registry address, the shape of a wrong host or port.
func connectionRefused() error {
	return collector.RegistryTransportFailure("oci", "", "ping", &url.Error{
		Op: "Get", URL: "https://registry.internal.example/v2/",
		Err: &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)},
	})
}

func TestSourceNextSkipsTransientTargetWithinTheSameCall(t *testing.T) {
	t.Parallel()

	healthy := &stubRegistryClient{
		tags: []string{"latest"},
		manifest: distribution.ManifestResponse{
			Digest: testManifestDigest, MediaType: ociregistry.MediaTypeOCIImageManifest,
			Body: testManifestBody(t), SizeBytes: 512,
		},
	}
	source := transportTestSource(t, map[string]RegistryClient{
		"team/first":  &pingFailingClient{pingErr: connectionReset()},
		"team/second": healthy,
	}, nil)

	// A skipped target must not be reported as a drained batch: the next
	// target scans in the same call instead of after one more poll interval.
	_, ok, err := source.Next(context.Background())
	if err != nil || !ok {
		t.Fatalf("Next() = ok %v err %v, want the healthy second target scanned in the same call", ok, err)
	}
}

func TestSourceNextEscalatesPersistentTransportFailureToFatal(t *testing.T) {
	t.Parallel()

	source := transportTestSource(t, map[string]RegistryClient{
		"team/first": &pingFailingClient{pingErr: connectionRefused()},
	}, nil)
	source.Config.Targets = source.Config.Targets[:1]

	limit := sdk.MaxConsecutiveTransportFailures
	for attempt := 1; attempt < limit; attempt++ {
		if _, ok, err := source.Next(context.Background()); err != nil || ok {
			t.Fatalf("attempt %d: Next() = ok %v err %v, want a skipped idle poll below the ceiling", attempt, ok, err)
		}
	}
	_, ok, err := source.Next(context.Background())
	if err == nil || ok {
		t.Fatalf("attempt %d: Next() = ok %v err %v, want a fatal error at the consecutive-failure ceiling", limit, ok, err)
	}
	if !strings.Contains(err.Error(), "consecutive") {
		t.Fatalf("Next() error = %q, want it to name the consecutive-failure ceiling", err)
	}
}

func TestSourceNextResetsTransportFailureCountAfterSuccess(t *testing.T) {
	t.Parallel()

	failing := &pingFailingClient{pingErr: connectionRefused()}
	healthy := &stubRegistryClient{
		tags: []string{"latest"},
		manifest: distribution.ManifestResponse{
			Digest: testManifestDigest, MediaType: ociregistry.MediaTypeOCIImageManifest,
			Body: testManifestBody(t), SizeBytes: 512,
		},
	}
	var client RegistryClient = failing
	source := transportTestSource(t, nil, nil)
	source.Config.Targets = source.Config.Targets[:1]
	source.ClientFactory = ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
		return client, nil
	})

	limit := sdk.MaxConsecutiveTransportFailures
	for attempt := 1; attempt < limit; attempt++ {
		if _, _, err := source.Next(context.Background()); err != nil {
			t.Fatalf("attempt %d: Next() error = %v", attempt, err)
		}
	}
	client = healthy
	if _, ok, err := source.Next(context.Background()); err != nil || !ok {
		t.Fatalf("recovery Next() = ok %v err %v, want the target scanned", ok, err)
	}
	client = failing
	// A fresh outage after recovery starts a new consecutive run.
	for attempt := 1; attempt < limit; attempt++ {
		if _, _, err := source.Next(context.Background()); err != nil {
			t.Fatalf("post-recovery attempt %d: Next() error = %v, the count must have reset", attempt, err)
		}
	}
}

func TestSourceNextTransientLogCarriesAttemptAndCauseClass(t *testing.T) {
	t.Parallel()

	var logs bytes.Buffer
	source := transportTestSource(t, map[string]RegistryClient{
		"team/first": &pingFailingClient{pingErr: connectionRefused()},
	}, nil)
	source.Config.Targets = source.Config.Targets[:1]
	source.Logger = slog.New(slog.NewJSONHandler(&logs, nil))

	for range 2 {
		if _, _, err := source.Next(context.Background()); err != nil {
			t.Fatalf("Next() error = %v", err)
		}
	}
	out := logs.String()
	for _, want := range []string{`"consecutive_transport_failures":2`, `"cause_class":"refused"`} {
		if !strings.Contains(out, want) {
			t.Fatalf("transient log missing %s:\n%s", want, out)
		}
	}
	if strings.Contains(out, "registry.internal.example") {
		t.Fatalf("transient log leaks the registry host:\n%s", out)
	}
}
