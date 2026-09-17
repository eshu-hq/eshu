// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package extensionhost

import (
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/component"
	"github.com/eshu-hq/eshu/go/internal/scope"
)

// TestNewSourceAcceptsGrantedCoreFactKind is the #6709 slice-2 TDD red test:
// a granted core-owned declaration must survive Source construction so an
// authorized first-party producer can activate. Without grant-aware
// construction the install-time grant from slice 1 admits components that
// can never run.
func TestNewSourceAcceptsGrantedCoreFactKind(t *testing.T) {
	t.Parallel()

	manifest := testManifest()
	manifest.Spec.EmittedFacts[0].Kind = "aws_resource"
	grants := []component.ProducerGrant{{
		ProducerID:     manifest.Metadata.ID,
		Version:        manifest.Metadata.Version,
		Kind:           "aws_resource",
		SchemaVersions: []string{"1.0.0"},
		Scope:          "repo",
		ExpiresAt:      time.Now().Add(time.Hour).UTC(),
	}}
	source, err := NewSource(Config{
		Manifest:            manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              &recordingRunner{},
		Clock:               testObservedAt,
		Grants:              grants,
	})
	if err != nil {
		t.Fatalf("NewSource(granted core kind) error = %v, want nil", err)
	}
	if source == nil {
		t.Fatal("NewSource(granted core kind) = nil, want source")
	}
}

// TestNewSourceRejectsUngrantedCoreFactKind locks the fail-closed default:
// a core-owned declaration without a live grant still fails construction
// with the actionable core-owned error.
func TestNewSourceRejectsUngrantedCoreFactKind(t *testing.T) {
	t.Parallel()

	manifest := testManifest()
	manifest.Spec.EmittedFacts[0].Kind = "aws_resource"
	_, err := NewSource(Config{
		Manifest:            manifest,
		CollectorInstanceID: "scorecard-instance",
		ScopeKind:           scope.KindRepository,
		ConfigHandle:        "cfg-scorecard",
		Config:              map[string]any{"fixture": "scorecard"},
		Runner:              &recordingRunner{},
		Clock:               testObservedAt,
	})
	if err == nil {
		t.Fatal("NewSource(ungranted core kind) error = nil, want core-owned rejection")
	}
	if !strings.Contains(err.Error(), "aws_resource") || !strings.Contains(err.Error(), "core-owned") {
		t.Fatalf("NewSource() error = %v, want actionable core-owned fact-kind error", err)
	}
}
