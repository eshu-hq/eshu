// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ociruntime

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry"
	"github.com/eshu-hq/eshu/go/internal/collector/ociregistry/distribution"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestSourceNextDeclaresTagListTruncationInBand(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		tags              []string
		tagLimit          int
		tagListIncomplete bool
		wantWarning       bool
		wantTags          []string
	}{
		{
			name:        "over limit",
			tags:        []string{"v2", "v1"},
			tagLimit:    1,
			wantWarning: true,
			wantTags:    []string{"v1"},
		},
		{
			name:              "short page below limit with continuation",
			tags:              []string{"v1"},
			tagLimit:          3,
			tagListIncomplete: true,
			wantWarning:       true,
			wantTags:          []string{"v1"},
		},
		{
			name:              "empty incomplete page",
			tagLimit:          3,
			tagListIncomplete: true,
			wantWarning:       true,
		},
		{
			name:        "exactly at limit",
			tags:        []string{"v1"},
			tagLimit:    1,
			wantWarning: false,
			wantTags:    []string{"v1"},
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			client := &stubRegistryClient{
				tags:              tt.tags,
				tagListIncomplete: tt.tagListIncomplete,
				manifest: distribution.ManifestResponse{
					Digest:    testManifestDigest,
					MediaType: ociregistry.MediaTypeOCIImageManifest,
					Body:      testManifestBody(t),
					SizeBytes: 512,
				},
			}
			source := Source{
				Config: Config{
					CollectorInstanceID: "oci-runtime-truncation-test",
					Targets: []TargetConfig{{
						Provider:     ociregistry.ProviderGHCR,
						Registry:     "registry.example.com",
						Repository:   "team/api",
						TagLimit:     tt.tagLimit,
						FencingToken: 11,
					}},
				},
				ClientFactory: ClientFactoryFunc(func(context.Context, TargetConfig) (RegistryClient, error) {
					return client, nil
				}),
				Clock: func() time.Time {
					return time.Date(2026, 7, 29, 12, 0, 0, 0, time.UTC)
				},
			}

			collected, ok, err := source.Next(context.Background())
			if err != nil {
				t.Fatalf("Next() error = %v", err)
			}
			if !ok {
				t.Fatal("Next() ok = false, want true")
			}

			var warnings []facts.Envelope
			var observedTags []string
			for _, envelope := range drainFacts(t, collected) {
				switch envelope.FactKind {
				case facts.OCIRegistryWarningFactKind:
					if envelope.Payload["warning_code"] == ociregistry.WarningTagListTruncated {
						warnings = append(warnings, envelope)
					}
				case facts.OCIImageTagObservationFactKind:
					if tag, ok := envelope.Payload["tag"].(string); ok {
						observedTags = append(observedTags, tag)
					}
				}
			}
			if got := len(warnings); got != boolCount(tt.wantWarning) {
				t.Fatalf("tag-list truncation warnings = %d, want %d", got, boolCount(tt.wantWarning))
			}
			if !slices.Equal(observedTags, tt.wantTags) {
				t.Fatalf("observed tags = %v, want %v", observedTags, tt.wantTags)
			}
			if !tt.wantWarning {
				return
			}
			warning := warnings[0]
			if got, want := warning.Payload["repository_id"], "oci-registry://registry.example.com/team/api"; got != want {
				t.Fatalf("warning repository_id = %v, want %q", got, want)
			}
			if got := warning.Payload["digest"]; got != "" {
				t.Fatalf("warning digest = %v, want blank repository-level warning", got)
			}
		})
	}
}

func boolCount(value bool) int {
	if value {
		return 1
	}
	return 0
}
