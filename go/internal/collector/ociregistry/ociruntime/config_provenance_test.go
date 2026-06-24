// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociruntime

import (
	"context"
	"encoding/json"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSourceNextPreservesImageConfigProvenanceLabels(t *testing.T) {
	t.Parallel()

	client := &configBlobRegistryClient{
		tags: []string{"prod"},
		manifest: distribution.ManifestResponse{
			Digest:    testManifestDigest,
			MediaType: ociregistry.MediaTypeOCIImageManifest,
			Body:      testManifestBody(t),
			SizeBytes: 512,
		},
		blobs: map[string]distribution.BlobResponse{
			testConfigDigest: {
				Body: testImageConfigBody(t, map[string]string{
					"org.opencontainers.image.source":   "https://github.com/acme/payments-api",
					"org.opencontainers.image.revision": "0123456789abcdef0123456789abcdef01234567",
					"com.example.private.token":         "secret",
				}),
				MediaType: "application/vnd.oci.image.config.v1+json",
				Digest:    testConfigDigest,
				SizeBytes: 256,
			},
		},
	}
	source := Source{
		Config: Config{
			CollectorInstanceID: "oci-runtime-test",
			Targets: []TargetConfig{{
				Provider:   ociregistry.ProviderGHCR,
				Registry:   "ghcr.io",
				Repository: "acme/payments-api",
				References: []string{"prod"},
			}},
		},
		ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
			return client, nil
		}),
		Clock: func() time.Time { return time.Date(2026, 6, 7, 9, 0, 0, 0, time.UTC) },
	}

	collected, ok, err := source.Next(context.Background())
	if err != nil {
		t.Fatalf("Next() error = %v", err)
	}
	if !ok {
		t.Fatal("Next() ok = false, want true")
	}
	envelopes := drainFacts(t, collected)
	manifest := requireFactKind(t, envelopes, facts.OCIImageManifestFactKind)
	labels, ok := manifest.Payload["config_labels"].(map[string]string)
	if !ok {
		t.Fatalf("config_labels = %#v, want map[string]string", manifest.Payload["config_labels"])
	}
	if got := labels["org.opencontainers.image.source"]; got != "https://github.com/acme/payments-api" {
		t.Fatalf("source label = %q", got)
	}
	if got := labels["org.opencontainers.image.revision"]; got != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("revision label = %q", got)
	}
	if got := labels["com.example.private.token"]; got != ociregistry.RedactedValue {
		t.Fatalf("private label = %q, want redacted", got)
	}
	if got, want := client.blobCalls, 1; got != want {
		t.Fatalf("blobCalls = %d, want %d", got, want)
	}
}

