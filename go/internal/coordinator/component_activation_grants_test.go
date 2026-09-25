// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// grantedCoreKindHome installs and enables a scorecard-shaped component whose
// manifest declares the core-owned fact kind aws_resource. The install-time
// grant is recorded first, exactly as an operator would, and the returned
// registry lets a test then change the grant set before planning.
func grantedCoreKindHome(t *testing.T) (string, component.Registry) {
	t.Helper()

	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "manifest.yaml")
	raw := strings.Replace(
		scorecardManifestYAML(),
		"kind: dev.eshu.examples.scorecard.snapshot",
		"kind: aws_resource",
		1,
	)
	if err := os.WriteFile(manifestPath, []byte(raw), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	home := filepath.Join(dir, "components")
	registry := component.NewRegistry(home)
	if err := registry.RecordGrant(scorecardAWSResourceGrant("scorecard", "aws_resource", time.Now().Add(time.Hour))); err != nil {
		t.Fatalf("RecordGrant() error = %v, want nil", err)
	}
	grants, err := registry.ProducerGrants()
	if err != nil {
		t.Fatalf("ProducerGrants() error = %v, want nil", err)
	}
	manifest, err := component.LoadManifestWithGrants(manifestPath, grants)
	if err != nil {
		t.Fatalf("LoadManifestWithGrants() error = %v, want nil", err)
	}
	verification := scorecardAllowlistPolicy().VerifyWithGrants(context.Background(), manifest, grants)
	if !verification.Allowed {
		t.Fatalf("VerifyWithGrants() denied granted manifest: %#v", verification)
	}
	if _, err := registry.Install(manifestPath, verification); err != nil {
		t.Fatalf("Install() error = %v, want nil", err)
	}
	enableScorecardComponent(t, home, component.Activation{
		InstanceID:    "scorecard-primary",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    writeScorecardActivationConfig(t),
	})
	return home, registry
}

func scorecardAWSResourceGrant(scopeName, kind string, expiresAt time.Time) component.ProducerGrant {
	return component.ProducerGrant{
		ProducerID:     "dev.eshu.examples.scorecard",
		Version:        "0.1.0",
		Kind:           kind,
		SchemaVersions: []string{"1.0.0"},
		Scope:          scopeName,
		ExpiresAt:      expiresAt.UTC(),
	}
}

// TestLoadConfigAdmitsGrantedCoreKindComponentActivation is the #7153 red
// test: a component whose manifest declares a core-owned kind under a live
// producer grant is admitted by install, enable, and readback, so the
// coordinator must plan its activation instead of aborting startup.
func TestLoadConfigAdmitsGrantedCoreKindComponentActivation(t *testing.T) {
	t.Parallel()

	home, _ := grantedCoreKindHome(t)

	cfg, err := LoadConfig(componentCoordinatorEnv(home, nil))
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}
	if got, want := len(cfg.CollectorInstances), 1; got != want {
		t.Fatalf("collector instances = %d, want %d", got, want)
	}
	if got, want := cfg.CollectorInstances[0].CollectorKind, scope.CollectorKind("scorecard"); got != want {
		t.Fatalf("collector kind = %q, want %q", got, want)
	}
}

// TestLoadConfigDoesNotAdmitCoreKindActivationWithoutLiveGrant proves the
// #7153 fix does not widen admission: once the covering grant is revoked,
// expired, or replaced by a grant for another kind or scope, the coordinator
// must skip the activation (Readback marks it failed) without failing startup.
func TestLoadConfigDoesNotAdmitCoreKindActivationWithoutLiveGrant(t *testing.T) {
	t.Parallel()

	future := time.Now().Add(time.Hour)
	for _, tt := range []struct {
		name   string
		mutate func(t *testing.T, registry component.Registry)
	}{
		{
			name: "revoked",
			mutate: func(t *testing.T, registry component.Registry) {
				if err := registry.RevokeGrant("dev.eshu.examples.scorecard", "0.1.0", "aws_resource", "scorecard"); err != nil {
					t.Fatalf("RevokeGrant() error = %v, want nil", err)
				}
			},
		},
		{
			name: "expired",
			mutate: func(t *testing.T, registry component.Registry) {
				expired := scorecardAWSResourceGrant("scorecard", "aws_resource", time.Now().Add(-time.Hour))
				if err := registry.RecordGrant(expired); err != nil {
					t.Fatalf("RecordGrant(expired) error = %v, want nil", err)
				}
			},
		},
		{
			name: "grant for a different scope",
			mutate: func(t *testing.T, registry component.Registry) {
				if err := registry.RevokeGrant("dev.eshu.examples.scorecard", "0.1.0", "aws_resource", "scorecard"); err != nil {
					t.Fatalf("RevokeGrant() error = %v, want nil", err)
				}
				if err := registry.RecordGrant(scorecardAWSResourceGrant("aws", "aws_resource", future)); err != nil {
					t.Fatalf("RecordGrant(other scope) error = %v, want nil", err)
				}
			},
		},
		{
			name: "grant for a different kind",
			mutate: func(t *testing.T, registry component.Registry) {
				if err := registry.RevokeGrant("dev.eshu.examples.scorecard", "0.1.0", "aws_resource", "scorecard"); err != nil {
					t.Fatalf("RevokeGrant() error = %v, want nil", err)
				}
				if err := registry.RecordGrant(scorecardAWSResourceGrant("scorecard", "vpc", future)); err != nil {
					t.Fatalf("RecordGrant(other kind) error = %v, want nil", err)
				}
			},
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			home, registry := grantedCoreKindHome(t)
			tt.mutate(t, registry)

			cfg, err := LoadConfig(componentCoordinatorEnv(home, map[string]string{
				"ESHU_COLLECTOR_INSTANCES_JSON": staticGitCollectorJSON(),
			}))
			if err != nil {
				t.Fatalf("LoadConfig() error = %v, want nil (skip, not abort)", err)
			}
			if got, want := len(cfg.CollectorInstances), 1; got != want {
				t.Fatalf("collector instances = %d, want only static instance %d", got, want)
			}
			if got, want := cfg.CollectorInstances[0].InstanceID, "collector-git-primary"; got != want {
				t.Fatalf("instance id = %q, want %q", got, want)
			}
		})
	}
}
