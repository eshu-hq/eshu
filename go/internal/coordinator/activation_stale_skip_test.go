// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package coordinator

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/component"
)

// TestDesiredInstancesSkipsStaleMissingActivationFile is the #9634 RED: a
// registry activation whose config file no longer exists (values tree removed
// it while the registry entry persists) must be skipped, not fail the whole
// coordinator at startup.
func TestDesiredInstancesSkipsStaleMissingActivationFile(t *testing.T) {
	t.Parallel()

	good := writeActivationFile(t, `host:
  sourceSystem: openssf-scorecard
  scope:
    id: github.com/example/widgets
    kind: repository
`)
	stale := filepath.Join(t.TempDir(), "removed-activation.yaml")

	entry := testActivationReadbackEntry(good)
	entry.Activations = append(entry.Activations, component.Activation{
		InstanceID:    "stale-removed-activation",
		Mode:          "scheduled",
		ClaimsEnabled: true,
		ConfigPath:    stale,
	})

	instances, err := desiredInstancesForComponent(entry, testActivationManifest("scorecard"))
	if err != nil {
		t.Fatalf("desiredInstancesForComponent() error = %v, want nil (stale activation skipped)", err)
	}
	if got, want := len(instances), 1; got != want {
		t.Fatalf("instances = %d, want %d (only the live activation planned)", got, want)
	}
	if !strings.Contains(instances[0].Configuration, "github.com/example/widgets") {
		t.Fatalf("configuration = %s, want the live activation's scope", instances[0].Configuration)
	}
}

// TestDesiredInstancesStillFailsClosedOnUnreadableActivationFile guards the
// #9634 skip against overreach: a config path that exists but cannot be read
// as a file (or decodes badly) is a real error, not a stale reference, and
// must still fail startup closed.
func TestDesiredInstancesStillFailsClosedOnUnreadableActivationFile(t *testing.T) {
	t.Parallel()

	dir := t.TempDir() // a directory is not a readable config file.

	entry := testActivationReadbackEntry(dir)

	if _, err := desiredInstancesForComponent(entry, testActivationManifest("scorecard")); err == nil {
		t.Fatal("desiredInstancesForComponent() error = nil, want closed failure for an unreadable activation file")
	}
}

// TestDesiredInstancesStillFailsClosedOnMalformedActivationFile guards the
// #9634 skip a second way: present-but-invalid YAML must fail closed.
func TestDesiredInstancesStillFailsClosedOnMalformedActivationFile(t *testing.T) {
	t.Parallel()

	bad := writeActivationFile(t, "host:\n\tsourceSystem: [unclosed\n")

	entry := testActivationReadbackEntry(bad)

	if _, err := desiredInstancesForComponent(entry, testActivationManifest("scorecard")); err == nil {
		t.Fatal("desiredInstancesForComponent() error = nil, want closed failure for a malformed activation file")
	}
}