func TestSourceNextWarnsForUnavailableAndOversizedImageConfig(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name          string
		configSize    int64
		blobBody      []byte
		blobErr       error
		wantWarning   string
		wantBlobCalls int
	}{
		{
			name:          "unavailable",
			configSize:    256,
			blobErr:       collector.RegistryHTTPFailure("oci", "", "get_blob", 404, nil),
			wantWarning:   "config_blob_unavailable",
			wantBlobCalls: 1,
		},
		{
			name:          "oversized descriptor",
			configSize:    1048577,
			wantWarning:   "config_blob_oversized",
			wantBlobCalls: 0,
		},
		{
			name:          "oversized response",
			configSize:    256,
			blobBody:      make([]byte, 1048577),
			wantWarning:   "config_blob_oversized",
			wantBlobCalls: 1,
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			client := &configBlobRegistryClient{
				tags: []string{"prod"},
				manifest: distribution.ManifestResponse{
					Digest:    testManifestDigest,
					MediaType: ociregistry.MediaTypeOCIImageManifest,
					Body:      testManifestBodyWithConfigSize(t, tc.configSize),
					SizeBytes: 512,
				},
				blobs: map[string]distribution.BlobResponse{
					testConfigDigest: {
						Body:      tc.blobBody,
						MediaType: "application/vnd.oci.image.config.v1+json",
						Digest:    testConfigDigest,
						SizeBytes: int64(len(tc.blobBody)),
					},
				},
				blobErr: tc.blobErr,
			}
			source := Source{
				Config: Config{
					CollectorInstanceID: "oci-runtime-test",
					Targets: []TargetConfig{{
						Provider:   ociregistry.ProviderGHCR,
						Registry:   "ghcr.io",
						Repository: "acme/payments-api",
						References: []string{"prod"},
					}},
				},
				ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
					return client, nil
				}),
				Clock: func() time.Time { return time.Date(2026, 6, 7, 9, 0, 0, 0, time.UTC) },
			}

			collected, ok, err := source.Next(context.Background())
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}
			if !ok {
				t.Fatal("Next() ok = false, want true")
			}
			envelopes := drainFacts(t, collected)
			if got, want := client.blobCalls, tc.wantBlobCalls; got != want {
				t.Fatalf("blobCalls = %d, want %d", got, want)
			}
			warnings := warningCodes(envelopes)
			if !slices.Contains(warnings, tc.wantWarning) {
				t.Fatalf("warning codes = %#v, want %q", warnings, tc.wantWarning)
			}
			manifest := requireFactKind(t, envelopes, facts.OCIImageManifestFactKind)
			if labels := manifest.Payload["config_labels"]; labels != nil {
				t.Fatalf("config_labels = %#v, want nil for %s", labels, tc.name)
			}
		})
	}
}

type configBlobRegistryClient struct {
	tags      []string
	manifest  distribution.ManifestResponse
	referrers distribution.ReferrersResponse
	blobs     map[string]distribution.BlobResponse
	blobErr   error
	blobCalls int
}

func (c *configBlobRegistryClient) Ping(context.Context) error { return nil }

func (c *configBlobRegistryClient) ListTags(context.Context, string) ([]string, error) {
	return append([]string(nil), c.tags...), nil
}

func (c *configBlobRegistryClient) GetManifest(context.Context, string, string) (distribution.ManifestResponse, error) {
	return c.manifest, nil
}

func (c *configBlobRegistryClient) GetBlob(_ context.Context, _ string, digest string) (distribution.BlobResponse, error) {
	c.blobCalls++
	if c.blobErr != nil {
		return distribution.BlobResponse{}, c.blobErr
	}
	return c.blobs[digest], nil
}

func (c *configBlobRegistryClient) ListReferrers(context.Context, string, string) (distribution.ReferrersResponse, error) {
	return c.referrers, nil
}

func testImageConfigBody(t *testing.T, labels map[string]string) []byte {
	t.Helper()
	body := map[string]any{
		"architecture": "amd64",
		"os":           "linux",
		"config": map[string]any{
			"Labels": labels,
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal config blob: %v", err)
	}
	return encoded
}

func testManifestBodyWithConfigSize(t *testing.T, configSize int64) []byte {
	t.Helper()
	body := map[string]any{
		"schemaVersion": 2,
		"mediaType":     ociregistry.MediaTypeOCIImageManifest,
		"config": map[string]any{
			"mediaType": "application/vnd.oci.image.config.v1+json",
			"digest":    testConfigDigest,
			"size":      configSize,
		},
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	return encoded
}

func requireFactKind(t *testing.T, envelopes []facts.Envelope, kind string) facts.Envelope {
	t.Helper()
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			return envelope
		}
	}
	t.Fatalf("fact kind %q missing from %#v", kind, factKinds(envelopes))
	return facts.Envelope{}
}

func warningCodes(envelopes []facts.Envelope) []string {
	var out []string
	for _, envelope := range envelopes {
		if envelope.FactKind != facts.OCIRegistryWarningFactKind {
			continue
		}
		if code, ok := envelope.Payload["warning_code"].(string); ok && code != "" {
			out = append(out, code)
		}
	}
	return out
}
